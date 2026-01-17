#!/bin/bash

# TAS Development Mode Startup Script
# Supports both Docker Compose and Kubernetes local development

set -e

echo "🚀 Starting TAS Development Environment..."

# Function to detect environment
detect_environment() {
    if command -v kubectl &> /dev/null && kubectl cluster-info &> /dev/null; then
        echo "kubernetes"
    elif command -v docker &> /dev/null && docker info &> /dev/null; then
        echo "docker"
    else
        echo "none"
    fi
}

# Function to get private network IP
get_private_ip() {
    # Try to detect private network IP
    local ip=$(ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}')
    if [[ -z "$ip" ]]; then
        ip=$(hostname -I | awk '{print $1}')
    fi
    echo "$ip"
}

# Parse command line arguments
MODE="auto"
ENVIRONMENT=$(detect_environment)
PRIVATE_IP=$(get_private_ip)

while [[ $# -gt 0 ]]; do
    case $1 in
        --docker)
            MODE="docker"
            shift
            ;;
        --k8s|--kubernetes)
            MODE="kubernetes"
            shift
            ;;
        --ip)
            PRIVATE_IP="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--docker|--k8s] [--ip IP_ADDRESS]"
            echo ""
            echo "Options:"
            echo "  --docker      Force Docker Compose mode"
            echo "  --k8s         Force Kubernetes mode"
            echo "  --ip IP       Use specific private IP address"
            echo "  --help        Show this help"
            echo ""
            echo "Auto-detected environment: $ENVIRONMENT"
            echo "Auto-detected private IP: $PRIVATE_IP"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Override environment if mode is specified
if [[ "$MODE" != "auto" ]]; then
    ENVIRONMENT="$MODE"
fi

echo "🔧 Environment: $ENVIRONMENT"
echo "🌐 Private IP: $PRIVATE_IP"

# Create or update .env file for development
create_dev_env() {
    echo "📝 Creating development .env file..."
    cat > .env << EOF
# TAS Development Environment Variables
# Auto-generated on $(date)

# Private Network Configuration
PRIVATE_IP=$PRIVATE_IP
ENVIRONMENT=development
LOG_LEVEL=debug

# Shared Infrastructure
REDIS_PASSWORD=
POSTGRES_USER=tasuser
POSTGRES_PASSWORD=taspassword
PGADMIN_EMAIL=admin@example.com
PGADMIN_PASSWORD=admin123
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin123
GRAFANA_PASSWORD=admin123
KEYCLOAK_ADMIN_PASSWORD=admin123
KEYCLOAK_DB_PASSWORD=keycloak123
JWT_SECRET_KEY=dev-jwt-secret-key-change-in-production

# Development-specific settings
ENABLE_DEBUG=true
ENABLE_TRACING=true
ENABLE_METRICS=true
PROMETHEUS_RETENTION=24h
LOKI_RETENTION=24h
EOF
    echo "✅ Development .env file created"
}

# Start Docker Compose development mode
start_docker_dev() {
    echo "🐳 Starting Docker Compose development mode..."
    
    # Check if Docker is running
    if ! docker info > /dev/null 2>&1; then
        echo "❌ Docker is not running. Please start Docker first."
        exit 1
    fi
    
    create_dev_env
    
    # Start with development overrides
    echo "🏗️  Starting shared infrastructure with development configuration..."
    docker-compose -f docker-compose.shared-infrastructure.yml -f docker-compose.dev.yml up -d
    
    # Wait for core services
    echo "⏳ Waiting for core services to be healthy..."
    sleep 20
    
    # Check service health
    echo "🔍 Checking service health..."
    docker-compose -f docker-compose.shared-infrastructure.yml -f docker-compose.dev.yml ps
    
    echo ""
    echo "🎉 Development environment is ready!"
    echo ""
    echo "📊 Development Service URLs:"
    echo "   🎯 DASHBOARD: http://$PRIVATE_IP:8090"
    echo "   📈 GRAFANA: http://$PRIVATE_IP:3000 (admin/admin123)"
    echo "   📊 PROMETHEUS: http://$PRIVATE_IP:9090"
    echo "   📝 LOKI: http://$PRIVATE_IP:3100"
    echo "   🔧 ALLOY: http://$PRIVATE_IP:12345"
    echo "   🗄️  MINIO: http://$PRIVATE_IP:9001 (minioadmin/minioadmin123)"
    echo "   🛢️  PGADMIN: http://$PRIVATE_IP:5050 (admin@example.com/admin123)"
    echo ""
    echo "🔧 Development Features Enabled:"
    echo "   - Enhanced logging (debug level)"
    echo "   - All container log collection"
    echo "   - Local network optimization"
    echo "   - Development data sources"
    echo ""
}

# Start Kubernetes development mode
start_k8s_dev() {
    echo "☸️  Starting Kubernetes development mode..."
    
    # Check if kubectl is available and cluster is accessible
    if ! kubectl cluster-info > /dev/null 2>&1; then
        echo "❌ Kubernetes cluster is not accessible. Please check your kubectl configuration."
        exit 1
    fi
    
    create_dev_env
    
    # Create development namespace
    echo "📦 Creating development namespace..."
    kubectl apply -f k8s-dev/namespace.yaml
    
    # Deploy development services
    echo "🚀 Deploying development services..."
    kubectl apply -f k8s-dev/loki-dev.yaml
    kubectl apply -f k8s-dev/alloy-dev.yaml
    
    # Wait for pods to be ready
    echo "⏳ Waiting for pods to be ready..."
    kubectl wait --for=condition=ready pod -l app=loki-dev -n tas-dev --timeout=120s
    kubectl wait --for=condition=ready pod -l app=alloy-dev -n tas-dev --timeout=120s
    
    # Get service URLs
    echo ""
    echo "🎉 Kubernetes development environment is ready!"
    echo ""
    echo "📊 Access services via port-forward:"
    echo "   kubectl port-forward -n tas-dev svc/loki-dev 3100:3100"
    echo "   kubectl port-forward -n tas-dev svc/alloy-dev 12345:12345"
    echo ""
    echo "🔧 Check pod status:"
    echo "   kubectl get pods -n tas-dev"
    echo ""
    echo "📝 View logs:"
    echo "   kubectl logs -f -n tas-dev -l app=loki-dev"
    echo "   kubectl logs -f -n tas-dev -l app=alloy-dev"
    echo ""
}

# Main execution
case $ENVIRONMENT in
    docker)
        start_docker_dev
        ;;
    kubernetes)
        start_k8s_dev
        ;;
    *)
        echo "❌ No suitable environment detected."
        echo "Please ensure either Docker or Kubernetes is available and running."
        echo "Use --help for more information."
        exit 1
        ;;
esac

echo "✨ Development environment startup complete!"
echo ""
echo "📝 Next steps:"
echo "   1. Access Grafana and configure your dashboards"
echo "   2. Start your microservices"
echo "   3. Check logs in Grafana Explore -> Loki-Dev"
echo ""
echo "🛑 To stop:"
if [[ "$ENVIRONMENT" == "docker" ]]; then
    echo "   docker-compose -f docker-compose.shared-infrastructure.yml -f docker-compose.dev.yml down"
elif [[ "$ENVIRONMENT" == "kubernetes" ]]; then
    echo "   kubectl delete namespace tas-dev"
fi