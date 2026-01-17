# TAS Platform Architecture Overview

## Metadata

- **Document Type**: System Architecture Documentation
- **Service Scope**: All TAS services
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

This document provides a comprehensive architectural overview of the Tributary AI Services (TAS) platform - an enterprise-grade AI infrastructure providing secure AI workflows, multi-modal data processing, intelligent agent orchestration, and vector database management.

### Architecture Philosophy

TAS uses a **microservices architecture** with **shared infrastructure services**. Individual application services exist as independent components, all leveraging centralized infrastructure (Redis, PostgreSQL, Kafka, Keycloak, MinIO, Prometheus, Grafana) via a shared network.

### Key Architectural Principles

1. **Space-Based Multi-Tenancy** - Top-level isolation boundaries with independent resources
2. **Service Independence** - Each microservice owns its data and logic
3. **Event-Driven Communication** - Async communication via Kafka for loose coupling
4. **Centralized Observability** - Unified monitoring, logging, and alerting
5. **Polyglot Persistence** - Right database for the right job (Neo4j, PostgreSQL, Redis, DeepLake)
6. **API-First Design** - RESTful HTTP APIs with OpenAPI specifications

---

## High-Level Architecture Diagram

```mermaid
graph TB
    subgraph "Client Layer"
        Browser[Web Browser]
        Mobile[Mobile App Future]
    end

    subgraph "Edge Layer"
        Ingress[NGINX Ingress Controller]
        CertMgr[cert-manager ACME/Let's Encrypt]
    end

    subgraph "Application Services"
        Frontend[Aether Frontend<br/>React 19 + TypeScript<br/>Port: 3001]
        Backend[Aether Backend<br/>Go + Gin<br/>Port: 8080]
        AudiModal[AudiModal<br/>Go Multi-Modal Processing<br/>Port: 8084]
        DeepLake[DeepLake API<br/>Python Vector DB<br/>Port: 8000]
        LLMRouter[TAS LLM Router<br/>Go Secure Routing<br/>Port: 8085]
        AgentBuilder[TAS Agent Builder<br/>Go Dynamic Agents<br/>Port: 8087]
        MCP[TAS MCP<br/>Go + Node.js Federation<br/>Port: 8082]
    end

    subgraph "Data Layer"
        Neo4j[Neo4j Graph DB<br/>User/Space/Document<br/>Ports: 7474/7687]
        Postgres[PostgreSQL<br/>AudiModal Data<br/>Port: 5432]
        Redis[Redis Cache<br/>Sessions + Queues<br/>Port: 6379]
        MinIO[MinIO Object Storage<br/>Documents + Models<br/>Ports: 9000/9001]
        VectorDB[DeepLake Vector Storage<br/>Embeddings]
    end

    subgraph "Infrastructure Services"
        Keycloak[Keycloak OIDC/OAuth2<br/>Port: 8081]
        Kafka[Kafka Message Broker<br/>Port: 9092]
        Zookeeper[Zookeeper<br/>Port: 2181]
    end

    subgraph "Observability Stack"
        Prometheus[Prometheus Metrics<br/>Port: 9090]
        Grafana[Grafana Dashboards<br/>Port: 3000]
        Loki[Loki Log Aggregation<br/>Port: 3100]
        Alloy[Alloy Log/Metrics Collector<br/>Port: 12345]
        AlertMgr[AlertManager<br/>Port: 9093]
        OTel[OpenTelemetry Collector<br/>Ports: 4317/4318]
    end

    %% Client connections
    Browser --> Ingress
    Mobile -.Future.-> Ingress

    %% Ingress routing
    Ingress --> Frontend
    Ingress --> Backend
    Ingress --> Grafana
    Ingress --> Keycloak

    %% TLS certificate management
    CertMgr -.Manages.-> Ingress

    %% Frontend to Backend
    Frontend --> Backend

    %% Backend to services
    Backend --> Neo4j
    Backend --> Redis
    Backend --> MinIO
    Backend --> AudiModal
    Backend --> DeepLake
    Backend --> Kafka
    Backend --> LLMRouter
    Backend --> AgentBuilder

    %% AudiModal connections
    AudiModal --> Postgres
    AudiModal --> MinIO
    AudiModal --> DeepLake
    AudiModal --> Kafka

    %% DeepLake connections
    DeepLake --> VectorDB
    DeepLake --> MinIO

    %% LLM Router connections
    LLMRouter --> Redis
    LLMRouter -.External LLM APIs.-> Outside[OpenAI, Anthropic, etc.]

    %% Agent Builder connections
    AgentBuilder --> Postgres
    AgentBuilder --> LLMRouter

    %% MCP connections
    MCP --> Backend
    MCP --> Redis

    %% Authentication
    Frontend --> Keycloak
    Backend --> Keycloak
    AudiModal --> Keycloak
    DeepLake --> Keycloak

    %% Kafka consumers
    Kafka --> Backend
    Kafka --> AudiModal

    %% Zookeeper for Kafka
    Kafka --> Zookeeper

    %% Observability data flows
    Frontend -.Metrics.-> Prometheus
    Backend -.Metrics.-> Prometheus
    AudiModal -.Metrics.-> Prometheus
    DeepLake -.Metrics.-> Prometheus
    LLMRouter -.Metrics.-> Prometheus
    AgentBuilder -.Metrics.-> Prometheus

    Frontend -.Logs.-> Alloy
    Backend -.Logs.-> Alloy
    AudiModal -.Logs.-> Alloy
    DeepLake -.Logs.-> Alloy
    LLMRouter -.Logs.-> Alloy
    AgentBuilder -.Logs.-> Alloy

    Alloy --> Loki
    Alloy --> Prometheus

    Frontend -.Traces.-> OTel
    Backend -.Traces.-> OTel
    AudiModal -.Traces.-> OTel
    OTel --> Prometheus

    Prometheus --> Grafana
    Loki --> Grafana
    Prometheus --> AlertMgr

    style Browser fill:#e1f5ff
    style Frontend fill:#4CAF50
    style Backend fill:#2196F3
    style AudiModal fill:#FF9800
    style DeepLake fill:#9C27B0
    style LLMRouter fill:#F44336
    style AgentBuilder fill:#00BCD4
    style Neo4j fill:#4DB33D
    style Postgres fill:#336791
    style Keycloak fill:#4D4D4D
    style Kafka fill:#231F20
    style Prometheus fill:#E6522C
    style Grafana fill:#F46800
```

