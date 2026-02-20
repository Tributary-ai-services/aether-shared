# Argo Workflows Reference for TAS

This document provides a comprehensive reference for Argo Workflows design patterns, concepts, and usage in TAS. The Aether workflow builder generates Argo Workflow CRDs that follow these patterns.

**TAS Deployment**: Argo Workflows v3.6.4, namespace `argo`
**Documentation**: [argo-workflows.readthedocs.io](https://argo-workflows.readthedocs.io/en/latest/)

---

## Architecture: Argo Events + Argo Workflows

Argo Workflows and Argo Events are separate but complementary systems:

```
┌─────────────────── Argo Events ───────────────────┐     ┌──── Argo Workflows ────┐
│                                                    │     │                        │
│  EventSource ──▶ EventBus ──▶ Sensor ──▶ Trigger ──┼────▶│  Workflow (CRD)        │
│  (webhook,       (NATS)      (filter +   (submit)  │     │  ├── Steps / DAG       │
│   calendar,                   match)               │     │  ├── Templates          │
│   kafka,                                           │     │  └── Artifacts (MinIO)  │
│   minio)                                           │     │                        │
└────────────────────────────────────────────────────┘     └────────────────────────┘
```

- **Argo Events** handles the "when" — event detection, filtering, routing
- **Argo Workflows** handles the "what" — execution of multi-step computational pipelines
- Events triggers Workflows via the `argoWorkflow` trigger type (submit operation)
- See `argo-events-reference.md` for the Events side

---

## Core CRDs

| CRD | Purpose | Namespace |
|-----|---------|-----------|
| `Workflow` | Single workflow execution (both spec AND state) | `argo` |
| `WorkflowTemplate` | Reusable workflow definitions (library) | `argo` |
| `ClusterWorkflowTemplate` | Cluster-wide reusable templates | cluster-scoped |
| `CronWorkflow` | Scheduled workflow execution | `argo` |

---

## Workflow Structure

A `Workflow` is a "live" Kubernetes object — it defines the execution spec AND stores runtime state.

### Minimal Workflow
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: hello-world-
spec:
  entrypoint: hello-world        # Which template to run first
  templates:
  - name: hello-world            # Template definition
    container:
      image: busybox
      command: [echo]
      args: ["hello world"]
```

### Full Workflow Spec Fields
```yaml
spec:
  entrypoint: main                    # Required: starting template
  arguments:                          # Workflow-level input parameters
    parameters:
    - name: param1
      value: default-value
  templates: [...]                    # Template definitions (see below)
  onExit: cleanup-template            # Exit handler (always runs)
  serviceAccountName: argo-workflow-runner
  artifactRepositoryRef:              # MinIO/S3 artifact config
    configMap: artifact-repositories
    key: default-v1
  volumeClaimTemplates: [...]         # Dynamic PVC creation
  volumes: [...]                      # Static volume mounts
  ttlStrategy:                        # Auto-cleanup
    secondsAfterCompletion: 3600
  podGC:                              # Pod garbage collection
    strategy: OnPodCompletion
  retryStrategy: {...}                # Workflow-level retry
  synchronization: {...}              # Concurrency control
  priority: 1                         # Scheduling priority
  parallelism: 5                      # Max concurrent nodes
```

---

## Template Types

Templates are the "functions" of a workflow. There are two categories:

### Template Definitions (do the work)

#### 1. Container Template
The most common type — runs a single container.
```yaml
- name: process-data
  inputs:
    parameters:
    - name: filename
  container:
    image: python:3.11-slim
    command: [python, /scripts/process.py]
    args: ["{{inputs.parameters.filename}}"]
    resources:
      requests:
        memory: "256Mi"
        cpu: "100m"
```

#### 2. Script Template
Convenience wrapper — embeds script source inline. Stdout is captured as `outputs.result`.
```yaml
- name: gen-random
  script:
    image: python:alpine3.23
    command: [python]
    source: |
      import json, random, sys
      result = [random.randint(1, 100) for _ in range(10)]
      json.dump(result, sys.stdout)
```

#### 3. Resource Template
Manages Kubernetes resources (create/apply/patch/delete).
```yaml
- name: create-configmap
  resource:
    action: create
    successCondition: status.phase == Active
    failureCondition: status.phase == Failed
    manifest: |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        generateName: my-config-
      data:
        key: "{{inputs.parameters.value}}"
```

#### 4. HTTP Template (v3.2+)
Executes HTTP requests. Response body → `outputs.result`.
```yaml
- name: call-api
  http:
    url: "http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/workflows/{{inputs.parameters.workflow-id}}/execute"
    method: POST
    headers:
    - name: Content-Type
      value: application/json
    body: '{{inputs.parameters.payload}}'
    timeoutSeconds: 30
    successCondition: "response.statusCode == 200"
```

#### 5. Suspend Template
Pauses execution until manual resume or timeout.
```yaml
- name: wait-for-approval
  suspend:
    duration: "3600s"    # Auto-resume after 1 hour (optional)
```
Resume via: `argo resume <workflow-name>` or API call.

#### 6. Container Set Template (v3.1+)
Multiple containers in a single pod, sharing volumes.
```yaml
- name: multi-step
  volumes:
  - name: workspace
    emptyDir: {}
  containerSet:
    volumeMounts:
    - mountPath: /workspace
      name: workspace
    containers:
    - name: download
      image: curlimages/curl:8.8.0
      command: [sh, -c]
      args: ["curl -o /workspace/data.json https://api.example.com/data"]
    - name: process
      image: python:alpine3.23
      command: [python, /workspace/process.py]
      dependencies: [download]    # Waits for download to finish
    - name: main                  # Required for artifact I/O
      image: busybox
      command: [cat, /workspace/result.json]
      dependencies: [process]
```

### Template Invocators (control flow)

#### 7. Steps Template
Sequential and parallel execution. Outer list = sequential, inner list = parallel.
```yaml
- name: pipeline
  steps:
  - - name: step1              # First: run alone
      template: prepare-data
  - - name: step2a             # Then: run in parallel
      template: process-chunk-a
    - name: step2b
      template: process-chunk-b
  - - name: step3              # Finally: run alone
      template: assemble-results
```

#### 8. DAG Template
Dependency-based execution graph. Tasks without dependencies run immediately.
```yaml
- name: diamond
  dag:
    tasks:
    - name: A
      template: echo
      arguments:
        parameters: [{name: message, value: A}]
    - name: B
      depends: "A"
      template: echo
      arguments:
        parameters: [{name: message, value: B}]
    - name: C
      depends: "A"
      template: echo
      arguments:
        parameters: [{name: message, value: C}]
    - name: D
      depends: "B && C"
      template: echo
      arguments:
        parameters: [{name: message, value: D}]
```

**`failFast`**: Set `failFast: false` to allow all branches to complete even if one fails.

---

## Parameters

### Input Parameters
```yaml
- name: my-template
  inputs:
    parameters:
    - name: message
      default: "hello"     # Optional default
  container:
    image: busybox
    command: [echo]
    args: ["{{inputs.parameters.message}}"]
```

### Workflow-Level Arguments
```yaml
spec:
  arguments:
    parameters:
    - name: global-param
      value: "default-value"
  # Access anywhere: {{workflow.parameters.global-param}}
```

### Passing Between Steps
```yaml
steps:
- - name: generate
    template: gen-data
- - name: consume
    template: use-data
    arguments:
      parameters:
      - name: data
        value: "{{steps.generate.outputs.result}}"        # Steps syntax
        # value: "{{tasks.generate.outputs.result}}"       # DAG syntax
```

### Output Parameters
```yaml
- name: gen-data
  container:
    image: busybox
    command: [sh, -c]
    args: ["echo 42 > /tmp/result.txt"]
  outputs:
    parameters:
    - name: answer
      valueFrom:
        path: /tmp/result.txt        # Read from file
    # OR the special 'result' parameter (stdout, up to 256KB):
    # Access as: {{steps.gen-data.outputs.result}}
```

### CLI Parameter Override
```bash
argo submit workflow.yaml -p message="custom value"
argo submit workflow.yaml --parameter-file params.yaml
argo submit --from workflowtemplate/my-template -p key=value
```

---

## Artifacts

Artifacts pass files/directories between steps via S3/MinIO.

### Artifact Passing Pattern
```yaml
templates:
- name: generate
  container:
    image: busybox
    command: [sh, -c]
    args: ["echo hello > /tmp/output.txt"]
  outputs:
    artifacts:
    - name: hello-art
      path: /tmp/output.txt

- name: consume
  inputs:
    artifacts:
    - name: message
      path: /tmp/message           # Mounted here in container
  container:
    image: busybox
    command: [cat, /tmp/message]

# In steps/dag:
- - name: step2
    template: consume
    arguments:
      artifacts:
      - name: message
        from: "{{steps.step1.outputs.artifacts.hello-art}}"
```

### S3/MinIO Artifact Repository
```yaml
# TAS uses artifact repository ref:
spec:
  artifactRepositoryRef:
    configMap: artifact-repositories
    key: default-v1

# Or inline:
outputs:
  artifacts:
  - name: data
    path: /mnt/out
    archive:
      none: {}                     # No compression
    s3:
      key: "{{workflow.name}}/results/data.json"
```

### Archive Strategies
- **Default**: tar + gzip compression
- `archive: { none: {} }` — Upload as-is (for directories, key must end with `/`)
- `archive: { tar: { compressionLevel: 6 } }` — Custom compression

### Artifact Garbage Collection (v3.4+)
```yaml
spec:
  artifactGC:
    strategy: OnWorkflowDeletion    # or OnWorkflowCompletion, Never
```

---

## Variables & Template Tags

### Workflow-Level
| Variable | Description |
|----------|-------------|
| `{{workflow.name}}` | Workflow name |
| `{{workflow.namespace}}` | Namespace |
| `{{workflow.uid}}` | Unique ID |
| `{{workflow.parameters.<NAME>}}` | Global parameter value |
| `{{workflow.creationTimestamp}}` | Creation time (RFC 3339) |
| `{{workflow.duration}}` | Elapsed seconds |
| `{{workflow.status}}` | Status (in exit handler only) |
| `{{workflow.failures}}` | Failure details JSON (in exit handler) |
| `{{workflow.scheduledTime}}` | CronWorkflow scheduled time |

### Step/Task References
| Variable | Description |
|----------|-------------|
| `{{steps.<NAME>.outputs.result}}` | Stdout of step |
| `{{steps.<NAME>.outputs.parameters.<PARAM>}}` | Output parameter |
| `{{steps.<NAME>.outputs.artifacts.<ART>}}` | Output artifact ref |
| `{{steps.<NAME>.status}}` | Step status |
| `{{steps.<NAME>.exitCode}}` | Exit code |
| `{{tasks.<NAME>.outputs.result}}` | Same for DAG tasks |

### Loop Variables
| Variable | Description |
|----------|-------------|
| `{{item}}` | Current item (scalar) |
| `{{item.<KEY>}}` | Current item field (map) |

### Retry Variables
| Variable | Description |
|----------|-------------|
| `{{lastRetry.exitCode}}` | Last retry exit code |
| `{{lastRetry.status}}` | Last retry status |
| `{{lastRetry.duration}}` | Last retry duration (seconds) |

### Expression Tags (v3.1+)
```yaml
# Computation in tags:
"{{=workflow.parameters.count > 5 ? 'large' : 'small'}}"
"{{=filter([1, 2, 3], { # > 1 })}}"
"{{=toJson(steps.generate.outputs.result)}}"
"{{=jsonpath(steps.data.outputs.result, '$.items[0].name')}}"
# Sprig functions: sprig.trim(), sprig.upper(), etc.
```

---

## Control Flow

### Conditionals (`when`)
```yaml
steps:
- - name: flip
    template: flip-coin
- - name: heads
    template: heads-handler
    when: "{{steps.flip.outputs.result}} == heads"
  - name: tails
    template: tails-handler
    when: "{{steps.flip.outputs.result}} == tails"
```

Operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `&&`, `||`, `=~` (regex)

### Loops

**withItems** — iterate over inline list:
```yaml
- - name: process
    template: handler
    arguments:
      parameters:
      - name: item
        value: "{{item}}"
    withItems: ["a", "b", "c"]
```

**withItems (maps)** — iterate over objects:
```yaml
    withItems:
    - { image: 'debian', tag: '9.1' }
    - { image: 'alpine', tag: '3.6' }
    # Access: {{item.image}}, {{item.tag}}
```

**withParam** — iterate over dynamic JSON array:
```yaml
- - name: generate
    template: gen-list
- - name: process
    template: handler
    withParam: "{{steps.generate.outputs.result}}"
```

**withSequence** — iterate N times:
```yaml
    withSequence:
      count: "5"             # {{item}} = 0, 1, 2, 3, 4
      # OR: start/end/format
```

All loop iterations run **in parallel** by default.

### Enhanced Depends (DAG, v2.9+)

Fine-grained dependency on task status:

| Status | Meaning |
|--------|---------|
| `.Succeeded` | Completed without error |
| `.Failed` | Non-zero exit code |
| `.Errored` | Infrastructure error |
| `.Skipped` | `when` condition was false |
| `.Omitted` | `depends` condition was false |
| `.Daemoned` | Daemon task running |

```yaml
dag:
  tasks:
  - name: deploy
    depends: "build.Succeeded && (test.Succeeded || test.Skipped)"
    template: deploy
```

For `withItems` tasks: `.AnySucceeded`, `.AllFailed`

---

## Retry Strategy

```yaml
retryStrategy:
  limit: "10"                      # Max retries
  retryPolicy: "OnFailure"        # Always | OnFailure | OnError | OnTransientError
  backoff:
    duration: "5s"                 # Initial delay
    factor: "2"                    # Exponential multiplier
    maxDuration: "5m"              # Max delay cap
  expression: "asInt(lastRetry.exitCode) > 1"  # Conditional retry (v3.2+)
```

**Retry Policies**:
- `Always` — Retry all failures
- `OnFailure` — Container failed (default)
- `OnError` — Controller/init error
- `OnTransientError` — Transient errors only

---

## Exit Handlers

Always execute, regardless of success/failure:

```yaml
spec:
  entrypoint: main
  onExit: cleanup                  # Always runs after main
  templates:
  - name: cleanup
    steps:
    - - name: notify
        template: send-notification
      - name: celebrate
        template: success-handler
        when: "{{workflow.status}} == Succeeded"
      - name: alert
        template: failure-handler
        when: "{{workflow.status}} != Succeeded"
```

`{{workflow.status}}` values: `Succeeded`, `Failed`, `Error`

---

## WorkflowTemplates (Reusable)

Store reusable workflow definitions as cluster resources:

### Define Template
```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: tas-document-processor
  namespace: argo
spec:
  entrypoint: process
  arguments:
    parameters:
    - name: document-id
    - name: processing-type
      value: "standard"
      enum: [standard, compliance, summarization]  # UI dropdown
  templates:
  - name: process
    inputs:
      parameters:
      - name: document-id
      - name: processing-type
    container:
      image: registry-api.tas.scharber.com/document-processor:latest
      command: [/process]
      args: ["--doc={{inputs.parameters.document-id}}", "--type={{inputs.parameters.processing-type}}"]
```

### Reference from Workflow
```yaml
spec:
  templates:
  - name: main
    steps:
    - - name: process
        templateRef:
          name: tas-document-processor    # WorkflowTemplate name
          template: process               # Template within it
        arguments:
          parameters:
          - name: document-id
            value: "doc-123"
```

### Submit from WorkflowTemplate
```yaml
spec:
  workflowTemplateRef:
    name: tas-document-processor
  arguments:
    parameters:
    - name: document-id
      value: "doc-456"
```

CLI: `argo submit --from workflowtemplate/tas-document-processor -p document-id=doc-789`

---

## CronWorkflows (Scheduled)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: CronWorkflow
metadata:
  name: daily-compliance-scan
spec:
  schedules:
  - "0 2 * * *"                    # 2:00 AM daily (v3.6+ supports list)
  timezone: "America/New_York"     # IANA timezone
  concurrencyPolicy: "Forbid"     # Allow | Forbid | Replace
  startingDeadlineSeconds: 300     # Grace period for missed schedules
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  suspend: false                   # Pause scheduling
  stopStrategy:                    # Auto-stop (v3.6+)
    expression: "cronworkflow.succeeded >= 10"
  workflowSpec:
    entrypoint: scan
    serviceAccountName: argo-workflow-runner
    templates:
    - name: scan
      container:
        image: registry-api.tas.scharber.com/compliance-scanner:latest
        command: [/scan]
```

**Concurrency Policies**:
- `Allow` — Run all scheduled instances concurrently
- `Forbid` — Skip new if previous still running
- `Replace` — Cancel previous, start new

---

## Workflow of Workflows

Parent workflow submits and monitors child workflows:

```yaml
- name: submit-child
  resource:
    action: create
    successCondition: status.phase == Succeeded
    failureCondition: status.phase in (Failed, Error)
    manifest: |
      apiVersion: argoproj.io/v1alpha1
      kind: Workflow
      metadata:
        generateName: child-workflow-
      spec:
        workflowTemplateRef:
          name: {{inputs.parameters.template}}
        arguments:
          parameters:
          - name: input
            value: {{inputs.parameters.data}}
```

---

## Key Design Patterns for TAS

### Pattern 1: HTTP Callback to Aether Backend
TAS workflows call back to aether-be rather than containing business logic:
```yaml
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
```

### Pattern 2: Map-Reduce for Document Processing
Process multiple documents in parallel, then aggregate:
```yaml
- name: main
  dag:
    tasks:
    - name: split
      template: split-documents
    - name: process
      template: process-document
      depends: "split"
      withParam: "{{tasks.split.outputs.result}}"
      arguments:
        parameters:
        - name: doc-id
          value: "{{item}}"
    - name: reduce
      template: aggregate-results
      depends: "process"
```

### Pattern 3: Conditional Processing Pipeline
Branch based on document type:
```yaml
- name: pipeline
  steps:
  - - name: classify
      template: classify-document
  - - name: process-pdf
      template: pdf-processor
      when: "{{steps.classify.outputs.result}} == pdf"
    - name: process-image
      template: image-processor
      when: "{{steps.classify.outputs.result}} == image"
  - - name: finalize
      template: save-results
```

### Pattern 4: Approval Workflow with Suspend
Human-in-the-loop approval:
```yaml
- name: approval-flow
  steps:
  - - name: prepare
      template: prepare-review
  - - name: wait-approval
      template: wait
  - - name: apply
      template: apply-changes
      when: "{{steps.wait-approval.outputs.result}} == approved"

- name: wait
  suspend:
    duration: "86400s"    # Auto-reject after 24h
```

### Pattern 5: CI/CD with Shared Volume
Build and test with shared workspace:
```yaml
spec:
  volumeClaimTemplates:
  - metadata:
      name: workdir
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 1Gi
  templates:
  - name: ci
    steps:
    - - name: build
        template: build-step
    - - name: test-a
        template: test-step
        arguments:
          parameters: [{name: suite, value: "unit"}]
      - name: test-b
        template: test-step
        arguments:
          parameters: [{name: suite, value: "integration"}]
  - name: build-step
    container:
      image: golang:1.21
      command: [go, build, ./...]
      volumeMounts:
      - name: workdir
        mountPath: /workspace
  - name: test-step
    inputs:
      parameters:
      - name: suite
    container:
      image: golang:1.21
      command: [go, test, "-run={{inputs.parameters.suite}}", ./...]
      volumeMounts:
      - name: workdir
        mountPath: /workspace
```

### Pattern 6: Workflow with Exit Handler Notification
```yaml
spec:
  entrypoint: main
  onExit: notify
  templates:
  - name: main
    dag:
      tasks:
      - name: process
        template: process-step
  - name: notify
    http:
      url: http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/webhooks/workflow-complete
      method: POST
      headers:
      - name: Content-Type
        value: application/json
      body: |
        {
          "workflow": "{{workflow.name}}",
          "status": "{{workflow.status}}",
          "duration": "{{workflow.duration}}"
        }
```

---

## TAS-Specific Configuration

### Service Account
```yaml
serviceAccountName: argo-workflow-runner
# Has permissions for: pods, configmaps, secrets, workflows, workflowtemplates, cronworkflows
```

### Artifact Repository (MinIO)
```yaml
artifactRepositoryRef:
  configMap: artifact-repositories
  key: default-v1
# Points to: minio-shared.tas-shared.svc.cluster.local:9000
# Bucket: argo-artifacts
```

### Workflow Naming
TAS uses `generateName` prefix patterns:
- `tas-workflow-` — Generic workflow executions
- `file-process-` — File upload processing
- `daily-analytics-` — Scheduled analytics

### Resource Limits Best Practice
```yaml
container:
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "512Mi"
      cpu: "500m"
```

---

## Mapping: Aether UI Workflow Steps → Argo Templates

| Aether Step Type | Argo Template Type | Notes |
|------------------|--------------------|-------|
| AI Analysis | Container/Script | Calls LLM Router via HTTP |
| Notification | HTTP Template | POST to notification service |
| Assemble Output | Script (Python) | Merge step outputs |
| Condition | DAG `when` / `depends` | Branch logic |
| Agent | Container | Calls Agent Builder API |
| Manual Approval | Suspend | Resume via API/UI |

### Step-to-Step Data Flow
```
Step A (output) ──▶ Artifact/Parameter ──▶ Step B (input)

# Via parameters (small data, <256KB):
{{steps.stepA.outputs.result}}
{{tasks.taskA.outputs.parameters.name}}

# Via artifacts (large data, files):
{{steps.stepA.outputs.artifacts.name}}
# Stored in MinIO: argo-artifacts/<workflow-name>/...
```

---

## Complete Example: TAS Document Processing Workflow

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: tas-doc-process-
  namespace: argo
spec:
  entrypoint: main
  serviceAccountName: argo-workflow-runner
  artifactRepositoryRef:
    configMap: artifact-repositories
    key: default-v1
  arguments:
    parameters:
    - name: workflow-id
    - name: document-id
    - name: processing-type
      value: "standard"
  onExit: notify-complete
  templates:
  # Main pipeline (DAG)
  - name: main
    dag:
      tasks:
      - name: fetch-document
        template: fetch-doc
        arguments:
          parameters:
          - name: doc-id
            value: "{{workflow.parameters.document-id}}"
      - name: classify
        template: classify-doc
        depends: "fetch-document"
      - name: extract-text
        template: extract
        depends: "classify"
        when: "{{tasks.classify.outputs.result}} != image-only"
      - name: analyze
        template: ai-analysis
        depends: "extract-text || (classify && classify.Succeeded)"
        arguments:
          parameters:
          - name: doc-id
            value: "{{workflow.parameters.document-id}}"
      - name: save-results
        template: save
        depends: "analyze"

  # Fetch document from MinIO
  - name: fetch-doc
    inputs:
      parameters:
      - name: doc-id
    container:
      image: curlimages/curl:8.8.0
      command: [sh, -c]
      args:
      - |
        curl -sf "http://audimodal.aether-be.svc.cluster.local:8084/api/v1/documents/{{inputs.parameters.doc-id}}" \
          -o /tmp/document.json
    outputs:
      artifacts:
      - name: document
        path: /tmp/document.json

  # Classify document type
  - name: classify-doc
    script:
      image: python:alpine3.23
      command: [python]
      source: |
        import json
        # Read document metadata and determine type
        # Output: text, pdf, image-only, mixed
        print("text")

  # Extract text content
  - name: extract
    container:
      image: registry-api.tas.scharber.com/text-extractor:latest
      command: [/extract]
    outputs:
      parameters:
      - name: text
        valueFrom:
          path: /tmp/extracted.txt

  # AI analysis via LLM Router
  - name: ai-analysis
    inputs:
      parameters:
      - name: doc-id
    container:
      image: curlimages/curl:8.8.0
      command: [sh, -c]
      args:
      - |
        curl -sf -X POST \
          "http://tas-llm-router.tas-llm-router.svc.cluster.local:8085/api/v1/chat/completions" \
          -H 'Content-Type: application/json' \
          -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Analyze document {{inputs.parameters.doc-id}}"}]}'

  # Save results to aether-be
  - name: save
    http:
      url: "http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/workflows/{{workflow.parameters.workflow-id}}/results"
      method: POST
      headers:
      - name: Content-Type
        value: application/json
      body: |
        {
          "workflow_id": "{{workflow.parameters.workflow-id}}",
          "document_id": "{{workflow.parameters.document-id}}",
          "status": "completed"
        }

  # Exit handler — always notify
  - name: notify-complete
    http:
      url: "http://aether-backend.aether-be.svc.cluster.local:8080/api/v1/webhooks/workflow-complete"
      method: POST
      headers:
      - name: Content-Type
        value: application/json
      body: |
        {
          "workflow": "{{workflow.name}}",
          "workflow_id": "{{workflow.parameters.workflow-id}}",
          "status": "{{workflow.status}}",
          "duration": "{{workflow.duration}}"
        }
```

---

## Summary: Key Principles

1. **Templates = Functions** — Template definitions do work, template invocators control flow
2. **DAGs over Steps** — Use DAGs for complex pipelines (better parallelism, clearer dependencies)
3. **Parameters for small data** — stdout/files up to 256KB
4. **Artifacts for large data** — Files/directories via MinIO
5. **HTTP callbacks** — Keep business logic in aether-be, use Argo for orchestration
6. **Exit handlers** — Always notify on completion/failure
7. **WorkflowTemplates** — Reuse common patterns across workflows
8. **CronWorkflows** — Native scheduling with timezone and concurrency control
9. **Retry with backoff** — Production workflows should always have retry strategies
10. **Resource limits** — Always set requests/limits for predictable scheduling
