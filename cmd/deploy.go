package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/constants"
	"github.com/wapsol/m2deploy/pkg/payload"
	"github.com/wapsol/m2deploy/pkg/prereq"
	"github.com/wapsol/m2deploy/pkg/ssh"
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
Applies manifests in the correct order.

Workspace can be specified in two ways:
1. --repo-url: Auto-derive workspace from repository URL (e.g., /tmp/wapsol/magnetiq2)
2. --workspace-path: Specify workspace path directly`,
	Example: `  # Using repo URL (auto-derives workspace path)
  m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2

  # Using workspace path directly
  m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2

  # With additional options
  m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 --validate --wait
  m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 --skip-import`,
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

	// Get workspace path (either from --workspace-path or derive from --repo-url)
	repoURL := viper.GetString("repo-url")
	workspacePath := viper.GetString("workspace-path")

	var workDir string
	if workspacePath != "" {
		// Use workspace-path directly if provided
		workDir = workspacePath
		logger.Info("Using workspace: %s (from --workspace-path)", workDir)
	} else if repoURL != "" {
		// Derive workspace from repo URL
		workDir = deriveWorkspaceFromRepoURL(repoURL)
		logger.Info("Using workspace: %s (derived from --repo-url)", workDir)
	} else {
		// Neither provided - fail with helpful message
		return fmt.Errorf("either --repo-url or --workspace-path is required\nUse --workspace-path to specify workspace directly, or --repo-url to auto-derive it")
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
	validator := payload.NewValidator(logger)
	// Basic validation - ensure k8s directory exists
	if err := validator.ValidateStructure(workDir); err != nil {
		return fmt.Errorf("payload validation failed: %w", err)
	}

	// Distribute Docker images to worker nodes via SSH (unless skipped)
	if !deploySkipImport {
		logger.Info("Step 1: Distributing images to worker nodes via SSH")
		logger.Info("")

		// Expand SSH key path (handle ~)
		sshKeyPath := viper.GetString("ssh-key")
		if strings.HasPrefix(sshKeyPath, "~") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			sshKeyPath = filepath.Join(homeDir, sshKeyPath[1:])
		}

		// Create SSH configuration
		sshConfig := &ssh.Config{
			User:          viper.GetString("ssh-user"),
			KeyPath:       sshKeyPath,
			Port:          viper.GetInt("ssh-port"),
			Timeout:       viper.GetInt("ssh-timeout"),
			WorkerTempDir: viper.GetString("worker-temp-dir"),
		}

		// Create distributor
		distributor := ssh.NewDistributor(logger, sshConfig)
		distributor.Parallel = viper.GetInt("parallel-workers")
		distributor.RetryCount = viper.GetInt("retry-count")
		distributor.MinWorkers = viper.GetInt("min-workers")
		distributor.KeepTarballs = viper.GetBool("skip-worker-cleanup")

		// Parse manual worker IPs if provided
		workersFlag := viper.GetString("workers")
		if workersFlag != "" {
			distributor.WorkerIPs = strings.Split(workersFlag, ",")
			for i := range distributor.WorkerIPs {
				distributor.WorkerIPs[i] = strings.TrimSpace(distributor.WorkerIPs[i])
			}
		}

		// Get worker nodes (either from k8s API or manual list)
		k8sClient := newK8sClient(logger)
		workers, err := distributor.GetWorkerNodes(k8sClient)
		if err != nil {
			return fmt.Errorf("failed to get worker nodes: %w", err)
		}

		logger.Info("Found %d worker nodes", len(workers))
		for _, w := range workers {
			logger.Info("  - %s (%s)", w.Name, w.IP)
		}
		logger.Info("")

		// Test SSH connectivity
		logger.Info("Testing SSH connectivity to all workers...")
		if err := distributor.TestConnectivity(workers); err != nil {
			return fmt.Errorf("SSH connectivity test failed: %w", err)
		}
		logger.Success("All workers reachable via SSH")
		logger.Info("")

		// Distribute each component
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

			// Distribute to all workers
			results, err := distributor.DistributeToAllWorkers(workers, tarballPath, component, imageName)
			if err != nil {
				return fmt.Errorf("failed to distribute %s: %w", component, err)
			}

			// Clean up local tarball
			if !viper.GetBool("dry-run") {
				os.Remove(tarballPath)
				logger.Debug("Removed local tarball: %s", tarballPath)
			}

			// Log distribution summary
			successCount := 0
			for _, result := range results {
				if result.Success {
					successCount++
				}
			}
			logger.Info("  Distributed %s to %d/%d workers", component, successCount, len(workers))
		}

		// Verify images on all workers
		logger.Info("")
		logger.Info("Verifying images on worker nodes...")
		for _, component := range components {
			imageName := cfg.GetLocalImageName(component)

			successCount := 0
			for _, worker := range workers {
				if err := distributor.VerifyImportOnWorker(worker, imageName); err != nil {
					logger.Warning("Verification failed on %s: %v", worker.Name, err)
				} else {
					logger.Success("✓ %s has %s", worker.Name, imageName)
					successCount++
				}
			}

			minRequired := distributor.MinWorkers
			if minRequired == 0 {
				minRequired = len(workers)
			}

			if successCount < minRequired {
				return fmt.Errorf("image %s not available on enough workers (%d/%d, minimum: %d)",
					imageName, successCount, len(workers), minRequired)
			}
		}

		logger.Info("")
		logger.Info("All images successfully distributed to worker nodes")
		logger.Info("")
	} else {
		logger.Info("Skipping image distribution (--skip-import flag set)")
		logger.Warning("Assuming images are already available on worker nodes")
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
