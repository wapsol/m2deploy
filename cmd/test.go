package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	testComponent string
	testTag       string
	testSkipStop  bool
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test Docker containers locally",
	Long: `Run Docker containers locally for testing before deploying to Kubernetes.
Containers are run on their default ports and can be stopped with --skip-stop=false.`,
	Example: `  m2deploy test --component backend --tag latest
  m2deploy test --component both
  m2deploy test --component frontend --skip-stop`,
	RunE: runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringVarP(&testComponent, "component", "c", ComponentBoth, "Component to test: backend, frontend, or both")
	testCmd.Flags().StringVarP(&testTag, "tag", "t", "", "Image tag (default: commit SHA)")
	testCmd.Flags().BoolVar(&testSkipStop, "skip-stop", false, "Don't stop containers after test")
	testCmd.MarkFlagRequired("component")
}

func runTest(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckTestPrereqs(viper.GetBool("use-sudo"))

	// If --check flag is set, print results and exit
	if viper.GetBool("check") {
		checker.PrintResults()
		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Otherwise, fail fast if prerequisites not met
	if checker.HasFailures() {
		checker.PrintResults()
		return formatPrereqError("test")
	}

	dockerClient := newDockerClient(logger)

	// Validate component
	if err := validateComponent(testComponent); err != nil {
		return err
	}

	// Test components
	components := map[string]int{}
	switch testComponent {
	case ComponentBackend:
		components[ComponentBackend] = BackendPort
	case ComponentFrontend:
		components[ComponentFrontend] = FrontendPort
	case ComponentBoth:
		components[ComponentBackend] = BackendPort
		components[ComponentFrontend] = FrontendPort
	}

	for component, port := range components {
		envVars := map[string]string{}
		if component == ComponentBackend {
			envVars["DATABASE_URL"] = "sqlite:///app/data/magnetiq.db"
		}

		if err := dockerClient.Run(component, port, envVars); err != nil {
			return err
		}

		logger.Info("Container running on port %d", port)
	}

	// Wait a bit and show logs
	logger.Info("Waiting for containers to start...")
	time.Sleep(ContainerStartupDelay)

	for component := range components {
		containerName := getTestContainerName(component)
		logs, err := dockerClient.GetLogs(containerName, 20)
		if err != nil {
			logger.Warning("Failed to get logs for %s: %v", component, err)
		} else {
			logger.Info("Recent logs from %s:\n%s", component, logs)
		}
	}

	if !testSkipStop {
		logger.Info("Stopping test containers...")
		for component := range components {
			containerName := getTestContainerName(component)
			if err := dockerClient.Stop(containerName); err != nil {
				logger.Warning("Failed to stop %s: %v", containerName, err)
			}
			dockerClient.Remove(containerName)
		}
	} else {
		logger.Info("Containers left running (use 'docker stop %s*' to stop)", TestContainerPrefix)
	}

	logger.Success("Container tests completed")
	return nil
}
