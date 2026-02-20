# Argo Events Reference for TAS Workflow Builder

This document provides a comprehensive reference for Argo Events patterns used in TAS. All workflow trigger configurations in the Aether UI must map to these Argo Events CRD patterns.

**Sources**: [Argo Events Documentation](https://argoproj.github.io/argo-events/)

---

## Architecture Overview

Argo Events uses three core CRDs that form a publish-subscribe event pipeline:

```
┌──────────────┐     ┌──────────┐     ┌──────────┐     ┌──────────────────┐
│ External     │────▶│ Event    │────▶│ Event    │────▶│ Sensor           │
│ Event        │     │ Source   │     │ Bus      │     │ (Dependencies +  │
│ (HTTP, Kafka │     │ (CRD)   │     │ (NATS/   │     │  Triggers)       │
│  S3, Cron)   │     │         │     │  Kafka)  │     │                  │
└──────────────┘     └──────────┘     └──────────┘     └────────┬─────────┘
                                                                │
                                                    ┌───────────┼───────────┐
                                                    ▼           ▼           ▼
                                              ┌──────────┐ ┌─────────┐ ┌────────┐
                                              │ Argo     │ │ HTTP    │ │ K8s    │
                                              │ Workflow │ │ Request │ │ Object │
                                              └──────────┘ └─────────┘ └────────┘
```

### Component Roles
- **EventSource** — Consumes external events, converts to CloudEvents, publishes to EventBus
- **EventBus** — Transport layer (NATS, JetStream, or Kafka) connecting EventSources to Sensors
- **Sensor** — Subscribes to EventBus, defines event dependencies and triggers to execute

### CloudEvents Format
All events follow the CloudEvents specification:
```json
{
  "context": {
    "type": "event_source_type",
    "specversion": "1.0",
    "source": "event_source_name",
    "id": "unique_event_id",
    "time": "2026-01-15T10:30:00Z",
    "datacontenttype": "application/json",
    "subject": "configuration_name"
  },
  "data": {
    "header": { "Content-Type": "application/json" },
    "body": { "key": "value" }
  }
}
```

---

## TAS Deployment Details

**Versions**: Argo Workflows v3.6.4, Argo Events v1.9.3
**Deploy script**: `aether-shared/k8s-shared-infrastructure/argo/deploy-argo.sh`
**Namespaces**: `argo` (workflows), `argo-events` (event sources, sensors, eventbus)
**Service Account**: `argo-workflow-runner` (argo ns), `argo-events-sa` (argo-events ns)

### EventBus (NATS)
```yaml
apiVersion: argoproj.io/v1alpha1
kind: EventBus
metadata:
  name: default
  namespace: argo-events
spec:
  nats:
    native:
      replicas: 3
      auth: token
```

---

## EventSource Types & TAS Configurations

### 1. Webhook EventSource

Exposes HTTP endpoints that external systems POST to.

**TAS manifest**: `argo/argo-events/event-sources/webhook-source.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: EventSource
metadata:
  name: webhook
  namespace: argo-events
  labels:
    app: argo-events
    project: tas
    type: webhook
spec:
  service:
    ports:
    - port: 12000
      targetPort: 12000
  webhook:
    # Generic workflow trigger webhook
    workflow-trigger:
      port: "12000"
      endpoint: /workflow-trigger
      method: POST
    # GitHub webhook for repository events
    github-events:
      port: "12000"
      endpoint: /github
      method: POST
    # TAS API callback webhook
    tas-api-callback:
      port: "12000"
      endpoint: /tas-callback
      method: POST
```

**Key fields**:
- `port` — Listener port (string, typically "12000")
- `endpoint` — URL path (e.g., `/workflow-trigger`)
- `method` — HTTP method (POST, PUT, GET)
- `service.ports` — K8s Service port mapping

**Generated URL pattern**: `http://<eventsource-name>-eventsource-svc.argo-events.svc.cluster.local:<port><endpoint>`
- Example: `http://webhook-eventsource-svc.argo-events:12000/workflow-trigger`

**Frontend mapping**: When user configures a webhook trigger, store:
- `config.argo_event_source` → EventSource name (e.g., "webhook")
- `config.argo_event_name` → Event name within source (e.g., "workflow-trigger")
- `config.webhook_url` → Generated URL for display
- `config.http_method` → POST/PUT

### 2. Calendar EventSource

Generates events on cron schedules or intervals.

**TAS manifest**: `argo/argo-events/event-sources/calendar-source.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: EventSource
metadata:
  name: calendar
  namespace: argo-events
  labels:
    app: argo-events
    project: tas
    type: calendar
spec:
  calendar:
    hourly:
      schedule: "0 * * * *"
      timezone: UTC
    daily:
      schedule: "0 0 * * *"
      timezone: UTC
    weekly:
      schedule: "0 0 * * 0"
      timezone: UTC
```

**Key fields**:
- `schedule` — Standard 5-field cron expression (minute hour day-of-month month day-of-week)
- `timezone` — IANA timezone (e.g., "UTC", "America/New_York")
- `interval` — Alternative to schedule, e.g., "30m", "1h"
- `exclusionDates` — Dates to skip (ISO 8601)
- `metadata` — Custom metadata map attached to events
- `persistence` — Catch-up configuration for missed events

**Frontend mapping**: SchedulePickerPanel converts friendly UI to:
- `config.cron` → Cron expression string
- `config.schedule` → Friendly settings object (for UI round-trip)
- `config.timezone` → IANA timezone string

**Backend translation**: Create/update calendar EventSource entry with the cron expression and timezone.

### 3. Kafka EventSource

Listens to Kafka topic messages.

**TAS manifest**: `argo/argo-events/event-sources/kafka-source.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: EventSource
metadata:
  name: kafka
  namespace: argo-events
  labels:
    app: argo-events
    project: tas
    type: kafka
spec:
  kafka:
    document-processed:
      url: kafka-shared.tas-shared.svc.cluster.local:9092
      topic: processing.complete
      partition: "0"
      jsonBody: true
      consumerGroup:
        groupName: argo-events-doc-processed
    workflow-execute:
      url: kafka-shared.tas-shared.svc.cluster.local:9092
      topic: workflow.execute
      partition: "0"
      jsonBody: true
      consumerGroup:
        groupName: argo-events-workflow-exec
    user-events:
      url: kafka-shared.tas-shared.svc.cluster.local:9092
      topic: user-events
      partition: "0"
      jsonBody: true
      consumerGroup:
        groupName: argo-events-user
```

**Key fields**:
- `url` — Kafka broker address
- `topic` — Kafka topic name
- `partition` — Partition number (string)
- `jsonBody` — Parse message body as JSON (boolean)
- `consumerGroup.groupName` — Consumer group identifier

**Frontend mapping**: Document Event trigger maps to:
- `config.event_type` → Maps to Kafka topic (e.g., "processing.completed" → topic "processing.complete")
- `config.notebook_ids` → Used for filtering in the sensor dependency filter

### 4. MinIO/S3 EventSource

Watches S3 bucket notifications for object creation/deletion.

**TAS manifest**: `argo/argo-events/event-sources/minio-source.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: EventSource
metadata:
  name: minio
  namespace: argo-events
  labels:
    app: argo-events
    project: tas
    type: minio
spec:
  minio:
    file-upload:
      bucket:
        name: uploads
      endpoint: minio-shared.tas-shared.svc.cluster.local:9000
      events:
      - s3:ObjectCreated:*
      insecure: true
      accessKey:
        name: minio-credentials
        key: accesskey
      secretKey:
        name: minio-credentials
        key: secretkey
    artifact-ready:
      bucket:
        name: argo-artifacts
      endpoint: minio-shared.tas-shared.svc.cluster.local:9000
      events:
      - s3:ObjectCreated:*
      insecure: true
      accessKey:
        name: minio-credentials
        key: accesskey
      secretKey:
        name: minio-credentials
        key: secretkey
```

**Key fields**:
- `bucket.name` — MinIO bucket to watch
- `endpoint` — MinIO server address
- `events` — S3 event types: `s3:ObjectCreated:*`, `s3:ObjectRemoved:*`, `s3:ObjectAccessed:*`
- `filter.prefix` — Object key prefix filter
- `filter.suffix` — Object key suffix filter (e.g., ".pdf")
- `accessKey`/`secretKey` — K8s Secret references
- `insecure` — Use HTTP instead of HTTPS

**Frontend mapping**: File Upload trigger maps to:
- `config.accepted_extensions` → Used to generate `filter.suffix` rules
- `config.source_filter` → Determines which bucket to watch

**Data payload path for MinIO events**: `notification.0.s3.bucket.name`, `notification.0.s3.object.key`

---

## Sensor Patterns

### Sensor Structure
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: sensor-name
  namespace: argo-events
spec:
  dependencies:                    # Events to listen for
  - name: dependency-name
    eventSourceName: source-name   # References an EventSource
    eventName: event-name          # Specific event within the source
    filters:                       # Optional event filtering
      data:
      - path: body.field
        type: string
        value: ["expected-value"]
  triggers:                        # Actions to take
  - template:
      name: trigger-name
      conditions: "dependency-name"  # Boolean expression
      # ... trigger type config
    retryStrategy:
      steps: 3
      duration: 5s
```

### TAS Sensor: Workflow Trigger (Webhook + Kafka)

**TAS manifest**: `argo/argo-events/sensors/workflow-trigger-sensor.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: workflow-trigger
  namespace: argo-events
spec:
  dependencies:
  - name: webhook-trigger
    eventSourceName: webhook
    eventName: workflow-trigger
  - name: kafka-workflow-exec
    eventSourceName: kafka
    eventName: workflow-execute
  triggers:
  # Trigger Argo Workflow from webhook
  - template:
      name: trigger-argo-workflow
      argoWorkflow:
        operation: submit
        source:
          resource:
            apiVersion: argoproj.io/v1alpha1
            kind: Workflow
            metadata:
              generateName: tas-workflow-
              namespace: argo
            spec:
              entrypoint: main
              serviceAccountName: argo-workflow-runner
              arguments:
                parameters:
                - name: workflow-id
                - name: input-data
              artifactRepositoryRef:
                configMap: artifact-repositories
                key: default-v1
              templates:
              - name: main
                steps:
                - - name: execute
                    template: execute-step
                    arguments:
                      parameters:
                      - name: workflow-id
                        value: "{{workflow.parameters.workflow-id}}"
                      - name: input-data
                        value: "{{workflow.parameters.input-data}}"
              - name: execute-step
                inputs:
                  parameters:
                  - name: workflow-id
                  - name: input-data
                container:
                  image: curlimages/curl:8.8.0
                  command: [sh, -c]
                  args:
                  - |
                    curl -sf -X POST \
                      "http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/workflows/{{inputs.parameters.workflow-id}}/execute" \
                      -H 'Content-Type: application/json' \
                      -d '{{inputs.parameters.input-data}}'
        parameters:
        - src:
            dependencyName: webhook-trigger
            dataKey: body.workflow_id
          dest: spec.arguments.parameters.0.value
        - src:
            dependencyName: webhook-trigger
            dataKey: body
          dest: spec.arguments.parameters.1.value
    retryStrategy:
      steps: 3
      duration: 5s
```

### TAS Sensor: Document Processing (Kafka + MinIO)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: document-processing
  namespace: argo-events
spec:
  dependencies:
  - name: doc-processed
    eventSourceName: kafka
    eventName: document-processed
  - name: file-uploaded
    eventSourceName: minio
    eventName: file-upload
  triggers:
  # HTTP callback to aether-be on document completion
  - template:
      name: notify-doc-complete
      http:
        url: http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/webhooks/document-processed
        method: POST
        headers:
          Content-Type: application/json
        payload:
        - src:
            dependencyName: doc-processed
            dataKey: body
          dest: document
    retryStrategy:
      steps: 3
      duration: 5s
  # Submit Argo Workflow on file upload
  - template:
      name: trigger-file-processing
      argoWorkflow:
        operation: submit
        source:
          resource:
            apiVersion: argoproj.io/v1alpha1
            kind: Workflow
            metadata:
              generateName: file-process-
              namespace: argo
            spec:
              entrypoint: process
              serviceAccountName: argo-workflow-runner
              arguments:
                parameters:
                - name: bucket
                - name: key
              templates:
              - name: process
                inputs:
                  parameters:
                  - name: bucket
                  - name: key
                container:
                  image: curlimages/curl:8.8.0
                  command: [sh, -c]
                  args:
                  - |
                    curl -sf -X POST \
                      "http://audimodal.aether-be.svc.cluster.local:8084/api/v1/process" \
                      -H 'Content-Type: application/json' \
                      -d '{"bucket":"{{inputs.parameters.bucket}}","key":"{{inputs.parameters.key}}"}'
        parameters:
        - src:
            dependencyName: file-uploaded
            dataKey: notification.0.s3.bucket.name
          dest: spec.arguments.parameters.0.value
        - src:
            dependencyName: file-uploaded
            dataKey: notification.0.s3.object.key
          dest: spec.arguments.parameters.1.value
    retryStrategy:
      steps: 3
      duration: 5s
```

### TAS Sensor: Scheduled Tasks (Calendar)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: scheduled-tasks
  namespace: argo-events
spec:
  dependencies:
  - name: daily-schedule
    eventSourceName: calendar
    eventName: daily
  triggers:
  - template:
      name: daily-analytics
      argoWorkflow:
        operation: submit
        source:
          resource:
            apiVersion: argoproj.io/v1alpha1
            kind: Workflow
            metadata:
              generateName: daily-analytics-
              namespace: argo
            spec:
              entrypoint: aggregate
              serviceAccountName: argo-workflow-runner
              templates:
              - name: aggregate
                container:
                  image: curlimages/curl:8.8.0
                  command: [sh, -c]
                  args:
                  - |
                    curl -sf -X POST \
                      "http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/workflows/analytics/aggregate" \
                      -H 'Content-Type: application/json' \
                      -d '{"period":"daily","date":"'$(date -u +%Y-%m-%d)'"}'
    retryStrategy:
      steps: 3
      duration: 30s
```

---

## Parameterization

Parameters allow dynamic trigger configuration from event payloads.

### Parameter Sources

```yaml
parameters:
- src:
    dependencyName: my-dependency      # Which event dependency
    dataKey: body.field.nested         # Dot-notation path in event data
    # OR
    contextKey: subject                # CloudEvent context field
    # OR
    dataTemplate: "{{ .Input.body.name | upper }}"  # Sprig template
    useRawData: true                   # Preserve JSON types (not stringify)
    value: "fallback-value"            # Default if key not found
  dest: spec.arguments.parameters.0.value  # Destination in trigger resource
  operation: ""                        # "append" or "prepend" (optional)
```

### Data Path Examples (by EventSource type)

| EventSource | Data Path | Value |
|-------------|-----------|-------|
| Webhook | `body.workflow_id` | Request body field |
| Webhook | `header.Authorization` | Request header |
| Kafka | `body.data.document_id` | Kafka message body field |
| MinIO | `notification.0.s3.bucket.name` | Bucket name |
| MinIO | `notification.0.s3.object.key` | Object key/path |
| Calendar | `eventTime` | UTC timestamp |
| Calendar | `userPayload.custom_field` | Custom metadata |

### Template Functions (Sprig)

```yaml
dataTemplate: "{{ .Input.body.name | title }}"      # Capitalize
dataTemplate: "{{ .Input.body.text | lower }}"       # Lowercase
dataTemplate: "{{ .Input.body.path | nospace }}"      # Remove spaces
dataTemplate: "{{ .Input.subject }}-{{ .Input.body.id }}"  # Concatenate
```

---

## Trigger Types

### 1. Argo Workflow Trigger

```yaml
triggers:
- template:
    name: my-workflow-trigger
    argoWorkflow:
      operation: submit    # submit | resubmit | resume | retry | suspend | terminate | stop
      source:
        resource:          # Inline workflow definition
          apiVersion: argoproj.io/v1alpha1
          kind: Workflow
          metadata:
            generateName: prefix-
            namespace: argo
          spec:
            entrypoint: main
            serviceAccountName: argo-workflow-runner
            arguments:
              parameters:
              - name: param1
            templates:
            - name: main
              container:
                image: alpine:latest
                command: [echo]
                args: ["{{workflow.parameters.param1}}"]
      parameters:          # Map event data → workflow params
      - src:
          dependencyName: dep-name
          dataKey: body.value
        dest: spec.arguments.parameters.0.value
```

**Operations**: `submit` (new), `resubmit` (re-run), `resume`, `retry`, `suspend`, `terminate`, `stop`

**Source alternatives**:
```yaml
source:
  resource: { ... }        # Inline (shown above)
  # OR
  git:
    url: "git@github.com:org/repo.git"
    cloneDirectory: /tmp/repo
    filePath: manifests/workflow.yaml
    sshKeySecret: { name: git-ssh, key: key }
  # OR
  s3:
    bucket: { name: workflows }
    endpoint: minio:9000
    key: workflow.yaml
    accessKey: { name: creds, key: accesskey }
    secretKey: { name: creds, key: secretkey }
  # OR
  configmap:
    name: trigger-store
    namespace: argo-events
    key: workflow.yaml
  # OR
  url:
    path: "https://raw.githubusercontent.com/org/repo/main/workflow.yaml"
```

### 2. HTTP Trigger

```yaml
triggers:
- template:
    name: http-trigger
    http:
      url: http://service.namespace:8080/api/endpoint
      method: POST         # GET | POST | PUT | DELETE | HEAD
      headers:
        Content-Type: application/json
        Authorization: "Bearer {{token}}"
      payload:             # Build request body from event data
      - src:
          dependencyName: dep-name
          dataKey: body
        dest: document
      - src:
          dependencyName: dep-name
          contextKey: id
        dest: event_id
    retryStrategy:
      steps: 3
      duration: 5s
    policy:
      status:
        allow: [200, 201, 202]
```

### 3. Kubernetes Object Trigger

```yaml
triggers:
- template:
    name: k8s-trigger
    k8s:
      operation: create    # create | update | patch | delete
      source:
        resource:
          apiVersion: v1
          kind: Pod
          metadata:
            generateName: event-pod-
            namespace: default
          spec:
            containers:
            - name: main
              image: alpine:latest
              command: [echo, "triggered"]
            restartPolicy: Never
      parameters:
      - src:
          dependencyName: dep-name
          dataKey: body.message
        dest: spec.containers.0.command.1
```

**RBAC required**: Sensor service account needs permissions for the K8s resource type.

### 4. Custom Trigger (gRPC)

```yaml
triggers:
- template:
    name: custom-trigger
    custom:
      serverURL: trigger-server.namespace.svc:9000
      spec:
        url: "https://example.com/resource.yaml"
      parameters:
      - src:
          dependencyName: dep-name
          dataKey: body.field
        dest: metadata.namespace
      payload:
      - src:
          dependencyName: dep-name
          dataKey: body
        dest: payload
```

**gRPC methods**: `FetchResource`, `Execute`, `ApplyPolicy`

---

## Trigger Conditions

Conditions use boolean expressions to control which triggers fire based on event dependencies.

```yaml
spec:
  dependencies:
  - name: webhook-dep
    eventSourceName: webhook
    eventName: example
  - name: minio-dep
    eventSourceName: minio
    eventName: file-upload
  triggers:
  # Only fires when webhook event received
  - template:
      name: webhook-only
      conditions: "webhook-dep"
      # ... trigger config
  # Only fires when minio event received
  - template:
      name: minio-only
      conditions: "minio-dep"
      # ... trigger config
  # Fires when BOTH events received
  - template:
      name: both-required
      conditions: "webhook-dep && minio-dep"
      # ... trigger config
  # Fires when EITHER event received
  - template:
      name: either-works
      conditions: "webhook-dep || minio-dep"
      # ... trigger config
```

**Operators**: `&&` (AND), `||` (OR), `!` (NOT), `-` (NOT), parentheses for grouping

---

## Trigger Policies

Policies determine success/failure of triggered resources.

### Resource Labels Policy (for Argo Workflows)
```yaml
triggers:
- template:
    name: with-policy
    argoWorkflow: { ... }
  policy:
    k8s:
      labels:
        workflows.argoproj.io/completed: "true"
      backoff:
        steps: 3
        duration: 5s
        factor: 2
      errorOnBackoffTimeout: true
```

### Status Policy (for HTTP triggers)
```yaml
triggers:
- template:
    name: http-with-policy
    http: { ... }
  policy:
    status:
      allow: [200, 201, 202]
```

---

## Retry Strategy

```yaml
retryStrategy:
  steps: 3            # Number of retries
  duration: 5s         # Wait between retries
  factor: 2            # Backoff multiplier (optional)
  jitter: 0.5          # Randomization factor (optional)
```

---

## Dependency Filters

Filter events before they reach triggers.

### Data Filter
```yaml
dependencies:
- name: filtered-dep
  eventSourceName: webhook
  eventName: example
  filters:
    data:
    - path: body.action
      type: string
      value: ["completed", "approved"]
    - path: body.priority
      type: number
      value: ["1", "2"]
      comparator: ">="
```

### Context Filter
```yaml
dependencies:
- name: filtered-dep
  eventSourceName: webhook
  eventName: example
  filters:
    context:
      type: webhook
      source: my-source
      subject: specific-subject
```

### Time Filter
```yaml
dependencies:
- name: filtered-dep
  filters:
    time:
      start: "09:00:00"
      stop: "17:00:00"
```

### Expression Filter
```yaml
dependencies:
- name: filtered-dep
  filters:
    exprs:
    - expr: action == 'completed' && priority >= 3
      fields:
      - name: action
        path: body.action
      - name: priority
        path: body.priority
```

---

## TAS Workflow Builder → Argo Events Mapping

This section maps the Aether UI trigger types to Argo Events CRDs.

### Manual Trigger
- **Argo pattern**: No EventSource needed — direct API call to aether-be `/api/v1/workflows/{id}/execute`
- **With Argo**: Can optionally POST to webhook EventSource at `/workflow-trigger` with `{ workflow_id, input_data }`
- **Config**: `input_parameters[]` define the form shown at run time

### Schedule Trigger
- **Argo pattern**: Calendar EventSource with cron + Sensor with argoWorkflow trigger
- **EventSource**: Calendar entry with `schedule` (cron) and `timezone`
- **Sensor dependency**: `eventSourceName: calendar`, `eventName: <schedule-name>`
- **Config fields**: `config.cron`, `config.schedule` (friendly), `config.timezone`

### File Upload Trigger
- **Argo pattern**: MinIO EventSource watching bucket + Sensor with argoWorkflow trigger
- **EventSource**: MinIO entry with `bucket.name`, `events: [s3:ObjectCreated:*]`, optional `filter.suffix`
- **Sensor parameterization**: Extract `notification.0.s3.bucket.name` and `notification.0.s3.object.key`
- **Config fields**: `config.accepted_extensions`, `config.source_filter`, `config.notebook_ids`

### Webhook Trigger
- **Argo pattern**: Webhook EventSource + Sensor
- **EventSource**: Webhook entry with `port`, `endpoint`, `method`
- **Config fields**: `config.argo_event_source`, `config.argo_event_name`, `config.http_method`, `config.webhook_url`

### API Trigger
- **Argo pattern**: Same as Manual — direct API call or webhook EventSource
- **Endpoint**: `/api/v1/workflows/{id}/execute` (auto-generated)
- **Config fields**: `config.api_endpoint` (read-only)

### Document Event Trigger
- **Argo pattern**: Kafka EventSource listening to processing topics + Sensor with filtered dependency
- **EventSource**: Kafka entry with topic mapping to event type
- **Event type mapping**:
  - `processing.completed` → Kafka topic `processing.complete`
  - `compliance.completed` → Kafka topic `compliance.complete`
  - `document.shared` → Kafka topic `user-events` (with data filter)
- **Config fields**: `config.event_type`, `config.notebook_ids`, `config.mime_filter`

---

## RBAC Reference

### Workflow Runner (argo namespace)
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: argo-workflow-runner
  namespace: argo
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argo-workflow-runner
rules:
- apiGroups: [""]
  resources: [pods, pods/log]
  verbs: [get, list, watch, create, delete]
- apiGroups: [""]
  resources: [configmaps, secrets]
  verbs: [get, list]
- apiGroups: [""]
  resources: [persistentvolumeclaims]
  verbs: [get, list, create, delete]
- apiGroups: [argoproj.io]
  resources: [workflows, workflowtemplates, cronworkflows]
  verbs: [get, list, watch, create, update, delete]
```

### Events Service Account (argo-events namespace)
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: argo-events-sa
  namespace: argo-events
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argo-events-role
rules:
- apiGroups: [""]
  resources: [pods, services, configmaps, secrets]
  verbs: [get, list, watch]
- apiGroups: [""]
  resources: [events]
  verbs: [create, patch]
- apiGroups: [argoproj.io]
  resources: [workflows]
  verbs: [get, list, create, update, delete]
- apiGroups: [argoproj.io]
  resources: [eventsources, sensors, eventbus]
  verbs: [get, list, watch, create, update, delete]
- apiGroups: [apps]
  resources: [deployments]
  verbs: [get, list, watch, create, update, delete]
```

---

## Design Principles for TAS Integration

1. **One EventSource per type** — TAS uses a single webhook EventSource, a single calendar EventSource, etc. Individual triggers are event names within the source.

2. **Sensors per workflow** — Each TAS workflow that uses Argo Events should have its own Sensor (or share the workflow-trigger sensor for webhook/API triggers).

3. **Parameterize everything** — Pass workflow ID and input data through event parameters. The Argo Workflow step calls back to aether-be to execute the actual workflow logic.

4. **HTTP callback pattern** — Argo Workflow steps use `curlimages/curl:8.8.0` to POST back to aether-be, which runs the workflow engine logic. This keeps business logic in aether-be, not in Argo templates.

5. **Retry with backoff** — All triggers include `retryStrategy` with 3 steps and 5s duration.

6. **Artifact storage via MinIO** — Argo Workflows use `artifactRepositoryRef` pointing to MinIO at `minio-shared.tas-shared.svc.cluster.local:9000`.

7. **Namespace isolation** — EventSources and Sensors in `argo-events`, Workflows in `argo`, applications in their own namespaces.
