# Kubernetes Migration Guide: Using Shared Services

This guide explains how to modify existing Kubernetes manifests to use the shared infrastructure services instead of individual service deployments.

## Overview

The shared infrastructure creates services in the `tas-shared` namespace that can be accessed by applications in other namespaces using service names:

### Core Infrastructure Services
- `redis-shared.tas-shared.svc.cluster.local` (short: `redis-shared.tas-shared`)
- `postgres-shared.tas-shared.svc.cluster.local` (short: `postgres-shared.tas-shared`)
- `kafka-shared.tas-shared.svc.cluster.local` (short: `kafka-shared.tas-shared`)
- `minio-shared.tas-shared.svc.cluster.local` (short: `minio-shared.tas-shared`)
- `keycloak-shared.tas-shared.svc.cluster.local` (short: `keycloak-shared.tas-shared`)

### Monitoring & Observability Services  
- `prometheus-shared.tas-shared.svc.cluster.local` (short: `prometheus-shared.tas-shared`)
- `grafana-shared.tas-shared.svc.cluster.local` (short: `grafana-shared.tas-shared`)
- `alertmanager-shared.tas-shared.svc.cluster.local` (short: `alertmanager-shared.tas-shared`)
- `otel-collector-shared.tas-shared.svc.cluster.local` (short: `otel-collector-shared.tas-shared`)

### Management Services
- `pgadmin-shared.tas-shared.svc.cluster.local` (short: `pgadmin-shared.tas-shared`)
- `dashboard-shared.tas-shared.svc.cluster.local` (short: `dashboard-shared.tas-shared`)

### External Access (via Ingress)
- `https://dashboard.tas.yourdomain.com` - Services Dashboard
- `https://grafana.tas.yourdomain.com` - Grafana Monitoring
- `https://prometheus.tas.yourdomain.com` - Prometheus Metrics
- `https://keycloak.tas.yourdomain.com` - Identity Management  
- `https://pgadmin.tas.yourdomain.com` - Database Administration
- `https://minio.tas.yourdomain.com` - MinIO Console
- `https://minio-api.tas.yourdomain.com` - MinIO API
- `https://alerts.tas.yourdomain.com` - AlertManager

## Step-by-Step Migration

### 1. Remove Redundant Service Deployments

**Before**: Each repository had its own Redis, Prometheus, etc.
**After**: Remove these deployments and reference shared services.

#### Files to Remove/Modify:

**aether-be repository:**
- Remove: `deployments/base/redis.yaml` (entire file)
- Modify: `deployments/base/configmap.yaml` - Update Redis host
- Modify: `deployments/base/deployment.yaml` - Update environment variables

**audimodal repository:**
- Remove: `deployments/kubernetes/statefulsets.yaml` - Redis StatefulSet section
- Modify: `deployments/kubernetes/configmap.yaml` - Update Redis host
- Modify: `deployments/kubernetes/deployment.yaml` - Update environment variables

**deeplake-api repository:**
- Modify: `deployment/kubernetes/configmap.yaml` - Update Redis URL

**tas-mcp repository:**
- Remove redundant service definitions
- Update service references

### 2. Update Environment Variables

#### Example ConfigMap Changes:

**Before:**
```yaml
# aether-be/deployments/base/configmap.yaml
data:
  REDIS_ADDR: "redis:6379"
  KEYCLOAK_URL: "http://keycloak:8080"
  S3_ENDPOINT: "http://minio:9000"
  KAFKA_BROKERS: "kafka:9092"
```

**After:**
```yaml
# aether-be/deployments/base/configmap.yaml
data:
  REDIS_ADDR: "redis-shared.tas-shared:6379"
  KEYCLOAK_URL: "http://keycloak-shared.tas-shared:8080"
  S3_ENDPOINT: "http://minio-shared.tas-shared:9000"
  KAFKA_BROKERS: "kafka-shared.tas-shared:9092"
```

#### Full Cross-Namespace Service Names:

For applications in different namespaces, use full service names:

```yaml
data:
  # Redis
  REDIS_URL: "redis://redis-shared.tas-shared:6379/0"
  REDIS_HOST: "redis-shared.tas-shared"
  REDIS_PORT: "6379"
  
  # PostgreSQL
  DB_HOST: "postgres-shared.tas-shared"
  DB_PORT: "5432"
  
  # Kafka
  KAFKA_BROKERS: "kafka-shared.tas-shared:9092"
  
  # MinIO/S3
  S3_ENDPOINT: "http://minio-shared.tas-shared:9000"
  
  # Keycloak
  KEYCLOAK_URL: "http://keycloak-shared.tas-shared:8080"
  
  # Monitoring
  PROMETHEUS_URL: "http://prometheus-shared.tas-shared:9090"
```

### 3. Remove Redundant Services from Kustomization

**Before:**
```yaml
# aether-be/deployments/base/kustomization.yaml
resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml
  - secret.yaml
  - neo4j.yaml
  - redis.yaml        # Remove this
  - keycloak.yaml      # Remove this
  - kafka.yaml         # Remove this
```