---

## Service Topology

### Application Services (Port Range: 3000-8999)

#### Aether Frontend (Port 3001)
- **Technology**: React 19, TypeScript, Vite, Tailwind CSS
- **Purpose**: User interface for document management and AI queries
- **State Management**: Redux Toolkit with persistent localStorage
- **Authentication**: Keycloak OIDC with JWT tokens
- **Key Features**:
  - Document upload and management UI
  - Notebook organization
  - Space-based multi-tenancy
  - Real-time status updates via polling
  - Dark mode support

#### Aether Backend (Port 8080)
- **Technology**: Go 1.21+, Gin HTTP framework
- **Purpose**: Core API for user, space, notebook, and document management
- **Database**: Neo4j graph database
- **Key Responsibilities**:
  - User authentication and authorization
  - Document lifecycle management
  - Space and notebook CRUD operations
  - Cross-service orchestration
  - Kafka event consumption
- **API Endpoints**:
  - `/api/v1/users` - User management
  - `/api/v1/spaces` - Space management
  - `/api/v1/notebooks` - Notebook CRUD
  - `/api/v1/documents` - Document upload and retrieval
  - `/api/v1/agents` - Agent management (proxy to Agent Builder)

#### AudiModal (Port 8084)
- **Technology**: Go, PDFium, Tesseract OCR, Whisper
- **Purpose**: Multi-modal document processing and analysis
- **Database**: PostgreSQL
- **Processing Capabilities**:
  - PDF text extraction and OCR
  - Document chunking (semantic, fixed, paragraph)
  - PII detection and DLP scanning
  - Language detection
  - Content classification
- **Processing Tiers**:
  - Tier 1: <10MB (real-time)
  - Tier 2: 10MB-1GB (async)
  - Tier 3: >1GB (batch)

#### DeepLake API (Port 8000)
- **Technology**: Python 3.9+, FastAPI, Deep Lake SDK
- **Purpose**: Vector database management and similarity search
- **Storage**: Deep Lake vector database
- **Key Features**:
  - Embedding generation (OpenAI, Cohere, local models)
  - Vector similarity search
  - Tenant-scoped dataset namespacing
  - Multi-model support
