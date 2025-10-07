package cmd

import (
	"github.com/wapsol/m2deploy/pkg/constants"
)

const (
	// Component names
	ComponentBackend  = "backend"
	ComponentFrontend = "frontend"
	ComponentBoth     = "both"

	// Default ports for local testing
	BackendPort  = 4036
	FrontendPort = 9036

	// Container name prefixes
	TestContainerPrefix = "m2deploy-test-"

	// Deployment name prefix
	DeploymentPrefix = "magnetiq-"

	// Default values
	DefaultTag             = "latest"
	DefaultBackupPath      = "./backups"
	DefaultBackupRetention = 5

	// Pod labels
	BackendPodLabel = "app=magnetiq-backend"
)

// Re-export common constants for convenience
const (
	TarballPathTemplate   = constants.TarballPathTemplate
	ManifestApplyDelay    = constants.ManifestApplyDelay
	PodStabilizationDelay = constants.PodStabilizationDelay
	ContainerStartupDelay = constants.ContainerStartupDelay
)
