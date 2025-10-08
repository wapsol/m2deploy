package constants

import "time"

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

	// File paths
	TarballPathTemplate = "/tmp/magnetiq-%s.tar"

	// Timing constants
	ManifestApplyDelay    = 500 * time.Millisecond
	PodStabilizationDelay = 5 * time.Second
	ContainerStartupDelay = 5 * time.Second

	// SSH distribution defaults
	DefaultSSHUser         = "ubuntu"
	DefaultSSHPort         = 22
	DefaultSSHTimeout      = 30 // seconds
	DefaultWorkerTempDir   = "/tmp"
	DefaultParallelWorkers = 3
	DefaultRetryCount      = 3

	// Containerd namespace for k8s
	ContainerdNamespace = "k8s.io"
)