- **Dataset Pattern**: `tenants/{tenant_id}/default`

#### TAS LLM Router (Port 8085/8086)
- **Technology**: Go, Fiber HTTP framework
- **Purpose**: Secure multi-provider LLM routing with compliance
- **Key Features**:
  - Multi-provider support (OpenAI, Anthropic, Cohere, local)
  - Request/response compliance scanning
  - Rate limiting and quota management
  - Cost tracking per tenant
  - Model fallback and retry logic
- **Ports**:
  - 8085: HTTP API
  - 8086: Metrics endpoint

#### TAS Agent Builder (Port 8087)
- **Technology**: Go
- **Purpose**: Dynamic intelligent agent generation
- **Database**: PostgreSQL (shared)
- **Features**:
  - Agent definition and configuration
  - Execution tracking and history
  - Integration with LLM Router
  - Space-based isolation

#### TAS MCP (Port 8082)
- **Technology**: Go + Node.js
- **Purpose**: Model Context Protocol federation server
- **Features**:
  - Protocol buffer serialization
  - Event distribution
  - Server registry
- **Ports**:
  - 8082: HTTP API
  - 50052: gRPC service

---

### Data Layer Services

#### Neo4j Graph Database (Ports 7474/7687)
- **Purpose**: Graph data for Aether Backend
- **Data Model**:
  - Nodes: User, Space, Notebook, Document, Team, Organization
  - Relationships: MEMBER_OF, OWNED_BY, BELONGS_TO, IN_SPACE
- **Isolation**: tenant_id and space_id on all nodes
- **Indexes**: Composite indexes on (space_id, status), (tenant_id, type)
- **Ports**:
  - 7474: HTTP API (Browser UI)
  - 7687: Bolt protocol (application access)

#### PostgreSQL (Port 5432)
- **Purpose**: Relational data for AudiModal, Agent Builder, Keycloak
- **Databases**:
  - `tas_shared`: AudiModal and Agent Builder tables
  - `keycloak`: Keycloak identity data
- **Key Tables**:
  - `tenants`, `files`, `chunks`, `processing_sessions` (AudiModal)
  - `agents`, `executions` (Agent Builder)
  - `user_entity`, `user_attribute`, `credential` (Keycloak)

#### Redis (Port 6379)
- **Purpose**: Caching and session management
- **Use Cases**:
  - User session storage
  - API rate limiting counters
  - Temporary data caching
  - Job queue coordination
- **Databases**:
  - DB 0: Session storage
  - DB 1: Rate limiting
  - DB 2: Cache

#### MinIO Object Storage (Ports 9000/9001)
- **Purpose**: S3-compatible object storage
- **Buckets**:
  - `aether-storage`: User-uploaded documents
  - `model-storage`: ML model artifacts
  - `loki-storage`: Log storage (Kubernetes)
- **Path Pattern**: `{bucket}/{tenant_id}/{space_id}/documents/{doc_id}/`
- **Ports**:
  - 9000: S3 API
  - 9001: Web console

#### Deep Lake Vector Storage
- **Purpose**: Efficient vector embedding storage
- **Features**:
  - Multi-dimensional vector indexing
  - Fast similarity search (cosine, euclidean, dot product)
  - Tensor storage (embeddings, text, metadata)
  - Version control for datasets
- **Namespacing**: Tenant-scoped datasets

---

### Infrastructure Services

#### Keycloak (Port 8081)
- **Purpose**: Centralized identity and access management
- **Protocol**: OIDC/OAuth2/SAML
- **Realms**:
  - `master`: Admin realm
  - `aether`: Application realm
- **Clients**:
  - `aether-backend`: Confidential client (client secret)
  - `aether-frontend`: Public client (PKCE)
  - Service accounts for inter-service auth
- **Features**:
  - User registration and verification
  - JWT token issuance
  - Multi-factor authentication (future)
  - Social login (future)

#### Kafka + Zookeeper (Ports 9092/2181)
- **Purpose**: Event streaming and async communication
- **Topics**:
  - `aether.document.processed`: Document processing completion
  - `aether.document.failed`: Processing failures
  - `aether.user.created`: New user events
  - `aether.space.created`: New space events
