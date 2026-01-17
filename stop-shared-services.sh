#!/bin/bash

# Stop TAS Shared Services
# This script stops all shared infrastructure and dependent services

set -e

echo "🛑 Stopping TAS Shared Services..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to stop service if directory exists
stop_service() {
    local service_dir=$1
    local service_name=$2
    
    if [ -d "$service_dir" ]; then
        echo -e "${YELLOW}Stopping $service_name...${NC}"
        cd "$service_dir"
        if docker-compose down; then
            echo -e "${GREEN}✅ $service_name stopped${NC}"
        else
            echo -e "${RED}❌ Failed to stop $service_name${NC}"
        fi
        cd ..
    else
        echo -e "${YELLOW}⚠️ $service_dir not found, skipping $service_name${NC}"
    fi
}

# Stop individual services first (in case they depend on shared infrastructure)
echo -e "${YELLOW}📦 Stopping individual services...${NC}"
stop_service "aether-be" "Aether Backend"
stop_service "aether" "Aether Frontend" 
stop_service "deeplake-api" "DeepLake API"
stop_service "audimodal" "AudiModal"
stop_service "tas-mcp" "TAS MCP"

# Stop shared infrastructure last
echo -e "${YELLOW}🏗️ Stopping shared infrastructure...${NC}"
if docker-compose -f docker-compose.shared-infrastructure.yml down; then
    echo -e "${GREEN}✅ Shared infrastructure stopped${NC}"
else
    echo -e "${RED}❌ Failed to stop shared infrastructure${NC}"
fi

# Optional: Remove shared network (uncomment if needed)
# echo -e "${YELLOW}🌐 Removing shared network...${NC}"
# docker network rm tas-shared-network 2>/dev/null || echo "Network already removed or doesn't exist"

echo -e "${GREEN}🎉 All TAS services stopped!${NC}"
echo ""
echo "To restart services later:"
echo "  ./start-shared-services.sh"