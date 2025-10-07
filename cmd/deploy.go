package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/constants"
	"github.com/wapsol/m2deploy/pkg/payload"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	deployValidate  bool
	deployWait      bool
	deploySkipImport bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy Magnetiq2 to Kubernetes",
	Long: `Deploy the Magnetiq2 application to the Kubernetes cluster.
Automatically imports Docker images to k0s containerd before deploying.
Applies manifests in the correct order.`,
	Example: `  m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
  m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --validate --wait
  m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --skip-import`,
	RunE: runDeploy,
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().BoolVar(&deployValidate, "validate", false, "Validate manifests before applying")
	deployCmd.Flags().BoolVar(&deployWait, "wait", false, "Wait for deployments to be ready")
	deployCmd.Flags().BoolVar(&deploySkipImport, "skip-import", false, "Skip importing Docker images to k0s (images must already be in k0s)")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Validate --repo-url is provided (required for image import)
	repoURL := viper.GetString("repo-url")
	if repoURL == "" && !deploySkipImport {
		return fmt.Errorf("--repo-url is required (needed to derive workspace path for images)\nUse --skip-import if images are already in k0s")
	}

	// Derive workspace path from repo URL
	var workDir string
	if repoURL != "" {
		workDir = deriveWorkspaceFromRepoURL(repoURL)
		logger.Info("Using workspace: %s", workDir)
	}

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckDeployPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

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
		return formatPrereqError("deploy")
	}

	// Validate payload structure (k8s manifests must exist)
	if workDir != "" {
		validator := payload.NewValidator(logger)
		// Basic validation - ensure k8s directory exists
		if err := validator.ValidateStructure(workDir); err != nil {
			return fmt.Errorf("payload validation failed: %w", err)
		}
	}

	// Import Docker images to k0s (unless skipped)
	if !deploySkipImport {
		logger.Info("Step 1: Importing images from Docker daemon to k0s containerd")

		dockerClient := newDockerClient(logger)
		cfg := getConfig()

		components := []string{constants.ComponentBackend, constants.ComponentFrontend}
		for _, component := range components {
			imageName := cfg.GetLocalImageName(component)
			tarballPath := fmt.Sprintf(constants.TarballPathTemplate, component)

			// Save image to tarball
			logger.Info("Exporting %s from Docker daemon...", imageName)
			if err := dockerClient.SaveImage(component, tarballPath); err != nil {
				return fmt.Errorf("failed to save %s image: %w\nMake sure you have built the images with 'build' command", component, err)
			}
			logger.Info("Saved to temporary tarball: %s", tarballPath)

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
		logger.Info("")
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
		logger.Info("List k0s images: sudo k0s ctr images list | grep magnetiq")
		logger.Info("")
	} else {
		logger.Info("Skipping image import (--skip-import flag set)")
		logger.Warning("Assuming images are already in k0s containerd")
		logger.Info("")
	}

	k8sClient := newK8sClient(logger)

	logger.Info("Step 2: Deploying to Kubernetes cluster")

	if deployValidate {
		logger.Info("Validation enabled - checking manifests before applying")
	}
	if deployWait {
		logger.Info("Wait enabled - will wait for deployments to be ready")
	}

	// Deploy application with options
	if err := k8sClient.DeployWithOptions(workDir, deployValidate, deployWait); err != nil {
		return err
	}

	logger.Success("Deployment completed successfully")
	logger.Info("")
	logger.Info("Deployment location: Kubernetes namespace '%s'", viper.GetString("namespace"))
	logger.Info("Check pods: sudo k0s kubectl -n %s get pods", viper.GetString("namespace"))
	logger.Info("Check services: sudo k0s kubectl -n %s get svc", viper.GetString("namespace"))
	logger.Info("")
	if !deployWait {
		logger.Info("Run 'm2deploy verify' for detailed deployment health status")
	}

	return nil
}
