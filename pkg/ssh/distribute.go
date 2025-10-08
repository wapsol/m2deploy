package ssh

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/constants"
)

// Config holds SSH connection parameters
type Config struct {
	User          string
	KeyPath       string
	Port          int
	Timeout       int // seconds
	WorkerTempDir string
}

// Distributor handles distributing images to worker nodes
type Distributor struct {
	Logger        *config.Logger
	SSHConfig     *Config
	Parallel      int  // Max parallel distributions
	RetryCount    int  // Number of retries per worker
	MinWorkers    int  // Minimum successful workers required
	KeepTarballs  bool // Keep tarballs on workers for debugging
	WorkerIPs     []string // Manual worker IPs (overrides auto-discovery)
}

// WorkerNode represents a k8s worker node
type WorkerNode struct {
	Name      string
	IP        string
	Reachable bool
	LastError error
}

// DistributionResult tracks per-worker results
type DistributionResult struct {
	Worker    *WorkerNode
	Component string
	Success   bool
	Duration  time.Duration
	Error     error
}

// NewDistributor creates a new distributor instance
func NewDistributor(logger *config.Logger, sshConfig *Config) *Distributor {
	return &Distributor{
		Logger:       logger,
		SSHConfig:    sshConfig,
		Parallel:     3,
		RetryCount:   3,
		MinWorkers:   0, // 0 means all required
		KeepTarballs: false,
		WorkerIPs:    nil,
	}
}

// GetWorkerNodes returns worker nodes
// If WorkerIPs is set, uses manual list; otherwise discovers from k8s
func (d *Distributor) GetWorkerNodes(k8sClient interface{}) ([]*WorkerNode, error) {
	// If manual worker IPs provided, use those
	if len(d.WorkerIPs) > 0 {
		d.Logger.Debug("Using manual worker list: %v", d.WorkerIPs)
		workers := make([]*WorkerNode, len(d.WorkerIPs))
		for i, ip := range d.WorkerIPs {
			workers[i] = &WorkerNode{
				Name: fmt.Sprintf("worker-%d", i+1),
				IP:   ip,
			}
		}
		return workers, nil
	}

	// Otherwise discover from k8s API
	d.Logger.Debug("Discovering workers from k8s API")

	// Cast k8sClient to the actual type and call GetWorkerIPs
	// For now, we'll implement a simple interface check
	type WorkerIPGetter interface {
		GetWorkerIPs() ([]string, error)
	}

	getter, ok := k8sClient.(WorkerIPGetter)
	if !ok {
		return nil, fmt.Errorf("k8s client does not implement GetWorkerIPs method")
	}

	ips, err := getter.GetWorkerIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker IPs from k8s: %w", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no worker nodes found in cluster")
	}

	workers := make([]*WorkerNode, len(ips))
	for i, ip := range ips {
		workers[i] = &WorkerNode{
			Name: fmt.Sprintf("worker-%s", ip),
			IP:   ip,
		}
	}

	return workers, nil
}

