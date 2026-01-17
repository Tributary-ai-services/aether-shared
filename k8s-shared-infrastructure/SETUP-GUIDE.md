# TAS Kubernetes Shared Infrastructure Setup Guide

This guide walks you through deploying the complete TAS shared infrastructure to Kubernetes with external access, TLS certificates, and production-ready configuration.

## Prerequisites

### Required Tools
- **kubectl** - Kubernetes command-line tool
- **A Kubernetes cluster** with:
  - LoadBalancer support (cloud provider or MetalLB)
  - Persistent storage (dynamic provisioning recommended)
  - At least 4GB RAM and 2 CPU cores available

### Domain Requirements
- **Domain name** with DNS control for subdomain creation
- **DNS provider** API access (if using DNS01 challenges for wildcard certificates)

### Optional Tools
- **Helm** (recommended for easier management)
- **k9s** or **Lens** for cluster visualization

## Pre-Deployment Configuration

### 1. Domain Configuration

Edit the following files to replace placeholder domains:

#### Update Ingress Configuration
```bash
# Edit k8s-shared-infrastructure/ingress.yaml
# Replace all instances of 'yourdomain.com' with your actual domain
sed -i 's/yourdomain.com/your-actual-domain.com/g' ingress.yaml
```

#### Update Certificate Configuration
```bash
# Edit k8s-shared-infrastructure/cert-manager.yaml
# Replace admin@example.com with your email address
sed -i 's/admin@example.com/your-email@your-domain.com/g' cert-manager.yaml
```

#### Update Dashboard URLs
```bash
# Edit k8s-shared-infrastructure/dashboard.yaml
# Update the dashboard HTML content with your actual domain
```

### 2. DNS Configuration

Set up DNS records pointing to your Kubernetes cluster's LoadBalancer IP:

```bash
# After deployment, get the LoadBalancer IP:
kubectl get service -n ingress-nginx ingress-nginx

# Create DNS A records:
dashboard.tas.your-domain.com    -> LOADBALANCER_IP
grafana.tas.your-domain.com      -> LOADBALANCER_IP
prometheus.tas.your-domain.com   -> LOADBALANCER_IP
keycloak.tas.your-domain.com     -> LOADBALANCER_IP
pgadmin.tas.your-domain.com      -> LOADBALANCER_IP
minio.tas.your-domain.com        -> LOADBALANCER_IP
minio-api.tas.your-domain.com    -> LOADBALANCER_IP
alerts.tas.your-domain.com       -> LOADBALANCER_IP
```

Or use a wildcard record:
```bash
*.tas.your-domain.com -> LOADBALANCER_IP
```

## Deployment Options

### Option 1: Full Deployment (Recommended)
```bash
cd k8s-shared-infrastructure
./deploy.sh
```

### Option 2: Step-by-Step Deployment
```bash
# 1. Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 2. Wait for cert-manager to be ready
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager

# 3. Deploy TAS infrastructure
kubectl apply -k .

# 4. Check deployment status
kubectl get pods -n tas-shared
```

### Option 3: Deployment with Options
```bash
# Skip ingress controller (if you have your own)
./deploy.sh --skip-ingress

# Skip cert-manager (if already installed)
./deploy.sh --skip-certs

# Dry run to see what would be deployed
./deploy.sh --dry-run

# Get help
./deploy.sh --help
```

## Post-Deployment Verification

### 1. Check Pod Status
```bash
kubectl get pods -n tas-shared
kubectl get pods -n cert-manager
kubectl get pods -n ingress-nginx
```

All pods should show `Running` or `Completed` status.

### 2. Check Services
```bash
kubectl get services -n tas-shared
kubectl get ingress -n tas-shared
```

### 3. Check Certificates
```bash
# Check certificate requests
kubectl get certificates -n tas-shared
kubectl get certificaterequests -n tas-shared

# Check certificate status
kubectl describe certificate dashboard-tls -n tas-shared
```

Certificates should show `Ready: True` status.

### 4. Test External Access

Visit your configured domains:
- `https://dashboard.tas.your-domain.com` - Main dashboard
- `https://grafana.tas.your-domain.com` - Grafana (admin/admin123)
- `https://prometheus.tas.your-domain.com` - Prometheus
- `https://keycloak.tas.your-domain.com` - Keycloak (admin/admin123)

## Configuration Management

### Environment Variables
The shared configuration is stored in ConfigMaps:

```bash
# View shared configuration
kubectl get configmap tas-shared-config -n tas-shared -o yaml

# Edit configuration
kubectl edit configmap tas-shared-config -n tas-shared

# Apply environment-specific configs
kubectl apply -f config.yaml
```

### Secrets Management
Update default passwords:

```bash
# Update PostgreSQL password
kubectl create secret generic postgres-shared-secret \
  --from-literal=username=tasuser \
  --from-literal=password=your-new-password \
  --dry-run=client -o yaml | kubectl apply -f -

# Update Grafana password
kubectl create secret generic grafana-shared-secret \
  --from-literal=admin-password=your-new-password \
  --dry-run=client -o yaml | kubectl apply -f -

# Update MinIO credentials
kubectl create secret generic minio-shared-secret \
  --from-literal=root-user=your-username \
  --from-literal=root-password=your-password \
  --dry-run=client -o yaml | kubectl apply -f -
```

