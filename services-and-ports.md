# TAS Services and Ports

> Comprehensive overview of all TAS (Tributary AI System) services, ports, and infrastructure components.

## 🚀 Shared Infrastructure Services (docker-compose.shared-infrastructure.yml)

The shared infrastructure provides common services used across multiple TAS components.

### Core Data Services
- **Redis Cache** (tas-redis-shared): `6379`
  - Purpose: Shared caching and session storage
  - Health Check: `redis-cli ping`
  - Volume: `redis_shared_data`

- **PostgreSQL Database** (tas-postgres-shared): `5432`
  - Purpose: Primary shared database for TAS services
  - Database: `tas_shared`
  - User: `tasuser` / Password: `taspassword` (configurable)
  - Health Check: `pg_isready -U tasuser`
  - Volume: `postgres_shared_data`

- **pgAdmin Database Administration** (tas-pgadmin-shared): `5050`
  - Purpose: Web-based PostgreSQL administration interface
  - Login: `admin@example.com` / `admin123` (configurable)
  - Access: http://localhost:5050
  - Volume: `pgadmin_shared_data`

### Message Queue & Streaming
- **Apache Kafka** (tas-kafka-shared): `9092` (external), `29092` (internal)
  - Purpose: Event streaming and messaging between services
  - Depends on: Zookeeper
  - Health Check: `kafka-broker-api-versions --bootstrap-server localhost:9092`
  - Volume: `kafka_shared_data`

- **Zookeeper** (tas-zookeeper-shared): `2181` (internal only)
  - Purpose: Kafka coordination service
  - Volume: `zookeeper_shared_data`, `zookeeper_shared_logs`

### Object Storage
- **MinIO Object Storage** (tas-minio-shared):
  - **API**: `9000`
  - **Console**: `9001`
  - Purpose: S3-compatible object storage for files, models, and artifacts
  - Credentials: `minioadmin` / `minioadmin123` (configurable)
  - Health Check: `curl -f http://localhost:9000/minio/health/live`
  - Volume: `minio_shared_data`

### Monitoring & Observability
- **Prometheus** (tas-prometheus-shared): `9090`
  - Purpose: Metrics collection and storage
  - Config: `./shared-monitoring/prometheus.yml`
  - Retention: 200h
  - Volume: `prometheus_shared_data`

- **Grafana** (tas-grafana-shared): `3000`
  - Purpose: Dashboards and visualization
  - Login: `admin` / `admin123` (configurable)
  - Config: `./shared-monitoring/grafana/`
  - Volume: `grafana_shared_data`

- **AlertManager** (tas-alertmanager-shared): `9093`
  - Purpose: Alert routing and management
  - Config: `./shared-monitoring/alertmanager/`
  - Volume: `alertmanager_shared_data`

- **Loki** (tas-loki-shared): `3100`
  - Purpose: Log aggregation and search
  - Config: `./shared-monitoring/loki/loki-config.yml`
  - Storage: MinIO backend (K8s) or filesystem (Docker)
  - Volume: `loki_shared_data`
  - Access: http://localhost:3100

- **Alloy** (tas-alloy-shared): `12345`
  - Purpose: Modern telemetry collector (replaces Promtail)
  - Config: `./shared-monitoring/alloy/alloy-config.alloy`
  - Collects: Container logs, system logs, metrics
  - Access: http://localhost:12345 (Web UI)

- **OpenTelemetry Collector** (tas-otel-collector-shared):
  - **OTLP gRPC**: `4317`
  - **OTLP HTTP**: `4318`
  - **Prometheus Metrics**: `8888`
  - **Prometheus Exporter**: `8889`
  - **Health Check**: `13133`
  - Purpose: Distributed tracing and telemetry collection
  - Config: `./shared-monitoring/otel/otel-collector-audimodal.yml`

### Management & Operations
- **Services Dashboard** (tas-dashboard-shared): `8090`
  - Purpose: Centralized dashboard with links to all shared services
  - Access: http://localhost:8090
  - Features: Quick links to all services, status indicators, auto-refresh
  - Health Check: `curl -f http://localhost:8090/health`

### Authentication & Security
- **Keycloak** (tas-keycloak-shared): `8081` (external), `8080` (internal)
  - Purpose: Identity and access management
  - Admin: `admin` / `admin123` (configurable)
  - Database: Dedicated PostgreSQL instance (tas-keycloak-db-shared)
  - Health Check: `curl -f http://localhost:8080/health/ready`

- **Keycloak Database** (tas-keycloak-db-shared): Internal only
  - Purpose: Keycloak's dedicated PostgreSQL instance
  - Database: `keycloak`
  - User: `keycloak` / Password: `keycloak123` (configurable)
  - Volume: `keycloak_db_shared_data`

### Network Configuration
- **Shared Network**: `tas-shared-network`
  - Type: Bridge network
  - Purpose: Inter-service communication for shared infrastructure

### Management Commands
```bash
# Start all shared infrastructure
docker-compose -f docker-compose.shared-infrastructure.yml up -d

# Stop all shared infrastructure  
docker-compose -f docker-compose.shared-infrastructure.yml down

# View logs
docker-compose -f docker-compose.shared-infrastructure.yml logs -f [service-name]

# Health check all services
docker-compose -f docker-compose.shared-infrastructure.yml ps
```

## 🎯 Application Services

