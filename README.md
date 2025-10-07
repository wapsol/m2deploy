# m2deploy - Magnetiq2 Deployment Tool

A comprehensive CLI tool for deploying, updating, and managing the Magnetiq2 application on Kubernetes (k0s) clusters.

## Features

- 🚀 **Complete Deployment Pipeline**: Clone, build, push, deploy, and verify in one command
- 🔄 **Rolling Updates**: Update deployments with automatic database backups and migrations
- 🗄️ **Database Management**: Backup, restore, and run migrations
- ↩️ **Rollback Support**: Quickly rollback to previous versions
- 🧪 **Local Testing**: Test containers locally before deployment
- 🎯 **Selective Operations**: Target specific components (backend/frontend)
- 📊 **Health Verification**: Check deployment health and status
- 🧹 **Cleanup**: Remove old images and containers

## Installation

### Prerequisites

- Go 1.23+ installed
- Docker installed and running
- k0s cluster running
- kubectl/k0s kubectl access
- Access to Harbor registry (crepo.re-cloud.io)

### Build from Source

```bash
cd m2deploy
go build -o m2deploy
sudo mv m2deploy /usr/local/bin/
```

### Directory Structure

m2deploy keeps tool code and application payloads completely separate:

```
/home/ubuntu/maint/m2deploy/      # Tool source code and binary
├── cmd/                          # Command implementations
├── pkg/                          # Packages
├── m2deploy                      # Binary (before installation)
└── README.md

/tmp/m2deploy-workspaces/         # Application payloads (temporary)
└── magnetiq2/                    # Cloned application code

/var/log/m2deploy/                # Centralized logging
└── operations.log                # All command logs with context
```

**Note**: Application payloads are cloned to `/tmp/m2deploy-workspaces/` by default, keeping them separate from the tool. Use `--work-dir` to specify a different location.

## Quick Start

### Deploy Everything (First Time)

```bash
# Run the complete pipeline
m2deploy all

# Or run individual steps
m2deploy clone
m2deploy build --component both
m2deploy push --component both
m2deploy deploy
m2deploy verify
```

### Update Existing Deployment

```bash
# Update with automatic backup and migration
m2deploy update --tag v1.2.3

# Update specific component
m2deploy update --component backend --branch develop

# Update without auto-migration
m2deploy update --tag latest --auto-migrate=false
```

### Rollback

```bash
# Rollback deployment
m2deploy rollback --component backend

# Rollback with database restore
m2deploy rollback --restore-db --backup-file ./backups/magnetiq-db-20240101-120000.db.gz
```

## Commands

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

# Build then deploy (deploy imports images automatically)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
```

**Workspace Path**: Automatically derived from `--repo-url`:
- `https://github.com/wapsol/magnetiq2` → `/tmp/wapsol/magnetiq2`
- `https://github.com/user/myapp` → `/tmp/user/myapp`

**Options:**
- `-c, --component` - Component to build: backend, frontend, or both (required)
- `-t, --tag` - Image tag (default: commit SHA)
- `-b, --branch` - Git branch to use with --fresh (default: main)
- `--fresh` - Clone fresh code from GitHub (required for first use, overwrites existing)

#### push
Push Docker images to the container registry.

```bash
m2deploy push --component backend --tag latest
m2deploy push --component both --tag v1.2.3 --retries 5
```

**Options:**
- `-c, --component` - Component to push: backend, frontend, or both (required)
- `-t, --tag` - Image tag (default: commit SHA)
- `--retries` - Number of retry attempts (default: 3)

#### test
Run containers locally for testing.

```bash
m2deploy test --component backend --tag latest
m2deploy test --component both
m2deploy test --component frontend --skip-stop
```

**Options:**
- `-c, --component` - Component to test: backend, frontend, or both (required)
- `-t, --tag` - Image tag (default: commit SHA)
- `--skip-stop` - Don't stop containers after test

### Deployment Operations

#### deploy
Deploy Magnetiq2 to Kubernetes. Automatically imports Docker images to k0s before deploying.

