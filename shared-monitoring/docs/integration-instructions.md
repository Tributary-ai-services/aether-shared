# Audimodal Observability Integration Instructions

## Quick Setup

### 1. Restart Shared Infrastructure
```bash
cd /path/to/aether-shared
docker-compose -f docker-compose.shared-infrastructure.yml down
docker-compose -f docker-compose.shared-infrastructure.yml up -d
```

### 2. Configure Audimodal to Connect to Shared Monitoring

Update your audimodal `docker-compose.yml` or environment variables:

```yaml
# Add to your audimodal docker-compose.yml
networks:
  default:
    external:
      name: tas-shared-network

services:
  audimodal-app:
    # ... existing config
    environment:
      # Enable metrics
      - METRICS_ENABLED=true
      - METRICS_PORT=8081
      
      # OpenTelemetry configuration
      - OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector-shared:4317
      - OTEL_SERVICE_NAME=audimodal
      - OTEL_SERVICE_VERSION=1.0.0
      - OTEL_RESOURCE_ATTRIBUTES=service.namespace=tas,environment=development
      
      # Tracing
      - OTEL_TRACES_EXPORTER=otlp
      - OTEL_METRICS_EXPORTER=otlp
      - OTEL_LOGS_EXPORTER=otlp
    
    ports:
      - "8080:8080"  # API port
      - "8081:8081"  # Metrics port (if needed externally)
    
    networks:
      - tas-shared-network
```

## Service URLs After Integration

- **Grafana**: http://localhost:3000 (admin:admin123)
- **Prometheus**: http://localhost:9090
- **AlertManager**: http://localhost:9093
- **OpenTelemetry Collector Health**: http://localhost:13133

## Grafana Dashboard Access

Once integrated, you'll find these Audimodal dashboards in Grafana:

1. **Audimodal Overview** - Main application metrics
2. **Audimodal Storage** - Storage usage and performance  
3. **Audimodal Processing** - Processing pipeline metrics

## Alerting Configuration

Alerts are automatically configured for:
- High storage usage (>80%)
- Processing queue backlogs (>1000 items)
- High error rates (>5% for API, >10% for processing)
- DLP violations (immediate for critical)
- Authentication failures (>10/sec)
- Slow performance (>2s API response, >5s embedding generation)

## Environment Variables for Audimodal

Set these in your audimodal application:

```bash
# Metrics
METRICS_ENABLED=true
METRICS_PORT=8081

# OpenTelemetry
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector-shared:4317
OTEL_SERVICE_NAME=audimodal
OTEL_SERVICE_VERSION=1.0.0
OTEL_RESOURCE_ATTRIBUTES=service.namespace=tas,environment=development

# Tracing
OTEL_TRACES_EXPORTER=otlp
OTEL_METRICS_EXPORTER=otlp
OTEL_LOGS_EXPORTER=otlp
```

## Verification Steps

### 1. Check Services are Running
```bash
docker ps | grep -E "(prometheus|grafana|alertmanager|otel)"
```

### 2. Verify Prometheus Targets
Go to http://localhost:9090/targets and ensure `audimodal` job shows as "UP"

### 3. Check Grafana Datasource
Go to http://localhost:3000/datasources and verify "Prometheus-Audimodal" is connected

### 4. Test AlertManager
Go to http://localhost:9093 to view the AlertManager UI

### 5. Verify OpenTelemetry Collector
```bash
curl http://localhost:13133
```

## Customizing Alerts

Edit these files and restart the stack:
- `shared-monitoring/prometheus/rules/audimodal-alerts.yml` - Alert rules
- `shared-monitoring/alertmanager/alertmanager.yml` - Alert routing and notifications

## Customizing Dashboards

1. Import/edit dashboards in Grafana UI at http://localhost:3000
2. Export the JSON and save to `shared-monitoring/grafana/dashboards/audimodal/`
3. Restart Grafana to persist changes

## Troubleshooting

### Metrics Not Showing
1. Check if audimodal is exposing metrics on port 8081
2. Verify network connectivity: `docker network ls | grep tas-shared`
3. Check Prometheus targets: http://localhost:9090/targets

### Dashboards Not Loading
1. Check Grafana logs: `docker logs tas-grafana-shared`
2. Verify dashboard files exist in `shared-monitoring/grafana/dashboards/audimodal/`
3. Check provisioning config in `shared-monitoring/grafana/provisioning/dashboards/audimodal.yml`

### Alerts Not Firing
1. Check Prometheus rules: http://localhost:9090/rules
2. Verify AlertManager config: http://localhost:9093
3. Test alert conditions manually in Prometheus

## Adding Custom Metrics

1. Add metrics to your Go code using the patterns in `pkg/monitoring/`
2. Re-run the sync script: `./scripts/sync-observability-to-shared.sh`
3. Restart the monitoring stack to pick up new alert rules

## Performance Considerations

The monitoring stack uses these resources:
- Prometheus: ~200MB RAM, stores 200h of data
- Grafana: ~100MB RAM  
- AlertManager: ~50MB RAM
- OTEL Collector: ~100MB RAM

Total additional overhead: ~450MB RAM for comprehensive monitoring.