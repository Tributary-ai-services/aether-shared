# Audimodal Observability Integration

This directory contains the observability configuration for the Audimodal application,
synced from the main audimodal repository.

## Components

### Metrics
- **Location**: `docs/audimodal-metrics.md`
- **Description**: Complete list of metrics exposed by Audimodal

### Prometheus
- **Configuration**: `prometheus/prometheus-audimodal.yml`
- **Alert Rules**: `prometheus/rules/audimodal-alerts.yml`
- **Scrape Targets**: audimodal-app:8080, audimodal-app:8081

### Grafana
- **Dashboards**: `grafana/dashboards/audimodal/`
  - audimodal-overview.json - Main application dashboard
  - audimodal-storage.json - Storage metrics dashboard
  - audimodal-processing.json - Processing pipeline dashboard
- **Provisioning**: Auto-provisioned via configuration files

### OpenTelemetry
- **Configuration**: `otel/otel-collector-audimodal.yml`
- **Endpoints**: 
  - OTLP gRPC: 4317
  - OTLP HTTP: 4318
  - Metrics: 8889

## Integration Steps

1. Ensure the shared monitoring stack is running:
   ```bash
   cd ../aether-shared
   ./start-shared-services.sh
   ```

2. Update audimodal docker-compose to connect to shared network:
   ```yaml
   networks:
     default:
       external:
         name: tas-shared-network
   ```

3. Configure audimodal to export metrics:
   - Set environment variable: `METRICS_ENABLED=true`
   - Set metrics endpoint: `METRICS_PORT=8081`
   - Set OTLP endpoint: `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317`

4. Access dashboards:
   - Grafana: http://localhost:3000
   - Prometheus: http://localhost:9090
   - AlertManager: http://localhost:9093

## Sync Process

To update the observability configuration:
```bash
cd audimodal
./scripts/sync-observability-to-shared.sh
```

Last synced: Wed Sep 10 10:51:20 MDT 2025
