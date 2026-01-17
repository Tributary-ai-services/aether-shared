# Grafana Dashboard Inventory

**Last Updated**: 2025-12-10
**Total Dashboards**: 19

## Quick Reference

| Category | Dashboard | UID | Key Metrics | Data Source |
|----------|-----------|-----|-------------|-------------|
| Infrastructure | TAS Infrastructure Overview | `tas-infrastructure-overview` | All services status, CPU, memory, network, alerts | Prometheus |
| Infrastructure | Redis Infrastructure | `redis-infrastructure` | Connections, memory, hit rate, commands/sec | Prometheus |
| Infrastructure | PostgreSQL Infrastructure | `postgresql-infrastructure` | Connections, transactions, DB size, cache hits | Prometheus |
| Infrastructure | Kafka Infrastructure | `kafka-infrastructure` | Broker throughput, message rate, consumer lag | Prometheus |
| Infrastructure | MinIO Infrastructure | `minio-infrastructure` | Storage capacity, object count, throughput | Prometheus |
| Infrastructure | Keycloak Infrastructure | `keycloak-infrastructure` | Active sessions, auth rate, response time | Prometheus |
| LLM Router | LLM Router Overview | N/A | Request routing, model usage, costs | Prometheus |
| LLM Router | LLM Router Security | N/A | WAF blocks, rate limits, suspicious activity | Prometheus |
| LLM Router | LLM Router Working | N/A | Operational metrics (working version) | Prometheus |
| LLM Router | LLM Router Security Working | N/A | Security metrics (working version) | Prometheus |
| AudiModal | AudiModal Overview | N/A | Service status, requests, memory, goroutines | Prometheus |
| AudiModal | AudiModal Storage | N/A | Heap memory, allocation rate, GC metrics | Prometheus |
| Loki | TAS Infrastructure Logs | N/A | Centralized log viewing and search | Loki |
| DeepLake | Service Overview | N/A | API health, request rates, latency | Prometheus |
| DeepLake | Vector Operations | N/A | Search performance, embedding operations | Prometheus |
| DeepLake | Search Performance | N/A | Query latency, result quality | Prometheus |
| DeepLake | Tenant Analytics | N/A | Per-tenant usage, storage, operations | Prometheus |
| DeepLake | Cache and Errors | N/A | Cache hits, error rates, retries | Prometheus |
| Aether | Aether Backend Metrics | `aether-backend` | Request rate, response time, errors, CPU, memory | Prometheus |

---

## Infrastructure Dashboards

### 1. TAS Infrastructure Overview

**File**: `shared-monitoring/grafana/dashboards/infrastructure/tas-infrastructure-overview.json`
**UID**: `tas-infrastructure-overview`
**Folder**: Infrastructure

#### Purpose
Unified view of all shared infrastructure services health and resource usage.

#### Key Panels
- **Service Status Grid**: Up/down status for Redis, PostgreSQL, Kafka, MinIO, Keycloak
- **Overall CPU Usage**: Aggregate CPU usage across all services
- **Overall Memory Usage**: Aggregate memory consumption
- **Network Traffic**: Combined network I/O for all services
- **Active Alerts**: Current firing alerts from AlertManager

#### Use Cases
- Quick health check of entire infrastructure
- Identify resource bottlenecks
- Monitor overall system health
- Alert triage and investigation

#### Metrics Used
- `redis_up`, `pg_up`, `kafka_server_replicamanager_leadercount`
- `node_cpu_seconds_total`, `node_memory_MemAvailable_bytes`
- `container_network_receive_bytes_total`, `container_network_transmit_bytes_total`
- `ALERTS{alertstate="firing"}`

#### Maintenance Notes
- Update when new infrastructure services are added
- Adjust thresholds based on actual usage patterns
- Review quarterly for relevance

---

### 2. Redis Infrastructure

**File**: `shared-monitoring/grafana/dashboards/infrastructure/redis.json`
**UID**: `redis-infrastructure`
**Folder**: Infrastructure

#### Purpose
Monitor Redis cache performance, memory usage, and client connections.

#### Key Panels
- **Redis Status**: Binary up/down indicator
- **Connected Clients**: Active client connection count
- **Memory Usage**: Used vs max memory configuration
- **Cache Hit Rate**: Percentage of successful cache lookups
- **Commands per Second**: Rate of Redis commands by type
- **Total Keys**: Number of keys in each database
- **Network I/O**: Bytes in/out over time

