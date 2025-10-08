package k8s

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/constants"
)

// Client handles Kubernetes operations
type Client struct {
	Logger     *config.Logger
	DryRun     bool
	Namespace  string
	Kubeconfig string
	UseSudo    bool
}

// NewClient creates a new Kubernetes client
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

// ValidateManifest validates a manifest using kubectl dry-run
func (c *Client) ValidateManifest(manifestPath string) error {
	c.Logger.Debug("Validating manifest: %s", manifestPath)

	cmd := c.buildKubectlCmd("apply", "--dry-run=client", "-f", manifestPath)
	output, err := cmd.CombinedOutput()

	if err != nil {
		c.Logger.Error("Validation failed for %s:", manifestPath)
		c.Logger.Error(string(output))
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	c.Logger.Debug("Manifest validated: %s", manifestPath)
	return nil
}

// Apply applies Kubernetes manifests
func (c *Client) Apply(manifestPath string) error {
	c.Logger.Info("Applying manifest: %s", manifestPath)

	if c.DryRun {
		c.Logger.DryRun("Would apply manifest %s", manifestPath)
		return nil
	}

	cmd := c.buildKubectlCmd("apply", "-f", manifestPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	c.Logger.Debug("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply manifest %s: %w", manifestPath, err)
	}

	c.Logger.Success("Applied manifest: %s", manifestPath)
	return nil
}

// Delete deletes Kubernetes resources
func (c *Client) Delete(manifestPath string) error {
	c.Logger.Info("Deleting resources from: %s", manifestPath)

	if c.DryRun {
		c.Logger.DryRun("Would delete resources from %s", manifestPath)
		return nil
	}

	cmd := c.buildKubectlCmd("delete", "-f", manifestPath, "--ignore-not-found=true")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete resources from %s: %w", manifestPath, err)
	}

	c.Logger.Success("Deleted resources from: %s", manifestPath)
	return nil
}

// Deploy deploys the application with proper ordering
func (c *Client) Deploy(workDir string) error {
	return c.DeployWithOptions(workDir, false, false)
}

// DeployWithOptions deploys with validation and wait options
func (c *Client) DeployWithOptions(workDir string, validate bool, wait bool) error {
	c.Logger.Info("Deploying Magnetiq2 application")

	k8sDir := filepath.Join(workDir, "k8s")

	// Deployment order with criticality flags
	type manifestEntry struct {
		path     string
		optional bool // If true, skip if missing without warning
	}

	deploymentOrder := []manifestEntry{
		{"namespace.yaml", false},
		{"rbac.yaml", false},
		{"storage.yaml", false},
		{"backend/pvc.yaml", false},
		{"configmap.yaml", false},
		{"secrets.yaml", false},
		{"backend/service.yaml", false},
		{"frontend/service.yaml", false},
		{"backend/deployment.yaml", false},
		{"frontend/deployment.yaml", false},
		{"network-policy.yaml", true}, // Optional
		{"hpa.yaml", true},            // Optional
		{"ingress.yaml", false},
	}

	// Phase 1: Validation (if requested)
	if validate {
		c.Logger.Info("Validating manifests...")
		for _, entry := range deploymentOrder {
			manifestPath := filepath.Join(k8sDir, entry.path)
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				if !entry.optional {
					c.Logger.Warning("Required manifest not found: %s", entry.path)
				}
				continue
			}

			if err := c.ValidateManifest(manifestPath); err != nil {
				return fmt.Errorf("validation failed for %s: %w", entry.path, err)
			}
		}
		c.Logger.Success("All manifests validated successfully")
	}

	// Phase 2: Apply manifests
	for _, entry := range deploymentOrder {
		manifestPath := filepath.Join(k8sDir, entry.path)
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			if entry.optional {
				c.Logger.Debug("Optional manifest not found, skipping: %s", entry.path)
			} else {
				c.Logger.Warning("Manifest not found, skipping: %s", entry.path)
			}
			continue
		}

		if err := c.Apply(manifestPath); err != nil {
			return fmt.Errorf("failed to apply %s: %w", entry.path, err)
		}

		// Small delay between applies
		time.Sleep(constants.ManifestApplyDelay)
	}

	c.Logger.Success("Deployment completed")

	// Phase 3: Wait for pods (if requested)
	if wait {
		c.Logger.Info("Waiting for deployments to be ready...")
		time.Sleep(constants.PodStabilizationDelay) // Initial stabilization

		for _, deploymentName := range []string{"magnetiq-backend", "magnetiq-frontend"} {
			if err := c.WaitForRollout(deploymentName, 5*time.Minute); err != nil {
				c.Logger.Warning("Deployment %s not ready: %v", deploymentName, err)
			}
		}

		// Check pod health
		c.Logger.Info("Checking pod health...")
		if err := c.CheckPodHealth(); err != nil {
			c.Logger.Warning("Some pods are not healthy: %v", err)
			c.Logger.Info("Run 'm2deploy verify' for detailed status")
		} else {
			c.Logger.Success("All pods are running and healthy")
		}
	}

	return nil
}

