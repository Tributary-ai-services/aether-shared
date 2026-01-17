# Aether Application Kubernetes Manifests

This directory contains Kubernetes manifests for deploying the Aether application components in a Kubernetes cluster. These manifests are designed to work with the shared infrastructure services provided by the TAS platform.

## 📋 Prerequisites

1. **Shared Infrastructure**: The TAS shared infrastructure must be running (Redis, PostgreSQL, Kafka, Keycloak, MinIO, etc.)
2. **Docker Images**: Build the aether frontend and backend Docker images locally
3. **Kubernetes Cluster**: A running Kubernetes cluster (local or remote)
4. **kubectl**: Kubernetes CLI tool configured to access your cluster

## 🏗️ Architecture

The manifests define:

### Aether Frontend (`aether-frontend.yaml`)
- **Deployment**: Nginx-based React application container
- **ConfigMap**: Non-sensitive configuration (API URLs, Keycloak settings)
- **Secret**: Development credentials (username/password) and API keys
- **Services**: ClusterIP service for internal access + NodePort for local development

### Aether Backend (`aether-backend.yaml`)
- **Deployment**: Go-based API server
- **ConfigMap**: Non-sensitive configuration (database connections, service URLs)
- **Secret**: Sensitive data (Keycloak client secret, JWT secret)
- **Services**: ClusterIP service for internal access + NodePort for local development

## 🚀 Deployment

### 1. Build Docker Images

First, ensure the Docker images are built locally:

```bash
# Build frontend image
cd /path/to/aether
DOCKER_BUILDKIT=0 docker build -t aether_aether-frontend:latest \
  --build-arg VITE_DEV_USERNAME=john@scharber.com \
  --build-arg VITE_DEV_PASSWORD=test123 \
  --build-arg VITE_DEV_MODE=true \
  .

# Build backend image  
cd /path/to/aether-be
DOCKER_BUILDKIT=0 docker build -t aether-be_aether-backend:latest .
```

### 2. Deploy to Kubernetes

```bash
# Deploy both frontend and backend
kubectl apply -f aether-frontend.yaml
kubectl apply -f aether-backend.yaml

# Or deploy all at once
kubectl apply -f .
```

### 3. Verify Deployment

```bash
# Check pod status
kubectl get pods -n aether

# Check services
kubectl get services -n aether

# View logs
kubectl logs -f deployment/aether-frontend -n aether
kubectl logs -f deployment/aether-backend -n aether
```

## 🔧 Configuration

### Development Credentials

The frontend includes development credentials for automatic authentication:
- **Username**: `john@scharber.com` 
- **Password**: `test123`

These are stored in the `aether-frontend-dev-credentials` Secret and can be updated:

```bash
kubectl create secret generic aether-frontend-dev-credentials \
  --from-literal=VITE_DEV_USERNAME=your-username \
  --from-literal=VITE_DEV_PASSWORD=your-password \
  --namespace=aether \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Service Dependencies

The manifests assume the following shared infrastructure services are available:
- `tas-keycloak-shared:8080` - Keycloak authentication
- `tas-redis-shared:6379` - Redis cache
- `tas-minio-shared:9000` - MinIO object storage
- `tas-kafka-shared:29092` - Kafka messaging
- `neo4j:7687` - Neo4j graph database (deploy separately)

### API Endpoints

External access for local development:
- **Frontend**: `http://localhost:30001` (NodePort)
- **Backend**: `http://localhost:30080` (NodePort)

## 🔍 Troubleshooting

### Image Pull Issues
If using local images with `imagePullPolicy: Never`, ensure images are available on all cluster nodes:

```bash
# For local clusters (minikube, kind, etc.)
docker images | grep aether
```

### Service Discovery
Verify that shared infrastructure services are accessible:

```bash
# Test from within a pod
kubectl run debug --image=busybox -it --rm --restart=Never -n aether -- nslookup tas-keycloak-shared
```

### Configuration Issues
Check ConfigMap and Secret values:

```bash
kubectl describe configmap aether-frontend-config -n aether
kubectl describe secret aether-frontend-dev-credentials -n aether
```

## 🛠️ Customization

### Environment-Specific Configuration

For different environments (dev/staging/prod), create separate ConfigMaps:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: aether-frontend-config-prod
  namespace: aether
data:
  VITE_DEV_MODE: "false"
  VITE_KEYCLOAK_URL: "https://keycloak.prod.yourdomain.com"
  # ... other prod-specific values
```

### Scaling

Adjust replica counts for production:

```yaml
spec:
  replicas: 3  # Scale horizontally
```

### Resource Limits

Modify resource requests/limits based on your cluster capacity:

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "2Gi" 
    cpu: "1000m"
```

## 🔐 Security Notes

- **Development Mode**: These manifests include development credentials and should NOT be used in production
- **Secrets Management**: In production, use external secret management (e.g., Kubernetes External Secrets, HashiCorp Vault)
- **Network Policies**: Consider implementing NetworkPolicies to restrict pod-to-pod communication
- **RBAC**: Apply principle of least privilege for service accounts

## 📚 Related Documentation

- [TAS Shared Infrastructure](../README.md)
- [Kubernetes Migration Guide](../k8s-shared-infrastructure/KUBERNETES-MIGRATION-GUIDE.md)
- [Setup Guide](../k8s-shared-infrastructure/SETUP-GUIDE.md)