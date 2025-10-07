package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	rollbackComponent string
	rollbackRestoreDB bool
	rollbackBackupFile string
	rollbackWait      bool
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback to previous version",
	Long: `Rollback the deployment to the previous version.
Optionally restores the database from a backup.`,
	Example: `  m2deploy rollback --component backend
  m2deploy rollback --component both --restore-db --backup-file ./backups/magnetiq-db-20240101-120000.db.gz
  m2deploy rollback --component frontend`,
	RunE: runRollback,
}

func init() {
	rootCmd.AddCommand(rollbackCmd)

	rollbackCmd.Flags().StringVarP(&rollbackComponent, "component", "c", ComponentBoth, "Component to rollback: backend, frontend, or both")
	rollbackCmd.Flags().BoolVar(&rollbackRestoreDB, "restore-db", false, "Restore database from backup")
	rollbackCmd.Flags().StringVar(&rollbackBackupFile, "backup-file", "", "Database backup file to restore (required if --restore-db)")
	rollbackCmd.Flags().BoolVar(&rollbackWait, "wait", true, "Wait for rollback to complete")
}

func runRollback(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckRollbackPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

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
		return formatPrereqError("rollback")
	}

	k8sClient := newK8sClient(logger)

	// Validate restore-db flags
	if rollbackRestoreDB && rollbackBackupFile == "" {
		return fmt.Errorf("--backup-file is required when --restore-db is set")
	}

	components := getComponents(rollbackComponent)

	// Restore database first (if requested)
	if rollbackRestoreDB {
		logger.Info("Restoring database from backup")
		dbClient := newDBClient(logger)
		if err := dbClient.Restore(rollbackBackupFile); err != nil {
			return fmt.Errorf("database restore failed: %w", err)
		}
	}

	// Rollback deployments
	for _, component := range components {
		deploymentName := getDeploymentName(component)
		logger.Info("Rolling back %s", deploymentName)

		if err := k8sClient.Rollback(deploymentName); err != nil {
			return err
		}

		// Wait for rollback if requested
		if rollbackWait {
			if err := k8sClient.WaitForRollout(deploymentName, 3*time.Minute); err != nil {
				return fmt.Errorf("rollback failed for %s: %w", deploymentName, err)
			}
		}
	}

	logger.Success("Rollback completed successfully")
	logger.Info("Run 'm2deploy verify' to check deployment health")

	return nil
}
