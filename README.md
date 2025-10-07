# m2deploy - Generic Web Application Deployment Tool

A comprehensive CLI tool for deploying, updating, and managing web applications on Kubernetes (k0s) clusters.

**Version:** 2.0.0

## Overview

m2deploy is a production-ready deployment orchestration tool that handles the complete lifecycle of web applications on Kubernetes. Originally built for Magnetiq2, it has evolved into a generic, reusable tool for any web application with backend/frontend architecture.

### Key Features

- 🚀 **Complete Deployment Pipeline**: Clone, build, deploy, and verify in one command
- 🔄 **Rolling Updates**: Update deployments with automatic database backups and migrations
- 🗄️ **Database Management**: Backup, restore, and run migrations
- ↩️ **Rollback Support**: Quickly rollback to previous versions
- 🧪 **Local Testing**: Test containers locally before deployment
- 🎯 **Selective Operations**: Target specific components (backend/frontend)
- 📊 **Health Verification**: Check deployment health and status
- 🔒 **SSL/TLS Automation**: Automatic certificate provisioning via cert-manager
- 🐳 **External Builder**: Resource-isolated builds prevent memory exhaustion
- 🧹 **Cleanup**: Remove old images and containers
- ⚙️ **Flexible Configuration**: YAML config file or command-line flags

### What's New in v2.0

✅ **Generic Application Support** - Deploy any web application, not just Magnetiq2
✅ **External Builder Architecture** - Prevents resource exhaustion during builds
✅ **SSL/TLS Automation** - Automatic Let's Encrypt certificates via cert-manager
✅ **Registry Authentication** - Built-in Docker registry login
✅ **Sudo Support** - Works with or without Docker group membership
✅ **Dynamic Configuration** - YAML manifest manipulation for multi-app deployments

