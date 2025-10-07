package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the application configuration
type Config struct {
	RepoURL       string
	WorkDir       string
	Namespace     string
	Kubeconfig    string
	DryRun        bool
	Verbose       bool
	LocalImageTag string
	ImagePrefix   string // Container image prefix (e.g., "magnetiq", "myapp/prod")
}

// GetLocalImageName returns the local image name for a component
func (c *Config) GetLocalImageName(component string) string {
	return fmt.Sprintf("%s/%s:%s", c.ImagePrefix, component, c.LocalImageTag)
}

// ResolveImageTag resolves the image tag to use with clear precedence:
// 1. Command flag (highest priority)
// 2. Global flag
// 3. Git commit SHA (if workDir is a git repo)
// 4. "latest" (fallback)
func (c *Config) ResolveImageTag(logger *Logger, commandTag, workDir string) string {
	// 1. Command flag takes precedence
	if commandTag != "" {
		logger.Debug("Using command flag tag: %s", commandTag)
		return commandTag
	}

	// 2. Global flag
	if c.LocalImageTag != "" && c.LocalImageTag != "latest" {
		logger.Debug("Using global flag tag: %s", c.LocalImageTag)
		return c.LocalImageTag
	}

	// 3. Try to get git commit SHA (only if workDir is valid)
	if workDir != "" {
		if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
			// workDir is a git repository, try to get commit SHA
			commit, err := getGitCommitSHA(workDir)
			if err == nil && commit != "" {
				logger.Debug("Using git commit SHA as tag: %s", commit)
				return commit
			}
			logger.Debug("Failed to get git commit SHA, using fallback")
		}
	}

	// 4. Fallback to "latest"
	logger.Debug("Using fallback tag: latest")
	return "latest"
}

// getGitCommitSHA gets the current git commit SHA from a directory
func getGitCommitSHA(workDir string) (string, error) {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "--short", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// IsRunningAsRoot checks if the current process is running as root (UID 0)
func IsRunningAsRoot() bool {
	return os.Geteuid() == 0
}

// ShouldUseSudo determines if sudo should be used for Docker/k0s commands
// Takes into account both explicit flag and auto-detection when running as root
func ShouldUseSudo(explicitFlag bool, flagWasSet bool) bool {
	// If flag was explicitly set, respect it
	if flagWasSet {
		return explicitFlag
	}

	// If running as root and flag not explicitly set, enable sudo for subprocesses
	// This handles the case: sudo ./m2deploy build ...
	if IsRunningAsRoot() {
		return true
	}

	// Default: no sudo
	return false
}

// Logger provides logging functionality
type Logger struct {
	Verbose     bool
	LogFile     *os.File
	CommandName string // Track which command is logging
	SessionID   string // Session correlation ID
}

// NewLogger creates a new logger instance
func NewLogger(verbose bool) *Logger {
	return &Logger{Verbose: verbose, LogFile: nil}
}

// NewLoggerWithFile creates a logger with file output and command context
func NewLoggerWithFile(verbose bool, logFilePath string, commandName string) (*Logger, error) {
	logger := &Logger{
		Verbose:     verbose,
		CommandName: commandName,
		SessionID:   generateSessionID(),
	}

	// Skip file logging if path is empty
	if logFilePath == "" {
		return logger, nil
	}

	// Create log directory if it doesn't exist
	logDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return logger, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in append mode
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return logger, fmt.Errorf("failed to open log file: %w", err)
	}

	logger.LogFile = file

	// Write session header
	logger.logSessionHeader()

	return logger, nil
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), os.Getpid())
}

// logSessionHeader logs command invocation details
func (l *Logger) logSessionHeader() {
	if l.LogFile != nil {
		fmt.Fprintf(l.LogFile, "\n=== SESSION START: %s ===\n", l.SessionID)
		fmt.Fprintf(l.LogFile, "Command: %s\n", l.CommandName)
		fmt.Fprintf(l.LogFile, "User: %s\n", os.Getenv("USER"))
		if wd, err := os.Getwd(); err == nil {
			fmt.Fprintf(l.LogFile, "Working Dir: %s\n", wd)
		}
		fmt.Fprintf(l.LogFile, "Args: %v\n", os.Args[1:])
		fmt.Fprintf(l.LogFile, "===========================\n\n")
	}
}

// Close closes the log file if open
func (l *Logger) Close() error {
	if l.LogFile != nil {
		fmt.Fprintf(l.LogFile, "\n=== SESSION END: %s ===\n\n", l.SessionID)
		return l.LogFile.Close()
	}
	return nil
}

// writeToFile writes a message to the log file with timestamp and command context
func (l *Logger) writeToFile(level, message string) {
	if l.LogFile != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		if l.CommandName != "" {
			fmt.Fprintf(l.LogFile, "[%s] [%s] [%s] %s\n", timestamp, l.CommandName, level, message)
		} else {
			fmt.Fprintf(l.LogFile, "[%s] [%s] %s\n", timestamp, level, message)
		}
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[INFO] %s\n", message)
	l.writeToFile("INFO", message)
}

// Success logs a success message
func (l *Logger) Success(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[SUCCESS] %s\n", message)
	l.writeToFile("SUCCESS", message)
}

// Warning logs a warning message
func (l *Logger) Warning(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[WARNING] %s\n", message)
	l.writeToFile("WARNING", message)
}

// WarningDetailed logs a concise warning to console, detailed to log file
func (l *Logger) WarningDetailed(consoleMsg string, logMsg string) {
	fmt.Printf("[WARNING] %s\n", consoleMsg)
	l.writeToFile("WARNING", logMsg)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[ERROR] %s\n", message)
	l.writeToFile("ERROR", message)
}

// Debug logs a debug message (only if verbose is enabled)
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.Verbose {
		message := fmt.Sprintf(format, args...)
		fmt.Printf("[DEBUG] %s\n", message)
		l.writeToFile("DEBUG", message)
	}
}

// DryRun logs a dry-run message
func (l *Logger) DryRun(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[DRY-RUN] %s\n", message)
	l.writeToFile("DRY-RUN", message)
}
