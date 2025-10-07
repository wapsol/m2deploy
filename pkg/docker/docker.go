package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wapsol/m2deploy/pkg/builder"
	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/constants"
)

// Client handles Docker operations
type Client struct {
	Logger         *config.Logger
	DryRun         bool
	Config         *config.Config
	UseSudo        bool
	ExternalBuilder *builder.ExternalBuilder // Optional: use external build script
}

// NewClient creates a new Docker client
func NewClient(logger *config.Logger, dryRun bool, cfg *config.Config, useSudo bool) *Client {
	return &Client{
		Logger:          logger,
		DryRun:          dryRun,
		Config:          cfg,
		UseSudo:         useSudo,
		ExternalBuilder: nil, // Will be initialized if needed
	}
}

// EnableExternalBuilder enables the external build script mode
// This isolates build processes to prevent resource exhaustion
func (c *Client) EnableExternalBuilder() {
	c.ExternalBuilder = builder.NewExternalBuilder(c.Logger, c.DryRun, c.Config, c.UseSudo)
	c.Logger.Debug("External builder enabled")
}

// buildDockerCmd builds a docker command with optional sudo
func (c *Client) buildDockerCmd(args ...string) *exec.Cmd {
	if c.UseSudo {
		allArgs := append([]string{"docker"}, args...)
		return exec.Command("sudo", allArgs...)
	}
	return exec.Command("docker", args...)
}

// runCmdWithError runs a command and returns detailed error with stderr/stdout
func (c *Client) runCmdWithError(cmd *exec.Cmd) error {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Combine stderr and stdout for full error context
		var errMsg strings.Builder
		if stderr.Len() > 0 {
			errMsg.WriteString(strings.TrimSpace(stderr.String()))
		}
		if stdout.Len() > 0 {
			if errMsg.Len() > 0 {
				errMsg.WriteString("; ")
			}
			errMsg.WriteString(strings.TrimSpace(stdout.String()))
		}

		if errMsg.Len() > 0 {
			return fmt.Errorf("%v: %s", err, errMsg.String())
		}
		return err
	}
	return nil
}

// Build builds a Docker image with local tag
// If external builder is enabled, delegates to build script; otherwise uses inline Docker build
func (c *Client) Build(workDir, component string) error {
	// Use external builder if enabled (recommended for production)
	if c.ExternalBuilder != nil {
		c.Logger.Debug("Using external builder for %s", component)
		return c.ExternalBuilder.Build(workDir, component)
	}

	// Fall back to inline build (original implementation)
	c.Logger.Debug("Using inline Docker build for %s", component)
	return c.buildInline(workDir, component)
}

// buildInline performs an inline Docker build (original implementation)
// This method captures output in memory and can cause resource exhaustion
func (c *Client) buildInline(workDir, component string) error {
	imageName := c.Config.GetLocalImageName(component)
	contextPath := filepath.Join(workDir, component)

	c.Logger.Info("Building %s image: %s", component, imageName)

	if c.DryRun {
		c.Logger.DryRun("Would build image %s from %s", imageName, contextPath)
		return nil
	}

	args := []string{
		"build",
		"--network=host",
		"-t", imageName,
		"--target", "production",
		contextPath,
	}

	cmd := c.buildDockerCmd(args...)

	// Capture output to avoid cluttering console with Docker warnings
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	c.Logger.Debug("Executing: docker %s", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		// On error, show the output
		if stderr.Len() > 0 {
			c.Logger.Error("Docker build failed: %s", stderr.String())
		}
		return fmt.Errorf("failed to build %s image: %w", component, err)
	}

	// Log warnings to file only (not console)
	if stderr.Len() > 0 {
		c.Logger.Debug("Docker build warnings: %s", strings.TrimSpace(stderr.String()))
	}

	c.Logger.Success("Built %s image: %s", component, imageName)
	return nil
}

