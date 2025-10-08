# SSH-Based Image Distribution Plan for m2deploy

**Date:** October 2025
**Purpose:** Enable m2deploy to distribute Docker images to k0s worker nodes via SSH instead of using a container registry
**Status:** Planning / Ready for Implementation

---

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Solution Overview](#solution-overview)
3. [System Prerequisites](#system-prerequisites)
4. [Architecture & Code Changes](#architecture--code-changes)
5. [New Command-Line Flags](#new-command-line-flags)
6. [Image Naming Strategy](#image-naming-strategy)
7. [Workflow Logic](#workflow-logic)
8. [Error Handling](#error-handling)
9. [Implementation Checklist](#implementation-checklist)
10. [Testing Plan](#testing-plan)

---

## Problem Statement

### Current Situation
- m2deploy builds Docker images locally with tag `magnetiq/backend:latest`
- `deploy` command tries to import to k0s containerd at `/run/k0s/containerd.sock`
- **Problem:** Controller-only nodes don't have `/run/k0s/containerd.sock`
- **Problem:** Even if they did, worker nodes run the pods and need the images
- **Problem:** K8s manifests expect `crepo.re-cloud.io/magnetiq/v2/backend:latest` from Harbor registry

### Why Existing Approach Fails
```
Controller Node (rc3.k0s2.master1):
  ├─ Runs k0s controller process
  ├─ Has Docker daemon (/run/containerd/containerd.sock)
  ├─ Does NOT have k0s containerd socket (/run/k0s/containerd.sock)
  └─ Does NOT run workload pods

Worker Nodes (5 nodes):
  ├─ Run kubelet + containerd
  ├─ Each has own containerd runtime
  ├─ Pull images from their local containerd store (namespace: k8s.io)
  └─ Currently cannot access locally-built images
```

### Why Not Use Registry?
User preference to avoid external dependencies during development/testing phase. Registry-based distribution will be added later for production workflows.

---

## Solution Overview

**Distribute images via SSH using this flow:**

```
1. Build images on controller with Docker
2. Save images to tarballs
3. SCP tarballs to each worker node
4. SSH to worker and import into containerd (k8s.io namespace)
5. Verify import success on all workers
6. Deploy k8s manifests (images now available locally)
```

### Key Benefits
- No external registry dependency
- Full control over distribution process
- Works in air-gapped environments
- Simple troubleshooting (direct SSH access)

### Trade-offs
- Requires SSH key setup (one-time)
- Slower than registry (sequential network copies)
- More complex error handling
- Doesn't scale to 100+ nodes (but fine for 5 workers)

---

## System Prerequisites

### 1. SSH Key Setup (One-Time)

**On Controller Node:**
```bash
# Generate SSH key if not exists (run as ubuntu user)
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""

# Copy public key to all 5 worker nodes
ssh-copy-id ubuntu@10.112.3.182  # rc3-k0s-2-worker-1
ssh-copy-id ubuntu@10.112.0.95   # rc3-k0s-2-worker-2
ssh-copy-id ubuntu@10.112.2.96   # rc3-k0s-2-worker-3
ssh-copy-id ubuntu@10.112.3.77   # rc3-k0s-2-worker-4
ssh-copy-id ubuntu@10.112.3.197  # rc3-k0s-2-worker0-ops-1

# Verify connectivity (should complete without password)
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo -n "Testing $ip: "
  ssh -o ConnectTimeout=3 -o StrictHostKeyChecking=no ubuntu@$ip "hostname" || echo "FAILED"
done
```

**Expected Output:**
```
Testing 10.112.3.182: rc3-k0s-2-worker-1
Testing 10.112.0.95: rc3-k0s-2-worker-2
Testing 10.112.2.96: rc3-k0s-2-worker-3
Testing 10.112.3.77: rc3-k0s-2-worker-4
Testing 10.112.3.197: rc3-k0s-2-worker0-ops-1
```

### 2. Worker Node Permissions

**On Each Worker Node:**
```bash
# Verify ctr access (should work without password if sudo is configured)
sudo ctr --version

# Check k8s.io namespace exists
sudo ctr -n k8s.io namespaces list

# Optional: Add passwordless sudo for ctr (more secure than full sudo)
echo "ubuntu ALL=(ALL) NOPASSWD: /usr/bin/ctr" | sudo tee /etc/sudoers.d/ctr-access
sudo chmod 0440 /etc/sudoers.d/ctr-access
```

### 3. Disk Space Requirements

**Controller Node:**
- `/tmp`: 800MB free (temporary storage for 2 tarballs)
- Cleaned up immediately after distribution

**Worker Nodes:**
- `/tmp`: 400MB free per node (transient during import)
- Cleaned up after import completes
- Total imported images: ~400MB per node in containerd store

**Check Disk Space:**
```bash
# Controller
df -h /tmp

# Each worker
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo "$ip: $(ssh ubuntu@$ip 'df -h /tmp | tail -1')"
done
```

### 4. Network Connectivity

**Bandwidth:**
- Minimum: 10 Mbps between controller and workers
- Recommended: 100 Mbps for faster distribution
- Total transfer per deployment: ~400MB × 5 nodes = 2GB

**Firewall:**
- Port 22 (SSH) must be open from controller to all workers

**Test Network Speed:**
```bash
# Use iperf3 if available
ssh ubuntu@10.112.3.182 "iperf3 -s -1" &
iperf3 -c 10.112.3.182 -t 5
```

---

## Architecture & Code Changes

### New Package: `pkg/ssh/distribute.go`

**Purpose:** Handle all SSH-based image distribution logic

```go
package ssh

import (
    "github.com/wapsol/m2deploy/pkg/config"
    "github.com/wapsol/m2deploy/pkg/k8s"
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
    K8sClient     *k8s.Client
    Parallel      int  // Max parallel distributions
    RetryCount    int  // Number of retries per worker
    MinWorkers    int  // Minimum successful workers required
    KeepTarballs  bool // Keep tarballs on workers for debugging
}

// WorkerNode represents a k8s worker node
type WorkerNode struct {
    Name       string
    IP         string
    Reachable  bool
    LastError  error
}

// DistributionResult tracks per-worker results
type DistributionResult struct {
    Worker       *WorkerNode
    Component    string
    Success      bool
    Duration     time.Duration
    Error        error
    BytesCopied  int64
}

// Methods to implement:

// NewDistributor creates a new distributor instance
func NewDistributor(logger *config.Logger, sshConfig *Config, k8sClient *k8s.Client) *Distributor

// GetWorkerNodes queries Kubernetes API for worker node IPs
func (d *Distributor) GetWorkerNodes() ([]*WorkerNode, error)

// TestConnectivity tests SSH connectivity to all workers
func (d *Distributor) TestConnectivity(workers []*WorkerNode) error

// DistributeToWorker distributes a single tarball to a single worker
func (d *Distributor) DistributeToWorker(worker *WorkerNode, tarballPath, component string) (*DistributionResult, error)

// DistributeToAllWorkers distributes tarball to all workers (with parallelism)
func (d *Distributor) DistributeToAllWorkers(tarballPath, component, imageName string) ([]*DistributionResult, error)

// VerifyImportOnWorker verifies image exists in worker's containerd
func (d *Distributor) VerifyImportOnWorker(worker *WorkerNode, imageName string) error

// CleanupOnWorker removes tarball from worker's /tmp
func (d *Distributor) CleanupOnWorker(worker *WorkerNode, tarballPath string) error

// Private helper methods:

// scpToWorker copies file to worker using SCP
func (d *Distributor) scpToWorker(worker *WorkerNode, localPath, remotePath string) error

// sshExec executes command on worker via SSH
func (d *Distributor) sshExec(worker *WorkerNode, command string) (string, error)

// importOnWorker imports tarball into containerd on worker
func (d *Distributor) importOnWorker(worker *WorkerNode, tarballPath, imageName string) error
```

**Key Implementation Details:**

1. **SSH Library:** Use `golang.org/x/crypto/ssh` for SSH connections
2. **SCP Implementation:** Use `github.com/bramvdbogaerde/go-scp` or implement custom
3. **Parallel Distribution:** Use goroutines with `sync.WaitGroup` and semaphore pattern
4. **Progress Tracking:** Report progress via logger: `[2/5] Distributing to worker-2 (10.112.0.95)...`

### Updated: `pkg/docker/docker.go`

Add new method:

```go
// SaveAndDistribute saves image to tarball and distributes to all workers
// Returns error if distribution fails to minimum required workers
func (c *Client) SaveAndDistribute(component, imageName string, distributor *ssh.Distributor) error {
    // 1. Generate tarball path
    tarballPath := fmt.Sprintf(constants.TarballPathTemplate, component)

    // 2. Save image to tarball (existing logic)
    c.Logger.Info("Saving %s image to tarball...", component)
    if err := c.SaveImage(component, tarballPath); err != nil {
        return fmt.Errorf("failed to save %s image: %w", component, err)
    }

    // 3. Distribute to all workers
    c.Logger.Info("Distributing %s to worker nodes...", component)
    results, err := distributor.DistributeToAllWorkers(tarballPath, component, imageName)
    if err != nil {
        return fmt.Errorf("distribution failed: %w", err)
    }

    // 4. Check results
    successCount := 0
    for _, result := range results {
        if result.Success {
            successCount++
        } else {
            c.Logger.Warning("Failed to distribute to %s: %v", result.Worker.Name, result.Error)
        }
    }

    if successCount < distributor.MinWorkers {
        return fmt.Errorf("only %d/%d workers received image (minimum: %d)",
            successCount, len(results), distributor.MinWorkers)
    }

    c.Logger.Success("Distributed %s to %d/%d workers", component, successCount, len(results))

    // 5. Cleanup local tarball
    if !c.DryRun {
        os.Remove(tarballPath)
        c.Logger.Debug("Removed local tarball: %s", tarballPath)
    }

    return nil
}
```

### Updated: `cmd/deploy.go`

Replace image import section (lines 99-150) with:

```go
// Import Docker images to worker nodes (unless skipped)
if !deploySkipImport {
    logger.Info("Step 1: Distributing images to worker nodes via SSH")
    logger.Info("")

    // Create SSH configuration
    sshConfig := &ssh.Config{
        User:          viper.GetString("ssh-user"),
        KeyPath:       expandPath(viper.GetString("ssh-key")),
        Port:          viper.GetInt("ssh-port"),
        Timeout:       viper.GetInt("ssh-timeout"),
        WorkerTempDir: viper.GetString("worker-temp-dir"),
    }

    // Create distributor
    distributor := ssh.NewDistributor(logger, sshConfig, k8sClient)
    distributor.Parallel = viper.GetInt("parallel-workers")
    distributor.RetryCount = viper.GetInt("retry-count")
    distributor.MinWorkers = viper.GetInt("min-workers")
    distributor.KeepTarballs = viper.GetBool("skip-worker-cleanup")

    // Get worker nodes from k8s API
    workers, err := distributor.GetWorkerNodes()
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
        return fmt.Errorf("SSH connectivity test failed: %w\nEnsure SSH keys are set up: ssh-copy-id ubuntu@<worker-ip>", err)
    }
    logger.Success("All workers reachable via SSH")
    logger.Info("")

    // Distribute each component
    cfg := getConfig()
    components := []string{constants.ComponentBackend, constants.ComponentFrontend}

    for _, component := range components {
        imageName := cfg.GetLocalImageName(component)

        // Use SaveAndDistribute which handles save + distribute + cleanup
        if err := dockerClient.SaveAndDistribute(component, imageName, distributor); err != nil {
            return fmt.Errorf("failed to distribute %s: %w", component, err)
        }
    }

    // Verify imports on all workers
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

        if successCount < distributor.MinWorkers {
            return fmt.Errorf("image %s not available on enough workers (%d/%d, minimum: %d)",
                imageName, successCount, len(workers), distributor.MinWorkers)
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
```

### Updated: `cmd/root.go`

Add new persistent flags in `init()` function:

```go
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

// Bind to viper
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
```

Add variable declarations at top:

```go
var (
    // ... existing vars ...

    // SSH configuration
    sshUser          string
    sshKey           string
    sshPort          int
    sshTimeout       int

    // Distribution behavior
    workerTempDir     string
    parallelWorkers   int
    retryCount        int
    minWorkers        int
    skipWorkerCleanup bool
    workers           string
)
```

### New Command: `cmd/distribute.go` (Optional)

Standalone command for testing/troubleshooting:

```go
var distributeCmd = &cobra.Command{
    Use:   "distribute",
    Short: "Distribute Docker images to worker nodes",
    Long: `Distribute locally-built Docker images to k8s worker nodes via SSH.

This command is useful for:
- Testing SSH connectivity and distribution process
- Re-distributing images after failed deployments
- Selective distribution to specific workers`,
    Example: `  # Distribute all components to all workers
  m2deploy distribute --workspace-path /tmp/wapsol/magnetiq2

  # Distribute only backend to specific workers
  m2deploy distribute --component backend --workers 10.112.3.182,10.112.0.95`,
    RunE: runDistribute,
}

func init() {
    rootCmd.AddCommand(distributeCmd)

    distributeCmd.Flags().StringVar(&distributeComponent, "component", "", "Component to distribute (backend/frontend, empty for all)")
}

func runDistribute(cmd *cobra.Command, args []string) error {
    // Similar logic to deploy, but only handles distribution
    // No k8s deployment step
}
```

### Updated: `pkg/constants/constants.go`

Add new constants:

```go
// SSH distribution
const (
    DefaultSSHUser         = "ubuntu"
    DefaultSSHPort         = 22
    DefaultSSHTimeout      = 30
    DefaultWorkerTempDir   = "/tmp"
    DefaultParallelWorkers = 3
    DefaultRetryCount      = 3
)

// Containerd namespace for k8s
const ContainerdNamespace = "k8s.io"
```

---

## New Command-Line Flags

### SSH Configuration Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--ssh-user` | string | `ubuntu` | SSH username for worker nodes |
| `--ssh-key` | string | `~/.ssh/id_rsa` | Path to SSH private key |
| `--ssh-port` | int | `22` | SSH port on worker nodes |
| `--ssh-timeout` | int | `30` | SSH connection timeout (seconds) |

**Usage Example:**
```bash
./m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 \
  --ssh-user ubuntu \
  --ssh-key ~/.ssh/id_ed25519 \
  --ssh-timeout 60
```

### Distribution Behavior Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--worker-temp-dir` | string | `/tmp` | Temporary directory on workers for tarballs |
| `--parallel-workers` | int | `3` | Max workers to distribute to in parallel |
| `--retry-count` | int | `3` | Number of retries per worker on failure |
| `--min-workers` | int | `0` | Minimum successful workers (0 = all required) |
| `--skip-worker-cleanup` | bool | `false` | Keep tarballs on workers for debugging |
| `--workers` | string | `""` | Comma-separated IPs (overrides auto-discovery) |

**Usage Examples:**

```bash
# Faster distribution (5 workers in parallel, accept failures)
./m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 \
  --parallel-workers 5 \
  --min-workers 3

# Debugging (keep tarballs, manual worker list)
./m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 \
  --skip-worker-cleanup \
  --workers "10.112.3.182,10.112.0.95"

# Conservative (1 worker at a time, no failures allowed)
./m2deploy deploy --workspace-path /tmp/wapsol/magnetiq2 \
  --parallel-workers 1 \
  --retry-count 5
```

### Existing Flags (Still Relevant)

| Flag | Description |
|------|-------------|
| `--skip-import` | Skip image distribution entirely (assumes images already on workers) |
| `--dry-run` | Show what would be done without executing |
| `--verbose` | Show detailed SSH/SCP output |
| `--use-sudo` | Use sudo for Docker commands on controller |

---

## Image Naming Strategy

### The Challenge

**Current Situation:**
- Local build produces: `magnetiq/backend:latest`
- K8s manifest expects: `crepo.re-cloud.io/magnetiq/v2/backend:latest`
- Containerd needs exact match for imagePullPolicy to work

### Solution: Use Full Registry Name Everywhere

**1. Update Default `--image-prefix`:**

In `cmd/root.go`:
```go
rootCmd.PersistentFlags().StringVar(&imagePrefix, "image-prefix",
    "crepo.re-cloud.io/magnetiq/v2",
    "Container image prefix")
```

**2. Build with Full Name:**

```bash
./m2deploy build --workspace-path /tmp/wapsol/magnetiq2 \
  --image-prefix crepo.re-cloud.io/magnetiq/v2
```

This produces: `crepo.re-cloud.io/magnetiq/v2/backend:latest`

**3. Import with `--base-name` Flag:**

On each worker during import:
```bash
sudo ctr -n k8s.io images import \
  --base-name crepo.re-cloud.io/magnetiq/v2 \
  /tmp/magnetiq-backend.tar
```

This ensures containerd registers the image with the exact name k8s expects.

**4. Update K8s Manifests:**

Change `imagePullPolicy` to prevent registry pulls:

```yaml
# k8s/backend/deployment.yaml
spec:
  containers:
  - name: backend
    image: crepo.re-cloud.io/magnetiq/v2/backend:latest
    imagePullPolicy: Never  # Changed from Always
```

Options for `imagePullPolicy`:
- `Never`: Only use local image, never pull (recommended for SSH distribution)
- `IfNotPresent`: Use local if available, pull if missing (works but slower first time)
- `Always`: Always pull from registry (incompatible with SSH distribution)

**5. Verification:**

After import, verify on worker:
```bash
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images list" | grep magnetiq
```

Expected output:
```
crepo.re-cloud.io/magnetiq/v2/backend:latest
crepo.re-cloud.io/magnetiq/v2/frontend:latest
```

### Alternative: Image Re-tagging

If you prefer to keep local builds as `magnetiq/*`, add re-tag step:

```bash
# After building locally
docker tag magnetiq/backend:latest crepo.re-cloud.io/magnetiq/v2/backend:latest
docker tag magnetiq/frontend:latest crepo.re-cloud.io/magnetiq/v2/frontend:latest
```

This can be automated in m2deploy's `SaveImage()` method.

---

## Workflow Logic

### High-Level Flow

```
┌─────────────────────────────────────────────────┐
│ 1. Prerequisites Check                          │
│    - k0s running, kubectl access, disk space    │
│    - SSH key exists                             │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│ 2. Query Worker Nodes from k8s API              │
│    - Get node names and internal IPs            │
│    - Filter: role != control-plane              │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│ 3. Test SSH Connectivity                        │
│    - Parallel connectivity test to all workers  │
│    - Fail fast if any worker unreachable        │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│ 4. For Each Component (backend, frontend):      │
│    ┌────────────────────────────────────────┐   │
│    │ a. Save Docker image to tarball       │   │
│    │    /tmp/magnetiq-backend.tar          │   │
│    └────────────────┬───────────────────────┘   │
│                     ▼                            │
│    ┌────────────────────────────────────────┐   │
│    │ b. Distribute to Workers (Parallel)    │   │
│    │    - Split into batches of 3           │   │
│    │    - For each worker:                  │   │
│    │      * SCP tarball to worker:/tmp/     │   │
│    │      * SSH: ctr images import          │   │
│    │      * Verify import success           │   │
│    │      * Cleanup tarball (optional)      │   │
│    └────────────────┬───────────────────────┘   │
│                     ▼                            │
│    ┌────────────────────────────────────────┐   │
│    │ c. Check Results                       │   │
│    │    - Count successes                   │   │
│    │    - Abort if < min-workers            │   │
│    └────────────────┬───────────────────────┘   │
│                     ▼                            │
│    ┌────────────────────────────────────────┐   │
│    │ d. Cleanup Local Tarball               │   │
│    └────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│ 5. Verify All Workers Have All Images           │
│    - Query containerd on each worker            │
│    - Ensure exact image names match manifests   │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│ 6. Deploy K8s Manifests                         │
│    - Apply in order: namespace, configmap, ...  │
│    - Pods will use local images (no pull)       │
└─────────────────────────────────────────────────┘
```

### Detailed Per-Worker Logic

```go
func (d *Distributor) DistributeToWorker(worker *WorkerNode, tarballPath, component string) error {
    startTime := time.Now()

    // 1. Generate remote paths
    tarballName := filepath.Base(tarballPath)
    remoteTarball := filepath.Join(d.SSHConfig.WorkerTempDir, tarballName)

    // 2. SCP tarball to worker
    d.Logger.Info("  [%s] Copying tarball (%s)...", worker.Name, formatBytes(tarballSize))
    if err := d.scpToWorker(worker, tarballPath, remoteTarball); err != nil {
        return fmt.Errorf("SCP failed: %w", err)
    }

    // 3. Import to containerd with base-name
    d.Logger.Info("  [%s] Importing into containerd...", worker.Name)
    importCmd := fmt.Sprintf(
        "sudo ctr -n %s images import --base-name %s %s",
        constants.ContainerdNamespace,
        d.getImageBaseName(component),
        remoteTarball,
    )
    if _, err := d.sshExec(worker, importCmd); err != nil {
        return fmt.Errorf("import failed: %w", err)
    }

    // 4. Verify import
    d.Logger.Info("  [%s] Verifying import...", worker.Name)
    if err := d.VerifyImportOnWorker(worker, imageName); err != nil {
        return fmt.Errorf("verification failed: %w", err)
    }

    // 5. Cleanup (optional)
    if !d.KeepTarballs {
        d.Logger.Debug("  [%s] Cleaning up tarball...", worker.Name)
        d.CleanupOnWorker(worker, remoteTarball)
    }

    duration := time.Since(startTime)
    d.Logger.Success("  [%s] Completed in %s", worker.Name, duration)

    return nil
}
```

### Parallel Distribution Pattern

```go
func (d *Distributor) DistributeToAllWorkers(tarballPath, component, imageName string) ([]*DistributionResult, error) {
    workers, err := d.GetWorkerNodes()
    if err != nil {
        return nil, err
    }

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
                }

                startTime := time.Now()
                err := d.DistributeToWorker(w, tarballPath, component)
                duration := time.Since(startTime)

                if err == nil {
                    results[idx] = &DistributionResult{
                        Worker:    w,
                        Component: component,
                        Success:   true,
                        Duration:  duration,
                    }
                    return
                }

                lastErr = err
                time.Sleep(time.Second * 2) // Brief delay between retries
            }

            // All retries failed
            results[idx] = &DistributionResult{
                Worker:    w,
                Component: component,
                Success:   false,
                Error:     lastErr,
            }
        }(i, worker)
    }

    wg.Wait()
    return results, nil
}
```

---

## Error Handling

### 1. SSH Connection Failures

**Scenarios:**
- Worker node offline
- SSH key not authorized
- Network timeout
- Host key verification failed

**Handling:**
```go
// Test connectivity before attempting distribution
func (d *Distributor) TestConnectivity(workers []*WorkerNode) error {
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
        }
    }

    if len(failures) > 0 {
        return fmt.Errorf("SSH connectivity failed:\n  %s", strings.Join(failures, "\n  "))
    }

    return nil
}
```

**User Guidance:**
```
Error: SSH connectivity test failed:
  rc3-k0s-2-worker-1 (10.112.3.182): Permission denied (publickey)

To fix:
  1. Copy SSH key to worker: ssh-copy-id ubuntu@10.112.3.182
  2. Or specify different key: --ssh-key ~/.ssh/other_key
  3. Or specify different user: --ssh-user root
```

### 2. Partial Distribution Failures

**Scenario:** 3/5 workers succeed, 2/5 fail

**Strategy:** Use `--min-workers` flag to allow graceful degradation

```go
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

if successCount < minRequired {
    return fmt.Errorf("only %d/%d workers received image (minimum: %d)",
        successCount, len(workers), minRequired)
}

if successCount < len(workers) {
    d.Logger.Warning("Image distributed to %d/%d workers (some failures)",
        successCount, len(workers))
}
```

**User Options:**
```bash
# Require all workers
./m2deploy deploy --min-workers 0  # or omit (default)

# Allow 1 failure (4/5 workers okay)
./m2deploy deploy --min-workers 4

# Just need majority (3/5 workers okay)
./m2deploy deploy --min-workers 3
```

### 3. SCP Transfer Failures

**Scenarios:**
- Disk full on worker
- Network interruption mid-transfer
- Tarball corruption

**Handling:**
```go
func (d *Distributor) scpToWorker(worker *WorkerNode, localPath, remotePath string) error {
    // Get local file size for progress tracking
    localInfo, err := os.Stat(localPath)
    if err != nil {
        return fmt.Errorf("cannot stat local file: %w", err)
    }
    localSize := localInfo.Size()

    // SCP with retries
    for attempt := 1; attempt <= 3; attempt++ {
        err := d.doSCP(worker, localPath, remotePath)
        if err == nil {
            // Verify remote file size matches
            checkCmd := fmt.Sprintf("stat -c%%s %s", remotePath)
            output, err := d.sshExec(worker, checkCmd)
            if err != nil {
                return fmt.Errorf("cannot verify remote file: %w", err)
            }

            remoteSize, _ := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
            if remoteSize != localSize {
                return fmt.Errorf("size mismatch: local=%d remote=%d", localSize, remoteSize)
            }

            return nil // Success
        }

        d.Logger.Warning("  [%s] SCP attempt %d/3 failed: %v", worker.Name, attempt, err)
        time.Sleep(time.Second * 2)
    }

    return fmt.Errorf("SCP failed after 3 attempts")
}
```

### 4. Containerd Import Failures

**Scenarios:**
- Tarball corrupted
- Disk full on worker
- containerd not running
- Permission denied

**Handling:**
```go
func (d *Distributor) importOnWorker(worker *WorkerNode, tarballPath, imageName string) error {
    // Extract base name for --base-name flag
    parts := strings.Split(imageName, "/")
    baseName := strings.Join(parts[:len(parts)-1], "/")

    importCmd := fmt.Sprintf(
        "sudo ctr -n %s images import --base-name %s %s 2>&1",
        constants.ContainerdNamespace,
        baseName,
        tarballPath,
    )

    output, err := d.sshExec(worker, importCmd)
    if err != nil {
        // Parse common errors for better messages
        if strings.Contains(output, "no space left") {
            return fmt.Errorf("disk full on worker")
        }
        if strings.Contains(output, "permission denied") {
            return fmt.Errorf("sudo access required for ctr command")
        }
        if strings.Contains(output, "connection refused") {
            return fmt.Errorf("containerd not running on worker")
        }

        return fmt.Errorf("import failed: %s", output)
    }

    return nil
}
```

### 5. Progress and Timeout Handling

**Long Operations Need Feedback:**

```go
type ProgressTracker struct {
    TotalBytes    int64
    CopiedBytes   int64
    StartTime     time.Time
    LastReported  time.Time
    ReportEvery   time.Duration
}

func (p *ProgressTracker) Update(copied int64) {
    p.CopiedBytes = copied
    now := time.Now()

    if now.Sub(p.LastReported) >= p.ReportEvery {
        percent := float64(p.CopiedBytes) / float64(p.TotalBytes) * 100
        elapsed := now.Sub(p.StartTime)
        rate := float64(p.CopiedBytes) / elapsed.Seconds()

        fmt.Printf("\r  Progress: %.1f%% (%s / %s) @ %s/s",
            percent,
            formatBytes(p.CopiedBytes),
            formatBytes(p.TotalBytes),
            formatBytes(int64(rate)),
        )

        p.LastReported = now
    }
}
```

**Context with Timeout:**

```go
func (d *Distributor) sshExec(worker *WorkerNode, command string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(),
        time.Duration(d.SSHConfig.Timeout) * time.Second)
    defer cancel()

    // Run command with context
    // If timeout exceeded, context will be canceled
    return d.sshExecWithContext(ctx, worker, command)
}
```

---

## Implementation Checklist

### Phase 1: Core SSH Functionality (Week 1)

- [ ] **Create `pkg/ssh/` package**
  - [ ] Define structs: `Config`, `Distributor`, `WorkerNode`, `DistributionResult`
  - [ ] Implement SSH connection establishment
  - [ ] Implement SSH command execution with timeout
  - [ ] Test manual SSH operations

- [ ] **Implement SCP functionality**
  - [ ] Research Go SCP libraries (`go-scp` vs custom)
  - [ ] Implement file copy with progress tracking
  - [ ] Implement file size verification
  - [ ] Handle network interruptions gracefully

- [ ] **Test SSH/SCP in isolation**
  - [ ] Create test script: copy 100MB file to all workers
  - [ ] Measure transfer speeds
  - [ ] Test with intentional failures (offline worker, bad key)

### Phase 2: Worker Discovery (Week 1)

- [ ] **Implement worker node discovery**
  - [ ] Query k8s API for nodes
  - [ ] Filter control-plane nodes
  - [ ] Extract internal IPs
  - [ ] Handle nodes without IPs

- [ ] **Implement connectivity testing**
  - [ ] Parallel SSH connectivity test
  - [ ] Clear error messages for failures
  - [ ] Option to override with `--workers` flag

### Phase 3: Distribution Logic (Week 2)

- [ ] **Implement single-worker distribution**
  - [ ] SCP tarball to worker
  - [ ] SSH: run `ctr images import`
  - [ ] Verify import success
  - [ ] Cleanup tarball
  - [ ] Comprehensive error handling

- [ ] **Implement parallel distribution**
  - [ ] Semaphore pattern for parallelism control
  - [ ] WaitGroup for synchronization
  - [ ] Collect results from all goroutines
  - [ ] Aggregate success/failure counts

- [ ] **Implement retry logic**
  - [ ] Configurable retry count
  - [ ] Exponential backoff
  - [ ] Per-worker retry tracking

### Phase 4: Integration (Week 2)

- [ ] **Update `cmd/deploy.go`**
  - [ ] Replace k0s import with SSH distribution
  - [ ] Add SSH config initialization
  - [ ] Add distributor initialization
  - [ ] Wire up all flags

- [ ] **Update `cmd/root.go`**
  - [ ] Add all new flags (10+ flags)
  - [ ] Bind to viper
  - [ ] Set sensible defaults
  - [ ] Update help text

- [ ] **Update `pkg/docker/docker.go`**
  - [ ] Add `SaveAndDistribute()` method
  - [ ] Integrate with distributor
  - [ ] Handle cleanup

### Phase 5: Image Naming (Week 2)

- [ ] **Update image prefix default**
  - [ ] Change from `magnetiq` to `crepo.re-cloud.io/magnetiq/v2`
  - [ ] Update build.sh integration
  - [ ] Document flag usage

- [ ] **Update k8s manifests**
  - [ ] Change `imagePullPolicy: Always` to `Never`
  - [ ] Verify manifest still works with registry (for production)
  - [ ] Add comments explaining policy

### Phase 6: Testing (Week 3)

- [ ] **Unit tests**
  - [ ] SSH connection mocking
  - [ ] Worker discovery logic
  - [ ] Error handling paths

- [ ] **Integration tests**
  - [ ] End-to-end: build → distribute → deploy
  - [ ] Test with 1 worker (simple case)
  - [ ] Test with all 5 workers (full cluster)
  - [ ] Test with intentional failures

- [ ] **Edge case testing**
  - [ ] Worker goes offline mid-distribution
  - [ ] Disk full on worker
  - [ ] SSH key permission issues
  - [ ] Network timeout scenarios
  - [ ] Partial success with `--min-workers`

### Phase 7: Documentation (Week 3)

- [ ] **Update main README**
  - [ ] Add SSH distribution overview
  - [ ] Document prerequisite setup
  - [ ] Add troubleshooting section

- [ ] **Update CLAUDE.md**
  - [ ] Document new workflow
  - [ ] Add flag reference
  - [ ] Update deployment examples

- [ ] **Create troubleshooting guide**
  - [ ] Common SSH errors and fixes
  - [ ] Worker connectivity debugging
  - [ ] Image verification steps

### Phase 8: Optional Enhancements (Future)

- [ ] **Add `distribute` command**
  - [ ] Standalone distribution without deploy
  - [ ] Useful for testing and re-distribution

- [ ] **Add distribution metrics**
  - [ ] Track transfer speeds per worker
  - [ ] Report slowest/fastest workers
  - [ ] Estimate time remaining

- [ ] **Add resume capability**
  - [ ] Save distribution state
  - [ ] Resume from last successful worker
  - [ ] Useful for large clusters

---

## Testing Plan

### Prerequisites Test

**Verify system setup before coding:**

```bash
# 1. SSH key exists
ls -la ~/.ssh/id_rsa*

# 2. SSH access to all workers
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo -n "Testing $ip: "
  ssh -o ConnectTimeout=3 ubuntu@$ip "hostname" && echo "OK" || echo "FAILED"
done

# 3. Sudo access on workers for ctr
ssh ubuntu@10.112.3.182 "sudo ctr --version"

# 4. Disk space on workers
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo "$ip: $(ssh ubuntu@$ip 'df -h /tmp | tail -1 | awk "{print \$4}"')"
done

# 5. k8s API access
sudo k0s kubectl get nodes -o wide
```

### Unit Tests

Create `pkg/ssh/distribute_test.go`:

```go
func TestSSHConnection(t *testing.T) {
    // Test establishing SSH connection
    // Mock SSH client
}

func TestSCPTransfer(t *testing.T) {
    // Test SCP file transfer
    // Use test fixtures (small files)
}

func TestWorkerDiscovery(t *testing.T) {
    // Test k8s API query for workers
    // Mock k8s client
}

func TestParallelDistribution(t *testing.T) {
    // Test parallel goroutine logic
    // Verify semaphore limits parallelism
}

func TestRetryLogic(t *testing.T) {
    // Test retry on failure
    // Mock failing then succeeding operation
}
```

### Integration Tests

**Test 1: Single Worker Distribution**

```bash
# Build image
./m2deploy build --workspace-path /tmp/wapsol/magnetiq2 \
  --image-prefix crepo.re-cloud.io/magnetiq/v2 \
  --component backend

# Distribute to single worker
./m2deploy distribute \
  --workspace-path /tmp/wapsol/magnetiq2 \
  --workers 10.112.3.182 \
  --component backend

# Verify on worker
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images list" | grep magnetiq
```

**Test 2: Full Cluster Distribution**

```bash
# Build both components
./m2deploy build --workspace-path /tmp/wapsol/magnetiq2 --component both

# Distribute to all workers
./m2deploy deploy \
  --workspace-path /tmp/wapsol/magnetiq2 \
  --skip-deploy \
  --parallel-workers 3

# Verify on all workers
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo "=== $ip ==="
  ssh ubuntu@$ip "sudo ctr -n k8s.io images list" | grep magnetiq
done
```

**Test 3: Failure Handling**

```bash
# Simulate worker offline (block SSH with firewall)
sudo iptables -A OUTPUT -d 10.112.3.182 -j DROP

# Attempt distribution with min-workers
./m2deploy deploy \
  --workspace-path /tmp/wapsol/magnetiq2 \
  --min-workers 4 \
  --verbose

# Should succeed with 4/5 workers

# Restore connectivity
sudo iptables -D OUTPUT -d 10.112.3.182 -j DROP
```

**Test 4: Full Deploy Workflow**

```bash
# Clean slate: remove images from all workers
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  ssh ubuntu@$ip "sudo ctr -n k8s.io images rm crepo.re-cloud.io/magnetiq/v2/backend:latest || true"
  ssh ubuntu@$ip "sudo ctr -n k8s.io images rm crepo.re-cloud.io/magnetiq/v2/frontend:latest || true"
done

# Run full deploy
./m2deploy all \
  --repo-url https://github.com/wapsol/magnetiq2 \
  --image-prefix crepo.re-cloud.io/magnetiq/v2

# Verify pods running
sudo k0s kubectl -n magnetiq-v2 get pods -o wide

# Check which worker each pod is on
sudo k0s kubectl -n magnetiq-v2 get pods -o custom-columns=NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase
```

### Performance Testing

**Measure Distribution Time:**

```bash
# Time full distribution
time ./m2deploy deploy \
  --workspace-path /tmp/wapsol/magnetiq2 \
  --skip-deploy \
  --parallel-workers 1  # Sequential

# Compare with parallel
time ./m2deploy deploy \
  --workspace-path /tmp/wapsol/magnetiq2 \
  --skip-deploy \
  --parallel-workers 5  # All at once

# Expected results:
# Sequential: ~5-10 minutes (depends on network)
# Parallel:   ~1-2 minutes
```

**Network Bandwidth Test:**

```bash
# Measure actual transfer speed
# Total size: ~400MB per worker × 5 workers = 2GB
# Expected time: 2GB / 100Mbps ≈ 2-3 minutes (theoretical)

# Real-world: 3-5 minutes (SSH overhead, parallelism limits)
```

---

## Troubleshooting Guide

### Problem: SSH Connection Refused

**Symptoms:**
```
Error: SSH connectivity test failed:
  rc3-k0s-2-worker-1 (10.112.3.182): dial tcp 10.112.3.182:22: connect: connection refused
```

**Causes:**
1. SSH daemon not running on worker
2. Firewall blocking port 22
3. Wrong IP address

**Solutions:**
```bash
# Check SSH daemon on worker
ssh ubuntu@10.112.3.182 "systemctl status sshd"

# Test connectivity from controller
telnet 10.112.3.182 22

# Verify IP is correct
sudo k0s kubectl get nodes -o wide | grep worker-1
```

### Problem: Permission Denied (publickey)

**Symptoms:**
```
Error: SSH connectivity test failed:
  rc3-k0s-2-worker-1 (10.112.3.182): Permission denied (publickey)
```

**Causes:**
1. SSH key not copied to worker
2. Wrong SSH key specified
3. Wrong username

**Solutions:**
```bash
# Copy SSH key to worker
ssh-copy-id ubuntu@10.112.3.182

# Try with different key
./m2deploy deploy --ssh-key ~/.ssh/id_ed25519 ...

# Try with different user
./m2deploy deploy --ssh-user root ...

# Verify key permissions
ls -la ~/.ssh/id_rsa
# Should be: -rw------- (600)
chmod 600 ~/.ssh/id_rsa
```

### Problem: Disk Full on Worker

**Symptoms:**
```
Error: Failed to distribute backend to rc3-k0s-2-worker-1: disk full on worker
```

**Causes:**
1. `/tmp` partition full
2. Containerd storage full
3. Old tarballs not cleaned up

**Solutions:**
```bash
# Check disk space on worker
ssh ubuntu@10.112.3.182 "df -h"

# Clean up old tarballs manually
ssh ubuntu@10.112.3.182 "rm -f /tmp/magnetiq-*.tar"

# Clean up old containerd images
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images list | grep magnetiq"
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images rm <image-name>"

# Use different temp directory with more space
./m2deploy deploy --worker-temp-dir /var/tmp ...
```

### Problem: Import Fails but SCP Succeeds

**Symptoms:**
```
[INFO]   [worker-1] Copying tarball (340 MB)... OK
[ERROR]  [worker-1] Importing into containerd... FAILED
```

**Causes:**
1. Tarball corrupted during transfer
2. containerd not running
3. Wrong namespace
4. sudo access denied

**Solutions:**
```bash
# Verify tarball integrity on worker
ssh ubuntu@10.112.3.182 "md5sum /tmp/magnetiq-backend.tar"
# Compare with local
md5sum /tmp/magnetiq-backend.tar

# Check containerd status
ssh ubuntu@10.112.3.182 "sudo systemctl status containerd"

# Test manual import
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images import /tmp/magnetiq-backend.tar"

# Check sudo access
ssh ubuntu@10.112.3.182 "sudo -n ctr --version"
```

### Problem: Images Imported but Pods Still Pull from Registry

**Symptoms:**
```
Events:
  Warning  Failed     1m    kubelet  Failed to pull image "crepo.re-cloud.io/magnetiq/v2/backend:latest":
           rpc error: code = Unknown desc = failed to pull and unpack image "crepo.re-cloud.io/magnetiq/v2/backend:latest":
           failed to resolve reference "crepo.re-cloud.io/magnetiq/v2/backend:latest": failed to do request:
           Head "https://crepo.re-cloud.io/v2/magnetiq/v2/backend/manifests/latest": dial tcp: lookup crepo.re-cloud.io: no such host
```

**Causes:**
1. `imagePullPolicy: Always` in manifest (forces registry pull)
2. Image name mismatch (local vs manifest)
3. Wrong containerd namespace

**Solutions:**
```bash
# 1. Update k8s manifests
# Change imagePullPolicy to Never or IfNotPresent
sed -i 's/imagePullPolicy: Always/imagePullPolicy: Never/' k8s/*/deployment.yaml

# 2. Verify image name matches exactly
# On worker:
ssh ubuntu@10.112.3.182 "sudo ctr -n k8s.io images list" | grep magnetiq
# Should show: crepo.re-cloud.io/magnetiq/v2/backend:latest

# 3. Check manifest expected name
grep "image:" k8s/backend/deployment.yaml
# Should match exactly what's in containerd

# 4. Re-deploy with updated manifests
./m2deploy deploy --skip-import ...
```

### Problem: Slow Distribution (Takes > 10 Minutes)

**Causes:**
1. Low network bandwidth
2. Large image sizes
3. Sequential distribution (--parallel-workers 1)
4. Network congestion

**Solutions:**
```bash
# Increase parallelism
./m2deploy deploy --parallel-workers 5 ...

# Check network speed to workers
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo "Testing $ip"
  ssh ubuntu@$ip "iperf3 -s -1" &
  sleep 1
  iperf3 -c $ip -t 5
done

# Optimize images (reduce size)
# - Use multi-stage builds
# - Remove unnecessary dependencies
# - Use alpine base images

# Check for network issues
for ip in 10.112.3.182 10.112.0.95 10.112.2.96 10.112.3.77 10.112.3.197; do
  echo "$ip: $(ping -c 3 $ip | grep avg)"
done
```

---

## Summary

This plan provides a complete blueprint for implementing SSH-based image distribution in m2deploy without requiring a container registry.

### Key Deliverables

1. **New Package:** `pkg/ssh/distribute.go` (~300 lines)
2. **Updated Commands:** `cmd/deploy.go`, `cmd/root.go`
3. **10+ New Flags:** SSH config and distribution behavior
4. **Comprehensive Error Handling:** Retries, timeouts, partial failures
5. **Parallel Distribution:** Configurable parallelism for performance
6. **Testing Suite:** Unit tests + integration tests
7. **Documentation:** Troubleshooting guide, examples

### Estimated Timeline

- **Week 1:** Core SSH/SCP functionality + worker discovery
- **Week 2:** Distribution logic + integration with deploy command
- **Week 3:** Testing + documentation + bug fixes

### Next Steps

1. Review and approve this plan
2. Set up SSH keys on all worker nodes (prerequisite)
3. Begin implementation with Phase 1 (Core SSH Functionality)
4. Iterate based on testing results

---

**Document Version:** 1.0
**Last Updated:** October 2025
**Maintained By:** m2deploy Development Team
