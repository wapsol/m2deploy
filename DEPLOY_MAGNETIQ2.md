# Deploy Magnetiq2 to k0s - Step by Step Guide

## Prerequisites Check

```bash
# 1. Verify m2deploy is installed
m2deploy --version
# Should show: m2deploy version 2.0.0

# 2. Verify k0s cluster is running
sudo k0s kubectl get nodes
# Should show 5 ready nodes

# 3. Verify cert-manager is running
sudo k0s kubectl get pods -n cert-manager
# Should show cert-manager pods running

# 4. Verify Harbor registry secret exists
sudo k0s kubectl get secret harbor-regcred -n default
# Should show: harbor-regcred

# 5. Check if you're in docker group
groups | grep docker
# If not, you'll need to use --use-sudo flag
```

## Option 1: Quick Deploy (One Command)

```bash
# Deploy with one command
m2deploy all \
  --ingress-host magnetiq2.voltaic.systems \
  --verbose

# If not in docker group, add --use-sudo:
m2deploy all \
  --ingress-host magnetiq2.voltaic.systems \
  --use-sudo \
  --verbose
```

## Option 2: Step-by-Step Deploy (For Troubleshooting)

### Step 1: Login to Harbor Registry

```bash
# Interactive login (will prompt for password)
m2deploy login --username YOUR_HARBOR_USERNAME

# Or with password from environment
export HARBOR_PASSWORD="your-password"
m2deploy login --username YOUR_HARBOR_USERNAME --password $HARBOR_PASSWORD
```

### Step 2: Clone Repository

```bash
m2deploy clone --verbose

# Verify clone succeeded
ls -la magnetiq2/
```

### Step 3: Build Docker Images

```bash
# Build both backend and frontend
m2deploy build --component both --verbose

# Or build individually
m2deploy build --component backend --verbose
m2deploy build --component frontend --verbose

# With sudo if needed
m2deploy build --component both --use-sudo --verbose
```

### Step 4: Test Containers Locally (Optional)

```bash
# Test containers before pushing
m2deploy test --component both --verbose

# This will:
# 1. Run containers on localhost
# 2. Show logs
# 3. Automatically stop containers
```

### Step 5: Push Images to Harbor

```bash
m2deploy push --component both --verbose

# With sudo if needed
m2deploy push --component both --use-sudo --verbose
```

### Step 6: Deploy to Kubernetes

```bash
m2deploy deploy \
  --ingress-host magnetiq2.voltaic.systems \
  --verbose

# This will:
# 1. Update ingress hostname in manifests
# 2. Configure TLS with Let's Encrypt
# 3. Copy registry secret to namespace
# 4. Deploy all resources in correct order
```

### Step 7: Verify Deployment

```bash
# Check deployment status
m2deploy verify

# Check pods
sudo k0s kubectl -n magnetiq-v2 get pods

# Check ingress
sudo k0s kubectl -n magnetiq-v2 get ingress

# Check certificate (wait ~2 minutes for issuance)
sudo k0s kubectl -n magnetiq-v2 get certificate

# Check certificate details
sudo k0s kubectl -n magnetiq-v2 describe certificate
```

## DNS Configuration

### Point DNS to Ingress

```bash
# Get ingress IP
sudo k0s kubectl -n magnetiq-v2 get ingress magnetiq-ingress -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# Or if using hostname
sudo k0s kubectl -n magnetiq-v2 get ingress magnetiq-ingress -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

Then create DNS A record:
```
magnetiq2.voltaic.systems → [INGRESS_IP]
```

## Verify SSL Certificate

```bash
# Wait 2-3 minutes for cert-manager to issue certificate
watch sudo k0s kubectl -n magnetiq-v2 get certificate

# When Ready=True, test HTTPS
curl -I https://magnetiq2.voltaic.systems

# Check certificate details
echo | openssl s_client -connect magnetiq2.voltaic.systems:443 2>/dev/null | openssl x509 -noout -dates
```

## Test Application

```bash
# Test backend health endpoint
curl https://magnetiq2.voltaic.systems/health

# Test API docs
curl https://magnetiq2.voltaic.systems/docs

# Test frontend
curl -I https://magnetiq2.voltaic.systems/

# Open in browser
# https://magnetiq2.voltaic.systems
```

## Troubleshooting

### Issue: Docker permission denied

**Solution:**
```bash
# Add --use-sudo to commands
m2deploy build --component both --use-sudo

