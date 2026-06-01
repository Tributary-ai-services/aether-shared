---
name: aiqg-request-lifecycle
description: End-to-end cross-service flow of a single LLM request through the AIQG-mode gateway path — from customer redirect to TimescaleDB aggregation
metadata:
  type: flow
---

# AIQG Request Lifecycle (Cross-Service Flow)

This document traces a single LLM request from the customer's application through the AI Quality Gateway and into the analytics pipeline. It is the cross-service counterpart to per-service docs like [request-event](../../aiqg/request-event.md), [response-event](../../aiqg/response-event.md), and the [tas-llm-router AIQG extension](../../../../tas-llm-router/docs/AIQG-EXTENSION.md).

**Audience:** developers integrating new endpoints, operators debugging request-loss bugs, security reviewers auditing the data flow.

**Scope:** AIQG-mode requests only — i.e., requests carrying both `TAS-Auth: tas_qg_live_*` and an inbound `Authorization: Bearer ...` header. Internal-routing requests (no `TAS-Auth` header) follow the existing pre-AIQG flow and are out of scope here.

---

## 1. Services Involved

| Service | Role |
|---|---|
| **Customer application** | Origin of the request; sets `OPENAI_BASE_URL`/`ANTHROPIC_BASE_URL` to point at the AIQG ingress |
| **NGINX ingress (`gateway.aiqg.tas.io`)** | TLS termination, routes to `tas-llm-router-aiqg` Deployment |
| **tas-llm-router** (AIQG-extended) | The proxy; captures + scores + emits CloudEvents |
| **aiqg-dashboard-be** | Validates `TAS-Auth` tokens via `/internal/auth/validate` |
| **Redis (`tas-redis-shared`)** | Token cache, policy resolution cache, recent-session lookup |
| **Neo4j (`neo4j.aether-be.svc`)** | Source of truth for accounts, tokens, bundles, route rules |
| **Gatekeeper scanner** (linked library inside tas-llm-router) | Hyperscan tagging including AIQG-specific rule packs |
| **Databunker** | PII tokenization when `pii_tokenize_input` rule fires |
| **Vendor (OpenAI, Anthropic, ...)** | Upstream LLM API |
| **Kafka (`kafka-shared`)** | Per-request CloudEvents: `tas.aiqg.{request,response,findings}.v1` |
| **tas-spark-jobs / aiqg_aggregator** | Rolls events into TimescaleDB |
| **TimescaleDB (on `postgres-shared`)** | `aiqg.request_events`, `aiqg.response_events`, `aiqg.metrics_*`, `aiqg.audit_log` |
| **MinIO** | (Sampled) payload retention; report artifacts |
| **Loki** (via Alloy) | Service logs collected from all `aiqg-*` namespaces |

---

## 2. End-to-End Sequence

