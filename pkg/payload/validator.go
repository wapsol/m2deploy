package payload

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wapsol/m2deploy/pkg/config"
)

// ValidationError represents a validation error with context
type ValidationError struct {
	Path    string
	Message string
}

// Validator validates payload structure and requirements
type Validator struct {
	Logger *config.Logger
}

// NewValidator creates a new payload validator
func NewValidator(logger *config.Logger) *Validator {
	return &Validator{
		Logger: logger,
	}
}

// ValidateStructure performs basic structure validation (required directories and files exist)
func (v *Validator) ValidateStructure(workDir string) error {
	v.Logger.Debug("Validating payload structure at: %s", workDir)

	required := []string{
		"backend",
		"frontend",
		"k8s",
		"scripts/build.sh",
	}

	var missing []string
	for _, path := range required {
		fullPath := filepath.Join(workDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			missing = append(missing, path)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("payload missing required files/directories: %v", missing)
	}

	// Validate build.sh is executable
	scriptPath := filepath.Join(workDir, "scripts/build.sh")
	info, _ := os.Stat(scriptPath)
	if info.Mode()&0111 == 0 {
		v.Logger.Warning("build.sh is not executable, attempting to fix")
		if err := os.Chmod(scriptPath, 0755); err != nil {
			v.Logger.Warning("Failed to make build.sh executable: %v", err)
		} else {
			v.Logger.Debug("Made build.sh executable")
		}
	}

	v.Logger.Debug("Payload structure validation passed")
	return nil
}

// ValidatePayload performs comprehensive payload validation
func (v *Validator) ValidatePayload(workDir string) []ValidationError {
	v.Logger.Debug("Performing comprehensive payload validation at: %s", workDir)

	errors := []ValidationError{}

	// Check required directories
	requiredDirs := map[string]string{
		"backend":  "Backend application code",
		"frontend": "Frontend application code",
		"k8s":      "Kubernetes manifests",
		"scripts":  "Build and deployment scripts",
	}

	for dir, desc := range requiredDirs {
		path := filepath.Join(workDir, dir)
		if stat, err := os.Stat(path); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Path:    dir,
				Message: fmt.Sprintf("Missing %s directory", desc),
			})
		} else if !stat.IsDir() {
			errors = append(errors, ValidationError{
				Path:    dir,
				Message: fmt.Sprintf("%s exists but is not a directory", desc),
			})
		}
	}

	// Check required files
	requiredFiles := map[string]string{
		"scripts/build.sh":            "Build script for external builder",
		"backend/Dockerfile":          "Backend Dockerfile",
		"frontend/Dockerfile":         "Frontend Dockerfile",
		"k8s/namespace.yaml":          "Kubernetes namespace manifest",
		"k8s/backend/deployment.yaml": "Backend deployment manifest",
		"k8s/frontend/deployment.yaml": "Frontend deployment manifest",
		"k8s/backend/service.yaml":    "Backend service manifest",
		"k8s/frontend/service.yaml":   "Frontend service manifest",
	}

	for file, desc := range requiredFiles {
		path := filepath.Join(workDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Path:    file,
				Message: fmt.Sprintf("Missing %s", desc),
			})
		}
	}

	// Check build.sh is executable
	scriptPath := filepath.Join(workDir, "scripts/build.sh")
	if stat, err := os.Stat(scriptPath); err == nil {
		if stat.Mode()&0111 == 0 {
			errors = append(errors, ValidationError{
				Path:    "scripts/build.sh",
				Message: "Build script is not executable (needs chmod +x)",
			})
		}
	}

	if len(errors) == 0 {
		v.Logger.Debug("Comprehensive payload validation passed")
	} else {
		v.Logger.Debug("Payload validation found %d issues", len(errors))
	}

	return errors
}

// PrintValidationErrors prints validation errors in a formatted way
func (v *Validator) PrintValidationErrors(errors []ValidationError) {
	v.Logger.Error("Payload validation failed with %d error(s):", len(errors))
	for _, err := range errors {
		v.Logger.Error("  - %s: %s", err.Path, err.Message)
	}
	v.Logger.Info("")
	v.Logger.Info("Payload Requirements:")
	v.Logger.Info("  The application payload must contain:")
	v.Logger.Info("  - backend/          (Backend source code)")
	v.Logger.Info("  - frontend/         (Frontend source code)")
	v.Logger.Info("  - k8s/              (Kubernetes manifests)")
	v.Logger.Info("  - scripts/build.sh  (Build script for docker images)")
	v.Logger.Info("")
	v.Logger.Info("For complete requirements, see PAYLOAD_CONTRACT.md in the m2deploy repository.")
	v.Logger.Info("Reference implementation: https://github.com/wapsol/magnetiq2")
}
