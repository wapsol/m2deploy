package builder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wapsol/m2deploy/pkg/config"
)

// ExternalBuilder handles building Docker images using an external script
// to isolate resource usage from the main m2deploy process
type ExternalBuilder struct {
	Logger     *config.Logger
	DryRun     bool
	Config     *config.Config
	UseSudo    bool
	ScriptPath string // Path to build.sh script
}

// BuildResult contains information about a completed build
type BuildResult struct {
	Component string
	ImageName string
	LogFile   string
	Success   bool
	Duration  time.Duration
	Error     error
}

// NewExternalBuilder creates a new external builder instance
func NewExternalBuilder(logger *config.Logger, dryRun bool, cfg *config.Config, useSudo bool) *ExternalBuilder {
	return &ExternalBuilder{
		Logger:  logger,
		DryRun:  dryRun,
		Config:  cfg,
		UseSudo: useSudo,
		// ScriptPath will be set relative to workspace
	}
}

// Build builds a Docker image using the external build script
// This runs the build in a separate process, avoiding resource exhaustion in m2deploy
func (eb *ExternalBuilder) Build(workDir, component string) error {
	startTime := time.Now()

	imageName := eb.Config.GetLocalImageName(component)
	tag := eb.Config.LocalImageTag

	eb.Logger.Info("Building %s image using external builder: %s", component, imageName)

	if eb.DryRun {
		eb.Logger.DryRun("Would run external build script for %s", component)
		return nil
	}

	// Determine script path
	scriptPath := filepath.Join(workDir, "scripts", "build.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("build script not found: %s", scriptPath)
	}

	// Generate log file path
	logDir := "/var/log/m2deploy"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// Fall back to /tmp if we can't create /var/log/m2deploy
		logDir = "/tmp"
		eb.Logger.Debug("Using fallback log directory: %s", logDir)
	}
	logFile := filepath.Join(logDir, fmt.Sprintf("build-%s-%s.log", component, tag))

	// Prepare environment variables
	env := os.Environ()
	env = append(env, "BUILD_TARGET=production")
	env = append(env, "BUILD_NETWORK=host")
	env = append(env, "REGISTRY_PREFIX=magnetiq") // Match GetLocalImageName pattern

	if eb.UseSudo {
		env = append(env, "USE_SUDO=true")
	} else {
		env = append(env, "USE_SUDO=false")
	}

	// Build command
	cmd := exec.Command(scriptPath, component, tag, logFile)
	cmd.Env = env
	cmd.Dir = workDir

	// Stream output to logger (not to memory buffers)
	// This is the key difference: we let the script handle output streaming
	// and we just capture the final result
	eb.Logger.Info("Starting build process for %s (logs: %s)", component, logFile)
	eb.Logger.Debug("Executing: %s %s %s %s", scriptPath, component, tag, logFile)

	// Run the command and wait for completion
	output, err := cmd.CombinedOutput()

	duration := time.Since(startTime)

	if err != nil {
		// Log error details
		eb.Logger.Error("Build failed for %s after %s", component, duration)
		if len(output) > 0 {
			// Only log last few lines to avoid clutter
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			lastLines := lines
			if len(lines) > 10 {
				lastLines = lines[len(lines)-10:]
			}
			eb.Logger.Error("Last output lines:")
			for _, line := range lastLines {
				eb.Logger.Error("  %s", line)
			}
		}
		eb.Logger.Error("Full build log: %s", logFile)
		return fmt.Errorf("build script failed for %s: %w", component, err)
	}

	eb.Logger.Success("Built %s image in %s: %s", component, duration, imageName)
	eb.Logger.Debug("Build log available at: %s", logFile)

	return nil
}

// BuildAsync builds an image asynchronously and returns immediately
// The caller can monitor progress by tailing the log file
func (eb *ExternalBuilder) BuildAsync(workDir, component string) (*BuildProcess, error) {
	imageName := eb.Config.GetLocalImageName(component)
	tag := eb.Config.LocalImageTag

	eb.Logger.Info("Starting async build for %s: %s", component, imageName)

	if eb.DryRun {
		eb.Logger.DryRun("Would start async build for %s", component)
		return &BuildProcess{
			Component: component,
			ImageName: imageName,
			LogFile:   "/tmp/dryrun.log",
			Done:      true,
		}, nil
	}

	// Determine script path
	scriptPath := filepath.Join(workDir, "scripts", "build.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("build script not found: %s", scriptPath)
	}

	// Generate log file path
	logDir := "/var/log/m2deploy"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logDir = "/tmp"
	}
	logFile := filepath.Join(logDir, fmt.Sprintf("build-%s-%s.log", component, tag))

	// Prepare environment
	env := os.Environ()
	env = append(env, "BUILD_TARGET=production")
	env = append(env, "BUILD_NETWORK=host")
	env = append(env, "REGISTRY_PREFIX=magnetiq") // Match GetLocalImageName pattern

	if eb.UseSudo {
		env = append(env, "USE_SUDO=true")
	} else {
		env = append(env, "USE_SUDO=false")
	}

	// Build command
	cmd := exec.Command(scriptPath, component, tag, logFile)
	cmd.Env = env
	cmd.Dir = workDir

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start build process: %w", err)
	}

	eb.Logger.Info("Build process started for %s (PID: %d, logs: %s)", component, cmd.Process.Pid, logFile)

	// Create build process tracker
	bp := &BuildProcess{
		Component: component,
		ImageName: imageName,
		LogFile:   logFile,
		StartTime: time.Now(),
		Cmd:       cmd,
		Done:      false,
	}

	return bp, nil
}

// BuildProcess tracks an asynchronous build process
type BuildProcess struct {
	Component string
	ImageName string
	LogFile   string
	StartTime time.Time
	Cmd       *exec.Cmd
	Done      bool
	Error     error
}

// Wait waits for the build process to complete
func (bp *BuildProcess) Wait() error {
	if bp.Done {
		return bp.Error
	}

	err := bp.Cmd.Wait()
	bp.Done = true
	bp.Error = err

	return err
}

// IsRunning checks if the build process is still running
func (bp *BuildProcess) IsRunning() bool {
	if bp.Done {
		return false
	}

	// Check if process is still alive
	if bp.Cmd.Process == nil {
		return false
	}

	// Try to send signal 0 (doesn't actually send a signal, just checks if process exists)
	err := bp.Cmd.Process.Signal(os.Signal(nil))
	return err == nil
}

// Duration returns how long the build has been running
func (bp *BuildProcess) Duration() time.Duration {
	return time.Since(bp.StartTime)
}

// TailLog returns the last N lines from the build log
func (bp *BuildProcess) TailLog(lines int) ([]string, error) {
	content, err := os.ReadFile(bp.LogFile)
	if err != nil {
		return nil, err
	}

	allLines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(allLines) <= lines {
		return allLines, nil
	}

	return allLines[len(allLines)-lines:], nil
}