- **Consumer Groups**:
  - `aether-backend-consumers`: Backend event processing
  - `audimodal-consumers`: AudiModal event handling
- **Retention**: 7 days default

---

### Observability Stack

#### Prometheus (Port 9090)
- **Purpose**: Metrics collection and storage
- **Scrape Targets**: All application services `/metrics` endpoints
- **Retention**: 15 days
- **Key Metrics**:
  - Request rates, latencies, error rates (RED metrics)
  - Resource utilization (CPU, memory, disk)
  - Business metrics (uploads, processing times, user counts)

#### Grafana (Port 3000)
- **Purpose**: Visualization and dashboards
- **Data Sources**: Prometheus, Loki, Neo4j
- **Pre-built Dashboards**:
  - TAS Infrastructure Overview
  - LLM Router Metrics
  - AudiModal Processing
  - Kubernetes Cluster Health
  - Loki Logging Dashboard
- **Alerting**: Integrated with AlertManager

#### Loki (Port 3100)
- **Purpose**: Log aggregation and querying
- **Storage**:
  - Docker Compose: Filesystem
  - Kubernetes: MinIO object storage
- **Retention**: 30 days
- **Features**:
  - Label-based indexing (service, namespace, pod)
  - LogQL query language
  - Integration with Grafana Explore

#### Alloy (Port 12345)
- **Purpose**: Modern telemetry collector (replaces Promtail)
- **Capabilities**:
  - Log collection from all containers/pods
  - Metrics collection and forwarding
  - Log parsing and enrichment
  - Multi-destination forwarding

#### AlertManager (Port 9093)
- **Purpose**: Alert routing and notification
- **Integrations**: Slack, email, PagerDuty
- **Alert Rules**:
  - Service down alerts
  - High error rate alerts
  - Disk space warnings
  - Processing failure alerts

#### OpenTelemetry Collector (Ports 4317/4318)
- **Purpose**: Distributed tracing collection
- **Protocols**:
  - 4317: gRPC
  - 4318: HTTP
- **Exporters**: Prometheus, Jaeger (future)

---

## Network Architecture

### Docker Compose Deployment

#### Shared Network
- **Network Name**: `tas-shared-network`
- **Driver**: Bridge
- **Subnet**: Auto-assigned
- **Service Discovery**: Container names (DNS)

#### Container Communication Examples
```yaml
# Backend connecting to Neo4j
NEO4J_URI: bolt://neo4j:7687

# Backend connecting to Redis
REDIS_ADDR: tas-redis-shared:6379

# AudiModal connecting to PostgreSQL
DATABASE_URL: postgresql://tasuser:password@tas-postgres-shared:5432/tas_shared

# Any service connecting to Kafka
KAFKA_BROKERS: tas-kafka-shared:9092
```

#### Port Mapping Strategy
- External ports may differ from internal container ports
- Example: Keycloak container port 8080 → host port 8081
- See `aether-shared/services-and-ports.md` for complete mapping

### Kubernetes Deployment

#### Namespaces
- `tas-shared`: Infrastructure services (Keycloak, PostgreSQL, Redis, Kafka, MinIO)
- `aether-be`: Aether Backend + Neo4j
- `ingress-nginx`: NGINX Ingress Controller
- `cert-manager`: Certificate management
- `monitoring`: Prometheus, Grafana, Loki stack

#### Service Discovery
- **Cluster DNS**: `{service-name}.{namespace}.svc.cluster.local`
- **Example**: `keycloak-shared.tas-shared.svc.cluster.local:8080`

#### Ingress Configuration
```yaml
# Aether Frontend
https://aether.tas.scharber.com → aether-frontend:80

# Grafana
https://grafana.tas.scharber.com → grafana:3000

# Keycloak
https://keycloak.tas.scharber.com → keycloak-shared:8080

# MinIO Console
https://minio.tas.scharber.com → minio-console:9001
```

#### TLS Certificates
- **Provider**: Let's Encrypt via cert-manager
- **Challenge**: HTTP-01 (ACME protocol)
- **Renewal**: Automatic (30 days before expiry)

---

## Data Flow Patterns

### Document Upload Flow