// SaveImage saves a Docker image to a tarball
func (c *Client) SaveImage(component, outputPath string) error {
	imageName := c.Config.GetLocalImageName(component)

	c.Logger.Info("Saving %s image to tarball", component)

	if c.DryRun {
		c.Logger.DryRun("Would save image %s to %s", imageName, outputPath)
		return nil
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	cmd := c.buildDockerCmd("save", imageName)
	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	c.Logger.Debug("Executing: docker save %s > %s", imageName, outputPath)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to save %s image: %w", component, err)
	}

	c.Logger.Success("Saved %s image to: %s", component, outputPath)
	return nil
}

// ImportToK0s imports a Docker image tarball into k0s containerd
func (c *Client) ImportToK0s(tarballPath string) error {
	c.Logger.Info("Importing image to k0s containerd")

	if c.DryRun {
		c.Logger.DryRun("Would import tarball %s to k0s", tarballPath)
		return nil
	}

	// Check if tarball exists
	if _, err := os.Stat(tarballPath); os.IsNotExist(err) {
		return fmt.Errorf("tarball not found: %s", tarballPath)
	}

	// Build k0s ctr command
	var cmd *exec.Cmd
	if c.UseSudo {
		cmd = exec.Command("sudo", "k0s", "ctr", "images", "import", tarballPath)
	} else {
		cmd = exec.Command("k0s", "ctr", "images", "import", tarballPath)
	}

	c.Logger.Debug("Executing: k0s ctr images import %s", tarballPath)

	// Use runCmdWithError to capture detailed errors
	if err := c.runCmdWithError(cmd); err != nil {
		// Concise console message, detailed log
		consoleMsg := "Failed to import image to k0s (see log for details)"
		logMsg := fmt.Sprintf("Failed to import image to k0s: %v", err)
		c.Logger.WarningDetailed(consoleMsg, logMsg)
		return fmt.Errorf("k0s import failed")
	}

	c.Logger.Success("Imported image to k0s containerd")
	return nil
}

// ListK0sImages lists images in k0s containerd
func (c *Client) ListK0sImages() (string, error) {
	c.Logger.Debug("Listing k0s containerd images")

	var cmd *exec.Cmd
	if c.UseSudo {
		cmd = exec.Command("sudo", "k0s", "ctr", "images", "list")
	} else {
		cmd = exec.Command("k0s", "ctr", "images", "list")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list k0s images: %w", err)
	}

	return string(output), nil
}

// VerifyImageInK0s verifies that an image exists in k0s containerd
func (c *Client) VerifyImageInK0s(component string) (bool, error) {
	imageName := c.Config.GetLocalImageName(component)

	c.Logger.Debug("Verifying image in k0s: %s", imageName)

	output, err := c.ListK0sImages()
	if err != nil {
		return false, err
	}

	// Check if image name appears in the output
	return strings.Contains(output, imageName), nil
}

// Run runs a Docker container for testing
func (c *Client) Run(component string, port int, envVars map[string]string) error {
	imageName := c.Config.GetLocalImageName(component)
	containerName := fmt.Sprintf("m2deploy-test-%s", component)

	c.Logger.Info("Running test container for %s", component)

	if c.DryRun {
		c.Logger.DryRun("Would run container %s from image %s on port %d", containerName, imageName, port)
		return nil
	}

	// Remove existing container if it exists
	c.Remove(containerName)

	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:%d", port, port),
	}

	// Add environment variables
	for key, value := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, imageName)

	cmd := c.buildDockerCmd(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	c.Logger.Debug("Executing: docker %s", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run %s container: %w", component, err)
	}

	c.Logger.Success("Started test container: %s", containerName)
	return nil
}

// Stop stops a Docker container
func (c *Client) Stop(containerName string) error {
	c.Logger.Info("Stopping container: %s", containerName)

	if c.DryRun {
		c.Logger.DryRun("Would stop container %s", containerName)
		return nil
	}

	cmd := c.buildDockerCmd("stop", containerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerName, err)
	}

	c.Logger.Success("Stopped container: %s", containerName)
	return nil
}

