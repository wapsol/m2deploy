package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/k8s"
)

var (
	undeployKeepNamespace bool
	undeployKeepPVCs      bool
	undeployForce         bool
)

var undeployCmd = &cobra.Command{
	Use:   "undeploy",
	Short: "Remove deployment from Kubernetes",
	Long: `Remove the Magnetiq2 deployment from the Kubernetes cluster.
Options allow preserving the namespace and/or persistent volume claims (data).`,
	Example: `  m2deploy undeploy
  m2deploy undeploy --keep-namespace
  m2deploy undeploy --keep-pvcs --keep-namespace
  m2deploy undeploy --force`,
	RunE: runUndeploy,
}

func init() {
	rootCmd.AddCommand(undeployCmd)

	undeployCmd.Flags().BoolVar(&undeployKeepNamespace, "keep-namespace", false, "Don't delete the namespace")
	undeployCmd.Flags().BoolVar(&undeployKeepPVCs, "keep-pvcs", false, "Preserve persistent volume claims (database data)")
	undeployCmd.Flags().BoolVar(&undeployForce, "force", false, "Skip confirmation prompt")
}

func runUndeploy(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()
	k8sClient := k8s.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)

	// Confirmation prompt (unless --force or --dry-run)
	if !undeployForce && !viper.GetBool("dry-run") {
		logger.Warning("This will remove the Magnetiq2 deployment from Kubernetes")
		if !undeployKeepPVCs {
			logger.Warning("Database data will be DELETED (use --keep-pvcs to preserve)")
		}
		logger.Info("Press Ctrl+C to cancel, or Enter to continue...")
		var input string
		fmt.Scanln(&input)
	}

	workDir := viper.GetString("work-dir")

	if err := k8sClient.Undeploy(workDir, undeployKeepNamespace, undeployKeepPVCs); err != nil {
		return err
	}

	logger.Success("Undeployment completed")
	if undeployKeepPVCs {
		logger.Info("Database data preserved in PVCs")
	}

	return nil
}
