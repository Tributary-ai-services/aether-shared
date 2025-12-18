# LLM Router Grafana Dashboards

This directory contains Grafana dashboards for monitoring the LLM Router WAF service.

## Dashboards

### 1. LLM Router - Overview (`llm-router-overview.json`)
Comprehensive overview dashboard showing:
- **Request Rate**: Total requests per minute across providers
- **Response Status Codes**: HTTP status code distribution  
- **Response Time Percentiles**: 95th and 50th percentile latency
- **Token Usage Rate**: Input/output token consumption
- **Cost Rate**: Estimated costs per provider/model
- **Error Rate**: Error percentage by provider
- **Active Connections**: Current connection count
- **Rate Limit Usage**: Rate limiting utilization
- **Provider Health Status**: Health status table for all providers

### 2. LLM Router - Security (`llm-router-security.json`)
Security-focused dashboard showing:
- **Authentication Rate**: Success/failure authentication attempts
- **Rate Limiting**: Rate limit hits and blocks
- **Security Events**: Security events by type and severity
- **Input Validation & Sanitization**: Validation failures and sanitization
- **Top Client IPs**: Highest traffic IP addresses
- **Recent Audit Events**: Latest audit log entries
- **Active API Keys**: Current number of active keys
- **Threat Level**: Current security threat assessment
- **Security Score**: Overall security score gauge

## Prerequisites

### Metrics Endpoint
The LLM Router must expose a Prometheus metrics endpoint at `/metrics` with the following metric names:

#### Core Metrics
- `llm_router_requests_total` - Total requests (labels: provider, model, method, status_code)
- `llm_router_request_duration_seconds_bucket` - Request duration histogram
- `llm_router_tokens_total` - Token usage (labels: provider, model, type=input/output)
- `llm_router_cost_total` - Cost tracking (labels: provider, model)
- `llm_router_errors_total` - Error count (labels: provider, error_type)

#### Connection & Rate Limiting
- `llm_router_active_connections` - Current active connections
- `llm_router_rate_limit_usage` - Rate limit utilization (labels: provider)
- `llm_router_rate_limit_hits_total` - Rate limit hits (labels: tier)
- `llm_router_blocked_requests_total` - Blocked requests (labels: reason)

#### Security Metrics
- `llm_router_auth_attempts_total` - Authentication attempts (labels: result)
- `llm_router_security_events_total` - Security events (labels: event_type, severity)
- `llm_router_validation_failures_total` - Validation failures (labels: type)
- `llm_router_input_sanitized_total` - Sanitized inputs
- `llm_router_audit_events_total` - Audit events (labels: event_type, severity)
- `llm_router_active_api_keys` - Number of active API keys
- `llm_router_threat_level` - Current threat level (0-3)
- `llm_router_security_score` - Security score (0-100)

#### Provider Health
- `llm_router_provider_health` - Provider health status (labels: provider)

### Prometheus Configuration
Uncomment the LLM Router scrape job in `/home/jscharber/eng/TAS/aether-shared/shared-monitoring/prometheus.yml`:

```yaml
- job_name: 'llm-router'
  static_configs:
    - targets: ['llm-router-aether-app:8080']
  metrics_path: '/metrics'
  scrape_interval: 15s
  scrape_timeout: 10s
  relabel_configs:
    - source_labels: [__address__]
      target_label: instance
      replacement: 'llm-router-aether'
    - source_labels: [__address__]
      target_label: service
      replacement: 'llm-router'
```

## Installation

The dashboards are automatically provisioned via Grafana's dashboard provisioning system. They will appear in the "LLM Router" folder in Grafana once:

1. The metrics endpoint is implemented in the LLM Router
2. The Prometheus scrape job is uncommented
3. Grafana is restarted to pick up the new dashboards

## Usage

1. Navigate to Grafana at `http://localhost:3000`
2. Login with credentials from the shared infrastructure
3. Browse to **Dashboards > LLM Router** folder
4. Select either **Overview** or **Security** dashboard
5. Use the time range picker and variable filters at the top

## Variables

Both dashboards support filtering by:
- **Provider**: Filter by OpenAI, Anthropic, etc.
- **Model**: Filter by specific models within providers
- **Severity**: (Security dashboard) Filter by security event severity

## Alerts

TODO: Add alerting rules for:
- High error rates
- Excessive costs
- Security events
- Provider health issues
- Rate limit violations

## Troubleshooting

### No Data Displayed
1. Verify LLM Router `/metrics` endpoint returns Prometheus format metrics
2. Check Prometheus targets page: `http://localhost:9090/targets`
3. Confirm LLM Router target is "UP" and scraping successfully
4. Verify metric names match those expected by the dashboard queries

### Dashboard Import Issues
If dashboards don't appear automatically:
1. Restart Grafana: `docker-compose -f docker-compose.shared-infrastructure.yml restart grafana-shared`
2. Check Grafana logs for provisioning errors
3. Verify file permissions on dashboard JSON files
4. Check provisioning configuration in `provisioning/dashboards/llm-router.yml`