// Undeploy removes the application
func (c *Client) Undeploy(workDir string, keepNamespace, keepPVCs bool) error {
	c.Logger.Info("Undeploying Magnetiq2 application")

	k8sDir := filepath.Join(workDir, "k8s")

	// Deletion order (reverse of deployment)
	deletionOrder := []string{
		"ingress.yaml",
		"hpa.yaml",
		"network-policy.yaml",
		"frontend/deployment.yaml",
		"backend/deployment.yaml",
		"frontend/service.yaml",
		"backend/service.yaml",
		"secrets.yaml",
		"configmap.yaml",
	}

	if !keepPVCs {
		deletionOrder = append(deletionOrder, "backend/pvc.yaml", "storage.yaml")
	}

	deletionOrder = append(deletionOrder, "rbac.yaml")

	if !keepNamespace {
		deletionOrder = append(deletionOrder, "namespace.yaml")
	}

	for _, manifest := range deletionOrder {
		manifestPath := filepath.Join(k8sDir, manifest)
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			c.Logger.Warning("Manifest not found, skipping: %s", manifest)
			continue
		}

		if err := c.Delete(manifestPath); err != nil {
			c.Logger.Warning("Failed to delete %s: %v", manifest, err)
			continue
		}

		time.Sleep(constants.ManifestApplyDelay)
	}

	c.Logger.Success("Undeployment completed")
	return nil
}

// SetImage updates the image for a deployment
func (c *Client) SetImage(deployment, container, image string) error {
	c.Logger.Info("Updating image for %s/%s to %s", deployment, container, image)

	if c.DryRun {
		c.Logger.DryRun("Would update image for %s/%s to %s", deployment, container, image)
		return nil
	}

	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"set", "image",
		fmt.Sprintf("deployment/%s", deployment),
		fmt.Sprintf("%s=%s", container, image),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update image: %w", err)
	}

	c.Logger.Success("Image updated for %s/%s", deployment, container)
	return nil
}

// WaitForRollout waits for a deployment rollout to complete
func (c *Client) WaitForRollout(deployment string, timeout time.Duration) error {
	c.Logger.Info("Waiting for rollout of %s (timeout: %v)", deployment, timeout)

	if c.DryRun {
		c.Logger.DryRun("Would wait for rollout of %s", deployment)
		return nil
	}

	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"rollout", "status",
		fmt.Sprintf("deployment/%s", deployment),
		fmt.Sprintf("--timeout=%s", timeout.String()),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollout failed or timed out: %w", err)
	}

	c.Logger.Success("Rollout completed for %s", deployment)
	return nil
}

// Rollback rolls back a deployment
func (c *Client) Rollback(deployment string) error {
	c.Logger.Info("Rolling back deployment: %s", deployment)

	if c.DryRun {
		c.Logger.DryRun("Would rollback deployment %s", deployment)
		return nil
	}

	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"rollout", "undo",
		fmt.Sprintf("deployment/%s", deployment),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to rollback deployment: %w", err)
	}

	c.Logger.Success("Rolled back deployment: %s", deployment)
	return nil
}

// GetPods gets pods in the namespace
func (c *Client) GetPods() (string, error) {
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"get", "pods",
		"-o", "wide",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pods: %w", err)
	}

	return string(output), nil
}