**After:**
```yaml
# aether-be/deployments/base/kustomization.yaml
resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml
  - secret.yaml
  - neo4j.yaml
  # redis.yaml removed
  # keycloak.yaml removed  
  # kafka.yaml removed
```

### 4. Update Deployment Dependencies

**Before:**
```yaml
# Deployment with initContainers waiting for local services
spec:
  template:
    spec:
      initContainers:
      - name: wait-for-redis
        image: busybox
        command: ['sh', '-c', 'until nc -z redis 6379; do sleep 1; done']
```

**After:**
```yaml
# Deployment with initContainers waiting for shared services
spec:
  template:
    spec:
      initContainers:
      - name: wait-for-redis
        image: busybox
        command: ['sh', '-c', 'until nc -z redis-shared.tas-shared 6379; do sleep 1; done']
```

### 5. Network Policies (if used)

If using NetworkPolicies, update them to allow traffic to the `tas-shared` namespace:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-shared-services
  namespace: aether  # or your app namespace
spec:
  podSelector: {}
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: tas-shared
    ports:
    - protocol: TCP
      port: 6379  # Redis
    - protocol: TCP
      port: 5432  # PostgreSQL
    - protocol: TCP
      port: 9092  # Kafka
    - protocol: TCP
      port: 9000  # MinIO
    - protocol: TCP
      port: 8080  # Keycloak
```

## Example: Complete aether-be Migration

### 1. Remove Redis Deployment
```bash
rm aether-be/deployments/base/redis.yaml
```

### 2. Update ConfigMap
```yaml
# aether-be/deployments/base/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: aether-config
data:
  # Updated to use shared services
  REDIS_ADDR: "redis-shared.tas-shared:6379"
  NEO4J_URI: "bolt://neo4j:7687"  # Keep local Neo4j
  KEYCLOAK_URL: "http://keycloak-shared.tas-shared:8080"
  S3_ENDPOINT: "http://minio-shared.tas-shared:9000"
  KAFKA_BROKERS: "kafka-shared.tas-shared:9092"
```

### 3. Update Kustomization
```yaml
# aether-be/deployments/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml
  - secret.yaml
  - neo4j.yaml
  # redis.yaml removed
  # keycloak.yaml removed
  # kafka.yaml removed

commonLabels:
  app: aether-backend
  version: v1
```

## Deployment Order

1. **First**: Deploy shared infrastructure
   ```bash
   cd k8s-shared-infrastructure
   ./deploy.sh
   ```

2. **Second**: Deploy application services
   ```bash
   cd aether-be/deployments/base
   kubectl apply -k .
   ```

## Verification

### Check Service Connectivity
```bash
# Test Redis connectivity from app pod
kubectl exec -it <aether-pod> -- redis-cli -h redis-shared.tas-shared ping

# Test PostgreSQL connectivity
kubectl exec -it <app-pod> -- pg_isready -h postgres-shared.tas-shared -p 5432

# Test HTTP services
kubectl exec -it <app-pod> -- curl http://keycloak-shared.tas-shared:8080/health/ready
```

### Monitor Service Discovery
```bash
# Check if services are discoverable
kubectl exec -it <pod> -- nslookup redis-shared.tas-shared
kubectl exec -it <pod> -- nslookup postgres-shared.tas-shared
```

## Troubleshooting

### Common Issues

1. **Service Not Found**
   - Ensure shared infrastructure is deployed
   - Check namespace spelling: `tas-shared`
   - Verify service names: `redis-shared`, `postgres-shared`, etc.

2. **Connection Refused**
   - Check if shared services are running: `kubectl get pods -n tas-shared`
   - Verify service ports: `kubectl get svc -n tas-shared`

3. **DNS Resolution**
   - Test with full FQDN: `redis-shared.tas-shared.svc.cluster.local`
   - Check CoreDNS: `kubectl get pods -n kube-system | grep coredns`

### Debug Commands
```bash
# Check shared infrastructure status
kubectl get all -n tas-shared

# Check service endpoints
kubectl get endpoints -n tas-shared

# Test connectivity
kubectl run debug --image=busybox -it --rm -- sh
# Inside pod: nc -zv redis-shared.tas-shared 6379

# Check logs
kubectl logs -n tas-shared deployment/redis-shared
kubectl logs -n tas-shared statefulset/postgres-shared
```

## Rollback Plan

If issues occur, you can quickly rollback:

1. **Restore original manifests** from git
2. **Deploy individual services** in each namespace
3. **Update ConfigMaps** back to local service names
4. **Remove shared infrastructure** when no longer needed

```bash
# Quick rollback
git checkout HEAD -- aether-be/deployments/base/
kubectl apply -k aether-be/deployments/base/
```

## Benefits After Migration

- **Resource Efficiency**: Single Redis instead of 4 separate instances
- **Centralized Monitoring**: One Grafana dashboard for all services
- **Simplified Management**: Update shared services once, affects all apps
- **Cost Reduction**: Fewer resources, smaller cluster footprint
- **Easier Backups**: Centralized data storage points