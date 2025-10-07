package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wapsol/m2deploy/pkg/constants"
)

// getComponents parses component string and returns list of components
func getComponents(component string) []string {
	switch component {
	case constants.ComponentBackend:
		return []string{constants.ComponentBackend}
	case constants.ComponentFrontend:
		return []string{constants.ComponentFrontend}
	case constants.ComponentBoth:
		return []string{constants.ComponentBackend, constants.ComponentFrontend}
	default:
		return []string{}
	}
}

// getDeploymentName returns the standardized deployment name for a component
func getDeploymentName(component string) string {
	return constants.DeploymentPrefix + component
}

// getTestContainerName returns the standardized test container name for a component
func getTestContainerName(component string) string {
	return constants.TestContainerPrefix + component
}

// validateComponent checks if a component string is valid
func validateComponent(component string) error {
	if component != constants.ComponentBackend && component != constants.ComponentFrontend && component != constants.ComponentBoth {
		return fmt.Errorf("invalid component: %s (must be %s, %s, or %s)",
			component, constants.ComponentBackend, constants.ComponentFrontend, constants.ComponentBoth)
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
// Uses formatError internally for consistency
func formatPrereqError(cmdName string) error {
	baseErr := fmt.Errorf("prerequisite check failed - see errors above")
	return formatError(cmdName, baseErr)
}

// promptForConfirmation prompts the user for confirmation
// Returns true if user confirms with "yes", false otherwise
func promptForConfirmation(message string) bool {
	fmt.Printf("\n⚠️  %s\n", message)
	fmt.Printf("Type 'yes' to continue: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "yes"
}
