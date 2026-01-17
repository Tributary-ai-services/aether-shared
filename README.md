# TAS Aether Shared Infrastructure

Shared infrastructure services for the TAS (Tributary AI System) platform, providing centralized services for databases, caching, messaging, monitoring, and certificate management.

## 🏗️ Architecture Overview

This repository provides a complete shared infrastructure stack with external access via HTTPS:

### Core Services
- **Redis** (6379) - Shared caching and session storage
- **PostgreSQL** (5432) - Primary shared database  
- **Kafka + Zookeeper** (9092/2181) - Event streaming and messaging
- **MinIO** (9000/9001) - S3-compatible object storage

### Monitoring & Observability  
- **Prometheus** (9090) - Metrics collection and storage
- **Grafana** (3000) - Dashboards and visualization
- **AlertManager** (9093) - Alert routing and management
- **OpenTelemetry Collector** (4317/4318) - Distributed tracing

### Security & Management
- **Keycloak** (8081) - Identity and access management
- **pgAdmin** (5050) - Database administration interface
- **Services Dashboard** (8090) - Centralized access dashboard

### External Access
All services are accessible via HTTPS with automatic Let's Encrypt certificates:
- `https://dashboard.tas.yourdomain.com` - Services Dashboard
- `https://grafana.tas.yourdomain.com` - Grafana Monitoring  
- `https://prometheus.tas.yourdomain.com` - Prometheus Metrics
- `https://keycloak.tas.yourdomain.com` - Identity Management
- `https://pgadmin.tas.yourdomain.com` - Database Administration
- `https://minio.tas.yourdomain.com` - MinIO Console
- `https://alerts.tas.yourdomain.com` - AlertManager

## 📁 File Structure

```
k8s-shared-infrastructure/
├── cert-manager.yaml                    # ClusterIssuers for Let's Encrypt
├── cert-manager-crds.yaml              # Custom Resource Definitions
├── cert-manager-deployment.yaml        # RBAC and ServiceAccounts
├── cert-manager-controllers.yaml       # cert-manager deployments
├── certificate-examples.yaml           # Certificate templates and examples
├── deploy-cert-manager.sh              # cert-manager deployment script
├── test-cert-manager.sh                # cert-manager testing script
├── ingress-controller.yaml             # NGINX Ingress Controller
├── ingress.yaml                        # Ingress resources with TLS
├── config.yaml                         # Centralized configuration
├── namespace.yaml                      # tas-shared namespace
├── redis.yaml                          # Redis cache service
├── postgres.yaml                       # PostgreSQL database
├── kafka.yaml                          # Kafka + Zookeeper
├── minio.yaml                          # MinIO object storage
├── keycloak.yaml                       # Keycloak identity management
├── monitoring.yaml                     # Prometheus, Grafana, OTel
├── pgadmin.yaml                        # Database administration
├── alertmanager.yaml                   # Alert management
├── dashboard.yaml                      # Services dashboard
├── deploy.sh                           # Main deployment script
├── kustomization.yaml                  # Kustomize configuration
├── SETUP-GUIDE.md                      # Detailed setup guide
└── KUBERNETES-MIGRATION-GUIDE.md       # Migration documentation

k8s-aether-app/
├── aether-frontend.yaml                 # Aether React frontend application
├── aether-backend.yaml                  # Aether Go backend API
├── deploy-aether.sh                     # Aether application deployment script
└── README.md                            # Aether application documentation
```

## 🚀 Quick Start

### Prerequisites
- **Kubernetes cluster** with LoadBalancer support
- **kubectl** configured to access your cluster  
- **Domain name** with DNS control
- **Valid email address** for Let's Encrypt registration

### 1. Clone and Configure
```bash
git clone <repository-url>
cd aether-shared/k8s-shared-infrastructure
```

### 2. Update Configuration Files
```bash
# Update email addresses for Let's Encrypt registration
sed -i 's/admin@gmail.com/your-email@yourdomain.com/g' cert-manager.yaml

# Update domain names for your services
sed -i 's/yourdomain.com/your-actual-domain.com/g' ingress.yaml
sed -i 's/yourdomain.com/your-actual-domain.com/g' dashboard.yaml
```