See [Version 2.0 Changes](#version-20-changes) for migration details.

---

## For Application Developers

**Want to deploy your own application with m2deploy?**

See [PAYLOAD_CONTRACT.md](PAYLOAD_CONTRACT.md) for complete documentation on:
- Required directory structure (`backend/`, `frontend/`, `k8s/`, `scripts/`)
- The `build.sh` contract and interface requirements
- Dockerfile and Kubernetes manifest requirements
- Validation and troubleshooting
- Migration guide for existing applications

m2deploy is **generic** and can deploy any web application that follows the payload contract.

---

## Quick Start

### Prerequisites

- Go 1.23+ (for building from source)
- Docker installed and running
- k0s cluster running
- kubectl/k0s kubectl access
- Access to container registry (e.g., Harbor)

### Installation

```bash
# Build from source
cd /home/ubuntu/maint/m2deploy
go build -o m2deploy

# Install globally
sudo mv m2deploy /usr/local/bin/

# Verify installation
m2deploy --version
```

### 5-Minute Deployment

```bash
# Deploy everything with one command (first time - clones source code)
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh

# If not in docker group, add --use-sudo
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh --use-sudo
```

This command will:
1. Clone source code to `/tmp/wapsol/magnetiq2`
2. Build backend and frontend Docker images (using external builder)
3. Import images to k0s containerd
4. Deploy to k0s cluster
5. Run database migrations
6. Verify deployment health

### Update Existing Deployment

```bash
# Update with automatic backup and migration
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3

# Update specific component
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --component backend
```

### Verify Deployment

```bash
m2deploy verify
```

---

## Architecture

### Workspace Separation

m2deploy keeps tool code and application payloads completely separate:

```
/home/ubuntu/maint/m2deploy/      # Tool source code and binary
├── cmd/                          # Command implementations
├── pkg/                          # Packages (docker, k8s, database, git, etc.)
├── m2deploy                      # Compiled binary
└── README.md

/tmp/<username>/<repo-name>/      # Application payload (auto-derived from --repo-url)
├── backend/                      # Backend source code
├── frontend/                     # Frontend source code
├── k8s/                          # Kubernetes manifests
├── scripts/                      # Build scripts (build.sh)
└── ...

/var/log/m2deploy/                # Centralized logging
├── operations.log                # All command logs with context
└── build-*.log                   # External builder logs
```

**Workspace Path Derivation:**
- `https://github.com/wapsol/magnetiq2` → `/tmp/wapsol/magnetiq2`
- `https://github.com/user/myapp` → `/tmp/user/myapp`

### External Builder Architecture

m2deploy v2.0 uses an **external builder** to prevent resource exhaustion during Docker image builds.

#### The Problem

**Before (Inline Build):**
- Docker builds ran inside the m2deploy Go process
- Build output captured in memory buffers (`bytes.Buffer`)
- Large builds (Node.js/Python with many dependencies) caused:
  - Memory exhaustion (~500MB+ captured in RAM)
  - CPU contention
  - Process crashes (OOM kills)
  - System instability

#### The Solution

**After (External Build - Default):**
- Build process **delegated to a separate subprocess**
- Runs `/tmp/<user>/<repo>/scripts/build.sh` (from application payload)
- Output **streamed to log files** (not memory)
- Parent process stays lightweight (~50MB)
- Subprocess cleaned up automatically after build

#### Architecture Flow

```
┌────────────────────────────────────────────┐
│         m2deploy (Go process)              │
│                                            │
│  --external-build=true  (DEFAULT)          │
│         │                                  │
│         ├─> Spawns subprocess              │
│         └─> Calls: /tmp/wapsol/magnetiq2/ │
│             scripts/build.sh               │
│                                            │
│  Memory: ~50MB (unchanged)                 │
└────────────────────────────────────────────┘
                  │
                  ▼ Fork & Exec
        ┌─────────────────────┐
        │  build.sh process   │
        │  (Isolated)         │
        │                     │
        │  • Runs docker build│
        │  • Streams to file  │
        │  • Memory: ~500MB   │
        │  • Auto cleanup     │
        └─────────────────────┘
```

#### Benefits

✅ **Resource Isolation** - Build process can't exhaust m2deploy's memory
✅ **Stability** - No crashes during large builds
✅ **Observability** - Logs persisted to `/var/log/m2deploy/build-<component>-<tag>.log`
✅ **Performance** - No buffer copying overhead
✅ **Flexibility** - Easy to customize build.sh without recompiling m2deploy

#### Usage

External builder is **enabled by default**:

```bash
# Build with external builder (default)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2

# Disable external builder (not recommended for production)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --external-build=false
```

#### Build Script Interface

The external builder expects `scripts/build.sh` in the application payload with this interface:

```bash
./scripts/build.sh --component <backend|frontend|both> \
                   --tag <tag> \
                   --registry <prefix> \
                   --log-file <path> \
                   --target production \
                   [--sudo]
```

**Exit Codes:**
- `0` - Build successful
- `1` - Invalid arguments
- `2` - Build failed
- `3` - Docker not available

#### Build Logs

Build logs are written to:
- **Primary location:** `/var/log/m2deploy/build-<component>-<tag>.log`
- **Fallback location:** `/tmp/build-<component>-<tag>.log` (if /var/log not writable)

#### Troubleshooting External Builds

**Build Script Not Found:**
```bash
# Use --fresh to clone the repository
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh
```

**Permission Denied:**
```bash
# Add --use-sudo flag
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --use-sudo
```

---

## Commands Reference

### Image Operations

#### build

Build Docker images for backend, frontend, or both. Uses existing source code at `/tmp/<username>/<repo-name>`.

Images are built and remain in Docker for local testing. Use `deploy` command to import images to k0s and deploy.

**IMPORTANT**: Source code must exist at the workspace path before building. Use `--fresh` flag to clone from GitHub on first use, or to re-clone and overwrite existing code.

```bash
# Build from existing source code
m2deploy build --component backend --repo-url https://github.com/wapsol/magnetiq2

# Clone fresh and build (first time or re-clone)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh

# Clone from specific branch and build
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh --branch develop

# Build with custom tag
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3

# Build with sudo (if not in docker group)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --use-sudo
```

**Options:**
- `-c, --component` - Component to build: backend, frontend, or both (default: both)
- `-t, --tag` - Image tag (default: latest)
- `-b, --branch` - Git branch to use with --fresh (default: main)
- `--fresh` - Clone fresh code from GitHub (required for first use, overwrites existing)
- `--external-build` - Use external build script (default: true, recommended)

#### push

Push Docker images to the container registry.

```bash
m2deploy push --component backend --tag latest
m2deploy push --component both --tag v1.2.3 --retries 5
```

**Options:**
- `-c, --component` - Component to push: backend, frontend, or both (default: both)
- `-t, --tag` - Image tag (default: latest)
- `--retries` - Number of retry attempts (default: 3)

#### test

Run containers locally for testing before deployment.

```bash
m2deploy test --component backend --tag latest
m2deploy test --component both
m2deploy test --component frontend --skip-stop
```

**Options:**
- `-c, --component` - Component to test: backend, frontend, or both (default: both)
- `-t, --tag` - Image tag (default: latest)
- `--skip-stop` - Don't stop containers after test

---

### Deployment Operations

#### deploy

Deploy application to Kubernetes. Automatically imports Docker images to k0s before deploying.

```bash
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --validate --wait
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --skip-import
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --ingress-host myapp.example.com
```

**Options:**
- `--validate` - Validate manifests before applying
- `--wait` - Wait for deployments to be ready
- `--skip-import` - Skip importing Docker images to k0s (use if images already in k0s)
- `--ingress-host` - Ingress hostname (e.g., magnetiq2.voltaic.systems)
- `--tls-secret-name` - Custom TLS secret name
- `--cert-issuer` - cert-manager ClusterIssuer (default: letsencrypt-prod)
- `--disable-tls` - Deploy without TLS/HTTPS

#### update

Update existing deployment with rolling update.

```bash
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --branch develop --component backend
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --commit abc123 --auto-migrate=false
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --tag latest --component both --wait
```

**Options:**
- `-c, --component` - Component to update (default: both)
- `-t, --tag` - Image tag (default: latest)
- `-b, --branch` - Git branch to update from
- `--commit` - Specific commit SHA
- `--auto-migrate` - Run database migrations (default: true)
- `--backup-db` - Backup database before update (default: true)
- `--wait` - Wait for rollout completion (default: true)

#### rollback

Rollback to previous version.

```bash
m2deploy rollback --component backend
m2deploy rollback --component both --restore-db --backup-file ./backups/magnetiq-db-20240101.db.gz
```

**Options:**
- `-c, --component` - Component to rollback (default: both)
- `--restore-db` - Restore database from backup
- `--backup-file` - Database backup file (required if --restore-db)
- `--wait` - Wait for rollback completion (default: true)

#### undeploy

Remove deployment from Kubernetes.

```bash
m2deploy undeploy
m2deploy undeploy --keep-namespace
m2deploy undeploy --keep-pvcs --keep-namespace
m2deploy undeploy --force
```

**Options:**
- `--keep-namespace` - Don't delete the namespace
- `--keep-pvcs` - Preserve persistent volume claims (database data)
- `--force` - Skip confirmation prompt

---

### Database Operations

#### db backup

Backup the SQLite database.

```bash
m2deploy db backup
m2deploy db backup --path ./my-backups --compress
m2deploy db backup --path ./backups --retention 10
```

**Options:**
- `--path` - Backup directory path (default: ./backups)
- `--compress` - Compress backup file (default: true)
- `--retention` - Number of backups to keep (default: 5)

#### db restore

Restore database from backup.

```bash
m2deploy db restore --file ./backups/magnetiq-db-20240101-120000.db.gz
m2deploy db restore --file ./magnetiq-db.db
```

**Options:**
- `--file` - Backup file to restore (required)

#### db migrate

Run database migrations.

```bash
m2deploy db migrate
```

#### db status

Check migration status.

```bash
m2deploy db status
```

---

### Utility Operations

#### verify

Verify deployment health.

```bash
m2deploy verify
m2deploy verify --namespace magnetiq-v2
```

#### cleanup

Remove images and containers.

```bash
m2deploy cleanup --component backend --tag old-version
m2deploy cleanup --component both --local
m2deploy cleanup --tag v1.0.0 --local
```

**Options:**
- `-c, --component` - Component to cleanup (default: both)
- `-t, --tag` - Image tag (default: latest)
- `--local` - Remove local images (default: true)
- `--registry` - Remove from registry (dangerous!)

#### all

Run complete deployment pipeline: build, deploy, migrate, and verify.

**IMPORTANT**: Source code must exist before running. Use `--fresh` flag to clone from GitHub on first use, or to re-clone and overwrite existing code.

```bash
# Run with existing source code
m2deploy all --repo-url https://github.com/wapsol/magnetiq2

# Clone fresh and run complete pipeline (first time)
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh

# Deploy specific branch
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh --branch develop

# Deploy with custom tag
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --tag v1.0.0

# Skip final verification
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --skip-verify
```

**Options:**
- `-b, --branch` - Git branch to use with --fresh (default: main)
- `-t, --tag` - Image tag (default: latest)
- `--fresh` - Clone fresh code from GitHub (required for first use, overwrites existing)
- `--skip-verify` - Skip deployment verification

#### login

Authenticate to container registry.

```bash
# Interactive login (will prompt for password)
m2deploy login --username myuser

# From environment variable
m2deploy login --username myuser --password $REGISTRY_PASSWORD

# From stdin
echo $PASSWORD | m2deploy login --username myuser --password-stdin
```

**Options:**
- `--username` - Registry username (required)
- `--password` - Registry password
- `--password-stdin` - Read password from stdin

---

## Global Options

Available for all commands:

### Required
- `--repo-url` - Git repository URL (workspace auto-derived: /tmp/<user>/<repo>) **[REQUIRED for most commands]**

### Application Configuration
- `--app-name` - Application name (default: magnetiq)
- `--image-prefix` - Container image prefix (default: magnetiq/v2)
- `--k8s-dir` - Kubernetes manifests directory (default: k8s)

### Kubernetes
- `--namespace` - Kubernetes namespace (default: magnetiq-v2)
- `--kubeconfig` - Path to kubeconfig file (default: k0s)

### Docker/Registry
- `--use-sudo` - Use sudo for Docker and k0s commands (auto-detected when running as root)
- `--local-image-tag` - Tag for local images (default: latest)
- `--external-build` - Use external build script to prevent resource exhaustion (default: true, recommended)

### Ingress/TLS
- `--ingress-host` - Ingress hostname (e.g., magnetiq2.voltaic.systems)
- `--tls-secret-name` - Custom TLS secret name
- `--cert-issuer` - cert-manager ClusterIssuer (default: letsencrypt-prod)
- `--disable-tls` - Deploy without TLS/HTTPS

### General
- `--dry-run` - Show what would be done without executing
- `-v, --verbose` - Verbose output
- `--check` - Check prerequisites and exit without executing
- `--log-file` - Path to log file (default: /var/log/m2deploy/operations.log)
- `--no-log-file` - Disable file logging

---

## Configuration

### Configuration File

Create a `m2deploy.yaml` file to set default options:

```yaml
# Repository
repo-url: https://github.com/wapsol/magnetiq2

# Application
app-name: magnetiq
image-prefix: magnetiq/v2

# Registry
registry: crepo.re-cloud.io
registry-username: ""  # Optional
use-sudo: false        # Set true if not in docker group

# Kubernetes
namespace: magnetiq-v2
k8s-dir: k8s

# Ingress/TLS
ingress-host: magnetiq2.voltaic.systems
cert-issuer: letsencrypt-prod
disable-tls: false

# Build
external-build: true   # Always use external builder (recommended)

# General
dry-run: false
verbose: true
log-file: /var/log/m2deploy/operations.log
```

Then simply run:
```bash
m2deploy all
```

---

## Common Workflows

### Initial Deployment

```bash
# 1. Run everything at once (first time - requires --fresh to clone)
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh

# Or step by step:
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
m2deploy verify
```

### Update to New Version

```bash
# Update with automatic backup and migration
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --branch main --tag v1.2.3

# Or manually:
m2deploy db backup
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh --tag v1.2.3
m2deploy db migrate
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3
m2deploy verify
```

### Rollback After Failed Update

```bash
# Rollback deployment only
m2deploy rollback --component backend

# Rollback deployment and database
m2deploy rollback --component backend --restore-db --backup-file ./backups/magnetiq-db-20240101.db.gz
```

### Update Only Frontend

```bash
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --component frontend --tag v1.0.1
```

### Database Operations

```bash
# Backup database
m2deploy db backup --path ./backups

# Restore from backup
m2deploy db restore --file ./backups/magnetiq-db-20240101-120000.db.gz

# Run migrations
m2deploy db migrate

# Check migration status
m2deploy db status
```

### Testing Before Deployment

```bash
# Build images (leave in Docker)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2

# Test locally
m2deploy test --component both

# If tests pass, deploy (imports automatically)
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
```

### Deploy Different Application

```bash
# Deploy a different application with custom configuration
m2deploy all \
  --repo-url https://github.com/myorg/myapp \
  --app-name myapp \
  --image-prefix myapp/prod \
  --namespace myapp-production \
  --ingress-host myapp.example.com \
  --fresh
```

### SSL/TLS Certificate Provisioning

```bash
# Deploy with automatic Let's Encrypt certificate
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --ingress-host myapp.example.com

# This will:
# 1. Update ingress manifest with hostname
# 2. Generate TLS secret name (e.g., myapp-example-com-tls)
# 3. Add cert-manager annotations
# 4. Configure SSL redirect
# 5. cert-manager will automatically:
#    - Create Certificate resource
#    - Perform HTTP-01 ACME challenge
#    - Request certificate from Let's Encrypt
#    - Store cert in TLS secret
#    - Auto-renew every 60 days

# Check certificate status
sudo k0s kubectl -n magnetiq-v2 get certificate

# Wait for certificate to be ready (~2 minutes)
watch sudo k0s kubectl -n magnetiq-v2 get certificate

# Verify HTTPS is working
curl -I https://myapp.example.com
```

---

## Troubleshooting

### Check Deployment Status

```bash
m2deploy verify
```

### View Logs

```bash
# Backend logs
kubectl -n magnetiq-v2 logs -l app=magnetiq-backend --tail=100

# Frontend logs
kubectl -n magnetiq-v2 logs -l app=magnetiq-frontend --tail=100

# All pods
kubectl -n magnetiq-v2 logs --all-containers=true --tail=50

# Build logs (external builder)
cat /var/log/m2deploy/build-backend-latest.log
cat /var/log/m2deploy/build-frontend-latest.log

# Operations log
tail -f /var/log/m2deploy/operations.log
```

### Check Pod Status

```bash
kubectl -n magnetiq-v2 get pods
kubectl -n magnetiq-v2 describe pod <pod-name>
```

### Docker Permission Denied

**Problem:** Cannot access Docker
```
ERROR: Cannot access Docker (try USE_SUDO=true)
```

**Solution:**
```bash
# Option 1: Use --use-sudo flag
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --use-sudo

# Option 2: Add user to docker group (requires logout)
sudo usermod -aG docker $USER
newgrp docker

# Option 3: Set in config file
echo "use-sudo: true" >> m2deploy.yaml
```

### Build Script Not Found

**Problem:**
```
ERROR: build script not found: /tmp/wapsol/magnetiq2/scripts/build.sh
```

**Solution:**
```bash
# Use --fresh to clone the repository
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh
```

### Certificate Not Issuing

**Problem:** Certificate stays in "Pending" state

**Check cert-manager logs:**
```bash
kubectl -n cert-manager logs -l app=cert-manager --tail=50

# Check certificate status
kubectl -n magnetiq-v2 describe certificate

# Check certificate request
kubectl -n magnetiq-v2 get certificaterequest
```

**Common causes:**
- DNS not pointing to ingress IP yet
- Firewall blocking port 80 (needed for HTTP-01 challenge)
- Rate limit from Let's Encrypt (use staging issuer for testing: `--cert-issuer letsencrypt-staging`)

### Pods Not Starting

**Check pod status:**
```bash
kubectl -n magnetiq-v2 get pods
kubectl -n magnetiq-v2 describe pod <pod-name>
kubectl -n magnetiq-v2 logs <pod-name>
```

**Common causes:**
- Image pull errors (check registry credentials)
- Database migration failures
- Resource constraints
- Misconfigured environment variables

### Images Won't Push to Registry

**Solution:**
```bash
# Login again
m2deploy login --username YOUR_USERNAME

# Check registry is accessible
ping crepo.re-cloud.io

# Try pushing with more retries
m2deploy push --component both --retries 5
```

### Rollback Failed Deployment

```bash
m2deploy rollback --component backend
```

### Clean Up Failed Deployments

```bash
m2deploy undeploy --keep-pvcs
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
```

### Build Logs Not Accessible

**Problem:** Build succeeds but log file not found at `/var/log/m2deploy/`

**Reason:** Directory not writable

**Solution:** Logs automatically fall back to `/tmp/build-<component>-<tag>.log`

```bash
# Check fallback location
cat /tmp/build-backend-latest.log
```

### Source Code Not Found

**Problem:**
```
ERROR: workspace not found: /tmp/wapsol/magnetiq2
```

**Solution:**
```bash
# Use --fresh flag to clone it
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh
```

### Memory Exhaustion During Builds

**Problem:** m2deploy crashes during build with OOM error

**Solution:** This should not happen in v2.0 with external builder. If it does:

```bash
# Verify external builder is enabled (should be by default)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --verbose

# Look for: "Using external builder for backend"

# If using inline build, re-enable external builder
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --external-build=true
```

---

## Version 2.0 Changes

### Major Version Bump: v1.0.0 → v2.0.0

Version 2.0 represents a significant evolution from a Magnetiq2-specific tool to a **generic, reusable web application deployment tool**.

### What's New

#### 1. Generic Application Support
**Before:** Hard-coded for Magnetiq2
**After:** Works with any webapp via configuration

```bash
# Deploy different app
m2deploy all \
  --repo-url https://github.com/myorg/myapp \
  --app-name myapp \
  --image-prefix myapp/prod \
  --namespace myapp-production \
  --ingress-host myapp.example.com \
  --fresh
```

#### 2. External Builder Architecture
**Before:** Builds ran in-process, causing memory exhaustion
**After:** Isolated subprocess builds prevent crashes

- Default enabled: `--external-build=true`
- Runs `scripts/build.sh` in separate process
- Logs to files instead of memory
- Prevents OOM kills on large builds

#### 3. SSL/TLS Automation
**Before:** Manual ingress editing required
**After:** Automatic certificate provisioning

```bash
# Auto-configures TLS with Let's Encrypt
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --ingress-host myapp.example.com
```

#### 4. Registry Authentication
**Before:** Manual `docker login` required
**After:** Built-in secure authentication

```bash
m2deploy login --username myuser
```

#### 5. Flexible Docker Permissions
**Before:** Required docker group membership
**After:** Can use sudo for Docker commands

```bash
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --use-sudo
```

#### 6. Enhanced Configuration
**New global flags:**
- `--app-name` - Application name
- `--image-prefix` - Container image prefix
- `--k8s-dir` - K8s manifests directory
- `--ingress-host` - Ingress hostname
- `--tls-secret-name` - Custom TLS secret name
- `--cert-issuer` - cert-manager ClusterIssuer
- `--disable-tls` - Deploy without TLS/HTTPS
- `--external-build` - Use external build script (default: true)

#### 7. New Packages

- `pkg/manifest/` - YAML manifest manipulation for dynamic configuration
- `pkg/k8s/cert.go` - TLS certificate operations and monitoring
- `pkg/builder/` - External builder implementation
- Enhanced `pkg/docker/` - Sudo support, dynamic image naming

### Breaking Changes

1. **docker.NewClient() signature changed:**
   - Old: `NewClient(logger, dryRun, registry)`
   - New: `NewClient(logger, dryRun, registry, useSudo, imagePrefix)`

2. **Default image path uses `--image-prefix` flag:**
   - Old: `crepo.re-cloud.io/magnetiq/v2/backend:tag`
   - New: Uses `--image-prefix` (default still `magnetiq/v2`)

3. **Root command description:**
   - Changed from "Magnetiq2 Deployment Tool" to "Generic Web Application Deployment Tool"

4. **External builder enabled by default:**
   - Old: Inline builds (memory exhaustion risk)
   - New: External builds (default, recommended)

### Migration Guide

**For existing deployments:**

No changes required! External builder is backward compatible and enabled by default.

**If you experience issues:**

1. Ensure `scripts/build.sh` exists in your application payload
2. Use `--fresh` to re-clone if script is missing
3. Check build logs at `/var/log/m2deploy/build-*.log`

**To disable external builder (not recommended):**

```bash
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --external-build=false
```

### New Dependencies

- `gopkg.in/yaml.v3` - YAML manipulation
- `golang.org/x/term` - Secure password input

---

## Development

### Project Structure

```
m2deploy/
├── cmd/              # Command implementations
│   ├── root.go       # Root command and global flags
│   ├── all.go        # All command (complete pipeline)
│   ├── build.go      # Build command
│   ├── push.go       # Push command
│   ├── test.go       # Test command
│   ├── deploy.go     # Deploy command
│   ├── update.go     # Update command
│   ├── rollback.go   # Rollback command
│   ├── db.go         # Database commands
│   ├── undeploy.go   # Undeploy command
│   ├── cleanup.go    # Cleanup command
│   ├── verify.go     # Verify command
│   ├── login.go      # Registry login command
│   └── clients.go    # Client initialization
├── pkg/              # Package implementations
│   ├── config/       # Configuration and logging
│   ├── git/          # Git operations
│   ├── docker/       # Docker operations
│   ├── database/     # Database operations
│   ├── k8s/          # Kubernetes operations
│   │   ├── k8s.go
│   │   └── cert.go   # Certificate operations
│   ├── manifest/     # YAML manifest manipulation
│   └── builder/      # External builder
├── main.go           # Application entry point
├── go.mod            # Go module definition
├── go.sum            # Go module checksums
└── README.md         # This file
```

### Building

```bash
go build -o m2deploy
```

### Testing with Dry-Run

```bash
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --dry-run --verbose
```

### Running Tests

```bash
go test ./...
```

### Code Style

- Follow standard Go formatting: `gofmt -s -w .`
- Use meaningful variable names
- Add comments for exported functions
- Keep functions focused and small

---

## Future Enhancements

### Planned Features

- [ ] Parallel builds for multiple components (backend + frontend simultaneously)
- [ ] Resource limits for external builder (CPU/memory via cgroups)
- [ ] Build progress monitoring and real-time updates
- [ ] Support custom build scripts per component
- [ ] Implement retry logic for transient k8s failures
- [ ] Add health check endpoints for verification
- [ ] Support multi-cluster deployments
- [ ] Add metrics collection and reporting
- [ ] Build cache management and cleanup
- [ ] Webhook notifications on deployment completion
- [ ] Integration with CI/CD systems (GitHub Actions, GitLab CI)

### Known Issues

- **`--use-sudo` flag behavior**: Currently auto-detected when running as root, but Kubernetes commands always use sudo (hardcoded) while Docker/k0s commands are conditional. This inconsistency should be addressed:
  - Option 1: Make all commands consistent (either all respect flag or all auto-detect)
  - Option 2: Remove flag entirely and always auto-detect based on actual command requirements
  - Option 3: Detect if user is in docker group and skip sudo for Docker commands
  - Related: `pkg/k8s/k8s.go:41` always adds sudo, `pkg/docker/docker.go` is conditional

---

## Support

For issues and questions:
- GitHub Issues: https://github.com/wapsol/magnetiq2/issues
- Documentation: This README
- Logs: `/var/log/m2deploy/operations.log`

## License

MIT License

---

**Status:** ✅ Production Ready
**Version:** 2.0.0
**Last Updated:** 2025-10-07