```bash
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --validate --wait
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2 --skip-import
```

**Options:**
- `--validate` - Validate manifests before applying
- `--wait` - Wait for deployments to be ready
- `--skip-import` - Skip importing Docker images to k0s (use if images already in k0s)

#### update
Update existing deployment with rolling update.

```bash
m2deploy update --tag v1.2.3
m2deploy update --branch develop --component backend
m2deploy update --commit abc123 --auto-migrate=false
m2deploy update --tag latest --component both --wait
```

**Options:**
- `-c, --component` - Component to update (default: both)
- `-t, --tag` - Image tag (default: commit SHA)
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
- `-t, --tag` - Image tag (default: commit SHA)
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

## Global Options

Available for all commands:

- `--repo-url` - Git repository URL (workspace auto-derived: /tmp/<user>/<repo>) **[REQUIRED]**
- `--namespace` - Kubernetes namespace (default: magnetiq-v2)
- `--kubeconfig` - Path to kubeconfig file (default: k0s)
- `--use-sudo` - Use sudo for Docker and k0s commands
- `--dry-run` - Show what would be done without executing
- `-v, --verbose` - Verbose output
- `--check` - Check prerequisites and exit without executing

## Configuration File

Create a `m2deploy.yaml` file to set default options:

```yaml
repo-url: https://github.com/wapsol/magnetiq2
namespace: magnetiq-v2
verbose: true
```

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

## Troubleshooting

### Check Deployment Status

```bash
m2deploy verify
```

### View Logs

```bash
kubectl -n magnetiq-v2 logs -l app=magnetiq-backend --tail=100
kubectl -n magnetiq-v2 logs -l app=magnetiq-frontend --tail=100
```

### Check Pod Status

```bash
kubectl -n magnetiq-v2 get pods
kubectl -n magnetiq-v2 describe pod <pod-name>
```

### Rollback Failed Deployment

```bash
m2deploy rollback --component backend
```

### Clean Up Failed Deployments

```bash
m2deploy undeploy --keep-pvcs
m2deploy deploy
```

## Development

### Project Structure

```
m2deploy/
├── cmd/              # Command implementations
│   ├── root.go       # Root command and global flags
│   ├── clone.go      # Clone command
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
│   └── all.go        # All command
├── pkg/              # Package implementations
│   ├── config/       # Configuration and logging
│   ├── git/          # Git operations
│   ├── docker/       # Docker operations
│   ├── database/     # Database operations
│   └── k8s/          # Kubernetes operations
├── main.go           # Application entry point
├── go.mod            # Go module definition
└── README.md         # This file
```

### Building

```bash
go build -o m2deploy
```

### Testing with Dry-Run

```bash
m2deploy all --dry-run --verbose
```

## License

MIT License

## Todos

Future improvements and known issues:

- [ ] **Rethink `--use-sudo` flag behavior**: Currently auto-detected when running as root, but Kubernetes commands always use sudo (hardcoded) while Docker/k0s commands are conditional. This inconsistency should be addressed:
  - Option 1: Make all commands consistent (either all respect flag or all auto-detect)
  - Option 2: Remove flag entirely and always auto-detect based on actual command requirements
  - Option 3: Detect if user is in docker group and skip sudo for Docker commands
  - Related: `pkg/k8s/k8s.go:41` always adds sudo, `pkg/docker/docker.go` is conditional

- [ ] Add parallel builds for multiple components (backend + frontend simultaneously)
- [ ] Implement resource limits for external builder (CPU/memory via cgroups)
- [ ] Add build progress monitoring and real-time updates
- [ ] Support custom build scripts per component
- [ ] Add configuration file support (m2deploy.yaml) - partially implemented
- [ ] Implement retry logic for transient k8s failures
- [ ] Add health check endpoints for verification
- [ ] Support multi-cluster deployments
- [ ] Add metrics collection and reporting

## Support

For issues and questions, visit: https://github.com/wapsol/magnetiq2/issues