```
User → Frontend → Backend → MinIO (storage)
                    ↓
                  Neo4j (metadata)
                    ↓
                AudiModal (processing)
                    ↓
              PostgreSQL (file records)
                    ↓
              DeepLake (embeddings)
                    ↓
                Kafka (completion event)
                    ↓
              Backend (status update)
                    ↓
                  Neo4j (final status)
```

### User Authentication Flow

```
Frontend → Keycloak (login)
            ↓
        JWT token
            ↓
Frontend → Backend (API request with token)
            ↓
        Backend → Keycloak (validate token)
            ↓
        Backend → Neo4j (fetch user data)
            ↓
        Frontend (user profile + spaces)
```

### AI Query Flow

```
Frontend → Backend → DeepLake (similarity search)
                        ↓
                    Vector results
                        ↓
              Backend → LLM Router (augmented prompt)
                        ↓
                    External LLM API
                        ↓
              Backend → Neo4j (save query history)
                        ↓
              Frontend (display results)
```

---

## Multi-Tenancy Architecture

### Space-Based Isolation

**Hierarchy**:
```
User → Spaces → Notebooks → Documents
```

**Tenant ID Propagation**:
1. User logs in, selects Space
2. Frontend sends `X-Space-ID` header with every API request
3. Backend looks up `tenant_id` for the space
4. Backend includes `tenant_id` in all downstream service calls
5. Each service enforces tenant isolation via database queries

### Tenant ID Mapping Chain

```
Keycloak User (id)
    ↓
Aether User (id = Keycloak id)
    ↓
Aether Space (space_id + tenant_id)
    ↓
AudiModal Tenant (id = tenant_id)
    ↓
DeepLake Dataset (tenants/{tenant_id}/default)
    ↓
Agent Builder Agent (space_id)
```

### Isolation Enforcement

**Neo4j (Aether Backend)**:
```cypher
// Every query MUST include space_id filter
MATCH (d:Document)
WHERE d.space_id = $space_id AND d.deleted_at IS NULL
RETURN d
```

**PostgreSQL (AudiModal)**:
```sql
-- Every query MUST include tenant_id filter
SELECT * FROM files
WHERE tenant_id = $1 AND deleted_at IS NULL;
```

**DeepLake**:
```python
# Dataset path includes tenant_id
dataset_path = f"tenants/{tenant_id}/default"
ds = deeplake.load(dataset_path)
```

---

## Security Architecture

### Authentication Layers

1. **Frontend → Keycloak**: User login via OIDC
2. **Frontend → Backend**: JWT token in Authorization header
3. **Backend → Services**: Service account tokens or API keys
4. **Service → Service**: Mutual TLS (future)

### Authorization Model

**Keycloak Roles**:
- `user`: Standard user
- `admin`: System administrator
- `developer`: API access

**Space Roles** (in Neo4j):
- `owner`: Full control
- `admin`: Manage members and content
- `member`: Read/write access
- `viewer`: Read-only access

### Data Protection

**At Rest**:
- PostgreSQL: Encrypted volumes (Kubernetes secrets)
- MinIO: Server-side encryption (AES-256)
- Neo4j: Encrypted backups

**In Transit**:
- HTTPS/TLS for all external communication
- Internal services: TLS optional (trusted network)
- Kafka: SASL/SSL (production)

**Secrets Management**:
- Kubernetes: Native Secrets (base64-encoded)
- Docker Compose: `.env` files (gitignored)
- Production: External secrets manager (AWS Secrets Manager, Vault - future)

---

## Scalability & Performance

### Horizontal Scaling

**Stateless Services** (can scale horizontally):
- Aether Backend (multiple replicas)
- AudiModal (worker pool model)
- DeepLake API (load balanced)
- LLM Router (stateless routing)

**Stateful Services** (vertical scaling or clustering):
- Neo4j (cluster mode with read replicas - future)
- PostgreSQL (primary + read replicas - future)
- Redis (cluster mode - future)
- Kafka (3+ broker cluster - production)

### Load Balancing

**Kubernetes**:
- Service type: ClusterIP (internal)
- Load balancing: kube-proxy (iptables)
- Ingress: NGINX with round-robin

**Docker Compose**:
- External load balancer required
- HAProxy or NGINX reverse proxy

