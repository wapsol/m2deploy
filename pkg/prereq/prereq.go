package prereq

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/wapsol/m2deploy/pkg/config"
)

// CheckResult represents the result of a prerequisite check
type CheckResult struct {
	Name     string
	Status   string // "pass", "fail", "warning"
	Message  string
	Required bool
}

// Checker handles prerequisite verification
type Checker struct {
	Logger  *config.Logger
	Results []CheckResult
}

// NewChecker creates a new prerequisite checker
func NewChecker(logger *config.Logger) *Checker {
	return &Checker{
		Logger:  logger,
		Results: []CheckResult{},
	}
}

// AddResult adds a check result
func (c *Checker) AddResult(result CheckResult) {
	c.Results = append(c.Results, result)
}

// PrintResults prints all check results with color coding
func (c *Checker) PrintResults() {
	c.Logger.Info("\n=== Prerequisite Check Results ===\n")
	for _, r := range c.Results {
		switch r.Status {
		case "pass":
			c.Logger.Success("✓ %s: %s", r.Name, r.Message)
		case "fail":
			c.Logger.Error("✗ %s: %s", r.Name, r.Message)
		case "warning":
			c.Logger.Warning("⚠ %s: %s", r.Name, r.Message)
		}
	}
}

// HasFailures returns true if any required check failed
func (c *Checker) HasFailures() bool {
	for _, r := range c.Results {
		if r.Required && r.Status == "fail" {
			return true
		}
	}
	return false
}

// CheckDocker verifies Docker is installed and running
func (c *Checker) CheckDocker(useSudo bool) {
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "docker", "info")
	} else {
		cmd = exec.Command("docker", "info")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse error for specific guidance
		errMsg := string(output)
		var message string

		if !useSudo && (contains(errMsg, "permission denied") || contains(errMsg, "connect to the Docker daemon socket")) {
			message = "Docker permission denied. Your user needs access to the Docker daemon socket.\n" +
				"   This typically happens because your user is not in the 'docker' group.\n" +
				"   Solutions:\n" +
				"   1. Run with --use-sudo flag: m2deploy build --use-sudo ...\n" +
				"   2. Add user to docker group (requires logout/login): sudo usermod -aG docker $USER && newgrp docker\n" +
				"   Note: Adding to docker group grants root-equivalent privileges on the Docker daemon."
		} else if contains(errMsg, "Cannot connect") || contains(errMsg, "Is the docker daemon running") {
			message = "Docker daemon is not running. The Docker service must be active to build and manage containers.\n" +
				"   Start Docker with: sudo systemctl start docker\n" +
				"   Enable on boot: sudo systemctl enable docker\n" +
				"   Check status: sudo systemctl status docker"
		} else if contains(errMsg, "command not found") || contains(errMsg, "No such file") {
			message = "Docker is not installed. Docker is required for building and managing container images.\n" +
				"   Install Docker: sudo apt update && sudo apt install docker.io\n" +
				"   Or use official Docker installation: https://docs.docker.com/engine/install/"
		} else {
			message = fmt.Sprintf("Docker is not accessible: %v\n   Check Docker installation and permissions.", err)
		}

		c.AddResult(CheckResult{
			Name:     "Docker",
			Status:   "fail",
			Message:  message,
			Required: true,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "Docker",
		Status:   "pass",
		Message:  "Docker is running and accessible",
		Required: true,
	})
}

// contains checks if a string contains a substring (case-insensitive helper)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// CheckK0s verifies k0s is installed and running
func (c *Checker) CheckK0s(useSudo bool) {
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "k0s", "status")
	} else {
		cmd = exec.Command("k0s", "status")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse error for specific guidance
		errMsg := string(output)
		var message string

		if !useSudo && contains(errMsg, "permission denied") {
			message = "k0s permission denied. k0s requires elevated privileges to access the cluster.\n" +
				"   Run with --use-sudo flag: m2deploy deploy --use-sudo ..."
		} else if contains(errMsg, "command not found") || contains(errMsg, "No such file") {
			message = "k0s is not installed. k0s is a lightweight Kubernetes distribution required for deployment.\n" +
				"   Install k0s: https://docs.k0sproject.io/stable/install/\n" +
				"   Quick install: curl -sSLf https://get.k0s.sh | sudo sh"
		} else {
			message = fmt.Sprintf("k0s is not running or not properly configured: %v\n"+
				"   Check k0s status: sudo k0s status\n"+
				"   View k0s logs: sudo journalctl -u k0s", err)
		}

		c.AddResult(CheckResult{
			Name:     "k0s",
			Status:   "fail",
			Message:  message,
			Required: true,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "k0s",
		Status:   "pass",
		Message:  "k0s is running and accessible",
		Required: true,
	})
}