```mermaid
sequenceDiagram
    autonumber
    participant C as Customer app
    participant N as NGINX ingress
    participant R as tas-llm-router (AIQG mode)
    participant D as aiqg-dashboard-be
    participant Rd as Redis
    participant G as Gatekeeper scanner
    participant V as Vendor (OpenAI/Anthropic)
    participant K as Kafka
    participant S as Spark aggregator
    participant T as TimescaleDB

    C->>N: POST /openai/v1/chat/completions<br/>Headers: TAS-Auth, Authorization
    N->>R: forward (TLS-decrypted)
    R->>R: DetectMode(req) → ModeAIQG
    R->>Rd: GET aiqg:token:sha256(TAS-Auth)
    alt cache miss
        R->>D: POST /internal/auth/validate
        D->>D: Neo4j MATCH (:AIQGToken)-[:OWNED_BY]->(:AIQGAccount)
        D-->>R: Account JSON (tenant_id, region, weights, retention)
        R->>Rd: SET 60s
    else cache hit
        Rd-->>R: Account JSON
    end
    R->>R: aiqg.WithBearer(ctx, Authorization)<br/>aiqg.WithAccount(ctx, account)
    R->>R: instrumentation.AttachTiming(ctx)
    R->>R: middleware.ParseTASHeaders (strip TAS-*)
    R->>G: Scan request with aiqg_workflows.yaml
    G-->>R: workflow_type, confidence, matched_signals
    R->>G: Scan with PII / antipattern rule packs
    G-->>R: tags[] (workflow:rag, antipattern:context_bloat, ...)
    R->>R: sampling.ShouldSample(ctx, classification, account_id)
    R->>R: policy.Resolve(ctx, req) → bundle_id + applied_rules
    R->>K: publish com.tas.aiqg.request.v1<br/>topic tas.aiqg.request.v1
    R->>V: forward request (Authorization passed unchanged)
    Note over R,V: net/http/httptrace fires hooks:<br/>DNSStart/Done, ConnectStart/Done,<br/>TLSHandshakeStart/Done, GotConn,<br/>WroteRequest, GotFirstResponseByte
    V-->>R: SSE stream chunks
    loop per chunk
        R->>R: instrumentation.StampChunk(ctx)
        R-->>C: chunk forwarded
    end
    V-->>R: stream complete
    R->>R: compute timing snapshot, token usage, validity
    R->>G: Scan response with output rule packs
    G-->>R: tags[] (quality:validity_passed, nist:safe, ...)
    R->>R: pkg/clear.Score(inputs) → CLEAR scores
    opt sampled for LLM-as-judge
        R->>V: judge call (via its own router path; dogfooded)
        V-->>R: groundedness, claim accuracy
    end
    R->>K: publish com.tas.aiqg.response.v1<br/>topic tas.aiqg.response.v1
    R->>K: publish AuditLogEntry events as needed
    R-->>C: stream complete
    K->>S: deliver event batch (2min watermark)
    S->>S: tumbling window aggregation
    S->>T: upsert into aiqg.metrics_1m<br/>(tenant_id, scope_type, scope_key, bucket_start)
    Note over T: Continuous aggregates roll up<br/>1m → 5m → 1h → 1d
```

---

## 3. Phase-by-Phase Detail

### Phase 1 — Ingress (steps 1–3 in the diagram)

The customer's SDK (OpenAI or Anthropic) has `*_BASE_URL` pointed at `https://gateway.aiqg.tas.io/openai/v1` (or `/anthropic/v1`). The SDK constructs a standard request with two headers:

- `Authorization: Bearer sk-...` (the customer's vendor API key — the SDK puts it here automatically)
- `TAS-Auth: tas_qg_live_...` (the customer's AIQG token — set via `default_headers={"TAS-Auth": "..."}`)

NGINX terminates TLS using a Let's Encrypt cert via `tas-ca-issuer`, then forwards to the `tas-llm-router-aiqg` Service. The same binary that serves internal traffic handles this request — only the per-request mode detection differs.

The first decision in `tas-llm-router` is `aiqg.DetectMode(req)`. With both headers present and the `tas_qg_live_` prefix matching, the request enters AIQG mode and a context value is set:

```go
ctx = aiqg.WithMode(ctx, aiqg.ModeAIQG)
```

If `Config.AIQG.StrictIngress=true` (which it is on this ingress), missing headers result in a structured 401 with diagnostic body — never a fall-through to internal-routing behavior.

### Phase 2 — Auth & Account Resolution (steps 4–7)

The `TAS-Auth` token is SHA-256'd and looked up in Redis (`aiqg:token:<sha256>`, 60s TTL). On a miss, `tas-llm-router` calls `POST /internal/auth/validate` on `aiqg-dashboard-be`, which:

1. Hashes the inbound plaintext
2. Runs the Cypher: `MATCH (t:AIQGToken {hashed_id: $h, revoked: false})-[:OWNED_BY]->(a:AIQGAccount)`
3. Returns the resolved `Account` (account_id, tenant_id, region, scoring_weights, payload_retention, quotas)
4. Touches `last_used_at` asynchronously

On any failure (token unknown, revoked, account suspended), `tas-llm-router` returns 401 to the customer and emits an `aiqg.audit_log_entry` of type `path_a_auth_rejected` or `token_revoked_request` to Kafka.

On success, the Account JSON is cached in Redis (60s TTL) and attached to the request context via `aiqg.WithAccount(ctx, account)`.

The inbound `Authorization` header is then attached to context for vendor forwarding:

```go
ctx = aiqg.WithBearer(ctx, req.Header.Get("Authorization"))
```

The bearer is **never persisted** anywhere — Redis, Neo4j, TimescaleDB, MinIO, Loki, Kafka. It lives only in process memory for the duration of the request.

### Phase 3 — Header Parsing & Stripping (step 9)

`internal/middleware/aiqg_headers.go` parses all `TAS-*` headers and strips them from the outbound request:

```
TAS-Policy, TAS-Policy-Bundle  → policy override
TAS-Workflow                   → classification override
TAS-Upstream-Authorization     → per-request bearer override (replaces ctx bearer)
TAS-Trace                      → response trace flag
TAS-Dry-Run                    → suppress enforcement
```

After parsing, every `TAS-*` header is removed via `req.Header.Del()` so vendors never see them.

### Phase 4 — Capture & Classification (steps 10–13)

`instrumentation.AttachTiming(ctx)` initializes a `TimingCollector` keyed by context — see [event-timestamps](../../aiqg/event-timestamps.md). The collector records `RequestReceivedAt` immediately.

The Gatekeeper scanner runs three rule-pack passes in a single Hyperscan invocation:

1. `aiqg_workflows.yaml` → workflow classification (single_turn_qa / rag / agentic / summarization / code_generation / classification_extraction / unknown — see [workflow-classification](../../aiqg/workflow-classification.md))
2. Existing PII / compliance / injection packs (HIPAA, GDPR, PCI-DSS, NIST AI RMF, etc.) — applied identically to all TAS scanning paths; no AIQG-specific change
3. `aiqg_*.yaml` antipattern packs (context bloat, prompt antipatterns, behavioral signals)

Output: a tag set per [tag-set](../../aiqg/tag-set.md) and the workflow classification with confidence.

`sampling.ShouldSample` produces a stratified decision per [account](../../aiqg/account.md).`scoring_weights` and recent anomaly history. The decision is logged but does not block the request.

`policy.Resolve` walks: `TAS-Policy*` header override → enabled route rules ordered by priority → account default → pass-through. Each match is cached in Redis (`aiqg:resolve:<tenant_id>:<fingerprint>`, 60s) and invalidated by `tas.aiqg.bundle.updated.v1` / `tas.aiqg.route_rule.changed.v1` events.

### Phase 5 — Request Event Emission (step 14)

Before forwarding to the vendor, `internal/events/aiqg_v1.go` constructs a `com.tas.aiqg.request.v1` CloudEvent and publishes to Kafka topic `tas.aiqg.request.v1` with `partitionKey = tenant_id`. The publish is fire-and-forget — failure does not block the request, but is logged as a critical SRE event.

Payload shape: per [request-event §2](../../aiqg/request-event.md) + [request-structure §2](../../aiqg/request-structure.md) embedded.

### Phase 6 — Forwarding & Streaming Capture (steps 15–19)

The vendor request is dispatched through an `http.Client` instrumented with `net/http/httptrace.ClientTrace`. The trace hooks fire at network milestones — DNS, TCP, TLS, write, first byte — and stamp into the `TimingCollector` from the context.

For streaming responses, the existing provider's `StreamCompletion()` loop gains one line: `instrumentation.StampChunk(ctx)` after each `stream.Recv()`. The chunk's wall-clock arrival time is appended to `TimingCollector.Chunks`. When mode is not AIQG, this call is a no-op — internal callers experience zero added latency.

Chunks flow back to the customer unchanged. The proxy never modifies streamed content in MVP.

### Phase 7 — Response Scoring (steps 20–24)

When the stream completes, the proxy:

1. Reads `TimingCollector.Snapshot()` to produce the [event-timestamps](../../aiqg/event-timestamps.md) sub-structure
2. Computes [token-accounting](../../aiqg/token-accounting.md) from vendor `usage` fields + the new `pkg/clear/cost.ActualCost()` function (does NOT modify the existing `EstimateCost()` — per [build-vs-reuse §1.2](../../aiqg/build-vs-reuse.md))
3. Runs the Gatekeeper scanner on the response with output rule packs → output tags
4. Calls `pkg/clear.Score(inputs)` to produce the five CLEAR scores + composite, embedding `scoring_version`, `score_weights_used`, `score_thresholds_used` for reproducibility (per [build-vs-reuse §7.2](../../aiqg/build-vs-reuse.md))

If the request was sampled (Phase 4), an asynchronous LLM-as-judge call runs against the response. The judge call goes through tas-llm-router's own provider clients — dogfooding the proxy. Judge output (groundedness, etc.) is patched into the response event before emission.

### Phase 8 — Response Event Emission (step 25)

`com.tas.aiqg.response.v1` is published to Kafka topic `tas.aiqg.response.v1` with `request_event_id` as a foreign-key field. Payload shape: per [response-event §2](../../aiqg/response-event.md) + embedded [response-structure](../../aiqg/response-structure.md) + [token-accounting](../../aiqg/token-accounting.md) + [event-timestamps](../../aiqg/event-timestamps.md) + CLEAR scores + tags + sampling decision + payload retention metadata.

Concurrent with the response event, [audit-log-entry](../../aiqg/audit-log-entry.md) records are published for each policy application:
- `policy_applied` for each enforced rule
- `policy_dry_run_match` for each dry-run rule that would have applied
- `header_override` if `TAS-Policy*` headers were used
- `policy_blocked_request` if the request was refused

### Phase 9 — Aggregation (steps 27–29)

`tas-spark-jobs/aiqg_aggregator` subscribes to `tas.aiqg.request.v1` and `tas.aiqg.response.v1`, joins them on `request_event_id`, and applies tumbling-window aggregation per [aggregated-metrics](../../aiqg/aggregated-metrics.md):

- Watermark: 2 minutes (matches existing `tas-spark-jobs/events_aggregator` pattern)
- Window: 1-minute tumbling, upserts into `aiqg.metrics_1m` keyed by `(tenant_id, scope_type, scope_key, bucket_start)`
- Continuous aggregates roll `1m → 5m → 1h → 1d` inside TimescaleDB itself (cheaper than Spark)

The dashboard backend (`aiqg-dashboard-be`) reads exclusively from these aggregate tables. It never scans raw `request_events` or `response_events` for dashboard queries — those tables exist for forensic drill-down, audit, and Day-1 report generation only.

### Phase 10 — Reporting (out-of-band)

Report generation is a separate flow triggered by scheduled jobs or user requests; it consumes the same TimescaleDB tables. See [aiqg-dashboard-be §7](../../../../aiqg-dashboard-be/ARCHITECTURE.md) and [report-snapshot.md](../../aiqg/report-snapshot.md).

---

## 4. Failure Modes & Mitigations

| Failure | Where | Behavior |
|---|---|---|
| Token validation 5xx from dashboard-be | Phase 2 | tas-llm-router returns 503 to customer with diagnostic body; cache miss not retried inline |
| Token unknown / revoked | Phase 2 | 401 to customer; audit log entry emitted regardless of Kafka availability (via separate retry path) |
| Vendor 5xx | Phase 6 | tas-llm-router's existing fallback chain may retry/swap models (or not, depending on policy); the response event records `status=vendor_error` either way |
| Vendor timeout | Phase 6 | tas-llm-router returns 504 to customer; response event emitted with `status=timeout`, `chunk_count` and `last_chunk_at` captured up to the moment of disconnection |
| Customer disconnect mid-stream | Phase 6 | tas-llm-router observes context cancellation; emits response event with `status=client_disconnect`, partial timing data |
| Kafka publish failure | Phase 5/8 | retried with exponential backoff up to 30s, then logged as critical SRE event. Per-request data is lost on persistent failure (the request itself succeeds); this is the documented trade-off — we never block customer traffic on Kafka availability |
| Spark job lag past 2min watermark | Phase 9 | late events dropped from aggregates (matches existing `tas-spark-jobs/events_aggregator` behavior). Forensic queries against the raw event hypertables remain accurate; only dashboards lag |
| Redis unavailable | Phase 2 / Phase 4 | token validation falls back to direct Neo4j hit (Phase 2); policy resolution falls back to recomputing (Phase 4). Adds ~5-20ms latency per request; graceful degradation |

---

## 5. SLO Targets (MVP)

| Boundary | Target |
|---|---|
| Gateway overhead (request_received → request_forwarded + response_complete - last_chunk) | < 50ms p99 |
| Token validation cache hit ratio | > 95% |
| Policy resolution cache hit ratio | > 95% |
| Kafka publish success rate | > 99.9% |
| Aggregator end-to-end lag (event time → metrics_1m row visible) | < 90s p95 |
| Day-1 report generation | < 30s p95 |

Breaches alerted via Prometheus rules in `shared-monitoring/prometheus/alerts/aiqg-slos.yml`.

---

## 6. Tenant Isolation Properties

Tenant `tenant_id` is the canonical scope key. Every event carries it; every aggregate row is keyed by it; every dashboard query MUST filter on it (enforced by middleware in `aiqg-dashboard-be`).

A single customer's request never has its data:
- Cached under a key that another tenant could read (Redis keys are tenant-prefixed)
- Aggregated into a row another tenant's dashboard query could match
- Stored in a MinIO object another tenant could fetch (object keys are tenant-prefixed)
- Logged into Loki with a label collision with another tenant (Alloy adds `tenant_id` as a structured field)

The only cross-tenant access path is the `aiqg:admin:cross_tenant` Keycloak role, used by TAS staff for support tickets. Every such access is logged via [audit-log-entry](../../aiqg/audit-log-entry.md) with `severity=critical`.

---

## 7. Related Documentation

- [tas-llm-router AIQG extension](../../../../tas-llm-router/docs/AIQG-EXTENSION.md) — implementation detail of Phases 1-8
- [aiqg-dashboard-be ARCHITECTURE](../../../../aiqg-dashboard-be/ARCHITECTURE.md) — the validation endpoint hit in Phase 2 and the report-generation worker
- [build-vs-reuse.md](../../aiqg/build-vs-reuse.md) — master plan; especially §1.2 (non-breaking), §7 (decisions), §10 (wire-compat checklist)
- [aiqg/request-event](../../aiqg/request-event.md), [response-event](../../aiqg/response-event.md), [event-timestamps](../../aiqg/event-timestamps.md), [token-accounting](../../aiqg/token-accounting.md), [tag-set](../../aiqg/tag-set.md), [policy-bundle](../../aiqg/policy-bundle.md), [route-rule](../../aiqg/route-rule.md), [audit-log-entry](../../aiqg/audit-log-entry.md), [aggregated-metrics](../../aiqg/aggregated-metrics.md), [report-snapshot](../../aiqg/report-snapshot.md), [workflow-classification](../../aiqg/workflow-classification.md)
- [id-mapping-chain](../mappings/id-mapping-chain.md) — full identifier flow
- [source-spec-v0.2.md §3](../../aiqg/source-spec-v0.2.md) — the product spec section on capture mechanics

---

## 8. Changelog

| Date | Version | Author | Change |
|---|---|---|---|
| 2026-05-31 | v1.0.0 | TAS Platform | Initial flow doc paired with the AIQG MVP architecture spec set |