# Or add user to docker group (requires logout)
sudo usermod -aG docker $USER
newgrp docker
```

### Issue: Certificate not issuing

**Check cert-manager logs:**
```bash
sudo k0s kubectl -n cert-manager logs -l app=cert-manager --tail=50

# Check certificate status
sudo k0s kubectl -n magnetiq-v2 describe certificate

# Check certificate request
sudo k0s kubectl -n magnetiq-v2 get certificaterequest
```

**Common causes:**
- DNS not pointing to ingress IP yet
- Firewall blocking port 80 (needed for HTTP-01 challenge)
- Rate limit from Let's Encrypt (use staging issuer for testing)

### Issue: Pods not starting

**Check pod status:**
```bash
sudo k0s kubectl -n magnetiq-v2 get pods
sudo k0s kubectl -n magnetiq-v2 describe pod POD_NAME
sudo k0s kubectl -n magnetiq-v2 logs POD_NAME
```

**Common causes:**
- Image pull errors (check registry credentials)
- Database migration failures
- Resource constraints

### Issue: Images won't push to registry

**Solution:**
```bash
# Login again
m2deploy login --username YOUR_USERNAME

# Check registry is accessible
ping crepo.re-cloud.io

# Try pushing with more retries
m2deploy push --component both --retries 5
```

## Update Deployment

### Update to New Version

```bash
# Pull latest code and deploy
m2deploy update --verbose

# Or specify tag
m2deploy update --tag v1.2.3 --verbose
```

### Rollback if Needed

```bash
# Rollback to previous version
m2deploy rollback --component both --verbose

# With database restore
m2deploy rollback \
  --restore-db \
  --backup-file ./backups/magnetiq-db-TIMESTAMP.db.gz
```

## Database Operations

### Backup Database

```bash
# Backup current database
m2deploy db backup --verbose

# Backups stored in ./backups/
ls -lh backups/
```

### Run Migrations

```bash
# Run database migrations
m2deploy db migrate

# Check migration status
m2deploy db status
```

## Monitoring

### Check Deployment Health

```bash
# Quick health check
m2deploy verify

# Detailed pod status
sudo k0s kubectl -n magnetiq-v2 get pods -o wide

# Watch pod status
watch sudo k0s kubectl -n magnetiq-v2 get pods

# Check resource usage
sudo k0s kubectl -n magnetiq-v2 top pods
```

### View Logs

```bash
# Backend logs
sudo k0s kubectl -n magnetiq-v2 logs -l app=magnetiq-backend --tail=100 -f

# Frontend logs
sudo k0s kubectl -n magnetiq-v2 logs -l app=magnetiq-frontend --tail=100 -f

# All pods
sudo k0s kubectl -n magnetiq-v2 logs --all-containers=true --tail=50
```

## Cleanup/Undeploy

### Remove Deployment (Keep Data)

```bash
m2deploy undeploy --keep-pvcs --verbose
```

### Complete Removal

```bash
# WARNING: This deletes all data!
m2deploy undeploy --force
```

## Configuration File

Create `m2deploy.yaml` for easier deployments:

```yaml
# m2deploy.yaml
repo-url: https://github.com/wapsol/magnetiq2
work-dir: ./magnetiq2
app-name: magnetiq
image-prefix: magnetiq/v2
registry: crepo.re-cloud.io
namespace: magnetiq-v2
ingress-host: magnetiq2.voltaic.systems
cert-issuer: letsencrypt-prod
use-sudo: false  # Set true if needed
verbose: true
```

Then simply run:
```bash
m2deploy all
```

## Success Checklist

- [ ] m2deploy v2.0.0 installed
- [ ] Logged into Harbor registry
- [ ] Repository cloned
- [ ] Images built successfully
- [ ] Images pushed to registry
- [ ] Deployed to k0s
- [ ] All pods running
- [ ] Certificate issued (Ready=True)
- [ ] DNS pointing to ingress
- [ ] HTTPS working
- [ ] Backend API responding
- [ ] Frontend accessible
- [ ] Database migrations completed

## Support

For issues:
1. Check this guide's troubleshooting section
2. Run `m2deploy verify` for deployment status
3. Check pod logs
4. Review GitHub issues: https://github.com/wapsol/magnetiq2/issues