// TestConnectivity tests SSH connectivity to all workers
func (d *Distributor) TestConnectivity(workers []*WorkerNode) error {
	d.Logger.Debug("Testing SSH connectivity to %d workers", len(workers))

	var failures []string

	for _, worker := range workers {
		// Quick connectivity test: just run 'hostname'
		_, err := d.sshExec(worker, "hostname")
		if err != nil {
			worker.Reachable = false
			worker.LastError = err
			failures = append(failures, fmt.Sprintf("%s (%s): %v", worker.Name, worker.IP, err))
		} else {
			worker.Reachable = true
			d.Logger.Debug("  ✓ %s (%s) reachable", worker.Name, worker.IP)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("SSH connectivity failed:\n  %s\n\nTo fix:\n  1. Copy SSH key: ssh-copy-id %s@<worker-ip>\n  2. Or specify different key: --ssh-key ~/.ssh/other_key",
			strings.Join(failures, "\n  "), d.SSHConfig.User)
	}

	return nil
}

// DistributeToWorker distributes a single tarball to a single worker
func (d *Distributor) DistributeToWorker(worker *WorkerNode, tarballPath, component, imageName string) (*DistributionResult, error) {
	startTime := time.Now()

	// Generate remote paths
	tarballName := filepath.Base(tarballPath)
	remoteTarball := filepath.Join(d.SSHConfig.WorkerTempDir, tarballName)

	// Get tarball size for logging
	tarballInfo, err := os.Stat(tarballPath)
	if err != nil {
		return &DistributionResult{
			Worker:    worker,
			Component: component,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     fmt.Errorf("cannot stat tarball: %w", err),
		}, err
	}
	tarballSize := tarballInfo.Size()

	d.Logger.Info("  [%s] Copying tarball (%.1f MB)...", worker.Name, float64(tarballSize)/1024/1024)

	// SCP tarball to worker
	if err := d.scpToWorker(worker, tarballPath, remoteTarball); err != nil {
		return &DistributionResult{
			Worker:    worker,
			Component: component,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     fmt.Errorf("SCP failed: %w", err),
		}, err
	}

	d.Logger.Info("  [%s] Importing into containerd...", worker.Name)

	// Import to containerd with base-name
	if err := d.importOnWorker(worker, remoteTarball, imageName); err != nil {
		return &DistributionResult{
			Worker:    worker,
			Component: component,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     fmt.Errorf("import failed: %w", err),
		}, err
	}

	d.Logger.Info("  [%s] Verifying import...", worker.Name)

	// Verify import
	if err := d.VerifyImportOnWorker(worker, imageName); err != nil {
		return &DistributionResult{
			Worker:    worker,
			Component: component,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     fmt.Errorf("verification failed: %w", err),
		}, err
	}

	// Cleanup (optional)
	if !d.KeepTarballs {
		d.Logger.Debug("  [%s] Cleaning up tarball...", worker.Name)
		d.CleanupOnWorker(worker, remoteTarball)
	}

	duration := time.Since(startTime)
	d.Logger.Success("  [%s] Completed in %s", worker.Name, duration)

	return &DistributionResult{
		Worker:    worker,
		Component: component,
		Success:   true,
		Duration:  duration,
		Error:     nil,
	}, nil
}

// DistributeToAllWorkers distributes tarball to all workers (with parallelism)
func (d *Distributor) DistributeToAllWorkers(workers []*WorkerNode, tarballPath, component, imageName string) ([]*DistributionResult, error) {
	d.Logger.Info("Distributing %s to %d workers (parallel: %d)", component, len(workers), d.Parallel)

	results := make([]*DistributionResult, len(workers))

	// Use semaphore to limit parallelism
	sem := make(chan struct{}, d.Parallel)
	var wg sync.WaitGroup

	for i, worker := range workers {
		wg.Add(1)

		go func(idx int, w *WorkerNode) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Attempt distribution with retries
			var lastErr error
			for attempt := 1; attempt <= d.RetryCount; attempt++ {
				if attempt > 1 {
					d.Logger.Info("  [%s] Retry %d/%d", w.Name, attempt, d.RetryCount)
					time.Sleep(time.Second * 2) // Brief delay between retries
				}

				result, err := d.DistributeToWorker(w, tarballPath, component, imageName)
				if err == nil {
					results[idx] = result
					return
				}

				lastErr = err
			}

			// All retries failed
			results[idx] = &DistributionResult{
				Worker:    w,
				Component: component,
				Success:   false,
				Error:     lastErr,
			}
			d.Logger.Warning("  [%s] Failed after %d attempts: %v", w.Name, d.RetryCount, lastErr)
		}(i, worker)
	}

	wg.Wait()

	// Count successes
	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
	}

	minRequired := d.MinWorkers
	if minRequired == 0 {
		minRequired = len(workers) // Default: all required
	}

	d.Logger.Info("")
	if successCount < minRequired {
		return results, fmt.Errorf("only %d/%d workers received image (minimum: %d)",
			successCount, len(workers), minRequired)
	}

	if successCount < len(workers) {
		d.Logger.Warning("Image distributed to %d/%d workers (some failures)", successCount, len(workers))
	} else {
		d.Logger.Success("Image distributed to all %d workers", successCount)
	}

	return results, nil
}

// VerifyImportOnWorker verifies image exists in worker's containerd
func (d *Distributor) VerifyImportOnWorker(worker *WorkerNode, imageName string) error {
	listCmd := fmt.Sprintf("sudo ctr -n %s images list", constants.ContainerdNamespace)
	output, err := d.sshExec(worker, listCmd)
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if !strings.Contains(output, imageName) {
		return fmt.Errorf("image %s not found in containerd", imageName)
	}

	return nil
}

// CleanupOnWorker removes tarball from worker's temp directory
func (d *Distributor) CleanupOnWorker(worker *WorkerNode, tarballPath string) error {
	cleanupCmd := fmt.Sprintf("rm -f %s", tarballPath)
	_, err := d.sshExec(worker, cleanupCmd)
	if err != nil {
		d.Logger.Warning("Failed to cleanup %s on %s: %v", tarballPath, worker.Name, err)
	}
	return err
}

