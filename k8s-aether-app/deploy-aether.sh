#!/bin/bash

# Deploy Aether Application to Kubernetes
# This script deploys the Aether frontend and backend to a Kubernetes cluster

set -e

echo "🚀 Deploying Aether Application to Kubernetes..."

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    print_error "kubectl is not installed or not in PATH"
    exit 1
fi

# Check if we can connect to the cluster
if ! kubectl cluster-info &> /dev/null; then
    print_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

print_success "Connected to Kubernetes cluster"

# Check if required Docker images exist
print_status "Checking for required Docker images..."

required_images=(
    "aether_aether-frontend:latest"
    "aether-be_aether-backend:latest"
)

missing_images=()
for image in "${required_images[@]}"; do
    if ! docker image inspect "$image" &> /dev/null; then
        missing_images+=("$image")
    fi
done

if [ ${#missing_images[@]} -ne 0 ]; then
    print_error "The following Docker images are missing:"
    for image in "${missing_images[@]}"; do
        echo "  - $image"
    done
    echo ""
    print_warning "Please build the required images before deploying:"
    echo "  Frontend: cd /path/to/aether && docker build -t aether_aether-frontend:latest ."
    echo "  Backend: cd /path/to/aether-be && docker build -t aether-be_aether-backend:latest ."
    exit 1
fi

print_success "All required Docker images found"

# Parse command line arguments
NAMESPACE="aether"
ACTION="apply"
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --namespace|-n)
            NAMESPACE="$2"
            shift 2
            ;;
        --delete)
            ACTION="delete"
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --namespace, -n   Kubernetes namespace (default: aether)"
            echo "  --delete          Delete resources instead of creating them"
            echo "  --dry-run         Show what would be applied without making changes"
            echo "  --help, -h        Show this help message"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Set dry-run flag if requested
DRY_RUN_FLAG=""
if [ "$DRY_RUN" = true ]; then
    DRY_RUN_FLAG="--dry-run=client"
    print_warning "Running in dry-run mode - no changes will be made"
fi

# Apply manifests
manifests=(
    "aether-frontend.yaml"
    "aether-backend.yaml"
)

print_status "Performing $ACTION on manifests in namespace: $NAMESPACE"

for manifest in "${manifests[@]}"; do
    if [ ! -f "$manifest" ]; then
        print_error "Manifest file not found: $manifest"
        continue
    fi
    
    print_status "Processing $manifest..."
    
    if [ "$ACTION" = "apply" ]; then
        if kubectl apply -f "$manifest" $DRY_RUN_FLAG; then
            print_success "Applied $manifest"
        else
            print_error "Failed to apply $manifest"
        fi
    elif [ "$ACTION" = "delete" ]; then
        if kubectl delete -f "$manifest" --ignore-not-found=true; then
            print_success "Deleted resources from $manifest"
        else
            print_error "Failed to delete resources from $manifest"
        fi
    fi
done

if [ "$DRY_RUN" = false ]; then
    if [ "$ACTION" = "apply" ]; then
        print_status "Waiting for deployments to be ready..."
        
        # Wait for deployments to be ready
        kubectl wait --for=condition=available --timeout=300s deployment/aether-frontend -n "$NAMESPACE" || print_warning "Frontend deployment did not become ready within 5 minutes"
        kubectl wait --for=condition=available --timeout=300s deployment/aether-backend -n "$NAMESPACE" || print_warning "Backend deployment did not become ready within 5 minutes"
        
        echo ""
        print_success "Aether application deployment completed!"
        echo ""
        print_status "Access URLs (for local development):"
        echo "  Frontend: http://localhost:30001"
        echo "  Backend:  http://localhost:30080"
        echo ""
        print_status "To check status:"
        echo "  kubectl get pods -n $NAMESPACE"
        echo "  kubectl get services -n $NAMESPACE"
        echo ""
        print_status "To view logs:"
        echo "  kubectl logs -f deployment/aether-frontend -n $NAMESPACE"
        echo "  kubectl logs -f deployment/aether-backend -n $NAMESPACE"
        
    elif [ "$ACTION" = "delete" ]; then
        print_success "Aether application resources deleted"
    fi
else
    print_status "Dry-run completed. Use 'kubectl apply -f .' to actually deploy."
fi