package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wapsol/m2deploy/pkg/config"
	"gopkg.in/yaml.v3"
)

// Client handles manifest operations
type Client struct {
	Logger *config.Logger
	DryRun bool
}

// NewClient creates a new manifest client
func NewClient(logger *config.Logger, dryRun bool) *Client {
	return &Client{
		Logger: logger,
		DryRun: dryRun,
	}
}

// UpdateNamespace updates namespace in all manifests
func (c *Client) UpdateNamespace(k8sDir, newNamespace string) error {
	c.Logger.Info("Updating namespace to: %s", newNamespace)

	if c.DryRun {
		c.Logger.DryRun("Would update namespace to %s", newNamespace)
		return nil
	}

	// Find all YAML files
	files, err := filepath.Glob(filepath.Join(k8sDir, "**/*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to find manifests: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var manifest map[string]interface{}
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			continue
		}

		// Update namespace in metadata
		if metadata, ok := manifest["metadata"].(map[string]interface{}); ok {
			metadata["namespace"] = newNamespace
		}

		output, err := yaml.Marshal(manifest)
		if err != nil {
			continue
		}

		if err := os.WriteFile(file, output, 0644); err != nil {
			c.Logger.Warning("Failed to update namespace in %s: %v", file, err)
		}
	}

	c.Logger.Success("Updated namespace in manifests")
	return nil
}