// scpToWorker copies file to worker using SCP
func (d *Distributor) scpToWorker(worker *WorkerNode, localPath, remotePath string) error {
	// Get local file info
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot stat local file: %w", err)
	}
	localSize := localInfo.Size()

	// Open local file
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("cannot open local file: %w", err)
	}
	defer localFile.Close()

	// Establish SSH connection
	client, err := d.getSSHClient(worker)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer client.Close()

	// Open SCP session
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("cannot create SSH session: %w", err)
	}
	defer session.Close()

	// Set up pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("cannot create stdin pipe: %w", err)
	}

	// Start SCP command
	scpCmd := fmt.Sprintf("scp -t %s", remotePath)
	if err := session.Start(scpCmd); err != nil {
		return fmt.Errorf("cannot start SCP: %w", err)
	}

	// Send file via SCP protocol
	// Format: C0644 <size> <filename>\n
	filename := filepath.Base(localPath)
	fmt.Fprintf(stdin, "C0644 %d %s\n", localSize, filename)

	// Copy file content
	_, err = io.Copy(stdin, localFile)
	if err != nil {
		return fmt.Errorf("file copy failed: %w", err)
	}

	// Send termination byte
	fmt.Fprint(stdin, "\x00")
	stdin.Close()

	// Wait for completion
	if err := session.Wait(); err != nil {
		return fmt.Errorf("SCP session failed: %w", err)
	}

	// Verify remote file size
	checkCmd := fmt.Sprintf("stat -c%%s %s", remotePath)
	output, err := d.sshExec(worker, checkCmd)
	if err != nil {
		return fmt.Errorf("cannot verify remote file: %w", err)
	}

	remoteSize, _ := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if remoteSize != localSize {
		return fmt.Errorf("size mismatch: local=%d remote=%d", localSize, remoteSize)
	}

	return nil
}

// sshExec executes command on worker via SSH with timeout
func (d *Distributor) sshExec(worker *WorkerNode, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(d.SSHConfig.Timeout)*time.Second)
	defer cancel()

	return d.sshExecWithContext(ctx, worker, command)
}

// sshExecWithContext executes command with context
func (d *Distributor) sshExecWithContext(ctx context.Context, worker *WorkerNode, command string) (string, error) {
	client, err := d.getSSHClient(worker)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("cannot create session: %w", err)
	}
	defer session.Close()

	// Run command with context timeout
	done := make(chan error, 1)
	var output []byte

	go func() {
		output, err = session.CombinedOutput(command)
		done <- err
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		return "", fmt.Errorf("command timeout after %ds", d.SSHConfig.Timeout)
	case err := <-done:
		if err != nil {
			return string(output), fmt.Errorf("command failed: %w: %s", err, string(output))
		}
		return string(output), nil
	}
}

// importOnWorker imports tarball into containerd on worker
func (d *Distributor) importOnWorker(worker *WorkerNode, tarballPath, imageName string) error {
	// Extract base name for --base-name flag
	// Example: crepo.re-cloud.io/magnetiq/v2/backend:latest -> crepo.re-cloud.io/magnetiq/v2
	parts := strings.Split(imageName, "/")
	baseName := strings.Join(parts[:len(parts)-1], "/")

	importCmd := fmt.Sprintf(
		"sudo ctr -n %s images import --base-name %s %s",
		constants.ContainerdNamespace,
		baseName,
		tarballPath,
	)

	output, err := d.sshExec(worker, importCmd)
	if err != nil {
		// Parse common errors for better messages
		errStr := strings.ToLower(output)
		if strings.Contains(errStr, "no space left") {
			return fmt.Errorf("disk full on worker")
		}
		if strings.Contains(errStr, "permission denied") {
			return fmt.Errorf("sudo access required for ctr command")
		}
		if strings.Contains(errStr, "connection refused") {
			return fmt.Errorf("containerd not running on worker")
		}

		return fmt.Errorf("import command failed: %w", err)
	}

	return nil
}

// getSSHClient creates SSH client connection to worker
func (d *Distributor) getSSHClient(worker *WorkerNode) (*ssh.Client, error) {
	// Read private key
	key, err := os.ReadFile(d.SSHConfig.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read SSH key %s: %w", d.SSHConfig.KeyPath, err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("cannot parse SSH key: %w", err)
	}

	// Configure SSH client
	sshConfig := &ssh.ClientConfig{
		User: d.SSHConfig.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Improve with proper host key verification
		Timeout:         time.Duration(d.SSHConfig.Timeout) * time.Second,
	}

	// Connect to worker
	addr := fmt.Sprintf("%s:%d", worker.IP, d.SSHConfig.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("SSH dial failed: %w", err)
	}

	return client, nil
}
