package cmd

import (
	"os"

	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/database"
	"github.com/wapsol/m2deploy/pkg/docker"
	"github.com/wapsol/m2deploy/pkg/git"
	"github.com/wapsol/m2deploy/pkg/k8s"
)

// Clients holds all service clients for the application
type Clients struct {
	Logger *config.Logger
	Docker *docker.Client
	K8s    *k8s.Client
	DB     *database.Client
	Git    *git.Client
	Config *config.Config
}

// NewClients creates all service clients from viper configuration
func NewClients() *Clients {
	logger := createLogger()
	cfg := getConfig()

	// Determine if sudo should be used (auto-detect or explicit)
	useSudo := getUseSudoWithAutoDetect(logger)

	dockerClient := docker.NewClient(logger, viper.GetBool("dry-run"), cfg, useSudo)

	// Enable external builder if flag is set (default: true)
	if viper.GetBool("external-build") {
		dockerClient.EnableExternalBuilder()
		logger.Debug("External builder mode enabled")
	}

	return &Clients{
		Logger: logger,
		Docker: dockerClient,
		K8s:    k8s.NewClient(logger, viper.GetBool("dry-run"), viper.GetString("namespace"), viper.GetString("kubeconfig")),
		DB:     database.NewClient(logger, viper.GetBool("dry-run"), viper.GetString("namespace"), viper.GetString("kubeconfig")),
		Git:    git.NewClient(logger, viper.GetBool("dry-run")),
		Config: cfg,
	}
}

// createLogger creates a logger with file support based on flags
func createLogger() *config.Logger {
	return createLoggerForCommand(getCurrentCommandName())
}

// createLoggerForCommand creates logger with specific command name
func createLoggerForCommand(commandName string) *config.Logger {
	verbose := viper.GetBool("verbose")

	// Check if file logging is disabled
	if viper.GetBool("no-log-file") {
		return config.NewLogger(verbose)
	}

	logFilePath := viper.GetString("log-file")
	logger, err := config.NewLoggerWithFile(verbose, logFilePath, commandName)
	if err != nil {
		// Fall back to console-only logging if file logging fails
		logger = config.NewLogger(verbose)
		logger.Warning("Failed to initialize file logging: %v", err)
		logger.Warning("Falling back to console-only logging")
	}

	return logger
}

// getCurrentCommandName extracts command name from os.Args
func getCurrentCommandName() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	return "unknown"
}

// getConfig creates a config object from viper settings
func getConfig() *config.Config {
	return &config.Config{
		RepoURL:       viper.GetString("repo-url"),
		WorkDir:       viper.GetString("work-dir"),
		Namespace:     viper.GetString("namespace"),
		Kubeconfig:    viper.GetString("kubeconfig"),
		DryRun:        viper.GetBool("dry-run"),
		Verbose:       viper.GetBool("verbose"),
		LocalImageTag: viper.GetString("local-image-tag"),
	}
}

// Legacy individual client constructors (for gradual migration)
// newDockerClient creates a new Docker client with configuration from viper
func newDockerClient(logger *config.Logger) *docker.Client {
	cfg := getConfig()

	// Determine if sudo should be used (auto-detect or explicit)
	useSudo := getUseSudoWithAutoDetect(logger)

	client := docker.NewClient(
		logger,
		viper.GetBool("dry-run"),
		cfg,
		useSudo,
	)

	// Enable external builder if flag is set (default: true)
	if viper.GetBool("external-build") {
		client.EnableExternalBuilder()
		logger.Debug("External builder mode enabled")
	}

	return client
}

// newK8sClient creates a new Kubernetes client with configuration from viper
func newK8sClient(logger *config.Logger) *k8s.Client {
	return k8s.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)
}

// newGitClient creates a new Git client with configuration from viper
func newGitClient(logger *config.Logger) *git.Client {
	return git.NewClient(logger, viper.GetBool("dry-run"))
}

// newDBClient creates a new database client with configuration from viper
func newDBClient(logger *config.Logger) *database.Client {
	return database.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)
}

// getUseSudoWithAutoDetect determines if sudo should be used, with auto-detection
func getUseSudoWithAutoDetect(logger *config.Logger) bool {
	// Check if --use-sudo flag was explicitly set by user
	flagWasSet := viper.IsSet("use-sudo")
	explicitValue := viper.GetBool("use-sudo")

	// Use helper function to determine final value
	useSudo := config.ShouldUseSudo(explicitValue, flagWasSet)

	// Log auto-detection for transparency
	if !flagWasSet && useSudo {
		logger.Debug("Running as root - automatically enabling sudo for Docker/k0s subprocesses")
		logger.Debug("(Use --use-sudo=false to disable)")
	}

	return useSudo
}
