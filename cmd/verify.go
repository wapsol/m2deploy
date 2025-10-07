package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/k8s"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify deployment health",
	Long: `Verify the health and status of the Magnetiq2 deployment.
Checks pods, services, ingress, and overall health.`,
	Example: `  m2deploy verify
  m2deploy verify --namespace magnetiq-v2`,
	RunE: runVerify,
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckVerifyPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

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
		return formatPrereqError("verify")
	}

	useSudo := getUseSudoWithAutoDetect(logger)
	k8sClient := k8s.NewClient(
		logger,
		false, // Never dry-run verify
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
		useSudo,
	)

	logger.Info("Verifying Magnetiq2 deployment in namespace: %s", viper.GetString("namespace"))

	// Check pods
	logger.Info("\n=== Pods ===")
	pods, err := k8sClient.GetPods()
	if err != nil {
		logger.Error("Failed to get pods: %v", err)
	} else {
		logger.Info(pods)
	}

	// Check services
	logger.Info("\n=== Services ===")
	services, err := k8sClient.GetServices()
	if err != nil {
		logger.Error("Failed to get services: %v", err)
	} else {
		logger.Info(services)
	}

	// Check ingress
	logger.Info("\n=== Ingress ===")
	ingress, err := k8sClient.GetIngress()
	if err != nil {
		logger.Error("Failed to get ingress: %v", err)
	} else {
		logger.Info(ingress)
	}

	// Check pod health
	logger.Info("\n=== Health Check ===")
	if err := k8sClient.CheckPodHealth(); err != nil {
		logger.Error("Health check failed: %v", err)
		logger.Info("Some pods are not in Running state")
		return fmt.Errorf("deployment is not healthy")
	}

	logger.Success("All pods are running and healthy")
	return nil
}
