#!/bin/bash

# cert-manager Deployment Script
# This script deploys cert-manager using local manifests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CERT_MANAGER_VERSION="v1.13.0"
NAMESPACE="cert-manager"
TIMEOUT=300

echo -e "${BLUE}🚀 Deploying cert-manager ${CERT_MANAGER_VERSION}${NC}"

# Function to check if a deployment is ready
check_deployment() {
    local deployment=$1
    local namespace=$2
    local max_attempts=30
    local attempt=1
    
    echo -e "${YELLOW}⏳ Waiting for $deployment to be ready...${NC}"
    
    while [ $attempt -le $max_attempts ]; do
        if kubectl get deployment "$deployment" -n "$namespace" -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -q "1"; then
            echo -e "${GREEN}✅ $deployment is ready!${NC}"
            return 0
        fi
        
        echo -e "   Attempt $attempt/$max_attempts - waiting for $deployment..."
        sleep 10
        ((attempt++))
    done
    
    echo -e "${RED}❌ $deployment failed to become ready after $max_attempts attempts${NC}"
    return 1
}

# Function to wait for webhook to be ready
wait_for_webhook() {
    echo -e "${YELLOW}⏳ Waiting for cert-manager webhook to be ready...${NC}"
    
    local max_attempts=60
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if kubectl get validatingadmissionwebhook cert-manager-webhook -o jsonpath='{.metadata.name}' 2>/dev/null | grep -q "cert-manager-webhook"; then
            # Test the webhook by creating a test certificate request
            if kubectl apply --dry-run=server -f - <<EOF 2>/dev/null
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert
  namespace: default
spec:
  secretName: test-cert-tls
  issuerRef:
    name: test-issuer
    kind: ClusterIssuer
  dnsNames:
  - test.example.com
EOF
            then
                echo -e "${GREEN}✅ cert-manager webhook is ready and responding!${NC}"
                return 0
            fi
        fi
        
        echo -e "   Attempt $attempt/$max_attempts - waiting for webhook..."
        sleep 5
        ((attempt++))
    done
    
    echo -e "${RED}❌ cert-manager webhook failed to become ready${NC}"
    return 1
}

# Parse command line arguments
USE_OFFICIAL_MANIFESTS=false
SKIP_WEBHOOK_CHECK=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --use-official)
            USE_OFFICIAL_MANIFESTS=true
            shift
            ;;
        --skip-webhook-check)
            SKIP_WEBHOOK_CHECK=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --use-official       Use official cert-manager manifests from GitHub"
            echo "  --skip-webhook-check Skip webhook readiness check"
            echo "  --dry-run           Show what would be deployed without applying"
            echo "  --help              Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Check prerequisites
echo -e "${BLUE}🔍 Checking prerequisites...${NC}"

if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed or not in PATH${NC}"
    exit 1
fi

if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}❌ Unable to connect to Kubernetes cluster${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Prerequisites check passed${NC}"

# Deploy cert-manager
if [ "$USE_OFFICIAL_MANIFESTS" = true ]; then
    echo -e "${BLUE}📋 Using official cert-manager manifests from GitHub...${NC}"
    
    if [ "$DRY_RUN" = true ]; then
        echo -e "${YELLOW}📄 Dry run - would apply the following:${NC}"
        echo "kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
        exit 0
    fi
    
    kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
else
    echo -e "${BLUE}📋 Using local cert-manager manifests...${NC}"
    
    if [ "$DRY_RUN" = true ]; then
        echo -e "${YELLOW}📄 Dry run - would apply the following manifests:${NC}"
        echo "cert-manager-crds.yaml"
        echo "cert-manager-deployment.yaml" 
        echo "cert-manager-controllers.yaml"
        exit 0
    fi
    
    # Deploy CRDs first
    echo -e "${BLUE}📄 Applying cert-manager CRDs...${NC}"
    kubectl apply -f cert-manager-crds.yaml
    
    # Deploy RBAC and basic resources
    echo -e "${BLUE}📄 Applying cert-manager RBAC and ServiceAccounts...${NC}"
    kubectl apply -f cert-manager-deployment.yaml
    
    # Deploy controllers
    echo -e "${BLUE}📄 Applying cert-manager controllers...${NC}"
    kubectl apply -f cert-manager-controllers.yaml
fi

# Wait for deployments to be ready
echo -e "${YELLOW}⏳ Waiting for cert-manager components to start up...${NC}"
sleep 15

# Check deployment readiness
echo -e "${BLUE}🔍 Checking deployment readiness...${NC}"

check_deployment "cert-manager" "$NAMESPACE"
check_deployment "cert-manager-cainjector" "$NAMESPACE"  
check_deployment "cert-manager-webhook" "$NAMESPACE"

# Wait for webhook to be fully ready
if [ "$SKIP_WEBHOOK_CHECK" = false ]; then
    wait_for_webhook
fi

# Deploy ClusterIssuers
echo -e "${BLUE}📄 Applying ClusterIssuers...${NC}"
kubectl apply -f cert-manager.yaml

echo ""
echo -e "${GREEN}🎉 cert-manager deployment complete!${NC}"
echo ""

# Show status information
echo -e "${BLUE}📊 cert-manager Status:${NC}"
echo -e "   Namespace: ${NAMESPACE}"
echo -e "   Version: ${CERT_MANAGER_VERSION}"
echo ""

echo -e "${GREEN}🔗 Verify Installation:${NC}"
echo -e "   Check pods: kubectl get pods -n ${NAMESPACE}"
echo -e "   Check CRDs: kubectl get crd | grep cert-manager"
echo -e "   Check issuers: kubectl get clusterissuers"
echo ""

echo -e "${BLUE}🧪 Test cert-manager:${NC}"
echo -e "   Create test certificate: kubectl apply -f certificate-examples.yaml"
echo -e "   Check certificate status: kubectl describe certificate test-certificate -n tas-shared"
echo -e "   Check certificate requests: kubectl get certificaterequests -n tas-shared"
echo ""

echo -e "${YELLOW}📝 Next Steps:${NC}"
echo -e "   1. Update email addresses in cert-manager.yaml ClusterIssuers"
echo -e "   2. Update domain names to match your actual domains"
echo -e "   3. Configure DNS records to point to your cluster"
echo -e "   4. Test certificate creation with staging issuer first"
echo -e "   5. Switch to production issuer after testing"
echo ""

echo -e "${RED}🗑️  To remove cert-manager:${NC}"
if [ "$USE_OFFICIAL_MANIFESTS" = true ]; then
    echo -e "   kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
else
    echo -e "   kubectl delete -f cert-manager-controllers.yaml"
    echo -e "   kubectl delete -f cert-manager-deployment.yaml"  
    echo -e "   kubectl delete -f cert-manager-crds.yaml"
fi
echo -e "   kubectl delete -f cert-manager.yaml"
echo ""

echo -e "${GREEN}✅ cert-manager is ready for use!${NC}"