#!/bin/bash

# TAS Shared Infrastructure Kubernetes Deployment Script
# This script deploys the complete TAS shared infrastructure with ingress and TLS support

set -e

echo "🚀 Deploying TAS Shared Infrastructure to Kubernetes..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="tas-shared"
CERT_MANAGER_VERSION="v1.13.0"
NGINX_INGRESS_VERSION="controller-v1.8.2"

# Function to check if a deployment is ready
check_deployment() {
    local deployment=$1
    local namespace=$2
    local max_attempts=30
    local attempt=1
    
    echo "⏳ Waiting for $deployment to be ready..."
    
    while [ $attempt -le $max_attempts ]; do
        if kubectl get deployment "$deployment" -n "$namespace" -o jsonpath='{.status.readyReplicas}' | grep -q "1"; then
            echo "✅ $deployment is ready!"
            return 0
        fi
        
        echo "   Attempt $attempt/$max_attempts - waiting for $deployment..."
        sleep 10
        ((attempt++))
    done
    
    echo "❌ $deployment failed to become ready after $max_attempts attempts"
    return 1
}

# Function to check if a statefulset is ready
check_statefulset() {
    local statefulset=$1
    local namespace=$2
    local max_attempts=30
    local attempt=1
    
    echo "⏳ Waiting for $statefulset to be ready..."
    
    while [ $attempt -le $max_attempts ]; do
        if kubectl get statefulset "$statefulset" -n "$namespace" -o jsonpath='{.status.readyReplicas}' | grep -q "1"; then
            echo "✅ $statefulset is ready!"
            return 0
        fi
        
        echo "   Attempt $attempt/$max_attempts - waiting for $statefulset..."
        sleep 10
        ((attempt++))
    done
    
    echo "❌ $statefulset failed to become ready after $max_attempts attempts"
    return 1
}

# Function to install cert-manager
install_cert_manager() {
    echo -e "${BLUE}📋 Installing cert-manager...${NC}"
    
    # Check if cert-manager is already installed
    if kubectl get namespace cert-manager &> /dev/null; then
        echo -e "${YELLOW}⚠️  cert-manager namespace already exists, skipping installation${NC}"
        return 0
    fi
    
    # Install cert-manager
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml
    
    # Wait for cert-manager to be ready
    echo -e "${YELLOW}⏳ Waiting for cert-manager to be ready...${NC}"
    kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager
    kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager-cainjector -n cert-manager
    kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager-webhook -n cert-manager
    
    echo -e "${GREEN}✅ cert-manager installed successfully${NC}"
}

# Function to install NGINX Ingress Controller
install_nginx_ingress() {
    echo -e "${BLUE}📋 Installing NGINX Ingress Controller...${NC}"
    
    # Check if ingress-nginx is already installed
    if kubectl get namespace ingress-nginx &> /dev/null; then
        echo -e "${YELLOW}⚠️  ingress-nginx namespace already exists, using local configuration${NC}"
    fi
    
    echo -e "${GREEN}✅ NGINX Ingress Controller configuration ready${NC}"
}

# Function to validate domain configuration
validate_domain_config() {
    echo -e "${BLUE}🔍 Validating domain configuration...${NC}"
    
    if grep -q "yourdomain.com" ingress.yaml; then
        echo -e "${RED}❌ Please update domain names in ingress.yaml before deployment${NC}"
        echo -e "${YELLOW}   Replace 'yourdomain.com' with your actual domain${NC}"
        echo -e "${YELLOW}   Update email addresses in cert-manager.yaml${NC}"
        exit 1
    fi
    
    if grep -q "admin@example.com" cert-manager.yaml; then
        echo -e "${RED}❌ Please update email address in cert-manager.yaml${NC}"
        echo -e "${YELLOW}   Replace 'admin@example.com' with your actual email${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✅ Domain configuration looks good${NC}"
}

# Check prerequisites
echo -e "${BLUE}🔍 Checking prerequisites...${NC}"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed or not in PATH${NC}"
    exit 1
fi

# Check if cluster is accessible
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}❌ Unable to connect to Kubernetes cluster${NC}"
    exit 1
fi

# Check if Helm is available (optional but recommended)
if ! command -v helm &> /dev/null; then
    echo -e "${YELLOW}⚠️  Helm is not installed - using kubectl apply instead${NC}"
fi

echo -e "${GREEN}✅ Prerequisites check passed${NC}"

# Parse command line arguments
SKIP_INGRESS=false
SKIP_CERTS=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-ingress)
            SKIP_INGRESS=true
            shift
            ;;
        --skip-certs)
            SKIP_CERTS=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --skip-ingress  Skip ingress controller installation"
            echo "  --skip-certs    Skip cert-manager installation"
            echo "  --dry-run       Show what would be deployed without applying"
            echo "  --help          Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Validate configuration before deployment