### Aether Platform Services
#### Frontend (aether/docker-compose.yml)
- **Aether Frontend**: `3001`
  - Purpose: Main TAS web interface
  - Framework: React/Next.js
  - Dependencies: Aether Backend API

#### Backend (aether-be/docker-compose.yml)  
- **Aether Backend API**: `8080`
  - Purpose: Main API server for TAS platform
  - Database: Neo4j graph database
  - Uses: Shared Redis, PostgreSQL for additional storage

- **Neo4j Graph Database**:
  - **HTTP**: `7474`
  - **Bolt**: `7687`
  - Purpose: Knowledge graph and relationship data
  - Browser: http://localhost:7474

### AudiModal/eAIIngest Services (audimodal/docker-compose.yml)
- **eAIIngest Application**: `8084`
  - Purpose: Audio/media ingestion and processing pipeline
  - Features: Multi-format audio processing, AI transcription
  - Uses: Shared MinIO for file storage, Kafka for messaging

- **PostgreSQL Database**: `5433` (external), `5432` (internal)
  - Purpose: eAIIngest metadata and job tracking
  - Note: Dedicated instance, separate from shared PostgreSQL

### TAS MCP Server (tas-mcp/docker-compose.yml)
- **HTTP API**: `8082`
  - Purpose: Model Context Protocol server
  - Features: Context management, model interaction coordination

- **gRPC**: `50052`
  - Purpose: High-performance inter-service communication
  - Used by: Internal TAS services requiring low-latency communication

- **Health Check**: `8083`
  - Purpose: Service health monitoring endpoint
  - Check: `curl http://localhost:8083/health`

### DeepLake API Service (deeplake-api/docker-compose.yml)
- **HTTP**: `8000`
  - Purpose: Vector database and ML dataset management
  - Features: Vector similarity search, dataset versioning

- **gRPC**: `50051`
  - Purpose: High-performance data operations
  - Used by: ML training and inference pipelines

- **Metrics**: `9091`
  - Purpose: Service-specific metrics (different from shared Prometheus)
  - Exports: Custom DeepLake performance metrics

### LLM Router Services 
#### Standalone Mode (tas-llm-router/docker/docker-compose.dev.yml)
For development/testing without shared infrastructure:

**Core Services:**
- **LLM Router App**: `8085`
- **Redis**: `6380`
- **PostgreSQL**: `5434`
- **Vault**: `8200`

**Observability Stack:**
- **Prometheus**: `9091`  
- **Grafana**: `3002`
- **Jaeger HTTP**: `14268`
- **Jaeger Web UI**: `16686`

**OpenTelemetry Collector:**
- **pprof**: `1888`
- **Prometheus**: `8888`
- **Exporter**: `8889`
- **Health**: `13133`
- **OTLP gRPC**: `4317`
- **OTLP HTTP**: `4318`

#### Aether-Shared Integration Mode (tas-llm-router/docker/docker-compose.aether-shared.yml)
For production deployment using shared infrastructure:

- **LLM Router App**: `8086`
  - Purpose: Enterprise LLM routing with WAF capabilities
  - Features: Multi-provider routing, cost optimization, security
  - Uses: All shared infrastructure services
  - Management: `./start-aether-shared.sh [start|stop|status|logs]`

- **Metrics Adapter**: `9092` (debug profile only)
  - Purpose: Local metrics debugging and development
  - Forward metrics to shared Prometheus

**Shared Infrastructure Usage:**
- Redis (`6379`): Rate limiting, caching
- PostgreSQL (`5432`): Request logs, analytics, configuration
- Prometheus (`9090`): Metrics aggregation
- Grafana (`3000`): Dashboards and visualization
- Keycloak (`8081`): Authentication and authorization
- Kafka (`9092`): Event streaming and audit logs
- MinIO (`9000`): Model artifacts, request/response storage

## 🔗 Service Dependencies

### High-Level Architecture
```
Internet → [Load Balancer] → [TAS Services] → [Shared Infrastructure]
```

### Service Dependency Map
```
Aether Frontend (3001) → Aether Backend (8080) → Neo4j + Shared Services
                                              ↓
eAIIngest (8084) ──────────────────→ Kafka + MinIO + Shared PostgreSQL
                                              ↓
TAS MCP (8082/50052) ──────────────→ Shared Services
                                              ↓
DeepLake API (8000/50051) ─────────→ Shared Services  
                                              ↓
LLM Router (8086) ─────────────────→ All Shared Services
```

## 🚦 Port Allocation Summary

### Reserved Port Ranges
- **3000-3099**: Web interfaces (Grafana: 3000, Aether: 3001, etc.)
- **4000-4499**: OpenTelemetry/Tracing (OTLP: 4317-4318)
- **5000-5099**: Databases & Admin Tools (PostgreSQL: 5432-5434, pgAdmin: 5050)
- **6000-6499**: Cache/Memory (Redis: 6379-6380)
- **7000-7499**: Graph databases (Neo4j: 7474, 7687)
- **8000-8199**: Application APIs & Management (8000, 8080-8086, Dashboard: 8090)
- **8200-8299**: Security/Vault (Vault: 8200)
- **9000-9199**: Storage/Monitoring (MinIO: 9000-9001, Prometheus: 9090-9093)
- **13000+**: Health checks and internal services
- **50000+**: gRPC services (50051-50052)

### Port Conflicts to Avoid
- Avoid using ports already allocated in this document
- Development services use higher port numbers (6380, 5434, etc.)
- Production services use standard ports with shared infrastructure