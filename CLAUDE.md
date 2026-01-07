# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

This is the `aether-shared` repository, which provides shared infrastructure services for the TAS (Tributary AI System) platform. It contains Docker Compose configurations, monitoring setups, and management scripts for common services used across multiple TAS components.

## Data Models & Schema Reference

### Centralized Data Model Documentation Hub
This repository hosts the **complete data model documentation** for all TAS services in a centralized location:

**Location**: `data-models/`

#### Cross-Service Documentation:
- **Platform ERD** (`data-models/cross-service/diagrams/platform-erd.md`) - Complete entity relationship diagram showing all services
- **Architecture Overview** (`data-models/cross-service/diagrams/architecture-overview.md`) - System-wide architecture with data flows
- **User Onboarding Flow** (`data-models/cross-service/flows/user-onboarding.md`) - Multi-service user registration process
- **Document Upload Flow** (`data-models/cross-service/flows/document-upload.md`) - End-to-end document processing pipeline
- **ID Mapping Chain** (`data-models/cross-service/mappings/id-mapping-chain.md`) - Cross-service identifier relationships

#### Service-Specific Data Models (38 Total Files):
- **aether-be** (`data-models/aether-be/`) - Neo4j graph nodes and relationships (7 files)
- **aether** (`data-models/aether/`) - React Redux store and API types (3 files)
- **audimodal** (`data-models/audimodal/`) - PostgreSQL entities (3 files)
- **deeplake-api** (`data-models/deeplake-api/`) - Vector database schemas (4 files)
- **tas-llm-router** (`data-models/tas-llm-router/`) - LLM request/response formats (3 files)
- **tas-mcp** (`data-models/tas-mcp/`) - Protocol buffer definitions (3 files)
- **tas-agent-builder** (`data-models/tas-agent-builder/`) - Agent entity models
- **tas-mcp-servers** (`data-models/tas-mcp-servers/`) - MCP server configurations
- **tas-workflow-builder** (`data-models/tas-workflow-builder/`) - Workflow definitions
- **keycloak** (`data-models/keycloak/`) - Identity and authentication models

#### When to Reference Data Models:
1. Before making schema changes that affect cross-service integration
2. When designing new features that span multiple services
3. When debugging data consistency issues across services
4. When onboarding new developers to understand the complete platform architecture
5. Before implementing new API endpoints that interact with shared infrastructure

**Main Documentation Hub**: `data-models/README.md` - Complete navigation for all data model files, including index and developer guide

## Architecture

### Shared Infrastructure Services
The repository defines a complete observability and data infrastructure stack:

- **Data Layer**: Redis cache (6379), PostgreSQL database (5432), MinIO object storage (9000/9001)
- **Message Queue**: Kafka with Zookeeper (9092/2181) for event streaming
- **Observability**: Prometheus (9090), Grafana (3000), AlertManager (9093), Loki (3100), Alloy (12345), OpenTelemetry Collector (4317/4318)
- **Security**: Keycloak identity management (8081) with dedicated PostgreSQL instance
- **Management**: Services dashboard (8090) and pgAdmin (5050)

### Service Network
All services run on a shared Docker bridge network `tas-shared-network` to enable inter-service communication.

### Dependent Services
The shared infrastructure supports multiple application services that run in separate repositories:
- Aether Backend (8080) with Neo4j graph database
- Aether Frontend (3001) 
- eAIIngest/AudiModal (8084) for audio processing
- TAS MCP Server (8082/50052) for model context protocol
- DeepLake API (8000/50051) for vector database operations
- LLM Router (8085/8086) for multi-provider LLM routing

## Common Commands

### Production Services
```bash
# Start all production services
./start-shared-services.sh

# Stop all services  
./stop-shared-services.sh
```

### Development Mode (NEW!)
```bash
# Auto-detect environment and start development mode
./start-dev-services.sh

# Force Docker Compose development mode
./start-dev-services.sh --docker

# Force Kubernetes development mode  
./start-dev-services.sh --k8s

# Specify private network IP
./start-dev-services.sh --ip 192.168.1.100
```

**Development Mode Features:**
- Enhanced debugging (debug log levels)
- Collects logs from ALL Docker containers (not just shared infrastructure)
- Optimized for private networks and local development
- Separate development configurations for Loki, Alloy, Prometheus, and Grafana
- Auto-detects Docker vs Kubernetes environments
- Creates development-specific .env file with debug settings

### Force Stop All Docker Containers
```bash
./start-shared-services.sh --kill-all
```