### 3. Deploy Infrastructure
```bash
# Deploy cert-manager first
./deploy-cert-manager.sh

# Deploy shared infrastructure
./deploy.sh
```

### 4. Configure DNS
Point your domains to the LoadBalancer IP:
```bash
# Get LoadBalancer IP
kubectl get service -n ingress-nginx ingress-nginx

# Create DNS records
*.tas.your-domain.com  →  LOADBALANCER_IP
```

### 5. Deploy Aether Application (Optional)
Deploy the Aether frontend and backend applications:
```bash
# Switch to aether application manifests
cd ../k8s-aether-app

# Build required Docker images first
cd /path/to/aether
DOCKER_BUILDKIT=0 docker build -t aether_aether-frontend:latest \
  --build-arg VITE_DEV_USERNAME=john@scharber.com \
  --build-arg VITE_DEV_PASSWORD=test123 \
  --build-arg VITE_DEV_MODE=true .

cd /path/to/aether-be  
DOCKER_BUILDKIT=0 docker build -t aether-be_aether-backend:latest .

# Deploy aether applications
cd /path/to/aether-shared/k8s-aether-app
./deploy-aether.sh

# Access applications (local development)
# Frontend: http://localhost:30001
# Backend: http://localhost:30080
```

## 🔒 Certificate Management with cert-manager

### Overview
This setup uses **cert-manager** with **Let's Encrypt** for automatic HTTPS certificate management:

- **ACME client built-in** - No separate installation needed
- **Automatic issuance** - Certificates requested via ingress annotations
- **Auto-renewal** - Certificates renewed before expiration (90-day lifecycle)
- **HTTP01 challenges** - Domain validation via nginx ingress

### cert-manager Files Location
All cert-manager files are in `/k8s-shared-infrastructure/`:

