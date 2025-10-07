# m2deploy Quick Start Guide

## Prerequisites Checklist

- ✅ k0s cluster is running
- ✅ Docker is installed and running
- ✅ Access to Harbor registry (crepo.re-cloud.io)
- ✅ Harbor credentials configured (harbor-regcred secret in default namespace)
- ✅ m2deploy installed: `which m2deploy`

## First Deployment

### Option 1: One Command Deploy (Recommended)

```bash
# Deploy everything with one command (requires --fresh to clone source code first time)
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --fresh
```

This will:
1. Clone source code to /tmp/wapsol/magnetiq2
2. Build backend and frontend Docker images
3. Import images to k0s containerd
4. Deploy to k0s cluster
5. Run database migrations
6. Verify deployment health

### Option 2: Step-by-Step Deploy

```bash
# 1. Build images (requires --fresh to clone first time)
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh

# 2. Deploy to k8s (automatically imports images)
m2deploy deploy --repo-url https://github.com/wapsol/magnetiq2

# 3. Run migrations
m2deploy db migrate

# 4. Verify deployment
m2deploy verify
```

## Update Deployment

### Update to Latest Code

```bash
# Update with automatic backup and migration
m2deploy update --repo-url https://github.com/wapsol/magnetiq2
```

### Update to Specific Version

```bash
# Tag with specific version
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --tag v1.2.3
```

### Update Only Backend or Frontend

```bash
# Backend only
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --component backend

# Frontend only
m2deploy update --repo-url https://github.com/wapsol/magnetiq2 --component frontend
```

## Database Operations

### Backup Database

```bash
m2deploy db backup
```

### Restore from Backup

```bash
m2deploy db restore --file ./backups/magnetiq-db-20240101-120000.db.gz
```

### Run Migrations

```bash
m2deploy db migrate
```

### Check Migration Status

```bash
m2deploy db status
```

## Rollback

### Rollback Deployment

```bash
# Rollback to previous version
m2deploy rollback

# Rollback with database restore
m2deploy rollback --restore-db --backup-file ./backups/magnetiq-db-20240101.db.gz
```

## Verify Deployment

```bash
# Check deployment health
m2deploy verify
```

## Undeploy

```bash
# Remove deployment (preserves data)
m2deploy undeploy --keep-pvcs

# Complete removal
m2deploy undeploy --force
```

## Common Issues

### Issue: Images fail to push to registry

**Solution**: Make sure you're logged into Docker registry
```bash
docker login crepo.re-cloud.io
```

### Issue: Pods not starting

**Solution**: Check pod logs and events
```bash
m2deploy verify
sudo k0s kubectl -n magnetiq-v2 get pods
sudo k0s kubectl -n magnetiq-v2 logs -l app=magnetiq-backend --tail=50
```

### Issue: Migration fails

**Solution**: Run migrations manually
```bash
m2deploy db migrate
```

### Issue: Want to test before deploying

**Solution**: Use dry-run mode
```bash
m2deploy all --repo-url https://github.com/wapsol/magnetiq2 --dry-run --verbose
```

### Issue: Source code not found

**Solution**: Use --fresh flag to clone it
```bash
m2deploy build --component both --repo-url https://github.com/wapsol/magnetiq2 --fresh
```

## Configuration

### Directory Structure

m2deploy separates tool code from application payloads:

- **Tool**: `/home/ubuntu/maint/m2deploy/` (source code and binary)
- **Workspace**: `/tmp/<username>/<repo-name>` (auto-derived from `--repo-url`)
  - Example: `https://github.com/wapsol/magnetiq2` → `/tmp/wapsol/magnetiq2`
- **Logs**: `/var/log/m2deploy/operations.log` (centralized logging)

Workspace paths are automatically calculated based on the repository URL.

### Optional Config File

Create a `m2deploy.yaml` file in your working directory:

```yaml
repo-url: https://github.com/wapsol/magnetiq2
namespace: magnetiq-v2
verbose: true
```

## Next Steps

1. Access your application at: https://magnetiq2.voltaic.systems (after DNS/Ingress configuration)
2. Monitor with: `m2deploy verify`
3. Set up regular backups: `m2deploy db backup`
4. Update regularly: `m2deploy update`

## Getting Help

```bash
# General help
m2deploy --help

# Command-specific help
m2deploy deploy --help
m2deploy update --help
m2deploy db --help
```

## Important Notes

- **--repo-url is required** for all commands that use source code
- Use `--fresh` flag to clone source code on first use
- Always backup before updates: `m2deploy db backup`
- Use `--dry-run` to preview actions: `m2deploy update --repo-url <url> --dry-run`
- Database backups are stored in `./backups/` by default
- Images are tagged with commit SHA by default
- Application code workspace is auto-derived: `/tmp/<user>/<repo>/` from repository URL
- Use `--fresh` flag to re-clone and overwrite existing code