#### Use Cases
- Identify cache performance issues
- Monitor memory consumption trends
- Detect connection leaks
- Optimize cache hit rates

#### Metrics Used
- `redis_up`, `redis_connected_clients`
- `redis_memory_used_bytes`, `redis_memory_max_bytes`
- `redis_keyspace_hits_total`, `redis_keyspace_misses_total`
- `redis_commands_total`, `redis_db_keys`
- `redis_net_input_bytes_total`, `redis_net_output_bytes_total`

#### Maintenance Notes
- Requires Redis Exporter configured with Prometheus
- Update job label if Redis service name changes
- Monitor for evicted keys (not currently tracked - TODO)

---

### 3. PostgreSQL Infrastructure

**File**: `shared-monitoring/grafana/dashboards/infrastructure/postgresql.json`
**UID**: `postgresql-infrastructure`
**Folder**: Infrastructure

#### Purpose
Database performance monitoring including connections, transactions, and query performance.

#### Key Panels
- **PostgreSQL Status**: Database availability
- **Active Connections**: Current active database connections
- **Transactions per Second**: Commit vs rollback rate
- **Database Size**: Storage consumption over time
- **Cache Hit Ratio**: Buffer cache effectiveness
- **Lock Count**: Current locks by type

#### Use Cases
- Monitor database health and performance
- Identify connection pool issues
- Track transaction rates
- Optimize query performance
- Detect blocking queries

#### Metrics Used
- `pg_up`
- `pg_stat_database_numbackends`
- `pg_stat_database_xact_commit`, `pg_stat_database_xact_rollback`
- `pg_database_size_bytes`
- `pg_stat_database_blks_hit`, `pg_stat_database_blks_read`
- `pg_locks_count`

#### Maintenance Notes
- Requires PostgreSQL Exporter
- Database name currently hardcoded to `tas_shared`
- Add slow query tracking (TODO)
- Add replication lag monitoring for HA setups (TODO)

---

### 4. Kafka Infrastructure

**File**: `shared-monitoring/grafana/dashboards/infrastructure/kafka.json`
**UID**: `kafka-infrastructure`
**Folder**: Infrastructure

#### Purpose
Monitor Kafka broker health, topic throughput, and consumer lag.

#### Key Panels
- **Active Brokers**: Number of online Kafka brokers
- **Broker Throughput**: Bytes in/out per second
- **Message Rate**: Messages produced per second
- **Partition Count**: Total partitions across topics
- **Consumer Lag**: Messages behind for each consumer group
- **Under-Replicated Partitions**: Partitions not fully replicated

#### Use Cases
- Monitor Kafka cluster health
- Identify slow consumers
- Detect replication issues
- Optimize producer/consumer performance
- Plan capacity for new topics

#### Metrics Used
- `kafka_server_replicamanager_leadercount`
- `kafka_server_brokertopicmetrics_bytesin_total`, `kafka_server_brokertopicmetrics_bytesout_total`
- `kafka_server_brokertopicmetrics_messagesin_total`
- `kafka_server_replicamanager_partitioncount`
- `kafka_consumergroup_lag`
- `kafka_server_replicamanager_underreplicatedpartitions`

#### Maintenance Notes
- Requires Kafka JMX Exporter
- Consumer lag requires consumer group monitoring
- Add topic-specific metrics (TODO)
- Add broker disk usage (TODO)

---

### 5. MinIO Infrastructure

**File**: `shared-monitoring/grafana/dashboards/infrastructure/minio.json`
**UID**: `minio-infrastructure`
**Folder**: Infrastructure

#### Purpose
Object storage capacity, operations, and performance monitoring.

#### Key Panels
- **Online Nodes**: Number of healthy MinIO nodes
- **Storage Capacity**: Total vs free storage space
- **Object Count by Bucket**: Number of objects in each bucket
- **Network Throughput**: Upload/download bandwidth
- **API Request Rate**: Operations per second by API call
- **Error Rate**: Failed API calls

#### Use Cases
- Monitor storage capacity
- Track object growth
- Optimize bandwidth usage
- Identify failing operations
- Plan storage expansion

