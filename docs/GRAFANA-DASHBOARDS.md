# Grafana Dashboard Management Guide

**Last Updated**: 2025-12-10
**Status**: Production

## Overview

This guide documents the centralized Grafana dashboard management system for the TAS (Tributary AI System) platform. All dashboards are stored in the `aether-shared` repository and automatically provisioned to the Kubernetes Grafana instance.

## Dashboard Organization

### Directory Structure

```
aether-shared/
└── shared-monitoring/
    └── grafana/
        ├── dashboards/           # Dashboard JSON files
        │   ├── llm-router/       # LLM Router dashboards
        │   ├── audimodal/        # AudiModal service dashboards
        │   ├── loki/             # Log aggregation dashboards
        │   ├── deeplake/         # Vector database dashboards
        │   ├── aether/           # Aether application dashboards
        │   └── infrastructure/   # Shared infrastructure dashboards
        └── provisioning/
            └── dashboards/       # Provisioning configuration
                ├── llm-router.yml
                ├── audimodal.yml
                ├── loki.yml
                ├── deeplake.yml
                ├── aether.yml
                └── infrastructure.yml
```

### Dashboard Categories

| Category | Count | Purpose |
|----------|-------|---------|
| **LLM Router** | 4 | LLM routing, security, cost optimization |
| **AudiModal** | 2 | Audio/media processing service metrics |
| **Loki** | 1 | Centralized log aggregation and viewing |
| **DeepLake** | 5 | Vector database operations and analytics |
| **Aether** | 1 | Aether backend API performance |
| **Infrastructure** | 6 | Shared services (Redis, Postgres, Kafka, MinIO, Keycloak, Overview) |
| **Total** | **19** | Complete TAS monitoring coverage |

## Adding New Dashboards

### Step 1: Create Dashboard JSON

1. **Design in Grafana UI** (recommended):
   - Access Grafana at https://grafana.tas.scharber.com
   - Create and configure your dashboard
   - Click Settings → JSON Model
   - Copy the JSON

2. **Or create manually**:
   - Follow the Grafana dashboard JSON schema
   - Ensure required fields: `title`, `uid`, `panels`, `version`

### Step 2: Save to Repository

Save the JSON file to the appropriate category directory:

```bash
# Example: Adding a new infrastructure dashboard
vim shared-monitoring/grafana/dashboards/infrastructure/prometheus.json

# Example: Adding a new application dashboard
vim shared-monitoring/grafana/dashboards/aether/aether-frontend.json
```

**Naming Convention**: Use lowercase with hyphens (e.g., `redis-cache.json`, `llm-router-overview.json`)

### Step 3: Validate Dashboard

Run the validation script to check for errors:

```bash
./scripts/validate-dashboards.sh shared-monitoring/grafana/dashboards/infrastructure/prometheus.json
```

The validator checks for:
- Valid JSON syntax
- Required fields (`title`, `panels`, `uid`)
- Panel configuration
- Data source references

### Step 4: Deploy to Kubernetes

**Option A: Deploy specific category** (faster):

```bash
# Recreate ConfigMap for the updated category
kubectl create configmap grafana-dashboards-infrastructure \
  --from-file=shared-monitoring/grafana/dashboards/infrastructure/ \
  --namespace=tas-shared \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart Grafana to reload
kubectl rollout restart deployment grafana-shared -n tas-shared
kubectl rollout status deployment grafana-shared -n tas-shared
```

**Option B: Deploy all dashboards** (comprehensive):

```bash
# Run the sync script
./scripts/sync-dashboards-to-k8s.sh

# Or manually for all categories
for category in llm-router audimodal loki deeplake aether infrastructure; do
  kubectl create configmap grafana-dashboards-$category \
    --from-file=shared-monitoring/grafana/dashboards/$category/ \
    --namespace=tas-shared \
    --dry-run=client -o yaml | kubectl apply -f -
done

kubectl rollout restart deployment grafana-shared -n tas-shared
```

### Step 5: Verify in Grafana

1. Wait for Grafana pod to be ready (~30 seconds)
2. Access https://grafana.tas.scharber.com
3. Navigate to Dashboards → Browse
4. Look for your dashboard in the appropriate folder

## Updating Existing Dashboards

### Quick Update Process

1. **Export latest from Grafana**:
   - Open dashboard in Grafana
   - Click Share → Export → Save to file
   - Or copy JSON from Settings → JSON Model

2. **Update repository file**:
   ```bash
   # Replace the existing dashboard file
   cp ~/Downloads/updated-dashboard.json \
     shared-monitoring/grafana/dashboards/infrastructure/redis.json
   ```