// GetServices gets services in the namespace
func (c *Client) GetServices() (string, error) {
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"get", "services",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get services: %w", err)
	}

	return string(output), nil
}

// GetIngress gets ingress resources in the namespace
func (c *Client) GetIngress() (string, error) {
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"get", "ingress",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get ingress: %w", err)
	}

	return string(output), nil
}

// CheckPodHealth checks if all pods are running and ready
func (c *Client) CheckPodHealth() error {
	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"get", "pods",
		"-o", "jsonpath={.items[*].status.phase}",
	)

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check pod health: %w", err)
	}

	phases := strings.Fields(string(output))
	for _, phase := range phases {
		if phase != "Running" && phase != "Succeeded" {
			return fmt.Errorf("found pod in %s state", phase)
		}
	}

	return nil
}

// CopySecretToNamespace copies a secret from one namespace to another
func (c *Client) CopySecretToNamespace(secretName, fromNamespace, toNamespace string) error {
	c.Logger.Info("Copying secret %s from %s to %s", secretName, fromNamespace, toNamespace)

	if c.DryRun {
		c.Logger.DryRun("Would copy secret %s from %s to %s", secretName, fromNamespace, toNamespace)
		return nil
	}

	// Get the secret
	getCmd := c.buildKubectlCmd(
		"-n", fromNamespace,
		"get", "secret", secretName,
		"-o", "yaml",
	)

	secretYAML, err := getCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	// Modify namespace and remove resourceVersion/uid
	modifiedYAML := string(secretYAML)
	modifiedYAML = strings.ReplaceAll(modifiedYAML, fmt.Sprintf("namespace: %s", fromNamespace), fmt.Sprintf("namespace: %s", toNamespace))

	// Apply to new namespace
	applyCmd := c.buildKubectlCmd("apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(modifiedYAML)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr

	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy secret: %w", err)
	}

	c.Logger.Success("Copied secret %s to namespace %s", secretName, toNamespace)
	return nil
}

// ScaleDeployment scales a deployment to a specific number of replicas
func (c *Client) ScaleDeployment(deployment string, replicas int) error {
	c.Logger.Info("Scaling %s to %d replicas", deployment, replicas)

	if c.DryRun {
		c.Logger.DryRun("Would scale %s to %d replicas", deployment, replicas)
		return nil
	}

	cmd := c.buildKubectlCmd(
		"-n", c.Namespace,
		"scale",
		fmt.Sprintf("deployment/%s", deployment),
		fmt.Sprintf("--replicas=%d", replicas),
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to scale deployment: %w", err)
	}

	c.Logger.Success("Scaled %s to %d replicas", deployment, replicas)
	return nil
}

// GetWorkerIPs returns the internal IPs of all worker nodes in the cluster
func (c *Client) GetWorkerIPs() ([]string, error) {
	c.Logger.Debug("Querying k8s API for worker node IPs")

	// Get nodes in JSON format
	cmd := c.buildKubectlCmd("get", "nodes", "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w: %s", err, string(output))
	}

	// Parse JSON response
	var nodeList struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Addresses []struct {
					Type    string `json:"type"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(output, &nodeList); err != nil {
		return nil, fmt.Errorf("failed to parse node list JSON: %w", err)
	}

	var workerIPs []string

	// Extract IPs from worker nodes (skip control-plane/master nodes)
	for _, node := range nodeList.Items {
		// Check if this is a control-plane/master node
		isControlPlane := false
		for label := range node.Metadata.Labels {
			if strings.Contains(label, "control-plane") || strings.Contains(label, "master") {
				isControlPlane = true
				break
			}
		}

		// Skip control-plane nodes
		if isControlPlane {
			c.Logger.Debug("Skipping control-plane node: %s", node.Metadata.Name)
			continue
		}

		// Extract internal IP
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				workerIPs = append(workerIPs, addr.Address)
				c.Logger.Debug("Found worker node %s with IP %s", node.Metadata.Name, addr.Address)
				break
			}
		}
	}

	if len(workerIPs) == 0 {
		return nil, fmt.Errorf("no worker nodes found in cluster")
	}

	c.Logger.Debug("Found %d worker nodes", len(workerIPs))
	return workerIPs, nil
}
