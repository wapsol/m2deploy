package constants

import "time"

const (
	// File paths
	TarballPathTemplate = "/tmp/magnetiq-%s.tar"

	// Timing constants
	ManifestApplyDelay    = 500 * time.Millisecond
	PodStabilizationDelay = 5 * time.Second
	ContainerStartupDelay = 5 * time.Second
)
