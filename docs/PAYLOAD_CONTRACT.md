# Payload Contract Documentation

This document defines the contract between **m2deploy** (the deployment tool) and **application payloads** (the repositories being deployed). Any application that wants to be deployed by m2deploy must conform to this contract.

## Overview

m2deploy is a **generic deployment tool** that can deploy any web application to Kubernetes (k0s) clusters. It expects application repositories to follow a specific structure and provide certain files and scripts.

### Separation of Concerns

- **m2deploy location**: `/home/ubuntu/maint/m2deploy/` (or wherever installed)
- **Application payload location**: `/tmp/<username>/<repo-name>/` (auto-derived from `--repo-url`)
- **Logs**: `/var/log/m2deploy/operations.log` and `/var/log/m2deploy/build-*.log`

m2deploy **never modifies itself** - it only operates on application payloads in the derived workspace.

## Required Directory Structure

Your application repository must have the following structure:

```
your-repo/
├── backend/              # Backend application code (REQUIRED)
│   ├── Dockerfile        # Backend Dockerfile (REQUIRED)
│   └── ...               # Your backend source code
│
├── frontend/             # Frontend application code (REQUIRED)
│   ├── Dockerfile        # Frontend Dockerfile (REQUIRED)
│   └── ...               # Your frontend source code
│
├── k8s/                  # Kubernetes manifests (REQUIRED)
│   ├── namespace.yaml    # Namespace definition (REQUIRED)
│   ├── backend/
│   │   ├── deployment.yaml  # Backend deployment (REQUIRED)
│   │   └── service.yaml     # Backend service (REQUIRED)
│   └── frontend/
│       ├── deployment.yaml  # Frontend deployment (REQUIRED)
│       └── service.yaml     # Frontend service (REQUIRED)
│
└── scripts/              # Build and deployment scripts (REQUIRED)
    └── build.sh          # Docker image builder (REQUIRED, executable)
```

### Optional Directories

```
your-repo/
├── docs/                 # Documentation (optional)
├── tests/                # Tests (optional)
└── db/                   # Database migrations/seeds (optional)
```

## The build.sh Contract

The most critical part of the contract is `scripts/build.sh`. This script is responsible for building Docker images and must conform to the following interface:

### Required Functionality

Your `build.sh` must:

1. **Accept command-line arguments** in the following format:
   ```bash
   ./scripts/build.sh \
     --component <backend|frontend|both> \
     --tag <image-tag> \
     --registry <registry-prefix> \
     --log-file <path-to-log-file> \
     --target <dockerfile-stage> \
     --network <network-mode> \
     [--sudo]
   ```

2. **Build Docker images** according to the parameters:
   - Use the `--component` to determine what to build
   - Tag images as: `<registry>/<component>:<tag>`
   - Support building both components when `--component both` is specified

3. **Use the correct Docker command prefix**:
   - If `--sudo` is passed, use `sudo docker` for all commands
   - Otherwise, use `docker` directly

4. **Write logs** to the specified `--log-file` (or stdout if not specified)

5. **Exit with status code 0** on success, non-zero on failure

6. **Be executable**: Must have `chmod +x scripts/build.sh` permissions

### Example Implementation Template

Here's a minimal `build.sh` that satisfies the contract:

```bash
#!/bin/bash
set -e  # Exit on error

# Default values
COMPONENT="both"
TAG="latest"
REGISTRY="myapp"
LOG_FILE=""
TARGET="production"
NETWORK=""
USE_SUDO=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --component) COMPONENT="$2"; shift 2 ;;
        --tag) TAG="$2"; shift 2 ;;
        --registry) REGISTRY="$2"; shift 2 ;;
        --log-file) LOG_FILE="$2"; shift 2 ;;
        --target) TARGET="$2"; shift 2 ;;
        --network) NETWORK="$2"; shift 2 ;;
        --sudo) USE_SUDO="sudo"; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Redirect output to log file if specified
if [[ -n "$LOG_FILE" ]]; then
    exec > "$LOG_FILE" 2>&1
fi

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Build function
build_component() {
    local component=$1
    local image_name="${REGISTRY}/${component}:${TAG}"

    echo "Building ${component}..."
    BUILD_CMD="$USE_SUDO docker build"
    [ -n "$NETWORK" ] && BUILD_CMD="$BUILD_CMD --network $NETWORK"
    BUILD_CMD="$BUILD_CMD --target $TARGET --tag $image_name"
    BUILD_CMD="$BUILD_CMD --file ${PROJECT_ROOT}/${component}/Dockerfile"
    BUILD_CMD="$BUILD_CMD ${PROJECT_ROOT}/${component}"

    eval $BUILD_CMD

    echo "Successfully built: ${image_name}"
}

# Build based on component
case $COMPONENT in
    backend)
        build_component "backend"
        ;;
    frontend)
        build_component "frontend"
        ;;
    both)
        build_component "backend"
        build_component "frontend"
        ;;
    *)
        echo "Invalid component: $COMPONENT"
        exit 1
        ;;
esac

echo "Build completed successfully"
exit 0
```

### How m2deploy Calls build.sh

When you run `m2deploy build` or `m2deploy all`, m2deploy will:

1. Clone/update your repository to `/tmp/<username>/<repo>/`
2. Validate that `scripts/build.sh` exists and is executable
3. Call your build.sh like this:

```bash
/tmp/<username>/<repo>/scripts/build.sh \
  --component backend \
  --tag latest \
  --registry magnetiq \
  --log-file /var/log/m2deploy/build-backend-latest.log \
  --target production \
  --network host \
  --sudo  # (if needed)
```

4. Monitor the build process and capture logs
5. Check the exit status (0 = success, non-zero = failure)

## Dockerfile Requirements

Your `backend/Dockerfile` and `frontend/Dockerfile` must:

1. **Support multi-stage builds** (recommended):
   ```dockerfile
   FROM node:18 AS base
   # ... base setup

   FROM base AS development
   # ... dev dependencies and setup

   FROM base AS production
   # ... production build
   ```

2. **Accept `--target` parameter** from build.sh to select the build stage

3. **Expose appropriate ports**:
   - Backend: typically 3000-4000 range
   - Frontend: typically 8000-9000 range

4. **Include health check** (recommended):
   ```dockerfile
   HEALTHCHECK --interval=30s --timeout=3s --start-period=60s \
     CMD curl -f http://localhost:3000/health || exit 1
   ```

## Kubernetes Manifest Requirements

Your Kubernetes manifests in `k8s/` must:

1. **Use consistent labeling**:
   ```yaml
   metadata:
     labels:
       app: myapp
       component: backend
   ```

2. **Define proper selectors** in deployments:
   ```yaml
   selector:
     matchLabels:
       app: myapp
       component: backend
   ```

3. **Use image pull policy**: `imagePullPolicy: Never` (for local k0s images)

4. **Configure resource limits** (recommended):
   ```yaml
   resources:
     requests:
       memory: "256Mi"
       cpu: "250m"
     limits:
       memory: "512Mi"
       cpu: "500m"
   ```

5. **Include readiness and liveness probes** (recommended):
   ```yaml
   livenessProbe:
     httpGet:
       path: /health
       port: 3000
     initialDelaySeconds: 30
     periodSeconds: 10

   readinessProbe:
     httpGet:
       path: /ready
       port: 3000
     initialDelaySeconds: 10
     periodSeconds: 5
   ```

## Validation

m2deploy includes a payload validator that checks:

### Basic Validation (Always Run)

- ✓ `backend/` directory exists
- ✓ `frontend/` directory exists
- ✓ `k8s/` directory exists
- ✓ `scripts/build.sh` exists and is executable

### Comprehensive Validation (Run with `--check`)

All basic checks plus:
- ✓ `backend/Dockerfile` exists
- ✓ `frontend/Dockerfile` exists
- ✓ `k8s/namespace.yaml` exists
- ✓ `k8s/backend/deployment.yaml` exists
- ✓ `k8s/backend/service.yaml` exists
- ✓ `k8s/frontend/deployment.yaml` exists
- ✓ `k8s/frontend/service.yaml` exists
- ✓ `scripts/build.sh` has executable permissions

