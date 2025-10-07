package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/constants"
	"github.com/wapsol/m2deploy/pkg/payload"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	updateComponent  string
	updateTag        string
	updateBranch     string
	updateCommit     string
	updateAutoMigrate bool
	updateBackupDB   bool
	updateWait       bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update existing deployment",
	Long: `Update the existing Magnetiq2 deployment with a new version.
This performs a rolling update by rebuilding images locally, importing to k0s,
and updating deployments. Includes automatic database backup and migration.`,
	Example: `  m2deploy update --tag v1.2.3
  m2deploy update --branch develop --component backend
  m2deploy update --commit abc123 --auto-migrate=false
  m2deploy update --tag latest --component both --wait`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVarP(&updateComponent, "component", "c", constants.ComponentBoth, "Component to update: backend, frontend, or both")
	updateCmd.Flags().StringVarP(&updateTag, "tag", "t", "", "Image tag (default: commit SHA)")
	updateCmd.Flags().StringVarP(&updateBranch, "branch", "b", "", "Git branch to update from")
	updateCmd.Flags().StringVar(&updateCommit, "commit", "", "Specific commit SHA to update to")
	updateCmd.Flags().BoolVar(&updateAutoMigrate, "auto-migrate", true, "Automatically run database migrations")
	updateCmd.Flags().BoolVar(&updateBackupDB, "backup-db", true, "Backup database before update")
	updateCmd.Flags().BoolVar(&updateWait, "wait", true, "Wait for rollout to complete")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Validate --repo-url is provided (required)
	repoURL := viper.GetString("repo-url")
	if repoURL == "" {
		return fmt.Errorf("--repo-url is required")
	}

	// Derive workspace path from repo URL
	workDir := deriveWorkspaceFromRepoURL(repoURL)

	logger.Info("Using workspace: %s", workDir)

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckUpdatePrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

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
		return formatPrereqError("update")
	}

	cfg := getConfig()
	gitClient := newGitClient(logger)
	dockerClient := newDockerClient(logger)
	k8sClient := newK8sClient(logger)
	dbClient := newDBClient(logger)

	// 1. Update repository
	logger.Info("Step 1/6: Preparing source code")

	// Check if directory exists - update requires existing source code
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		// Directory doesn't exist - fail fast
		return fmt.Errorf("source code not found at %s\nUpdate requires existing source code. Use 'build --fresh' to clone it first:\n  m2deploy build --component both --repo-url %s --fresh --import", workDir, repoURL)
	}

	// Directory exists - pull updates if branch specified
	if updateBranch != "" {
		logger.Info("Pulling latest changes from branch: %s", updateBranch)
		if err := gitClient.Pull(workDir, updateBranch); err != nil {
			return err
		}
	} else {
		logger.Info("Using existing source code (no branch specified)")
	}

	// Checkout specific commit if requested
	if updateCommit != "" {
		logger.Info("Checking out commit: %s", updateCommit)
		if err := gitClient.Checkout(workDir, updateCommit); err != nil {
			return err
		}
	}

	// Resolve image tag with clear precedence
	cfg.LocalImageTag = cfg.ResolveImageTag(logger, updateTag, workDir)

	// Validate payload structure
	validator := payload.NewValidator(logger)
	if err := validator.ValidateStructure(workDir); err != nil {
		return fmt.Errorf("payload validation failed: %w", err)
	}

	// 2. Backup database
	if updateBackupDB && (updateComponent == constants.ComponentBackend || updateComponent == constants.ComponentBoth) {
		logger.Info("Step 2/6: Backing up database")
		if err := dbClient.Backup(constants.DefaultBackupPath, true); err != nil {
			logger.Warning("Database backup failed: %v", err)
			logger.Warning("Continuing with update...")
		}
	} else {
		logger.Info("Step 2/6: Skipping database backup")
	}

	// 3. Build new images
	logger.Info("Step 3/6: Building new images")
	components := getComponents(updateComponent)
	for _, component := range components {
		logger.Info("Building %s...", component)
		if err := dockerClient.Build(workDir, component); err != nil {
			return err
		}
		logger.Success("Built %s image", component)
	}

	// Import images to k0s
	logger.Info("Importing images to k0s...")
	for _, component := range components {
		// Save image to tarball
		tarballPath := fmt.Sprintf(constants.TarballPathTemplate, component)
		if err := dockerClient.SaveImage(component, tarballPath); err != nil {
			return err
		}

		// Import to k0s
		if err := dockerClient.ImportToK0s(tarballPath); err != nil {
			return err
		}

		// Clean up tarball
		if !viper.GetBool("dry-run") {
			os.Remove(tarballPath)
		}
	}

	// 4. Run migrations (before updating backend)
	if updateAutoMigrate && (updateComponent == constants.ComponentBackend || updateComponent == constants.ComponentBoth) {
		logger.Info("Step 4/6: Running database migrations")
		if err := dbClient.Migrate(); err != nil {
			logger.Warning("Migrations failed: %v", err)
			logger.Info("You may need to run migrations manually with 'm2deploy db migrate'")
		}
	} else {
		logger.Info("Step 4/6: Skipping database migrations")
	}

	// 5. Update deployments
	logger.Info("Step 5/6: Updating Kubernetes deployments")
	for _, component := range components {
		deploymentName := getDeploymentName(component)
		containerName := component
		imageName := cfg.GetLocalImageName(component)

		if err := k8sClient.SetImage(deploymentName, containerName, imageName); err != nil {
			return err
		}
	}

	// 6. Wait for rollout
	if updateWait {
		logger.Info("Step 6/6: Waiting for rollout to complete")
		for _, component := range components {
			deploymentName := getDeploymentName(component)
			if err := k8sClient.WaitForRollout(deploymentName, 5*time.Minute); err != nil {
				logger.Error("Rollout failed for %s", deploymentName)
				logger.Info("Consider rolling back with 'm2deploy rollback --component %s'", component)
				return err
			}
		}
	} else {
		logger.Info("Step 6/6: Skipping rollout wait")
	}

	logger.Success("Update completed successfully")
	logger.Info("New version: %s", cfg.LocalImageTag)
	logger.Info("Run 'm2deploy verify' to check deployment health")

	return nil
}
