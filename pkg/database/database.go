package database

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wapsol/m2deploy/pkg/config"
)

// Client handles database operations
type Client struct {
	Logger     *config.Logger
	DryRun     bool
	Namespace  string
	Kubeconfig string
	UseSudo    bool
}

// NewClient creates a new database client
func NewClient(logger *config.Logger, dryRun bool, namespace, kubeconfig string, useSudo bool) *Client {
	return &Client{
		Logger:     logger,
		DryRun:     dryRun,
		Namespace:  namespace,
		Kubeconfig: kubeconfig,
		UseSudo:    useSudo,
	}
}

// buildKubectlCmd builds a kubectl command with proper kubeconfig
func (c *Client) buildKubectlCmd(args ...string) *exec.Cmd {
	var allArgs []string
	if c.Kubeconfig != "" {
		allArgs = append([]string{"k0s", "kubectl", "--kubeconfig", c.Kubeconfig}, args...)
	} else {
		allArgs = append([]string{"k0s", "kubectl"}, args...)
	}

	if c.UseSudo {
		return exec.Command("sudo", allArgs...)
	}
	return exec.Command(allArgs[0], allArgs[1:]...)
}

// Backup backs up the SQLite database from a pod
func (c *Client) Backup(backupPath string, compress bool) error {
	c.Logger.Info("Backing up database")

	if c.DryRun {
		c.Logger.DryRun("Would backup database to %s (compress: %v)", backupPath, compress)
		return nil
	}

	// Find backend pod
	podName, err := c.getBackendPod()
	if err != nil {
		return fmt.Errorf("failed to find backend pod: %w", err)
	}

	// Create backup directory
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(backupPath, fmt.Sprintf("magnetiq-db-%s.db", timestamp))

	// Copy database from pod
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"cp",
		fmt.Sprintf("%s:/app/data/magnetiq.db", podName),
		backupFile,
	)

	c.Logger.Debug("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}

	// Compress if requested
	if compress {
		c.Logger.Info("Compressing backup")
		compressCmd := exec.Command("gzip", backupFile)
		if err := compressCmd.Run(); err != nil {
			c.Logger.Warning("Failed to compress backup: %v", err)
		} else {
			backupFile += ".gz"
		}
	}

	c.Logger.Success("Database backed up to: %s", backupFile)
	return nil
}

// Restore restores the SQLite database to a pod
func (c *Client) Restore(backupFile string) error {
	c.Logger.Info("Restoring database from: %s", backupFile)

	if c.DryRun {
		c.Logger.DryRun("Would restore database from %s", backupFile)
		return nil
	}

	// Check if backup file exists
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupFile)
	}

	// Decompress if needed
	tempFile := backupFile
	if strings.HasSuffix(backupFile, ".gz") {
		c.Logger.Info("Decompressing backup")
		tempFile = strings.TrimSuffix(backupFile, ".gz")
		cmd := exec.Command("gunzip", "-c", backupFile)
		outFile, err := os.Create(tempFile)
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		defer outFile.Close()
		cmd.Stdout = outFile
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to decompress backup: %w", err)
		}
		defer os.Remove(tempFile)
	}

	// Find backend pod
	podName, err := c.getBackendPod()
	if err != nil {
		return fmt.Errorf("failed to find backend pod: %w", err)
	}

	// Copy database to pod
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"cp",
		tempFile,
		fmt.Sprintf("%s:/app/data/magnetiq.db", podName),
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
	}

	c.Logger.Success("Database restored successfully")
	return nil
}

// Migrate runs database migrations
func (c *Client) Migrate() error {
	c.Logger.Info("Running database migrations")

	if c.DryRun {
		c.Logger.DryRun("Would run database migrations")
		return nil
	}

	// Find backend pod
	podName, err := c.getBackendPod()
	if err != nil {
		return fmt.Errorf("failed to find backend pod: %w", err)
	}

	// Execute migration command in pod
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"exec",
		podName,
		"--",
		"python",
		"-m",
		"alembic",
		"upgrade",
		"head",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	c.Logger.Debug("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	c.Logger.Success("Database migrations completed")
	return nil
}

// Status checks the migration status
func (c *Client) Status() error {
	c.Logger.Info("Checking database migration status")

	// Find backend pod
	podName, err := c.getBackendPod()
	if err != nil {
		return fmt.Errorf("failed to find backend pod: %w", err)
	}

	// Execute migration status command
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"exec",
		podName,
		"--",
		"python",
		"-m",
		"alembic",
		"current",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	return nil
}

// getBackendPod finds the first running backend pod
func (c *Client) getBackendPod() (string, error) {
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"get", "pods",
		"-l", "app=magnetiq-backend",
		"-o", "jsonpath={.items[0].metadata.name}",
	)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get backend pod: %w", err)
	}

	podName := strings.TrimSpace(string(output))
	if podName == "" {
		return "", fmt.Errorf("no backend pod found")
	}

	return podName, nil
}

// CleanOldBackups removes old backup files, keeping only the most recent N backups
func (c *Client) CleanOldBackups(backupPath string, retention int) error {
	c.Logger.Info("Cleaning old backups (keeping %d most recent)", retention)

	if c.DryRun {
		c.Logger.DryRun("Would clean old backups in %s (retention: %d)", backupPath, retention)
		return nil
	}

	// List backup files
	pattern := filepath.Join(backupPath, "magnetiq-db-*.db*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(matches) <= retention {
		c.Logger.Info("No old backups to clean (found %d, retention: %d)", len(matches), retention)
		return nil
	}

	// Get file info and sort by modification time (oldest first)
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var files []fileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			c.Logger.Warning("Failed to stat file %s: %v", match, err)
			continue
		}
		files = append(files, fileInfo{path: match, modTime: info.ModTime()})
	}

	// Sort by modification time (oldest first) using standard library
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	// Remove oldest files, keeping only retention count
	toRemove := len(files) - retention
	removedCount := 0
	for i := 0; i < toRemove; i++ {
		if err := os.Remove(files[i].path); err != nil {
			c.Logger.Warning("Failed to remove old backup %s: %v", files[i].path, err)
		} else {
			c.Logger.Info("Removed old backup: %s", files[i].path)
			removedCount++
		}
	}

	c.Logger.Success("Cleaned %d old backup(s)", removedCount)
	return nil
}
