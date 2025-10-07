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