#### Core cert-manager Files
- `cert-manager.yaml` - **ClusterIssuers** (Let's Encrypt staging/production)
- `cert-manager-deployment.yaml` - RBAC, ServiceAccounts, basic resources
- `cert-manager-controllers.yaml` - Controller deployments (controller, webhook, cainjector)  
- `cert-manager-crds.yaml` - Custom Resource Definitions
- `certificate-examples.yaml` - Certificate templates and examples

#### Management Scripts
- `deploy-cert-manager.sh` - **Deploy cert-manager** (use this!)
- `test-cert-manager.sh` - **Test cert-manager** functionality

### cert-manager Deployment

#### Option 1: Deploy cert-manager Separately (Recommended)
```bash
cd k8s-shared-infrastructure

# Deploy cert-manager with official manifests
./deploy-cert-manager.sh --use-official

# Or deploy with local manifests
./deploy-cert-manager.sh

# Test the deployment
./test-cert-manager.sh
```

#### Option 2: Deploy with Main Infrastructure
```bash
# Deploy everything together (includes cert-manager)
./deploy.sh
```

### Certificate Configuration

#### 1. Update Email Address (REQUIRED)
```bash
# Edit cert-manager.yaml and update email addresses
vim cert-manager.yaml

# Find and replace:
email: admin@gmail.com  # CHANGE THIS TO YOUR ACTUAL EMAIL
```

#### 2. ClusterIssuers Available
- **letsencrypt-staging** - For testing (use first!)
- **letsencrypt-prod** - For production (use after testing)

#### 3. Certificate Creation Methods

**Automatic (Recommended)**: Certificates created by ingress annotations:
```yaml
# In ingress.yaml - already configured
annotations:
  cert-manager.io/cluster-issuer: "letsencrypt-prod"
```

**Manual**: Create certificates directly:
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-cert
  namespace: tas-shared
spec:
  secretName: my-cert-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - my-service.tas.your-domain.com
```

### Verify cert-manager Status

```bash
# Check cert-manager pods
kubectl get pods -n cert-manager

# Check ClusterIssuers are ready
kubectl get clusterissuers
# Should show: READY = True

# Check certificates
kubectl get certificates -n tas-shared

# Check certificate details
kubectl describe certificate <cert-name> -n tas-shared
```

### Certificate Troubleshooting

```bash
# Check certificate requests
kubectl get certificaterequests -n tas-shared

# Check ACME challenges (HTTP01)
kubectl get challenges -n tas-shared

# Check ACME orders
kubectl get orders -n tas-shared

# View cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager

# Test with staging first
kubectl apply -f certificate-examples.yaml
```

## 🌐 DNS Configuration

### Required DNS Records
Create these DNS records pointing to your LoadBalancer IP:

```bash
# Get your LoadBalancer IP
kubectl get service -n ingress-nginx ingress-nginx

# Create A records:
dashboard.tas.your-domain.com     →  LOADBALANCER_IP
grafana.tas.your-domain.com       →  LOADBALANCER_IP  
prometheus.tas.your-domain.com    →  LOADBALANCER_IP
keycloak.tas.your-domain.com      →  LOADBALANCER_IP
pgadmin.tas.your-domain.com       →  LOADBALANCER_IP
minio.tas.your-domain.com         →  LOADBALANCER_IP
alerts.tas.your-domain.com        →  LOADBALANCER_IP

# Or use a wildcard:
*.tas.your-domain.com            →  LOADBALANCER_IP
```

## 🧪 Testing and Validation

### Test cert-manager
```bash
./test-cert-manager.sh
```

### Test Services  
```bash
# Check all pods are running
kubectl get pods -n tas-shared

# Check ingress resources
kubectl get ingress -n tas-shared

# Test HTTPS endpoints
curl -I https://dashboard.tas.your-domain.com
curl -I https://grafana.tas.your-domain.com
```

### Test Certificate Issuance
```bash
# Create a test certificate with staging issuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert
  namespace: tas-shared
spec:
  secretName: test-cert-tls
  issuerRef:
    name: letsencrypt-staging
    kind: ClusterIssuer
  dnsNames:
  - test.tas.your-domain.com
EOF

# Check certificate status
kubectl describe certificate test-cert -n tas-shared

# Clean up test
kubectl delete certificate test-cert -n tas-shared
```

## 🐳 Docker Compose Usage

For development or testing, you can also run the services using Docker Compose:

### Start All Services
```bash
# Start shared infrastructure
./start-shared-services.sh

# Or manually with docker-compose
docker-compose -f docker-compose.shared-infrastructure.yml up -d
```

### Stop All Services
```bash
# Stop all services
./stop-shared-services.sh

# Or manually
docker-compose -f docker-compose.shared-infrastructure.yml down
```

### Service URLs (Docker Compose)
- **Dashboard**: http://localhost:8090
- **Grafana**: http://localhost:3000 (admin/admin123)
- **Prometheus**: http://localhost:9090
- **Keycloak**: http://localhost:8081 (admin/admin123)
- **pgAdmin**: http://localhost:5050 (admin@example.com/admin123)
- **MinIO Console**: http://localhost:9001 (minioadmin/minioadmin123)
- **AlertManager**: http://localhost:9093

## 🔧 Configuration Management

### Environment Variables
Shared configuration is managed via ConfigMaps in Kubernetes:

```bash
# View configuration
kubectl get configmap tas-shared-config -n tas-shared -o yaml

# Update configuration  
kubectl edit configmap tas-shared-config -n tas-shared

# Environment-specific configs
kubectl apply -f config.yaml  # Includes dev/staging/prod overlays
```

### Service Discovery
Applications can access shared services using cross-namespace service names:

```yaml
# Example application configuration
REDIS_URL: "redis://redis-shared.tas-shared:6379/0"
DATABASE_URL: "postgresql://tasuser:password@postgres-shared.tas-shared:5432/tas_shared" 
KAFKA_BROKERS: "kafka-shared.tas-shared:9092"
KEYCLOAK_URL: "http://keycloak-shared.tas-shared:8080"
PROMETHEUS_URL: "http://prometheus-shared.tas-shared:9090"
```

## 📊 Monitoring and Dashboards

### Grafana Dashboards
Pre-configured dashboards are available in `shared-monitoring/grafana/dashboards/`:
- **Kubernetes Overview** - Cluster and pod metrics
- **LLM Router Dashboards** - Request metrics, security, performance
- **AudiModal Dashboards** - Audio processing metrics

### Prometheus Metrics
Prometheus is configured to scrape metrics from:
- All Kubernetes services with proper annotations
- Application services across namespaces  
- Infrastructure components (cert-manager, ingress-nginx, etc.)

## 🔐 Security Considerations

### Production Security Checklist
- [ ] Update all default passwords in ConfigMaps/Secrets
- [ ] Configure proper RBAC for service accounts
- [ ] Enable Network Policies for service isolation
- [ ] Use external secret management (External Secrets Operator)
- [ ] Configure backup and disaster recovery procedures
- [ ] Set up monitoring and alerting for security events

### Default Credentials (Change These!)
```bash
# PostgreSQL
Username: tasuser
Password: taspassword

# Grafana  
Username: admin
Password: admin123

# Keycloak
Username: admin
Password: admin123

# MinIO
Username: minioadmin
Password: minioadmin123

# pgAdmin
Email: admin@example.com
Password: admin123
```

## 🛠️ Troubleshooting

### Common Issues

#### cert-manager Issues
```bash
# Check cert-manager status
kubectl get pods -n cert-manager
kubectl get clusterissuers

# View logs
kubectl logs -n cert-manager deployment/cert-manager
kubectl logs -n cert-manager deployment/cert-manager-webhook

# Test webhook
kubectl apply --dry-run=server -f certificate-examples.yaml
```

#### Certificate Issues
```bash
# Check certificate status
kubectl get certificates -n tas-shared
kubectl describe certificate <cert-name> -n tas-shared

# Check certificate requests and challenges
kubectl get certificaterequests -n tas-shared
kubectl get challenges -n tas-shared
kubectl get orders -n tas-shared

# Common fixes
# 1. Verify DNS records point to LoadBalancer IP
# 2. Check ClusterIssuer email is valid
# 3. Test with staging issuer first
# 4. Ensure ingress-nginx is running
```

#### Ingress Issues
```bash
# Check ingress controller
kubectl get pods -n ingress-nginx
kubectl get service -n ingress-nginx

# Check ingress resources
kubectl get ingress -n tas-shared
kubectl describe ingress <ingress-name> -n tas-shared

# Test connectivity
curl -v http://LOADBALANCER_IP/.well-known/acme-challenge/test
```

#### Service Issues
```bash
# Check pod status
kubectl get pods -n tas-shared
kubectl describe pod <pod-name> -n tas-shared

# Check services and endpoints
kubectl get services -n tas-shared
kubectl get endpoints -n tas-shared

# View logs
kubectl logs -f deployment/<service-name> -n tas-shared
```

### Getting Help
```bash
# View all resources
kubectl get all -n tas-shared

# Check events
kubectl get events -n tas-shared --sort-by='.lastTimestamp'

# Debug networking
kubectl run debug --image=busybox -it --rm -- sh
# Inside pod: nslookup redis-shared.tas-shared
```

## 📚 Additional Resources

### Documentation Files
- **SETUP-GUIDE.md** - Comprehensive deployment guide
- **KUBERNETES-MIGRATION-GUIDE.md** - Migrating from individual services
- **CLAUDE.md** - Architecture and development notes
- **services-and-ports.md** - Complete service reference

### Useful Commands Reference
```bash
# Deployment
./deploy.sh --help                    # Main deployment options
./deploy-cert-manager.sh --help       # cert-manager deployment options

# Monitoring
kubectl get certificates -n tas-shared -w    # Watch certificate status
kubectl get challenges -n tas-shared         # Check ACME challenges
kubectl get clusterissuers                   # Check issuer status

# Debugging  
kubectl describe certificate <name> -n tas-shared    # Certificate details
kubectl logs -n cert-manager deployment/cert-manager # cert-manager logs
kubectl get events -n tas-shared                     # Recent events
```

### External Links
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Let's Encrypt Documentation](https://letsencrypt.org/docs/)
- [NGINX Ingress Controller](https://kubernetes.github.io/ingress-nginx/)
- [Kubernetes Documentation](https://kubernetes.io/docs/)

## 🤝 Contributing

### Development Workflow
1. Make changes to the appropriate YAML files
2. Test with `kubectl apply --dry-run=server -k .`
3. Deploy to a test namespace first
4. Update documentation as needed
5. Test certificate issuance with staging issuer

### Adding New Services
1. Create service YAML in `k8s-shared-infrastructure/`
2. Add to `kustomization.yaml` resources list
3. Update ingress configuration if external access needed
4. Add monitoring configuration to Prometheus
5. Update documentation and service discovery info
