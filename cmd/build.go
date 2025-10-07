package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/payload"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	buildComponent string
	buildTag       string
	buildBranch    string
	buildFresh     bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build Docker images from source code",
	Long: `Build Docker images for components (backend, frontend, or both).

The build command:
1. Uses existing source code at /tmp/<username>/<repo-name>
2. Builds Docker images locally
3. Leaves images in Docker for testing/development

Images remain in Docker. Use 'deploy' command to import images to k0s and deploy.

Workspace path is automatically derived from --repo-url:
  https://github.com/wapsol/magnetiq2 → /tmp/wapsol/magnetiq2

IMPORTANT: Source code must exist before building. Use --fresh to clone it
from GitHub, or to re-clone and overwrite existing code.`,
	Example: `  # Build from existing source code
  m2deploy build --component backend --repo-url https://github.com/wapsol/magnetiq2

  # Clone fresh and build (first time or re-clone)
  m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh

  # Clone from specific branch and build
  m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh --branch develop

  # Build with custom tag
  m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3

  # Build then deploy (images automatically imported by deploy)
  m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2
  m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2`,
	RunE: runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)

	buildCmd.Flags().StringVarP(&buildComponent, "component", "c", "both", "Component to build: backend, frontend, or both")
	buildCmd.Flags().StringVarP(&buildTag, "tag", "t", "", "Image tag (overrides default)")
	buildCmd.Flags().StringVarP(&buildBranch, "branch", "b", "main", "Git branch to use with --fresh")
	buildCmd.Flags().BoolVar(&buildFresh, "fresh", false, "Clone fresh code from GitHub (overwrites existing)")
	buildCmd.MarkFlagRequired("component")
}

func runBuild(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Validate --repo-url is provided (required)
	repoURL := viper.GetString("repo-url")
	if repoURL == "" {
		return formatError("build", fmt.Errorf("--repo-url is required\n   This flag specifies the Git repository URL to derive the workspace path.\n   Example: --repo-url=https://github.com/wapsol/magnetiq2"))
	}

	// Derive workspace path from repo URL
	workDir := deriveWorkspaceFromRepoURL(repoURL)

	logger.Info("Using workspace: %s", workDir)

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckBuildPrereqs(viper.GetBool("use-sudo"))

	// If --check flag is set, print results and exit
	if viper.GetBool("check") {
		checker.PrintResults()

		// Also validate payload structure if workspace exists
		if _, err := os.Stat(workDir); err == nil {
			logger.Info("\n=== Payload Structure Validation ===")
			validator := payload.NewValidator(logger)
			validationErrors := validator.ValidatePayload(workDir)
			if len(validationErrors) > 0 {
				validator.PrintValidationErrors(validationErrors)
				os.Exit(1)
			} else {
				logger.Success("Payload structure is valid")
			}
		} else {
			logger.Warning("Workspace not found at %s - skipping payload validation", workDir)
			logger.Info("Use --fresh to clone the repository first")
		}

		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Otherwise, fail fast if prerequisites not met
	if checker.HasFailures() {
		checker.PrintResults()
		return formatPrereqError("build")
	}

	// Handle source code cloning/updating
	gitClient := newGitClient(logger)

	if buildFresh {
		// Check if directory exists and has content
		if stat, err := os.Stat(workDir); err == nil && stat.IsDir() {
			// Directory exists - confirm before deleting
			if !viper.GetBool("force") && !viper.GetBool("dry-run") {
				msg := fmt.Sprintf("Directory %s will be DELETED and re-cloned.\nAll local changes will be lost.", workDir)
				if !promptForConfirmation(msg) {
					return fmt.Errorf("operation cancelled by user")
				}
			}
		}

		// Remove existing directory and clone fresh
		logger.Info("Fresh clone requested - removing existing directory")
		if !viper.GetBool("dry-run") {
			os.RemoveAll(workDir)
		}
		// Clone fresh from GitHub
		logger.Info("Cloning fresh from %s (branch: %s)", repoURL, buildBranch)
		if err := gitClient.Clone(repoURL, workDir, buildBranch, 1); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		// Check if directory exists
		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			// Directory doesn't exist and --fresh not provided - fail fast
			return formatError("build", fmt.Errorf("source code not found at %s\n"+
				"   The workspace directory does not exist and --fresh flag was not provided.\n"+
				"   Solutions:\n"+
				"   1. Clone fresh code: m2deploy build --component %s --repo-url %s --fresh\n"+
				"   2. Clone manually: git clone %s %s", workDir, buildComponent, repoURL, repoURL, workDir))
		}
		// Directory exists - use existing code
		logger.Info("Using existing source code at %s", workDir)
		logger.Info("(Use --fresh to clone fresh code from GitHub)")
	}

	// Validate payload structure
	validator := payload.NewValidator(logger)
	if err := validator.ValidateStructure(workDir); err != nil {
		return fmt.Errorf("payload validation failed: %w\nUse --check to see detailed validation report", err)
	}

	dockerClient := newDockerClient(logger)
	cfg := getConfig()

	// Resolve image tag with clear precedence
	cfg.LocalImageTag = cfg.ResolveImageTag(logger, buildTag, workDir)

	// Validate component
	if err := validateComponent(buildComponent); err != nil {
		return err
	}

	// Build components
	components := getComponents(buildComponent)
	var builtImages []string

	for _, component := range components {
		// Build the image
		imageName := cfg.GetLocalImageName(component)
		logger.Info("Building %s...", component)
		if err := dockerClient.Build(workDir, component); err != nil {
			return err
		}
		logger.Success("Built image: %s (available in Docker daemon)", imageName)
		builtImages = append(builtImages, imageName)
	}

	logger.Success("All builds completed successfully")
	logger.Info("")
	logger.Info("Built images (in Docker daemon):")
	for _, img := range builtImages {
		logger.Info("  - %s", img)
	}
	logger.Info("")
	logger.Info("Useful commands:")
	logger.Info("  - List images: sudo docker images | grep magnetiq")
	if len(builtImages) > 0 {
		logger.Info("  - Inspect image: sudo docker inspect %s", builtImages[0])
	}
	logger.Info("")
	logger.Info("Next step: Use 'deploy' command to import images to k0s and deploy")

	return nil
}