### Caching Strategy

**Redis Caching**:
- User profile cache (TTL: 5 minutes)
- Space membership cache (TTL: 10 minutes)
- Keycloak public keys (TTL: 1 hour)

**Application-Level Caching**:
- Frontend: Redux state persistence
- Backend: In-memory LRU cache for frequent queries

### Database Optimization

**Neo4j**:
- Composite indexes on (space_id, status)
- Full-text search indexes on search_text fields
- Connection pooling (max 50 connections)

**PostgreSQL**:
- B-tree indexes on tenant_id, status, created_at
- JSONB GIN indexes for metadata searches
- Connection pooling (max 100 connections)

---

## Resilience & Fault Tolerance

### Circuit Breakers

**Backend → AudiModal**:
- Timeout: 30 seconds
- Failure threshold: 5 consecutive failures
- Half-open retry: After 60 seconds

**Backend → DeepLake**:
- Timeout: 10 seconds
- Failure threshold: 3 consecutive failures
- Half-open retry: After 30 seconds

### Retry Policies

**Exponential Backoff**:
```
Attempt 1: Immediate
Attempt 2: 1 second
Attempt 3: 2 seconds
Attempt 4: 4 seconds
Attempt 5: 8 seconds
Max attempts: 5
```

**Idempotency**:
- All POST/PUT requests use idempotency keys
- Duplicate detection via checksum (documents)
- Kafka exactly-once semantics (production)

### Health Checks

**Kubernetes**:
```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /health/ready
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 5
```

**Health Check Endpoints**:
- `/health`: Overall health (200 = healthy)
- `/health/live`: Liveness (is process running?)
- `/health/ready`: Readiness (can accept traffic?)
- `/metrics`: Prometheus metrics

### Backup & Recovery

**Neo4j**:
- Daily backups to MinIO/S3
- Point-in-time recovery (WAL logs)
- Backup retention: 30 days

**PostgreSQL**:
- WAL archiving to MinIO/S3
- Daily full backups
- Backup retention: 30 days

**MinIO**:
- Replication to remote bucket (production)
- Versioning enabled
- Lifecycle policies for old versions

---

## Deployment Strategies

### Development (Docker Compose)

**Start Command**:
```bash
./start-dev-services.sh
```

**Features**:
- Auto-detects Docker/Kubernetes environment
- Enhanced debugging (debug log levels)
- Collects logs from ALL containers
- Extended timeouts
- Development-specific .env file

**Access URLs**:
- Dashboard: http://localhost:8090
- Frontend: http://localhost:3001
- Backend: http://localhost:8080
- Grafana: http://localhost:3000
- Neo4j Browser: http://localhost:7474

### Staging (Kubernetes)

**Namespace**: `tas-staging`

**Deployment**:
```bash
cd aether-shared/k8s-shared-infrastructure
./deploy.sh --environment staging
```

**Features**:
- Reduced replica counts (1-2 per service)
- Shared infrastructure with production
- Separate databases and buckets
- TLS certificates via Let's Encrypt staging

### Production (Kubernetes)

**Namespace**: `tas-shared`, `aether-be`

**Deployment**:
```bash
cd aether-shared/k8s-shared-infrastructure
./deploy.sh --environment production
```

**Features**:
- High availability (3+ replicas per service)
- Resource limits and requests defined
- Horizontal Pod Autoscaler (HPA) enabled
- PodDisruptionBudget for safe rolling updates
- TLS certificates via Let's Encrypt production

**Rolling Update Strategy**:
```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: 1
    maxUnavailable: 0
```

---

## Monitoring & Alerting

### Service-Level Objectives (SLOs)

**Aether Backend**:
- Availability: 99.9% (8.76 hours downtime/year)
- P95 Latency: <500ms for API requests
- Error Rate: <0.1% of requests

**AudiModal**:
- Processing Success Rate: >95%
- P95 Processing Time: <60 seconds (Tier 2)
- Availability: 99.5%

**DeepLake API**:
- Query Latency P95: <200ms
- Embedding Generation P95: <2 seconds
- Availability: 99.9%

### Key Dashboards

1. **TAS Platform Overview**
   - Service health status grid
   - Request rates across all services
   - Error rates and latencies
   - Resource utilization

