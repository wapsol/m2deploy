package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	allBranch     string
	allTag        string
	allFresh      bool
	allSkipVerify bool
)

var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Run complete deployment pipeline",
	Long: `Run the complete deployment pipeline: build, deploy, migrate, and verify.

The pipeline automatically:
1. Uses existing source code at /tmp/<username>/<repo-name>
2. Builds Docker images and imports to k0s
3. Deploys to Kubernetes
4. Runs database migrations
5. Verifies deployment (unless --skip-verify)

IMPORTANT: Source code must exist before running. Use --fresh to clone it
from GitHub, or to re-clone and overwrite existing code.`,
	Example: `  # Run with existing source code
  m2deploy all --repo-url https://github.com/wapsol/magnetiq2

  # Clone fresh and run complete pipeline (first time)
  m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh

  # Deploy specific branch
  m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh --branch develop

  # Deploy with custom tag
  m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --tag v1.0.0

  # Skip final verification
  m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --skip-verify`,
	RunE: runAll,
}

func init() {
	rootCmd.AddCommand(allCmd)

	allCmd.Flags().StringVarP(&allBranch, "branch", "b", "main", "Git branch to use")
	allCmd.Flags().StringVarP(&allTag, "tag", "t", "", "Image tag (default: latest)")
	allCmd.Flags().BoolVar(&allFresh, "fresh", false, "Clone fresh code from GitHub (overwrites existing)")
	allCmd.Flags().BoolVar(&allSkipVerify, "skip-verify", false, "Skip deployment verification")
}

func runAll(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()
	cfg := getConfig()

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
	checker.CheckAllPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

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
		return formatPrereqError("all")
	}

	// Override tag if specified
	if allTag != "" {
		cfg.LocalImageTag = allTag
	}

	gitClient := newGitClient(logger)
	dockerClient := newDockerClient(logger)
	k8sClient := newK8sClient(logger)

	totalSteps := 4
	logger.Info("Starting complete deployment pipeline")

	// Step 1: Clone/Update Source Code
	logger.Info("\n=== Step 1/%d: Prepare Source Code ===", totalSteps)

	if allFresh {
		// Remove existing directory and clone fresh
		logger.Info("Fresh clone requested - removing existing directory")
		if !viper.GetBool("dry-run") {
			os.RemoveAll(workDir)
		}
		// Clone fresh from GitHub
		logger.Info("Cloning fresh from %s (branch: %s)", repoURL, allBranch)
		if err := gitClient.Clone(repoURL, workDir, allBranch, 1); err != nil {
			return err
		}
	} else {
		// Check if directory exists
		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			// Directory doesn't exist and --fresh not provided - fail fast
			return fmt.Errorf("source code not found at %s\nUse --fresh to clone it from GitHub:\n  m2deploy all --repo-url %s --fresh", workDir, repoURL)
		}
		// Directory exists - use existing code
		logger.Info("Using existing source code at %s", workDir)
	}

	// Step 2: Build Images
	logger.Info("\n=== Step 2/%d: Build Images ===", totalSteps)

	components := []string{ComponentBackend, ComponentFrontend}
	for _, component := range components {
		imageName := cfg.GetLocalImageName(component)
		logger.Info("Building %s...", component)
		if err := dockerClient.Build(workDir, component); err != nil {
			return fmt.Errorf("failed to build %s: %w", component, err)
		}
		logger.Success("Built image: %s (in Docker daemon)", imageName)
	}

	// Step 3: Deploy
	logger.Info("\n=== Step 3/%d: Deploy to Kubernetes ===", totalSteps)

	// Import images to k0s
	logger.Info("Importing images from Docker daemon to k0s containerd...")
	for _, component := range components {
		imageName := cfg.GetLocalImageName(component)
		tarballPath := fmt.Sprintf(TarballPathTemplate, component)

		// Save image to tarball
		logger.Info("Exporting %s from Docker daemon...", imageName)
		if err := dockerClient.SaveImage(component, tarballPath); err != nil {
			return fmt.Errorf("failed to save %s image: %w", component, err)
		}

		// Import to k0s
		logger.Info("Importing %s to k0s containerd...", imageName)
		if err := dockerClient.ImportToK0s(tarballPath); err != nil {
			return err
		}

		// Clean up tarball
		if !viper.GetBool("dry-run") {
			os.Remove(tarballPath)
			logger.Debug("Removed temporary tarball: %s", tarballPath)
		}
	}

	// Verify images in k0s
	logger.Info("Verifying images in k0s containerd...")
	for _, component := range components {
		imageName := cfg.GetLocalImageName(component)
		exists, err := dockerClient.VerifyImageInK0s(component)
		if err != nil {
			logger.Warning("Failed to verify %s: %v", imageName, err)
		} else if exists {
			logger.Success("✓ %s available in k0s containerd", imageName)
		} else {
			return fmt.Errorf("%s not found in k0s containerd", imageName)
		}
	}

	// Deploy to Kubernetes
	logger.Info("Deploying to Kubernetes...")
	if err := k8sClient.DeployWithOptions(workDir, true, !allSkipVerify); err != nil {
		return err
	}

	// Run migrations
	logger.Info("Running database migrations")
	dbClient := newDBClient(logger)
	if err := dbClient.Migrate(); err != nil {
		logger.Warning("Migration failed: %v", err)
		logger.Info("You may need to run migrations manually")
	}

	// Step 4: Final verification (if not skipped)
	if !allSkipVerify {
		logger.Info("\n=== Step 4/%d: Final Verification ===", totalSteps)
		logger.Info("Deployment verification completed by DeployWithOptions")
	} else {
		logger.Info("\n=== Step 4/%d: Verification (Skipped) ===", totalSteps)
	}

	logger.Success("\nComplete deployment pipeline finished successfully!")
	logger.Info("")
	logger.Info("Artifacts location:")
	logger.Info("  - Images: k0s containerd (tag: %s)", cfg.LocalImageTag)
	logger.Info("  - Deployment: Kubernetes namespace '%s'", viper.GetString("namespace"))
	logger.Info("")
	logger.Info("Useful commands:")
	logger.Info("  - List images: sudo k0s ctr images list | grep magnetiq")
	logger.Info("  - Check pods: sudo k0s kubectl -n %s get pods", viper.GetString("namespace"))
	logger.Info("  - Check services: sudo k0s kubectl -n %s get svc", viper.GetString("namespace"))
	logger.Info("")
	logger.Info("Run 'm2deploy verify' for detailed deployment health status")

	return nil
}
