#!/bin/bash

# TAS Shared Services Startup Script
# This script starts the shared infrastructure and then individual services

set -e

echo "🚀 Starting TAS Shared Infrastructure Services..."

# Function to check if a container is running
check_container() {
    docker ps -q -f name="$1" | grep -q . && echo "✅ $1 is running" || echo "❌ $1 is not running"
}

# Function to wait for service to be healthy
wait_for_service() {
    local service_name=$1
    local max_attempts=30
    local attempt=1
    
    echo "⏳ Waiting for $service_name to be healthy..."
    
    while [ $attempt -le $max_attempts ]; do
        if docker ps --filter "name=$service_name" --filter "health=healthy" | grep -q "$service_name"; then
            echo "✅ $service_name is healthy!"
            return 0
        fi
        
        echo "   Attempt $attempt/$max_attempts - waiting for $service_name..."
        sleep 10
        ((attempt++))
    done
    
    echo "❌ $service_name failed to become healthy after $max_attempts attempts"
    return 1
}

# Stop any running Docker processes if requested
if [ "$1" = "--kill-all" ]; then
    echo "🛑 Stopping all Docker containers..."
    docker stop $(docker ps -aq) 2>/dev/null || echo "No containers to stop"
    docker rm $(docker ps -aq) 2>/dev/null || echo "No containers to remove"
    echo "✅ All containers stopped and removed"
fi

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo "📝 Creating .env file with default values..."
    cat > .env << 'EOF'
# Shared Infrastructure Environment Variables
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
JWT_SECRET_KEY=your-jwt-secret-key-change-me-in-production
EOF
    echo "✅ .env file created. Please review and modify as needed."
fi

# Start shared infrastructure
echo "🏗️  Starting shared infrastructure services..."
docker-compose -f docker-compose.shared-infrastructure.yml up -d

# Wait for core services to be healthy
echo "⏳ Waiting for core services to start up..."
sleep 15

# Check service health
echo "🔍 Checking service health..."
wait_for_service "tas-redis-shared"
wait_for_service "tas-postgres-shared"
wait_for_service "tas-minio-shared"

echo ""
echo "🎉 Shared infrastructure is ready!"
echo ""
echo "📊 Service URLs:"
echo ""
echo "   🎯 DASHBOARD: http://localhost:8090 (All services in one place!)"
echo ""
echo "   - Redis: localhost:6379"
echo "   - PostgreSQL: localhost:5432"
echo "   - pgAdmin: http://localhost:5050"
echo "   - MinIO Console: http://localhost:9001"
echo "   - Kafka: localhost:9092"
echo "   - Prometheus: http://localhost:9090"
echo "   - Grafana: http://localhost:3000"
echo "   - Loki: http://localhost:3100"
echo "   - Alloy: http://localhost:12345"
echo "   - Keycloak: http://localhost:8081"
echo ""
echo "🚀 Starting individual service repositories..."

# Function to start service if directory exists
start_service() {
    local service_dir=$1
    local service_name=$2
    
    if [ -d "$service_dir" ]; then
        echo "▶️  Starting $service_name..."
        cd "$service_dir"
        docker-compose up -d
        cd - > /dev/null
        echo "✅ $service_name started"
    else
        echo "⚠️  $service_name directory not found: $service_dir"
    fi
}

# Start individual services
start_service "aether-be" "Aether Backend"
start_service "deeplake-api" "DeepLake API"
start_service "audimodal" "AudiModal"
start_service "tas-mcp" "TAS MCP Server"

echo ""
echo "🎉 All services are starting up!"
echo ""
echo "📝 To check service status:"
echo "   docker ps"
echo ""
echo "📝 To view logs:"
echo "   docker-compose -f docker-compose.shared-infrastructure.yml logs -f"
echo "   docker-compose logs -f  (in each service directory)"
echo ""
echo "📝 To stop all services:"
echo "   $0 --kill-all"
echo ""
echo "⚠️  Note: Individual services may take a few minutes to fully start up."
echo "   Check individual service logs if any issues occur."