if [ "$DRY_RUN" = false ]; then
    validate_domain_config
fi

# Install prerequisites
if [ "$SKIP_CERTS" = false ]; then
    install_cert_manager
fi

if [ "$SKIP_INGRESS" = false ]; then
    install_nginx_ingress
fi

# Apply manifests using kustomize
if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}📄 Dry run - would apply the following manifests:${NC}"
    kubectl kustomize .
    exit 0
else
    echo -e "${BLUE}📄 Applying Kubernetes manifests...${NC}"
    kubectl apply -k .
fi

# Wait for core services to be ready
echo -e "${YELLOW}⏳ Waiting for services to start up...${NC}"
sleep 15

# Check service readiness
echo -e "${BLUE}🔍 Checking service readiness...${NC}"

# Check StatefulSets
check_statefulset "postgres-shared" "$NAMESPACE"
check_statefulset "zookeeper-shared" "$NAMESPACE"
check_statefulset "kafka-shared" "$NAMESPACE"
check_statefulset "keycloak-db-shared" "$NAMESPACE"

# Check Deployments
check_deployment "redis-shared" "$NAMESPACE"
check_deployment "minio-shared" "$NAMESPACE"
check_deployment "prometheus-shared" "$NAMESPACE"
check_deployment "grafana-shared" "$NAMESPACE"
check_deployment "keycloak-shared" "$NAMESPACE"
check_deployment "pgadmin-shared" "$NAMESPACE"
check_deployment "alertmanager-shared" "$NAMESPACE"
check_deployment "otel-collector-shared" "$NAMESPACE"
check_deployment "dashboard-shared" "$NAMESPACE"

# Check Ingress Controller if not skipped
if [ "$SKIP_INGRESS" = false ]; then
    check_deployment "nginx-ingress-controller" "ingress-nginx"
fi

echo ""
echo -e "${GREEN}🎉 TAS Shared Infrastructure is deployed!${NC}"
echo ""
echo -e "${BLUE}📊 Service Information:${NC}"
echo -e "   Namespace: ${NAMESPACE}"
echo ""

# Show ingress URLs if ingress was deployed
if [ "$SKIP_INGRESS" = false ]; then
    echo -e "${GREEN}🌐 External Access URLs:${NC}"
    echo -e "   🎯 Dashboard: https://dashboard.tas.yourdomain.com"
    echo -e "   📊 Grafana: https://grafana.tas.yourdomain.com"
    echo -e "   📈 Prometheus: https://prometheus.tas.yourdomain.com"
    echo -e "   🔐 Keycloak: https://keycloak.tas.yourdomain.com"
    echo -e "   🗄️ pgAdmin: https://pgadmin.tas.yourdomain.com"
    echo -e "   💾 MinIO Console: https://minio.tas.yourdomain.com"
    echo -e "   🚨 AlertManager: https://alerts.tas.yourdomain.com"
    echo ""
    echo -e "${YELLOW}📋 Certificate Status:${NC}"
    echo -e "   Check certificates: kubectl get certificates -n ${NAMESPACE}"
    echo -e "   Check cert-manager: kubectl get certificaterequests -n ${NAMESPACE}"
    echo ""
fi

echo -e "${YELLOW}🔗 Alternative access via port forwarding:${NC}"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/dashboard-shared 8090:80"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/grafana-shared 3000:3000"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/prometheus-shared 9090:9090"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/keycloak-shared 8080:8080"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/pgadmin-shared 5050:80"
echo -e "   kubectl port-forward -n ${NAMESPACE} svc/minio-shared 9001:9001"
echo ""
echo -e "${BLUE}📝 Management Commands:${NC}"
echo -e "   Check pod status: kubectl get pods -n ${NAMESPACE}"
echo -e "   Check services: kubectl get services -n ${NAMESPACE}"
echo -e "   Check ingress: kubectl get ingress -n ${NAMESPACE}"
echo -e "   View logs: kubectl logs -n ${NAMESPACE} deployment/<service-name>"
echo ""
echo -e "${BLUE}🔧 Configuration:${NC}"
echo -e "   Shared config: kubectl get configmap tas-shared-config -n ${NAMESPACE} -o yaml"
echo -e "   Service discovery: Services available at <service>.${NAMESPACE}.svc.cluster.local"
echo ""
echo -e "${RED}🗑️  To remove the shared infrastructure:${NC}"
echo -e "   kubectl delete -k ."
if [ "$SKIP_CERTS" = false ]; then
    echo -e "   kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
fi
echo ""
echo -e "${GREEN}✅ Deployment complete! Individual application services can now reference shared services${NC}"
echo -e "${YELLOW}   using cross-namespace service names like: redis-shared.${NAMESPACE}.svc.cluster.local${NC}"