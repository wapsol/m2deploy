package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	repoURL       string
	workspacePath string
	namespace     string
	kubeconfig    string
	dryRun        bool
	verbose       bool
	useSudo       bool
	force         bool
	appName       string
	imagePrefix   string
	k8sDir        string
	localImageTag string
	checkOnly     bool
	logFile       string
	noLogFile     bool

	// SSH configuration
	sshUser    string
	sshKey     string
	sshPort    int
	sshTimeout int

	// Distribution behavior
	workerTempDir     string
	parallelWorkers   int
	retryCount        int
	minWorkers        int
	skipWorkerCleanup bool
	workers           string
)

var rootCmd = &cobra.Command{
	Use:   "m2deploy",
	Short: "Generic Web Application Deployment Tool",
	Long: `m2deploy is a comprehensive CLI tool for deploying, updating, and managing
web applications on Kubernetes (k0s) clusters.

It handles the complete deployment lifecycle including:
- Building Docker images locally
- Deploying to Kubernetes with custom configurations
- Database migrations and backups
- Rolling updates and rollbacks
- Health verification and smoke testing

Designed to be generic and reusable for any web application with
backend/frontend architecture.`,
	Version:       "2.0.0",
	SilenceUsage:  true,  // Don't show usage on errors
	SilenceErrors: false, // Still show error messages
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Error is already printed by cobra due to SilenceErrors: false
		// We just need to exit with non-zero status
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags - Basic
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without executing")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVar(&checkOnly, "check", false, "Check prerequisites and exit without executing")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "Skip confirmation prompts for destructive operations")

	// Global flags - Logging
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "/var/log/m2deploy/operations.log", "Path to log file (set empty to disable)")
	rootCmd.PersistentFlags().BoolVar(&noLogFile, "no-log-file", false, "Disable file logging")

	// Global flags - Repository and Workspace
	rootCmd.PersistentFlags().StringVar(&repoURL, "repo-url", "", "Git repository URL (workspace auto-derived: /tmp/<user>/<repo>)")
	rootCmd.PersistentFlags().StringVar(&workspacePath, "workspace-path", "", "Direct path to workspace (alternative to --repo-url)")

	// Global flags - Application
	rootCmd.PersistentFlags().StringVar(&appName, "app-name", "magnetiq", "Application name")
	rootCmd.PersistentFlags().StringVar(&imagePrefix, "image-prefix", "crepo.re-cloud.io/magnetiq/v2", "Container image prefix (e.g., crepo.re-cloud.io/magnetiq/v2)")

	// Global flags - Docker/Image
	rootCmd.PersistentFlags().BoolVar(&useSudo, "use-sudo", false, "Use sudo for Docker and k0s commands (auto-detected when running as root)")
	rootCmd.PersistentFlags().StringVar(&localImageTag, "local-image-tag", "latest", "Tag for local images")

	// Global flags - SSH Configuration
	rootCmd.PersistentFlags().StringVar(&sshUser, "ssh-user", "ubuntu", "SSH username for worker nodes")
	rootCmd.PersistentFlags().StringVar(&sshKey, "ssh-key", "~/.ssh/id_rsa", "SSH private key path")
	rootCmd.PersistentFlags().IntVar(&sshPort, "ssh-port", 22, "SSH port for worker nodes")
	rootCmd.PersistentFlags().IntVar(&sshTimeout, "ssh-timeout", 30, "SSH connection timeout in seconds")

	// Global flags - Distribution Behavior
	rootCmd.PersistentFlags().StringVar(&workerTempDir, "worker-temp-dir", "/tmp", "Temporary directory on worker nodes")
	rootCmd.PersistentFlags().IntVar(&parallelWorkers, "parallel-workers", 3, "Distribute to N workers in parallel")
	rootCmd.PersistentFlags().IntVar(&retryCount, "retry-count", 3, "Number of retries per worker on failure")
	rootCmd.PersistentFlags().IntVar(&minWorkers, "min-workers", 0, "Minimum workers that must succeed (0 = all required)")
	rootCmd.PersistentFlags().BoolVar(&skipWorkerCleanup, "skip-worker-cleanup", false, "Keep tarballs on workers for debugging")
	rootCmd.PersistentFlags().StringVar(&workers, "workers", "", "Comma-separated worker IPs (override auto-discovery)")

	// Global flags - Kubernetes
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "magnetiq-v2", "Kubernetes namespace")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: k0s)")
	rootCmd.PersistentFlags().StringVar(&k8sDir, "k8s-dir", "k8s", "Kubernetes manifests directory")

	// Bind flags to viper
	viper.BindPFlag("repo-url", rootCmd.PersistentFlags().Lookup("repo-url"))
	viper.BindPFlag("workspace-path", rootCmd.PersistentFlags().Lookup("workspace-path"))
	viper.BindPFlag("app-name", rootCmd.PersistentFlags().Lookup("app-name"))
	viper.BindPFlag("image-prefix", rootCmd.PersistentFlags().Lookup("image-prefix"))
	viper.BindPFlag("use-sudo", rootCmd.PersistentFlags().Lookup("use-sudo"))
	viper.BindPFlag("local-image-tag", rootCmd.PersistentFlags().Lookup("local-image-tag"))
	viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	viper.BindPFlag("k8s-dir", rootCmd.PersistentFlags().Lookup("k8s-dir"))
	viper.BindPFlag("dry-run", rootCmd.PersistentFlags().Lookup("dry-run"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("check", rootCmd.PersistentFlags().Lookup("check"))
	viper.BindPFlag("force", rootCmd.PersistentFlags().Lookup("force"))
	viper.BindPFlag("log-file", rootCmd.PersistentFlags().Lookup("log-file"))
	viper.BindPFlag("no-log-file", rootCmd.PersistentFlags().Lookup("no-log-file"))

	// Bind SSH and distribution flags
	viper.BindPFlag("ssh-user", rootCmd.PersistentFlags().Lookup("ssh-user"))
	viper.BindPFlag("ssh-key", rootCmd.PersistentFlags().Lookup("ssh-key"))
	viper.BindPFlag("ssh-port", rootCmd.PersistentFlags().Lookup("ssh-port"))
	viper.BindPFlag("ssh-timeout", rootCmd.PersistentFlags().Lookup("ssh-timeout"))
	viper.BindPFlag("worker-temp-dir", rootCmd.PersistentFlags().Lookup("worker-temp-dir"))
	viper.BindPFlag("parallel-workers", rootCmd.PersistentFlags().Lookup("parallel-workers"))
	viper.BindPFlag("retry-count", rootCmd.PersistentFlags().Lookup("retry-count"))
	viper.BindPFlag("min-workers", rootCmd.PersistentFlags().Lookup("min-workers"))
	viper.BindPFlag("skip-worker-cleanup", rootCmd.PersistentFlags().Lookup("skip-worker-cleanup"))
	viper.BindPFlag("workers", rootCmd.PersistentFlags().Lookup("workers"))
}

func initConfig() {
	// Only support environment variables and command-line flags
	// No config file support to force explicit parameterization
	viper.AutomaticEnv()
}
