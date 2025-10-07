package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/docker"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

// No flags needed - cleanup is always interactive

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Interactive cleanup of all development resources",
	Long: `Interactively clean up development and deployment artifacts with prompts for:
- Local Docker images
- Test containers
- K0s containerd images
- Kubernetes deployments
- Persistent volume claims (PVCs) with data
- Source code directory

Each operation requires confirmation for safety. Destructive operations
(PVCs and source code) require double confirmation.

The cleanup workflow:
  1. Remove local Docker images? [y/N]
  2. Remove test containers? [y/N]
  3. Remove k0s containerd images? [y/N]
  4. Remove Kubernetes deployment? [y/N]
     - Also remove persistent data (PVCs)? [DESTRUCTIVE] [y/N]
       - Are you ABSOLUTELY SURE? [y/N]
  5. Remove source code directory? [y/N]
     - Confirm deletion? [y/N]

Answer 'y' or 'yes' to confirm each operation, or 'n' to skip.`,
	Example: `  # Interactive cleanup (prompts for each operation)
  m2deploy cleanup

  # Cleanup with verbose logging
  m2deploy cleanup --verbose

  # Cleanup with custom log file
  m2deploy cleanup --log-file /tmp/cleanup.log

  # Dry-run to see what would be cleaned
  m2deploy cleanup --dry-run`,
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	// No flags needed - cleanup is always interactive
}

func runCleanup(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckCleanupPrereqs(viper.GetBool("use-sudo"))

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
		return formatPrereqError("cleanup")
	}

	dockerClient := newDockerClient(logger)

	// Always use both components in interactive mode
	components := []string{"backend", "frontend"}

	// Always run interactive cleanup
	return runInteractiveCleanup(logger, dockerClient, components)
}

// runInteractiveCleanup performs interactive cleanup with user prompts
func runInteractiveCleanup(logger *config.Logger, dockerClient interface{}, components []string) error {
	reader := bufio.NewReader(os.Stdin)

	logger.Info("Interactive cleanup mode")
	logger.Info("Components: %v", components)
	logger.Info("")

	// Prompt for Docker images
	if promptYesNo(reader, "Remove local Docker images?") {
		logger.Info("Removing local Docker images")
		for _, component := range components {
			client := dockerClient.(*docker.Client)
			if err := client.RemoveImage(component); err != nil {
				logger.Warning("Failed to remove local image for %s: %v", component, err)
			}
		}
	}

	// Prompt for test containers
	if promptYesNo(reader, "Remove test containers?") {
		logger.Info("Removing test containers")
		for _, component := range components {
			containerName := getTestContainerName(component)
			client := dockerClient.(*docker.Client)
			client.Remove(containerName)
		}
	}

	// Prompt for k0s images
	if promptYesNo(reader, "Remove k0s containerd images?") {
		logger.Info("Removing k0s containerd images")
		for _, component := range components {
			client := dockerClient.(*docker.Client)
			if err := client.RemoveK0sImage(component); err != nil {
				logger.Warning("Failed to remove k0s image for %s: %v", component, err)
			}
		}
	}

	logger.Info("")

	// Prompt for Kubernetes deployment removal
	if promptYesNo(reader, "Remove Kubernetes deployment?") {
		logger.Warning("This will undeploy the application from k8s cluster")

		// Create k8s client
		k8sClient := newK8sClient(logger)
		workDir := viper.GetString("work-dir")

		// Ask about PVCs with double confirmation
		keepPVCs := true
		logger.Info("")
		if promptYesNo(reader, "  Also remove persistent data (PVCs)? [DESTRUCTIVE]") {
			logger.Warning("WARNING: This will DELETE all database and media files!")
			if promptYesNo(reader, "    Are you ABSOLUTELY SURE?") {
				keepPVCs = false
				logger.Warning("PVCs will be deleted")
			} else {
				logger.Info("PVCs will be preserved")
			}
		} else {
			logger.Info("PVCs will be preserved")
		}

		logger.Info("")
		logger.Info("Undeploying application...")

		// Undeploy (keepNamespace=true to preserve namespace)
		if err := k8sClient.Undeploy(workDir, true, keepPVCs); err != nil {
			logger.Error("Failed to undeploy: %v", err)
		} else {
			if keepPVCs {
				logger.Info("Database data preserved in PVCs")
			}
		}
	}

	logger.Info("")

	// Prompt for source code directory removal
	if promptYesNo(reader, "Remove source code directory?") {
		workDir := viper.GetString("work-dir")
		logger.Warning("This will delete: %s", workDir)

		if promptYesNo(reader, "  Confirm deletion?") {
			logger.Info("Removing work directory...")
			if err := os.RemoveAll(workDir); err != nil {
				logger.Error("Failed to remove work directory: %v", err)
			} else {
				logger.Success("Removed work directory: %s", workDir)
			}
		} else {
			logger.Info("Source code directory preserved")
		}
	}

	logger.Success("Interactive cleanup completed")
	return nil
}

// promptYesNo prompts the user for a yes/no answer
func promptYesNo(reader *bufio.Reader, question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