## Application Integration

### Using Shared Services

Applications in other namespaces can access shared services using full service names:

```yaml
# Example application configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: your-app-namespace
data:
  REDIS_URL: "redis://redis-shared.tas-shared:6379/0"
  DATABASE_URL: "postgresql://tasuser:password@postgres-shared.tas-shared:5432/tas_shared"
  KAFKA_BROKERS: "kafka-shared.tas-shared:9092"
  S3_ENDPOINT: "http://minio-shared.tas-shared:9000"
  KEYCLOAK_URL: "http://keycloak-shared.tas-shared:8080"
  PROMETHEUS_URL: "http://prometheus-shared.tas-shared:9090"
```

### Network Policies

If using NetworkPolicies, allow traffic to the `tas-shared` namespace:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-shared-services
  namespace: your-app-namespace
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

## Monitoring and Observability

### Grafana Dashboards
- Access: `https://grafana.tas.your-domain.com`
- Default credentials: `admin` / `admin123`
- Pre-configured with Prometheus datasource

### Prometheus Monitoring
- Access: `https://prometheus.tas.your-domain.com`
- Configured for Kubernetes service discovery
- Scrapes all TAS services automatically

### AlertManager
- Access: `https://alerts.tas.your-domain.com`
- Configure alert routing in `alertmanager.yaml`

## Maintenance

### Updating Services
```bash
# Update a specific service
kubectl set image deployment/grafana-shared grafana=grafana/grafana:latest -n tas-shared

# Rolling restart
kubectl rollout restart deployment/prometheus-shared -n tas-shared

# Check rollout status
kubectl rollout status deployment/prometheus-shared -n tas-shared
```

### Backup Important Data
```bash
# Backup PostgreSQL
kubectl exec -n tas-shared postgres-shared-0 -- pg_dump -U tasuser tas_shared > backup.sql

# Backup Grafana dashboards
kubectl cp tas-shared/grafana-shared-xxx:/var/lib/grafana ./grafana-backup/

# Backup MinIO data (if not using external storage)
kubectl cp tas-shared/minio-shared-xxx:/data ./minio-backup/
```

### Certificate Renewal
Certificates are automatically renewed by cert-manager. To force renewal:

```bash
# Delete certificate to force renewal
kubectl delete certificate dashboard-tls -n tas-shared

# Check renewal status
kubectl describe certificaterequest -n tas-shared
```

## Troubleshooting

### Common Issues

#### 1. Pods Stuck in Pending
```bash
# Check node resources
kubectl describe nodes

# Check persistent volume claims
kubectl get pvc -n tas-shared

# Check events
kubectl get events -n tas-shared --sort-by=.metadata.creationTimestamp
```

#### 2. Certificate Issues
```bash
# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager

# Check certificate status
kubectl describe certificate -n tas-shared

# Check challenges (for HTTP01)
kubectl get challenges -n tas-shared
```

#### 3. Ingress Not Working
```bash
# Check ingress controller
kubectl get pods -n ingress-nginx

# Check ingress resources
kubectl describe ingress -n tas-shared

# Check LoadBalancer IP
kubectl get service -n ingress-nginx
```

#### 4. Service Connection Issues
```bash
# Test internal connectivity
kubectl run debug --image=busybox -it --rm -- sh
# Inside pod: nslookup redis-shared.tas-shared

# Check service endpoints
kubectl get endpoints -n tas-shared

# Check logs
kubectl logs -n tas-shared deployment/redis-shared
```

### Getting Help

```bash
# Check all resources
kubectl get all -n tas-shared

# Describe problematic resources
kubectl describe pod <pod-name> -n tas-shared

# View logs
kubectl logs -f deployment/<service-name> -n tas-shared

# Execute into running containers
kubectl exec -it deployment/redis-shared -n tas-shared -- redis-cli ping
```

## Cleanup

### Remove TAS Infrastructure
```bash
# Remove all TAS resources
kubectl delete -k .

# Remove cert-manager (if desired)
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Remove ingress controller (if desired)
kubectl delete namespace ingress-nginx
```

### Remove Persistent Data
```bash
# List persistent volumes
kubectl get pv

# Delete specific PVCs (this will delete data!)
kubectl delete pvc -n tas-shared --all
```

## Production Considerations

### Security Hardening
- Change all default passwords
- Enable network policies
- Use external secret management (e.g., External Secrets Operator)
- Configure RBAC properly
- Enable audit logging

### High Availability
- Run multiple replicas for stateless services
- Use StatefulSets for databases
- Configure pod disruption budgets
- Use multiple availability zones

### Backup Strategy
- Set up automated backups for databases
- Backup configuration and secrets
- Document recovery procedures
- Test backup restoration regularly

### Resource Management
- Set appropriate resource requests and limits
- Configure horizontal pod autoscaling
- Monitor resource usage
- Plan for capacity growth