// CheckK0sKubectl verifies kubectl access through k0s
func (c *Checker) CheckK0sKubectl(useSudo bool, namespace string) {
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "k0s", "kubectl", "get", "nodes")
	} else {
		cmd = exec.Command("k0s", "kubectl", "get", "nodes")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse error for specific guidance
		errMsg := string(output)
		var message string

		if !useSudo && (contains(errMsg, "permission denied") || contains(errMsg, "dial") && contains(errMsg, "timeout")) {
			message = "kubectl permission denied or timeout. Access to the Kubernetes API requires elevated privileges.\n" +
				"   Run with --use-sudo flag: m2deploy deploy --use-sudo ..."
		} else if contains(errMsg, "connection refused") || contains(errMsg, "dial") {
			message = "Cannot connect to Kubernetes API server. The k0s control plane may not be running.\n" +
				"   Check k0s status: sudo k0s status\n" +
				"   Ensure k0s controller is started: sudo systemctl status k0s\n" +
				"   View API server logs: sudo k0s kubectl logs -n kube-system kube-apiserver-*"
		} else {
			message = fmt.Sprintf("Cannot access Kubernetes cluster: %v\n"+
				"   Verify k0s is running and accessible.\n"+
				"   Test manually: sudo k0s kubectl get nodes", err)
		}

		c.AddResult(CheckResult{
			Name:     "kubectl",
			Status:   "fail",
			Message:  message,
			Required: true,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "kubectl",
		Status:   "pass",
		Message:  "kubectl access verified",
		Required: true,
	})

	// Check namespace exists
	if namespace != "" {
		var nsCmd *exec.Cmd
		if useSudo {
			nsCmd = exec.Command("sudo", "k0s", "kubectl", "get", "namespace", namespace)
		} else {
			nsCmd = exec.Command("k0s", "kubectl", "get", "namespace", namespace)
		}

		if err := nsCmd.Run(); err != nil {
			c.AddResult(CheckResult{
				Name:     "Namespace",
				Status:   "warning",
				Message:  fmt.Sprintf("Namespace '%s' does not exist (will be created during deployment)", namespace),
				Required: false,
			})
		} else {
			c.AddResult(CheckResult{
				Name:     "Namespace",
				Status:   "pass",
				Message:  fmt.Sprintf("Namespace '%s' exists", namespace),
				Required: false,
			})
		}
	}
}