#### Metrics Used
- `minio_cluster_nodes_online_total`
- `minio_cluster_capacity_usable_total_bytes`, `minio_cluster_capacity_usable_free_bytes`
- `minio_bucket_usage_object_total`
- `minio_s3_traffic_received_bytes`, `minio_s3_traffic_sent_bytes`
- `minio_s3_requests_total`, `minio_s3_requests_errors_total`

#### Maintenance Notes
- Requires MinIO Prometheus metrics endpoint
- Add bucket-specific quotas (TODO)
- Add lifecycle policy metrics (TODO)

---

### 6. Keycloak Infrastructure

**File**: `shared-monitoring/grafana/dashboards/infrastructure/keycloak.json`
**UID**: `keycloak-infrastructure`
**Folder**: Infrastructure

#### Purpose
Identity and access management monitoring including authentication rates and sessions.

#### Key Panels
- **Active Sessions**: Current authenticated user sessions
- **Authentication Rate**: Login attempts, successes, and failures
- **User Count by Realm**: Total users in each realm
- **Response Time**: 95th and 50th percentile latency
- **Error Rate**: 4xx and 5xx HTTP errors
- **Database Connection Pool**: Active vs idle connections

#### Use Cases
- Monitor authentication service health
- Detect brute force attacks
- Track user growth
- Optimize performance
- Identify database connection issues

#### Metrics Used
- `keycloak_active_sessions`
- `keycloak_login_attempts_total`, `keycloak_login_successes_total`, `keycloak_login_failures_total`
- `keycloak_realm_users_total`
- `keycloak_request_duration_seconds_bucket`
- `keycloak_http_requests_total`
- `keycloak_db_pool_active_connections`, `keycloak_db_pool_idle_connections`

#### Maintenance Notes
- Requires Keycloak metrics endpoint configured
- Metric names may vary by Keycloak version
- Add realm-specific dashboards for multi-tenant setups (TODO)
- Add token issuance metrics (TODO)

---

## LLM Router Dashboards

### 7-10. LLM Router Suite

**Files**:
- `llm-router-overview.json`
- `llm-router-security.json`
- `llm-router-working.json`
- `llm-router-security-working.json`

**Folder**: LLM Router

#### Purpose
Monitor multi-provider LLM routing including cost optimization, security (WAF), and performance.

#### Key Metrics
- Request routing by provider (OpenAI, Anthropic, Google, etc.)
- Cost tracking per model and provider
- WAF security events and blocks
- Rate limiting enforcement
- Response time by model
- Token usage and billing

#### Use Cases
- Cost optimization and budgeting
- Security threat detection
- Performance tuning
- Provider health monitoring
- Capacity planning

#### Maintenance Notes
- Two versions exist: production and "working" (development)
- Consider consolidating to single dashboard per type
- Update provider list as new LLM providers are added

---

## AudiModal Dashboards

### 11. AudiModal Overview

**File**: `audimodal-overview.json`
**Folder**: AudiModal

#### Purpose
General health and performance metrics for audio/media processing service.

#### Key Panels
- Service status indicator
- HTTP request rates to /metrics endpoint
- Memory usage (allocated and heap)
- Garbage collection rates
- Active goroutines
- Heap object count
- GC pause duration

#### Metrics Used
- `up{job="audimodal"}`
- `promhttp_metric_handler_requests_total`
- `go_memstats_alloc_bytes`, `go_memstats_heap_inuse_bytes`
- `go_gc_duration_seconds_count`
- `go_goroutines`, `go_memstats_heap_objects`

#### Maintenance Notes
- Currently uses Go runtime metrics only
- Add application-specific metrics for audio processing (TODO)
- Track file uploads, processing queue, failures

---

### 12. AudiModal Storage

**File**: `audimodal-storage.json`
**Folder**: AudiModal

#### Purpose
Detailed memory management and garbage collection analysis.

#### Key Panels
- Heap memory pie chart (allocated, idle, in-use)
- Memory usage trends over time
- Heap allocation rate
- Memory statistics grid
- GC impact on heap
- Memory system breakdown
- Allocation size distribution heatmap
- Memory efficiency metrics

#### Use Cases
- Diagnose memory leaks
- Optimize memory usage
- Tune garbage collection
- Capacity planning

#### Maintenance Notes
- Highly detailed - may be too granular for general monitoring
- Consider simplifying for production use
- Good for troubleshooting memory issues

---

## Loki Dashboard