3. **Validate changes**:
   ```bash
   ./scripts/validate-dashboards.sh \
     shared-monitoring/grafana/dashboards/infrastructure/redis.json
   ```

4. **Deploy update**:
   ```bash
   kubectl create configmap grafana-dashboards-infrastructure \
     --from-file=shared-monitoring/grafana/dashboards/infrastructure/ \
     --namespace=tas-shared \
     --dry-run=client -o yaml | kubectl apply -f -

   kubectl rollout restart deployment grafana-shared -n tas-shared
   ```

## Creating a New Dashboard Category

If you need to add a completely new category (e.g., "security"):

### 1. Create Directory

```bash
mkdir -p shared-monitoring/grafana/dashboards/security
```

### 2. Create Provisioning Config

```bash
cat > shared-monitoring/grafana/provisioning/dashboards/security.yml <<EOF
apiVersion: 1

providers:
  - name: 'Security Dashboards'
    orgId: 1
    folder: 'Security'
    folderUid: security
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards/security
EOF
```

### 3. Update Kubernetes Deployment

Edit `k8s-shared-infrastructure/monitoring.yaml`:

**Add volume mount** (around line 462):
```yaml
        - name: dashboards-security
          mountPath: /var/lib/grafana/dashboards/security
```

**Add volume** (around line 511):
```yaml
      - name: dashboards-security
        configMap:
          name: grafana-dashboards-security
```

### 4. Deploy

```bash
# Create ConfigMap
kubectl create configmap grafana-dashboards-security \
  --from-file=shared-monitoring/grafana/dashboards/security/ \
  --namespace=tas-shared

# Update provisioning
kubectl create configmap grafana-provisioning-dashboards \
  --from-file=shared-monitoring/grafana/provisioning/dashboards/ \
  --namespace=tas-shared \
  --dry-run=client -o yaml | kubectl apply -f -

# Apply deployment changes
kubectl apply -f k8s-shared-infrastructure/monitoring.yaml
```

## Dashboard Best Practices

### JSON Structure

Every dashboard should include:

```json
{
  "title": "Descriptive Dashboard Title",
  "uid": "unique-dashboard-id",
  "tags": ["category", "service"],
  "timezone": "browser",
  "editable": true,
  "panels": [ /* panel definitions */ ],
  "refresh": "5s",
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "version": 1
}
```

### Panel Design

- **Use consistent time ranges**: Typically `now-1h` to `now` for real-time monitoring
- **Add meaningful panel titles**: Describe what the metric shows
- **Set appropriate refresh rates**: 5s for critical metrics, 30s for general monitoring
- **Include units**: Set proper units for metrics (bytes, ops, percent, etc.)
- **Add tooltips**: Use legend formats to make metrics self-explanatory

### Data Source References

All dashboards should reference these data sources:

- **Prometheus**: Primary metrics data source
- **Loki**: Log aggregation data source
- **Grafana**: For built-in annotations

Example query:
```json
{
  "expr": "rate(http_requests_total{job=\"service-name\"}[5m])",
  "legendFormat": "{{method}} {{status}}",
  "refId": "A"
}
```

### Tags and Metadata

Use consistent tags for better organization:

```json
{
  "tags": [
    "infrastructure",  // Category
    "redis",           // Service name
    "cache"            // Function/role
  ]
}
```

## Troubleshooting

### Dashboard Not Appearing

1. **Check ConfigMap exists**:
   ```bash
   kubectl get configmap grafana-dashboards-infrastructure -n tas-shared
   ```

2. **Verify dashboard file in ConfigMap**:
   ```bash
   kubectl get configmap grafana-dashboards-infrastructure -n tas-shared -o yaml | grep "redis.json"
   ```

3. **Check Grafana logs**:
   ```bash
   kubectl logs -n tas-shared -l app=grafana-shared --tail=100 | grep -i "dashboard\|error"
   ```

4. **Verify provisioning config**:
   ```bash
   kubectl get configmap grafana-provisioning-dashboards -n tas-shared -o yaml
   ```

### Dashboard Shows "No Data"

1. **Check data source connection**:
   - Go to Configuration → Data Sources in Grafana
   - Test Prometheus and Loki connections

2. **Verify metric exists**:
   - Go to Explore tab
   - Select Prometheus data source
   - Run your query manually

3. **Check time range**:
   - Ensure time range selector shows recent data
   - Try "Last 5 minutes" to confirm data is flowing

### Validation Errors

Run the validation script to identify issues:

```bash
./scripts/validate-dashboards.sh shared-monitoring/grafana/dashboards/
```

Common errors:
- **Invalid JSON**: Fix syntax errors (trailing commas, missing quotes)
- **Missing required fields**: Add `title`, `uid`, `panels`
- **Invalid UID format**: Use alphanumeric with hyphens/underscores only
- **Empty panels array**: Add at least one panel or row

### ConfigMap Too Large

Kubernetes ConfigMaps have a 1MB limit. If exceeded:

1. **Split dashboards into multiple files**:
   ```bash
   # Instead of one large dashboard, create multiple smaller ones
   mv large-dashboard.json overview-dashboard.json
   # Extract some panels to a new file
   vim details-dashboard.json
   ```

2. **Reduce panel count**: Consolidate similar metrics into single panels

3. **Remove unnecessary data**: Strip comments, minimize whitespace

## Automation Scripts

### Validation Script

**Location**: `scripts/validate-dashboards.sh`

**Usage**:
```bash
# Validate single file
./scripts/validate-dashboards.sh path/to/dashboard.json

# Validate entire category
./scripts/validate-dashboards.sh shared-monitoring/grafana/dashboards/infrastructure/

# Validate all dashboards
./scripts/validate-dashboards.sh shared-monitoring/grafana/dashboards/
```

**Output**:
```
[INFO] Validating: redis.json
[✓] redis.json is valid

Validation Summary
==================
Total files:    19
Valid:          19
Invalid:        0
Warnings:       2
```

### Sync Script

**Location**: `scripts/sync-dashboards-to-k8s.sh`

**Usage**:
```bash
# Sync all dashboards
./scripts/sync-dashboards-to-k8s.sh

# Dry-run mode (preview changes)
./scripts/sync-dashboards-to-k8s.sh --dry-run

# Sync specific category only
./scripts/sync-dashboards-to-k8s.sh --category infrastructure
```

**Features**:
- Generates ConfigMaps from dashboard files
- Creates provisioning ConfigMap
- Applies all ConfigMaps to Kubernetes
- Restarts Grafana deployment
- Verifies deployment success

## Grafana Folders

Dashboards are automatically organized into folders based on their provisioning configuration:

| Folder Name | UID | Dashboards |
|-------------|-----|------------|
| LLM Router | `llm-router` | LLM routing, security, cost dashboards |
| AudiModal | `audimodal` | Audio processing metrics |
| TAS Infrastructure Logs | `loki` | Log viewing and analysis |
| DeepLake | `deeplake` | Vector DB operations |
| Aether | `aether` | Aether application metrics |
| Infrastructure | `infrastructure` | Shared services monitoring |

## Data Sources

### Prometheus

**URL**: http://prometheus-shared:9090
**Type**: Prometheus
**Access**: Proxy

**Configured Metrics**:
- All infrastructure services (Redis, Postgres, Kafka, MinIO, Keycloak)
- Application services (Aether, AudiModal, DeepLake, LLM Router)
- Kubernetes cluster metrics
- Custom application metrics

### Loki

**URL**: http://loki-shared:3100
**Type**: Loki
**Access**: Proxy

**Log Sources**:
- All Kubernetes pods in `tas-shared` namespace
- Application logs from all TAS services
- Infrastructure service logs
- System logs

## Dashboard Inventory

See [DASHBOARD-INVENTORY.md](./DASHBOARD-INVENTORY.md) for a complete list of all dashboards with descriptions, key metrics, and maintenance notes.

## Support and Resources

- **Grafana Documentation**: https://grafana.com/docs/grafana/latest/
- **Prometheus Query Guide**: https://prometheus.io/docs/prometheus/latest/querying/basics/
- **LogQL (Loki) Guide**: https://grafana.com/docs/loki/latest/logql/
- **Dashboard JSON Schema**: https://grafana.com/docs/grafana/latest/dashboards/json-model/

## Maintenance

### Regular Tasks

- **Monthly**: Review and update dashboard queries for new metrics
- **Quarterly**: Validate all dashboards still work with current service versions
- **Annually**: Archive unused dashboards, consolidate similar ones

### Version Control

All dashboard changes should be:
1. Committed to Git with descriptive messages
2. Tagged if part of a release
3. Documented in commit messages with dashboard names and changes

```bash
git add shared-monitoring/grafana/dashboards/infrastructure/redis.json
git commit -m "Update Redis dashboard: Add connection pool metrics"
```

---

**Questions or Issues?**

Contact the TAS platform team or open an issue in the aether-shared repository.