### Running Validation

```bash
# Basic validation (runs automatically during build/deploy)
m2deploy build --component both --repo-url https://github.com/user/repo --check

# Comprehensive validation
m2deploy build --component both --repo-url https://github.com/user/repo --check
```

## Configuration via m2deploy Flags

Your payload doesn't need to hardcode values - m2deploy passes configuration through command-line arguments:

| m2deploy Flag | Passed to build.sh as | Purpose |
|---------------|----------------------|---------|
| `--image-prefix` | `--registry` | Container image prefix (e.g., "magnetiq", "myapp/prod") |
| `--local-image-tag` or `--tag` | `--tag` | Image tag (default: git commit SHA or "latest") |
| `--use-sudo` | `--sudo` | Whether to use sudo for Docker/k0s commands |
| (auto-derived) | `--log-file` | Where to write build logs |
| (hardcoded) | `--target production` | Dockerfile build stage |
| (hardcoded) | `--network host` | Docker network mode for builds |

## Example: magnetiq2 Payload

The reference implementation is the magnetiq2 repository:

```bash
# Clone the reference
git clone https://github.com/wapsol/magnetiq2 /tmp/reference

# Examine the structure
tree /tmp/reference -L 2

# Study the build.sh contract
cat /tmp/reference/scripts/build.sh
```

Key features of magnetiq2's implementation:
- **Backward compatibility**: Supports both legacy (`./build.sh dev`) and modern (`--component`) modes
- **Comprehensive logging**: Timestamps, colors, detailed error messages
- **Docker build optimization**: Multi-stage builds, layer caching
- **Flexible configuration**: Environment-specific settings

## Troubleshooting

### "Payload validation failed"

Check that your repository has all required directories and files:
```bash
cd /tmp/username/repo
ls -la backend/ frontend/ k8s/ scripts/
```

### "build script not found"

Ensure `scripts/build.sh` exists and is executable:
```bash
chmod +x scripts/build.sh
```

### "Build script failed"

Check the build logs for detailed error messages:
```bash
cat /var/log/m2deploy/build-<component>-<tag>.log
```

### "Image not found in k0s"

Verify your build.sh actually created the image:
```bash
sudo docker images | grep <registry>/<component>
```

## Best Practices

1. **Test your build.sh locally** before using with m2deploy:
   ```bash
   cd /tmp/your-repo
   ./scripts/build.sh --component both --tag test --registry myapp
   ```

2. **Use multi-stage Docker builds** to keep images small

3. **Include comprehensive logging** in your build.sh for debugging

4. **Version your Kubernetes manifests** alongside your code

5. **Document any custom configuration** your app needs

6. **Include a README.md** explaining how to deploy with m2deploy

## Migration Guide

If you have an existing application, here's how to make it compatible with m2deploy:

### Step 1: Organize Directory Structure
```bash
mkdir -p backend frontend k8s/backend k8s/frontend scripts
mv <your-backend-code> backend/
mv <your-frontend-code> frontend/
```

### Step 2: Create Dockerfiles
- Add `backend/Dockerfile`
- Add `frontend/Dockerfile`

### Step 3: Create Kubernetes Manifests
- Add `k8s/namespace.yaml`
- Add `k8s/backend/deployment.yaml` and `service.yaml`
- Add `k8s/frontend/deployment.yaml` and `service.yaml`

### Step 4: Create build.sh
Use the template above or adapt from magnetiq2

### Step 5: Test Locally
```bash
# Test validation
m2deploy build --component both --repo-url file:///path/to/repo --check

# Test build
m2deploy build --component both --repo-url file:///path/to/repo --dry-run
```

### Step 6: Deploy
```bash
m2deploy all --repo-url https://github.com/user/repo --fresh
```

## Contract Version

This contract is for **m2deploy v2.0.0** and later.

For questions or issues, see: https://github.com/wapsol/m2deploy

---

**Last Updated**: 2025-10-07
**Contract Version**: 2.0.0
