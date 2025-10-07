package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wapsol/m2deploy/pkg/config"
	"github.com/wapsol/m2deploy/pkg/git"
)

// getComponents parses component string and returns list of components
func getComponents(component string) []string {
	switch component {
	case ComponentBackend:
		return []string{ComponentBackend}
	case ComponentFrontend:
		return []string{ComponentFrontend}
	case ComponentBoth:
		return []string{ComponentBackend, ComponentFrontend}
	default:
		return []string{}
	}
}

// getOrDetermineTag determines the tag to use, either from provided value or from git commit
func getOrDetermineTag(logger *config.Logger, workDir, providedTag string, dryRun bool) (string, error) {
	if providedTag != "" {
		return providedTag, nil
	}

	gitClient := git.NewClient(logger, dryRun)
	commit, err := gitClient.GetCurrentCommit(workDir)
	if err != nil {
		logger.Warning("Failed to get commit SHA, using '%s': %v", DefaultTag, err)
		return DefaultTag, nil
	}

	logger.Info("Using commit SHA as tag: %s", commit)
	return commit, nil
}

// getDeploymentName returns the standardized deployment name for a component
func getDeploymentName(component string) string {
	return DeploymentPrefix + component
}

// getTestContainerName returns the standardized test container name for a component
func getTestContainerName(component string) string {
	return TestContainerPrefix + component
}

// validateComponent checks if a component string is valid
func validateComponent(component string) error {
	if component != ComponentBackend && component != ComponentFrontend && component != ComponentBoth {
		return fmt.Errorf("invalid component: %s (must be %s, %s, or %s)",
			component, ComponentBackend, ComponentFrontend, ComponentBoth)
	}
	return nil
}

// deriveWorkspaceFromRepoURL derives the workspace path from a repository URL
// Pattern: /tmp/<username>/<repo-name>
// Examples:
//   https://github.com/wapsol/magnetiq2 -> /tmp/wapsol/magnetiq2
//   git@github.com:user/project.git -> /tmp/user/project
//   https://github.com/org/app.git -> /tmp/org/app
func deriveWorkspaceFromRepoURL(repoURL string) string {
	// Remove common prefixes
	url := strings.TrimPrefix(repoURL, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")

	// Replace : with / for SSH URLs (git@github.com:user/repo)
	url = strings.ReplaceAll(url, ":", "/")

	// Remove .git suffix if present
	url = strings.TrimSuffix(url, ".git")

	// Split by / and get last two parts (username/repo)
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		username := parts[len(parts)-2]
		repoName := parts[len(parts)-1]
		return filepath.Join("/tmp", username, repoName)
	}

	// Fallback to /tmp/m2deploy-workspace if parsing fails
	return "/tmp/m2deploy-workspace"
}

// formatError wraps an error with a helpful message to check --help
func formatError(cmdName string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\n\nRun 'm2deploy %s --help' for usage information", err, cmdName)
}

// formatPrereqError formats prerequisite check failure errors with help hint
func formatPrereqError(cmdName string) error {
	return fmt.Errorf("prerequisite check failed - see errors above\n\nRun 'm2deploy %s --help' for usage information", cmdName)
}
