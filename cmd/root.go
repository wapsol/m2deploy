package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	repoURL         string
	namespace       string
	kubeconfig      string
	dryRun          bool
	verbose         bool
	useSudo         bool
	appName         string
	k8sDir          string
	localImageTag   string
	checkOnly       bool
	logFile         string
	noLogFile       bool
	useExternalBuild bool // Use external build script to avoid resource exhaustion
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

	// Global flags - Logging
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "/var/log/m2deploy/operations.log", "Path to log file (set empty to disable)")
	rootCmd.PersistentFlags().BoolVar(&noLogFile, "no-log-file", false, "Disable file logging")

	// Global flags - Repository
	rootCmd.PersistentFlags().StringVar(&repoURL, "repo-url", "", "Git repository URL (workspace auto-derived: /tmp/<user>/<repo>) [REQUIRED]")

	// Global flags - Application
	rootCmd.PersistentFlags().StringVar(&appName, "app-name", "magnetiq", "Application name")

	// Global flags - Docker/Image
	rootCmd.PersistentFlags().BoolVar(&useSudo, "use-sudo", false, "Use sudo for Docker and k0s commands (auto-detected when running as root)")
	rootCmd.PersistentFlags().StringVar(&localImageTag, "local-image-tag", "latest", "Tag for local images")
	rootCmd.PersistentFlags().BoolVar(&useExternalBuild, "external-build", true, "Use external build script to prevent resource exhaustion (recommended)")

	// Global flags - Kubernetes
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "magnetiq-v2", "Kubernetes namespace")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: k0s)")
	rootCmd.PersistentFlags().StringVar(&k8sDir, "k8s-dir", "k8s", "Kubernetes manifests directory")

	// Bind flags to viper
	viper.BindPFlag("repo-url", rootCmd.PersistentFlags().Lookup("repo-url"))
	viper.BindPFlag("app-name", rootCmd.PersistentFlags().Lookup("app-name"))
	viper.BindPFlag("use-sudo", rootCmd.PersistentFlags().Lookup("use-sudo"))
	viper.BindPFlag("local-image-tag", rootCmd.PersistentFlags().Lookup("local-image-tag"))
	viper.BindPFlag("external-build", rootCmd.PersistentFlags().Lookup("external-build"))
	viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	viper.BindPFlag("k8s-dir", rootCmd.PersistentFlags().Lookup("k8s-dir"))
	viper.BindPFlag("dry-run", rootCmd.PersistentFlags().Lookup("dry-run"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("check", rootCmd.PersistentFlags().Lookup("check"))
	viper.BindPFlag("log-file", rootCmd.PersistentFlags().Lookup("log-file"))
	viper.BindPFlag("no-log-file", rootCmd.PersistentFlags().Lookup("no-log-file"))
}

func initConfig() {
	// Only support environment variables and command-line flags
	// No config file support to force explicit parameterization
	viper.AutomaticEnv()
}