### 13. TAS Infrastructure Logs

**File**: `tas-infrastructure-logs.json`
**Folder**: TAS Infrastructure Logs

#### Purpose
Centralized log viewing, searching, and analysis across all TAS services.

#### Key Features
- Log stream viewer with filtering
- LogQL query interface
- Log level breakdown
- Service-specific log filtering
- Time-based log browsing

#### Use Cases
- Troubleshooting application issues
- Security audit trail
- Error investigation
- Performance analysis
- Compliance and logging requirements

#### Maintenance Notes
- See [LOKI.md](../../aether-secrets/LOKI.md) for complete LogQL query examples
- Update as new services are added
- Create service-specific log dashboards as needed

---

## DeepLake Dashboards

### 14. Service Overview

**File**: `service-overview.json`
**Folder**: DeepLake

#### Purpose
High-level health monitoring for vector database API service.

#### Key Metrics
- API health and availability
- Request rates by endpoint
- Response latency percentiles
- Error rates
- Active connections

---

### 15. Vector Operations

**File**: `vector-operations.json`
**Folder**: DeepLake

#### Purpose
Monitor vector embedding operations and storage.

#### Key Metrics
- Embedding generation rate
- Vector insertion throughput
- Dimension statistics
- Storage usage by vector type

---

### 16. Search Performance

**File**: `search-performance.json`
**Folder**: DeepLake

#### Purpose
Track vector similarity search performance and quality.

#### Key Metrics
- Query latency by index type
- Search accuracy metrics
- Results returned per query
- Index build times

---

### 17. Tenant Analytics

**File**: `tenant-analytics.json`
**Folder**: DeepLake

#### Purpose
Multi-tenant usage tracking and billing analytics.

#### Key Metrics
- Storage per tenant
- Operations per tenant
- Cost attribution
- Quota enforcement

---

### 18. Cache and Errors

**File**: `cache-and-errors.json`
**Folder**: DeepLake

#### Purpose
Caching effectiveness and error tracking.

#### Key Metrics
- Cache hit/miss rates
- Error types and frequencies
- Retry attempts
- Failed operations

#### Maintenance Notes
- DeepLake dashboards are comprehensive and production-ready
- Update tenant list as new tenants are onboarded
- Consider adding ML model performance metrics

---

## Aether Dashboard

### 19. Aether Backend Metrics

**File**: `aether-backend.json`
**UID**: `aether-backend`
**Folder**: Aether

#### Purpose
Monitor Aether backend API performance and resource usage.

#### Key Panels
- **Request Rate**: HTTP requests by method and status code
- **Response Time**: 95th and 50th percentile latency
- **Error Rate**: 4xx and 5xx error rates
- **CPU Usage**: Process CPU consumption
- **Memory Usage**: Resident set size (RSS)

#### Use Cases
- Monitor API health
- Identify slow endpoints
- Track error trends
- Capacity planning
- Performance optimization

#### Metrics Used
- `http_requests_total{job="aether-backend"}`
- `http_request_duration_seconds_bucket{job="aether-backend"}`
- `process_cpu_seconds_total{job="aether-backend"}`
- `process_resident_memory_bytes{job="aether-backend"}`

#### Maintenance Notes
- Ensure Aether backend exports Prometheus metrics
- Add Neo4j query metrics (TODO)
- Add document processing metrics (TODO)
- Track AudiModal integration calls (TODO)

---

## Dashboard Maintenance Schedule

### Daily
- Monitor all dashboards for data freshness
- Check for "No Data" panels
- Verify Prometheus/Loki connectivity

### Weekly
- Review alert thresholds
- Check for dashboard errors in Grafana logs
- Test critical dashboard queries

### Monthly
- Update dashboard queries for new metrics
- Add panels for new features
- Archive unused panels

### Quarterly
- Review all dashboards for relevance
- Update documentation
- Consolidate duplicate dashboards
- Performance optimization

### Annually
- Major dashboard redesign if needed
- Align with new Grafana features
- Review and update all metadata

---

## Contributing

When adding or modifying dashboards:

1. Update this inventory with dashboard details
2. Add metrics documentation
3. Include use cases
4. Document maintenance requirements
5. Test thoroughly before deploying

---

**Dashboard Requests or Issues?**

Contact the TAS platform team or open an issue in the aether-shared repository.