// Remove removes a Docker container
func (c *Client) Remove(containerName string) error {
	if c.DryRun {
		c.Logger.DryRun("Would remove container %s", containerName)
		return nil
	}

	cmd := c.buildDockerCmd("rm", "-f", containerName)
	cmd.Run() // Ignore errors if container doesn't exist

	return nil
}

// GetLogs gets logs from a Docker container
func (c *Client) GetLogs(containerName string, tail int) (string, error) {
	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, containerName)

	cmd := c.buildDockerCmd(args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return string(output), nil
}

// BuildAndImportToK0s builds an image, saves it to tarball, imports to k0s, and cleans up
func (c *Client) BuildAndImportToK0s(workDir, component string) error {
	// Build the image
	if err := c.Build(workDir, component); err != nil {
		return err
	}

	// Generate tarball path using constant
	tarballPath := fmt.Sprintf(constants.TarballPathTemplate, component)

	// Save image to tarball
	c.Logger.Info("Saving %s image to tarball...", component)
	if err := c.SaveImage(component, tarballPath); err != nil {
		return err
	}

	// Import to k0s
	c.Logger.Info("Importing %s image to k0s...", component)
	if err := c.ImportToK0s(tarballPath); err != nil {
		return err
	}

	// Clean up tarball
	if !c.DryRun {
		if err := os.Remove(tarballPath); err != nil {
			c.Logger.Warning("Failed to remove tarball %s: %v", tarballPath, err)
		} else {
			c.Logger.Debug("Removed temporary tarball: %s", tarballPath)
		}
	}

	c.Logger.Success("Built and imported %s image to k0s", component)
	return nil
}

// RemoveImage removes a Docker image
func (c *Client) RemoveImage(component string) error {
	imageName := c.Config.GetLocalImageName(component)

	c.Logger.Info("Removing image: %s", imageName)

	if c.DryRun {
		c.Logger.DryRun("Would remove image %s", imageName)
		return nil
	}

	cmd := c.buildDockerCmd("rmi", imageName)
	if err := c.runCmdWithError(cmd); err != nil {
		// Concise message for console, detailed for log
		consoleMsg := fmt.Sprintf("Failed to remove image %s (see log for details)", imageName)
		logMsg := fmt.Sprintf("Failed to remove image %s: %v", imageName, err)
		c.Logger.WarningDetailed(consoleMsg, logMsg)
		return nil // Don't fail if image doesn't exist
	}

	c.Logger.Success("Removed image: %s", imageName)
	return nil
}

// RemoveK0sImage removes an image from k0s containerd
func (c *Client) RemoveK0sImage(component string) error {
	imageName := c.Config.GetLocalImageName(component)

	c.Logger.Info("Removing k0s image: %s", imageName)

	if c.DryRun {
		c.Logger.DryRun("Would remove k0s image %s", imageName)
		return nil
	}

	// Build k0s ctr command
	var cmd *exec.Cmd
	if c.UseSudo {
		cmd = exec.Command("sudo", "k0s", "ctr", "images", "rm", imageName)
	} else {
		cmd = exec.Command("k0s", "ctr", "images", "rm", imageName)
	}

	c.Logger.Debug("Executing: k0s ctr images rm %s", imageName)

	if err := c.runCmdWithError(cmd); err != nil {
		// Concise message for console, detailed for log
		consoleMsg := fmt.Sprintf("Failed to remove k0s image %s (see log for details)", imageName)
		logMsg := fmt.Sprintf("Failed to remove k0s image %s: %v", imageName, err)
		c.Logger.WarningDetailed(consoleMsg, logMsg)
		return nil // Don't fail if image doesn't exist
	}

	c.Logger.Success("Removed k0s image: %s", imageName)
	return nil
}