2. **Document Processing Pipeline**
   - Upload success/failure rates
   - Processing duration by tier
   - Embedding generation metrics
   - Queue depths and lag

3. **User Activity**
   - Active users (hourly, daily, weekly)
   - Documents uploaded per tenant
   - API usage by endpoint
   - Storage usage by tenant

4. **Infrastructure Health**
   - CPU, memory, disk usage
   - Network throughput
   - Database connection pools
   - Kafka consumer lag

### Alert Rules

**Critical** (Page immediately):
- Any service down for >5 minutes
- Error rate >10% for >5 minutes
- Database disk usage >95%
- Kafka consumer lag >5000 messages

**Warning** (Slack notification):
- Error rate >5% for >10 minutes
- P95 latency >1 second for >10 minutes
- Database disk usage >85%
- Memory usage >85%

---

## Technology Stack Summary

### Languages
- **Go 1.21+**: Backend services (Aether, AudiModal, LLM Router, Agent Builder)
- **Python 3.9+**: DeepLake API, ML services
- **TypeScript**: Aether Frontend (React 19)
- **Node.js**: TAS MCP components

### Frameworks
- **Backend**: Gin, Fiber, Echo (Go), FastAPI (Python)
- **Frontend**: React 19, Vite, Redux Toolkit
- **Styling**: Tailwind CSS

### Databases
- **Graph**: Neo4j 5.x
- **Relational**: PostgreSQL 15
- **Cache**: Redis 7
- **Vector**: Deep Lake
- **Object Storage**: MinIO (S3-compatible)

### Infrastructure
- **Orchestration**: Kubernetes (K3s), Docker Compose
- **Ingress**: NGINX Ingress Controller
- **Certificates**: cert-manager + Let's Encrypt
- **Message Queue**: Kafka + Zookeeper
- **Identity**: Keycloak (OIDC/OAuth2)

### Observability
- **Metrics**: Prometheus, OpenTelemetry
- **Logging**: Loki, Alloy
- **Visualization**: Grafana
- **Alerting**: AlertManager
- **Tracing**: OpenTelemetry Collector

---

## Related Documentation

### Internal References
- [Platform-wide ERD](./platform-erd.md) - Complete data model
- [User Onboarding Flow](../flows/user-onboarding.md) - User provisioning
- [Document Upload Flow](../flows/document-upload.md) - Document processing
- [Services and Ports](../../services-and-ports.md) - Port allocation guide
- [Space-Based Implementation](../../../SPACE_BASED_IMPLEMENTATION_PLAN.md) - Multi-tenancy design

### External References
- Neo4j Documentation: https://neo4j.com/docs/
- Kubernetes Documentation: https://kubernetes.io/docs/
- Kafka Documentation: https://kafka.apache.org/documentation/
- Keycloak Documentation: https://www.keycloak.org/documentation

---

## Known Limitations

1. **No Multi-Region Support**
   - All services run in single region
   - Future: Multi-region deployment with data replication

2. **Limited Auto-Scaling**
   - Manual scaling configuration required
   - Future: Dynamic HPA based on custom metrics

3. **No Disaster Recovery Automation**
   - Manual failover procedures
   - Future: Automated DR with active-passive setup

4. **Single Kafka Broker (Dev)**
   - Development uses single Kafka instance
   - Production: 3+ broker cluster required

---

## Future Enhancements

### Q1 2026
- Multi-factor authentication (MFA)
- Social login providers (Google, GitHub)
- Enhanced RBAC with fine-grained permissions

### Q2 2026
- Multi-region deployment
- Active-active database replication
- CDN integration for static assets

### Q3 2026
- Real-time collaboration features
- WebSocket support for live updates
- GraphQL API alongside REST

### Q4 2026
- Mobile apps (iOS, Android)
- Offline-first capabilities
- Edge computing deployment

---

## Changelog

### 2026-01-06
- Initial architecture documentation created
- Documented complete service topology
- Added network architecture for Docker and Kubernetes
- Documented data flow patterns
- Added security, scalability, and resilience sections
- Included monitoring and deployment strategies

---

**Maintained by**: TAS Platform Team
**Document Owner**: Platform Architecture Team
**Review Frequency**: Quarterly
**Next Review**: 2026-04-06