### Individual Service Management
```bash
# Start only shared infrastructure
docker-compose -f docker-compose.shared-infrastructure.yml up -d

# Stop shared infrastructure
docker-compose -f docker-compose.shared-infrastructure.yml down

# View logs for specific service
docker-compose -f docker-compose.shared-infrastructure.yml logs -f [service-name]

# Check service status
docker-compose -f docker-compose.shared-infrastructure.yml ps
```

### Service Health Checks
Most services include health checks accessible via:
- Redis: `redis-cli ping`  
- PostgreSQL: `pg_isready -U tasuser`
- MinIO: `curl -f http://localhost:9000/minio/health/live`
- Keycloak: `curl -f http://localhost:8080/health/ready`

## Key Configuration Files

- `docker-compose.shared-infrastructure.yml`: Main infrastructure service definitions
- `.env`: Environment variables for service credentials and configuration
- `services-and-ports.md`: Comprehensive documentation of all services and port allocations
- `shared-monitoring/prometheus.yml`: Prometheus scraping configuration
- `shared-monitoring/grafana/`: Grafana dashboards and provisioning
- `dashboard/`: Static HTML dashboard for service links

## Port Allocation

The repository follows a systematic port allocation strategy:
- 3000-3099: Web interfaces (Grafana, frontends)
- 4000-4499: OpenTelemetry/tracing
- 5000-5099: Databases and admin tools
- 6000-6499: Cache/memory stores
- 7000-7499: Graph databases  
- 8000-8199: Application APIs and management
- 9000-9199: Storage and monitoring
- 50000+: gRPC services

## Environment Variables

The `.env` file contains default credentials for all services. Key variables:
- Database credentials (POSTGRES_USER, POSTGRES_PASSWORD)
- Admin interface passwords (PGADMIN_PASSWORD, GRAFANA_PASSWORD)
- Storage credentials (MINIO_ROOT_USER, MINIO_ROOT_PASSWORD)
- Identity management (KEYCLOAK_ADMIN_PASSWORD)
- Security keys (JWT_SECRET_KEY)

All passwords should be changed for production deployments.

## Development Workflow

### Local Development Setup
1. **Choose your environment**: Docker Compose or Kubernetes
2. **Start development services**: `./start-dev-services.sh`
3. **Access development dashboard**: Auto-detects your private IP and provides URLs
4. **Enhanced logging**: All microservice logs automatically collected
5. **Debug-friendly**: Debug log levels, extended timeouts, enhanced monitoring

### Development-Specific Files
- `docker-compose.dev.yml`: Development overrides for Docker Compose
- `k8s-dev/`: Complete Kubernetes development manifests  
- `shared-monitoring/*/dev/`: Development-specific monitoring configurations
- `start-dev-services.sh`: Smart development startup script

### Production Workflow  
1. Ensure Docker is running
2. Run `./start-shared-services.sh` to start infrastructure
3. Access services via the dashboard at http://localhost:8090
4. Individual application services can then connect to shared infrastructure
5. Use `./stop-shared-services.sh` when finished

## Monitoring and Observability

- **Dashboard**: http://localhost:8090 - Central access point to all services
- **Grafana**: http://localhost:3000 - Visualization and dashboards  
- **Prometheus**: http://localhost:9090 - Metrics collection
- **Loki**: http://localhost:3100 - Log aggregation and search
- **Alloy**: http://localhost:12345 - Modern log/metrics collection agent
- **AlertManager**: http://localhost:9093 - Alert routing

Pre-configured dashboards exist for LLM Router, AudiModal, and Loki logging services in `shared-monitoring/grafana/dashboards/`.

## Centralized Logging with Loki

The TAS infrastructure includes a complete logging stack for centralized log aggregation and analysis:

### Components
- **Loki**: Log aggregation system that stores and indexes logs
- **Alloy**: Modern telemetry collector that replaces Promtail
- **Grafana**: Query and visualize logs with the integrated Loki data source

### Docker Compose Logging
- Alloy automatically collects logs from all containers in the `tas-shared-network`
- Logs are parsed, labeled, and forwarded to Loki for storage
- Access logs via Grafana's Explore tab or the "TAS Infrastructure Logs" dashboard

### Kubernetes Logging  
- Alloy DaemonSet collects logs from all pods in the `tas-shared` namespace
- Kubernetes metadata (pod, namespace, container) is automatically added as labels
- Support for structured logging and log level extraction

### Log Management
- **Retention**: 30 days default (configurable)
- **Storage**: MinIO object storage for Kubernetes, filesystem for Docker Compose
- **Querying**: LogQL query language for advanced log filtering and analysis
- **Alerting**: Integration with AlertManager for log-based alerts