// CheckGit verifies Git is installed
func (c *Checker) CheckGit() {
	cmd := exec.Command("git", "--version")
	if err := cmd.Run(); err != nil {
		c.AddResult(CheckResult{
			Name:     "Git",
			Status:   "fail",
			Message:  "Git is not installed. Git is required for cloning source code repositories.\n" +
				"   Install Git: sudo apt update && sudo apt install git\n" +
				"   Verify installation: git --version",
			Required: true,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "Git",
		Status:   "pass",
		Message:  "Git is installed",
		Required: true,
	})
}

// CheckDiskSpace verifies sufficient disk space is available (in GB)
func (c *Checker) CheckDiskSpace(path string, minGB uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		c.AddResult(CheckResult{
			Name:     "Disk Space",
			Status:   "warning",
			Message:  fmt.Sprintf("Cannot check disk space: %v", err),
			Required: false,
		})
		return
	}

	// Available space in bytes
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	availableGB := availableBytes / (1024 * 1024 * 1024)

	if availableGB < minGB {
		c.AddResult(CheckResult{
			Name:     "Disk Space",
			Status:   "warning",
			Message:  fmt.Sprintf("Low disk space: %d GB available (recommended: %d GB)\n"+
				"   Building Docker images requires temporary space for:\n"+
				"   - Image layers and build cache\n"+
				"   - Extracted source code and dependencies\n"+
				"   - Intermediate build artifacts\n"+
				"   Operations may fail if disk fills up. Consider freeing space or cleaning Docker cache:\n"+
				"   docker system prune -a --volumes", availableGB, minGB),
			Required: false,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "Disk Space",
		Status:   "pass",
		Message:  fmt.Sprintf("%d GB available", availableGB),
		Required: false,
	})
}

// CheckWorkDir verifies the working directory exists
func (c *Checker) CheckWorkDir(workDir string) {
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		c.AddResult(CheckResult{
			Name:     "Work Directory",
			Status:   "warning",
			Message:  fmt.Sprintf("Work directory '%s' does not exist (will be created during clone)", workDir),
			Required: false,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     "Work Directory",
		Status:   "pass",
		Message:  fmt.Sprintf("Work directory '%s' exists", workDir),
		Required: false,
	})
}

// CheckNetwork verifies network connectivity to a host
func (c *Checker) CheckNetwork(host string) {
	cmd := exec.Command("ping", "-c", "1", "-W", "2", host)
	if err := cmd.Run(); err != nil {
		c.AddResult(CheckResult{
			Name:     fmt.Sprintf("Network (%s)", host),
			Status:   "warning",
			Message:  fmt.Sprintf("Cannot reach %s. Network connectivity is required for:\n"+
				"   - Cloning repositories from GitHub\n"+
				"   - Pulling Docker base images\n"+
				"   - Accessing container registries\n"+
				"   Check your internet connection and firewall settings.", host),
			Required: false,
		})
		return
	}

	c.AddResult(CheckResult{
		Name:     fmt.Sprintf("Network (%s)", host),
		Status:   "pass",
		Message:  fmt.Sprintf("Can reach %s", host),
		Required: false,
	})
}

// CheckBuildPrereqs checks prerequisites for build operations
func (c *Checker) CheckBuildPrereqs(useSudo bool) {
	c.CheckGit()
	c.CheckDocker(useSudo)
	c.CheckDiskSpace(".", 5)
}

// CheckDeployPrereqs checks prerequisites for deployment operations
func (c *Checker) CheckDeployPrereqs(namespace string, useSudo bool) {
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
	c.CheckDiskSpace(".", 2)
}

// CheckTestPrereqs checks prerequisites for testing operations
func (c *Checker) CheckTestPrereqs(useSudo bool) {
	c.CheckDocker(useSudo)
}

// CheckClonePrereqs checks prerequisites for clone operations
func (c *Checker) CheckClonePrereqs() {
	c.CheckGit()
	c.CheckNetwork("github.com")
	c.CheckDiskSpace(".", 2)
}

// CheckDBPrereqs checks prerequisites for database operations
func (c *Checker) CheckDBPrereqs(namespace string, useSudo bool) {
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
}

// CheckAllPrereqs checks all prerequisites for complete pipeline
func (c *Checker) CheckAllPrereqs(namespace string, useSudo bool) {
	c.CheckGit()
	c.CheckDocker(useSudo)
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
	c.CheckDiskSpace(".", 10)
	c.CheckNetwork("github.com")
}

// CheckCleanupPrereqs checks prerequisites for cleanup operations
func (c *Checker) CheckCleanupPrereqs(useSudo bool) {
	c.CheckDocker(useSudo)
}

// CheckVerifyPrereqs checks prerequisites for verify operations
func (c *Checker) CheckVerifyPrereqs(namespace string, useSudo bool) {
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
}

// CheckUpdatePrereqs checks prerequisites for update operations
func (c *Checker) CheckUpdatePrereqs(namespace string, useSudo bool) {
	c.CheckGit()
	c.CheckDocker(useSudo)
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
	c.CheckDiskSpace(".", 5)
}

// CheckRollbackPrereqs checks prerequisites for rollback operations
func (c *Checker) CheckRollbackPrereqs(namespace string, useSudo bool) {
	c.CheckK0s(useSudo)
	c.CheckK0sKubectl(useSudo, namespace)
}
