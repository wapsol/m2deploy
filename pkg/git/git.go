package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/retry"
)

// Client handles git operations
type Client struct {
	Logger *config.Logger
	DryRun bool
}

// NewClient creates a new git client
func NewClient(logger *config.Logger, dryRun bool) *Client {
	return &Client{
		Logger: logger,
		DryRun: dryRun,
	}
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

// Clone clones a repository
func (c *Client) Clone(repoURL, workDir, branch string, depth int) error {
	c.Logger.Info("Cloning repository: %s", repoURL)

	if c.DryRun {
		c.Logger.DryRun("Would clone %s to %s (branch: %s, depth: %d)", repoURL, workDir, branch, depth)
		return nil
	}

	// Check if directory already exists
	if _, err := os.Stat(workDir); err == nil {
		c.Logger.Warning("Directory %s already exists, will pull latest changes", workDir)
		return c.Pull(workDir, branch)
	}

	args := []string{"clone"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, repoURL, workDir)

	c.Logger.Debug("Executing: git %s", strings.Join(args, " "))

	// Clone with retry for network resilience
	err := retry.WithRetryFunc(func() error {
		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}, c.Logger)

	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	c.Logger.Success("Repository cloned successfully")
	return nil
}

// Pull pulls latest changes from repository
func (c *Client) Pull(workDir, branch string) error {
	c.Logger.Info("Pulling latest changes from branch: %s", branch)

	if c.DryRun {
		c.Logger.DryRun("Would pull latest changes in %s (branch: %s)", workDir, branch)
		return nil
	}

	// Checkout branch
	if branch != "" {
		cmd := exec.Command("git", "checkout", branch)
		cmd.Dir = workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
		}
	}

	// Pull changes
	cmd := exec.Command("git", "pull")
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull changes: %w", err)
	}

	c.Logger.Success("Repository updated successfully")
	return nil
}

// Checkout checks out a specific commit or branch
func (c *Client) Checkout(workDir, ref string) error {
	c.Logger.Info("Checking out: %s", ref)

	if c.DryRun {
		c.Logger.DryRun("Would checkout %s in %s", ref, workDir)
		return nil
	}

	cmd := exec.Command("git", "checkout", ref)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", ref, err)
	}

	c.Logger.Success("Checked out %s", ref)
	return nil
}

// GetCurrentCommit returns the current commit SHA
func (c *Client) GetCurrentCommit(workDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCurrentBranch returns the current branch name
func (c *Client) GetCurrentBranch(workDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
