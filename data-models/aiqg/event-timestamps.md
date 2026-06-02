---
service: aiqg (tas-llm-router extension)
model: EventTimestamps
storage: PostgreSQL/TimescaleDB JSONB column + AIQG CloudEvent sub-object
version: 1.0.0
last_updated: 2026-05-31
status: initial spec draft
---

# AIQG Event Timestamps — Latency Decomposition Sub-Structure

## 1. Overview

`EventTimestamps` is the latency-decomposition sub-structure captured for every AIQG-mode request flowing through `tas-llm-router`. It is the single source of truth for the Latency (L) dimension of CLEAR — every visualization, SLO breach query, percentile aggregate, and Day-1 report waterfall reads from this structure. It is the data backing [source-spec-v0.2.md §2.2 Latency decomposition](./source-spec-v0.2.md) and the Day-1 Report "latency decomposition" section (Screen 5).

It is embedded in two places:

1. As a JSONB column named `timestamps` on both the [[request-event]] and [[response-event]] hypertable rows in TimescaleDB.
2. As a sub-object on the AIQG CloudEvent payload (`com.tas.aiqg.request.v1`, `com.tas.aiqg.response.v1`) published to Kafka topics `tas.aiqg.request.v1` / `tas.aiqg.response.v1`.

### 1.1 Non-breaking-change constraint

**Critical capture-side rule, per [build-vs-reuse.md §1.2 non-breaking-change constraint](./build-vs-reuse.md) and §2.3 timing:**

Chunk-level and checkpoint timing data **is NOT captured by extending the existing `internal/types/responses.ChatChunk` struct**. The struct stays exactly as it is today — adding a `ReceivedAt` field would change JSON marshaling and break positional struct literals across all existing callers (`tas-agent-builder`, `aether-be`, `audimodal`, `llm-invocation`).

Instead, timings are captured into a sidecar `internal/instrumentation.TimingCollector` keyed by `context.Context` value. Existing callers see no difference; AIQG-mode callers attach a collector to the context at request entry and read its snapshot at request close. If no collector is attached to the context (the default for non-AIQG callers), the stamp calls inside the provider implementations are no-ops.

### 1.2 Why this structure exists separately from the event payload

A single per-request struct would conflate "the request" with "how long the request took" and would make rolling up percentile latencies across millions of requests slower. By keeping `EventTimestamps` as a JSONB column on its own, TimescaleDB continuous aggregates can compute p50/p95/p99 of every latency component on schedule without touching the rest of the event payload.

---

## 2. Schema Definition

All checkpoint fields are microsecond-precision UTC timestamps (`timestamptz` in PostgreSQL, `RFC3339Nano` strings in JSONB / CloudEvent JSON, `time.Time` in Go internal representation). All checkpoint fields are nullable since some checkpoints don't apply to all requests (e.g., a request rejected at policy resolution never reaches `request_forwarded_at`).

### 2.1 Raw checkpoint fields (captured)

| Field | Type | Nullable | Source |
|---|---|---|---|
| `request_received_at` | timestamptz (µs) | No | gateway ingress middleware (`internal/middleware/aiqg_headers.go`) — anchor for every downstream delta |
| `auth_validated_at` | timestamptz (µs) | Yes | after `internal/security/aiqg_auth.go` resolves the `tas_qg_live_*` token |
| `headers_parsed_at` | timestamptz (µs) | Yes | after `internal/middleware/aiqg_headers.go` finishes parsing and stripping all `TAS-*` headers |
| `scan_complete_at` | timestamptz (µs) | Yes | after Gatekeeper Hyperscan has run all enabled inbound rule packs (workflow classifier + PII + antipatterns) — the dominant variable cost in the gateway front-half |
| `policy_resolved_at` | timestamptz (µs) | Yes | after `internal/policy/resolver.go` picks a bundle for this request |
| `request_event_emitted_at` | timestamptz (µs) | Yes | after `internal/events/aiqg_publisher.go` has produced the `com.tas.aiqg.request.v1` CloudEvent to Kafka and the producer has returned |
| `request_forwarded_at` | timestamptz (µs) | Yes | when the gateway writes the first byte to the upstream vendor connection |
| `dns_resolved_at` | timestamptz (µs) | Yes | `net/http/httptrace.ClientTrace.DNSDone` |
| `tcp_connected_at` | timestamptz (µs) | Yes | `net/http/httptrace.ClientTrace.ConnectDone` |
| `tls_handshake_complete_at` | timestamptz (µs) | Yes | `net/http/httptrace.ClientTrace.TLSHandshakeDone` |
| `ttfb_at` | timestamptz (µs) | Yes | `net/http/httptrace.ClientTrace.GotFirstResponseByte` — first byte of the vendor HTTP response (response **headers** arrival, NOT first content token) |
| `ttft_at` | timestamptz (µs) | Yes | stamped on the first SSE chunk whose parsed content delta is **non-empty** (skips role/heartbeat openers). Set inside the provider's stream loop via `timing.StampFirstContent(ctx)` — see [AIQG-EXTENSION §5.3](../../../tas-llm-router/docs/AIQG-EXTENSION.md). Distinct from `ttfb_at`. |
| `last_chunk_at` | timestamptz (µs) | Yes | last `stream.Recv()` before stream close |
| `response_complete_at` | timestamptz (µs) | Yes | last byte written back to the customer client |
| `chunk_count` | int4 | Yes | total SSE chunks observed during the stream |
| `inter_token_latency_p50_ms` | numeric(7,2) | Yes | per-request p50 of inter-chunk deltas in ms |
| `inter_token_latency_p95_ms` | numeric(7,2) | Yes | per-request p95 of inter-chunk deltas in ms |
| `inter_token_latency_p99_ms` | numeric(7,2) | Yes | per-request p99 of inter-chunk deltas in ms |

The new checkpoints (`headers_parsed_at`, `scan_complete_at`, `request_event_emitted_at`) were added 2026-06-01 to expose the previously-unmeasured front-half phases that the `gateway_overhead_ms` formula now includes — see `reviews/architect-review.md` §4 and `reviews/sre-review.md`.

### 2.2 Derived (generated) columns

These are TimescaleDB `GENERATED ALWAYS AS ... STORED` columns on the parent event rows. They are NOT stored inside the JSONB; they are pulled out of JSONB at write time so indexes and continuous aggregates can read them efficiently.

| Field | Type | Formula |
|---|---|---|
| `gateway_overhead_ms` | numeric(8,2) | **Forwarded path:** `(request_forwarded_at − request_received_at) + (response_complete_at − last_chunk_at)`, in ms. **Rejected path (request never forwarded — e.g., 401 / policy-blocked):** `response_complete_at − request_received_at`. Captures the TRUE customer-perceived overhead: auth + headers + scan + classify + policy + event emit + egress. Target: < 50ms p99 (see §4.3). |
| `gateway_overhead_pre_forward_ms` | numeric(8,2) | `request_forwarded_at − request_received_at`, in ms. The front-half overhead in isolation. |
| `gateway_overhead_egress_ms` | numeric(8,2) | `response_complete_at − last_chunk_at`, in ms. The back-half (post-stream) overhead in isolation. |
| `scan_overhead_ms` | numeric(8,2) | `scan_complete_at − headers_parsed_at`, in ms. The Gatekeeper Hyperscan cost across all enabled inbound rule packs. **The known wildcard.** |
| `policy_overhead_ms` | numeric(8,2) | `policy_resolved_at − scan_complete_at`, in ms. The route-matcher + bundle-resolve cost (Redis cache hit ≈ 1-3ms; Redis miss + Neo4j ≈ 20-60ms — see `reviews/architect-review.md` §4 Risk #4). |
| `emit_overhead_ms` | numeric(8,2) | `request_event_emitted_at − policy_resolved_at`, in ms. The CloudEvent construct + JSON serialize + Kafka Produce cost. |
| `network_round_trip_ms` | numeric(8,2) | `(tcp_connected_at − request_forwarded_at) + (dns_resolved_at − request_forwarded_at clamped ≥0) + (tls_handshake_complete_at − tcp_connected_at)`, expressed in ms |
| `vendor_ttft_ms` | numeric(8,2) | `ttft_at − request_forwarded_at`, expressed in ms. (TTFT here is "first non-empty content delta", not first response byte — see §2.1 `ttft_at`.) |
| `vendor_generation_ms` | numeric(8,2) | `last_chunk_at − ttft_at`, expressed in ms |
| `end_to_end_ms` | numeric(9,2) | `response_complete_at − request_received_at`, expressed in ms |

The phase-breakdown columns (`gateway_overhead_pre_forward_ms`, `scan_overhead_ms`, `policy_overhead_ms`, `emit_overhead_ms`) are designed so that when `gateway_overhead_ms` exceeds the SLO target, the dashboard can name the **dominant** phase responsible without parsing JSONB. See §4.6 SLO Breach Tracking.

### 2.3 DDL sketch

```sql
-- Parent hypertable (also referenced by [[request-event]] / [[response-event]])
CREATE TABLE aiqg.event_timestamps (
  event_id              UUID         NOT NULL,
  tenant_id             UUID         NOT NULL,
  account_id            UUID         NOT NULL,
  received_at           TIMESTAMPTZ  NOT NULL,        -- partition key, copied from request_received_at
  timestamps            JSONB        NOT NULL,        -- the raw checkpoint object

  -- Generated columns hoisted from JSONB for index-friendly access.
  --
  -- gateway_overhead_ms: TRUE customer-perceived gateway cost.
  -- Forwarded path: (request_forwarded_at - request_received_at) + (response_complete_at - last_chunk_at)
  -- Rejected path  (request never forwarded): response_complete_at - request_received_at
  -- Returns NULL only when response_complete_at itself is null (request still in flight).
  gateway_overhead_ms   NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE
      WHEN (timestamps->>'request_forwarded_at') IS NOT NULL
       AND (timestamps->>'last_chunk_at')         IS NOT NULL
       AND (timestamps->>'response_complete_at')  IS NOT NULL
      THEN GREATEST(0,
        EXTRACT(EPOCH FROM ((timestamps->>'request_forwarded_at')::timestamptz   - (timestamps->>'request_received_at')::timestamptz)) * 1000
        + EXTRACT(EPOCH FROM ((timestamps->>'response_complete_at')::timestamptz - (timestamps->>'last_chunk_at')::timestamptz))         * 1000
      )
      WHEN (timestamps->>'request_forwarded_at') IS NULL
       AND (timestamps->>'response_complete_at') IS NOT NULL
      THEN GREATEST(0,
        EXTRACT(EPOCH FROM ((timestamps->>'response_complete_at')::timestamptz - (timestamps->>'request_received_at')::timestamptz)) * 1000
      )
      ELSE NULL
    END
  ) STORED,

  -- Phase-breakdown columns let SLO-breach forensics name the dominant phase without parsing JSONB.
  gateway_overhead_pre_forward_ms NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE WHEN (timestamps->>'request_forwarded_at') IS NOT NULL
    THEN GREATEST(0,
      EXTRACT(EPOCH FROM ((timestamps->>'request_forwarded_at')::timestamptz - (timestamps->>'request_received_at')::timestamptz)) * 1000
    ) END
  ) STORED,

  gateway_overhead_egress_ms NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE WHEN (timestamps->>'response_complete_at') IS NOT NULL AND (timestamps->>'last_chunk_at') IS NOT NULL
    THEN GREATEST(0,
      EXTRACT(EPOCH FROM ((timestamps->>'response_complete_at')::timestamptz - (timestamps->>'last_chunk_at')::timestamptz)) * 1000
    ) END
  ) STORED,

  scan_overhead_ms       NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE WHEN (timestamps->>'scan_complete_at') IS NOT NULL AND (timestamps->>'headers_parsed_at') IS NOT NULL
    THEN GREATEST(0,
      EXTRACT(EPOCH FROM ((timestamps->>'scan_complete_at')::timestamptz - (timestamps->>'headers_parsed_at')::timestamptz)) * 1000
    ) END
  ) STORED,

  policy_overhead_ms     NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE WHEN (timestamps->>'policy_resolved_at') IS NOT NULL AND (timestamps->>'scan_complete_at') IS NOT NULL
    THEN GREATEST(0,
      EXTRACT(EPOCH FROM ((timestamps->>'policy_resolved_at')::timestamptz - (timestamps->>'scan_complete_at')::timestamptz)) * 1000
    ) END
  ) STORED,

  emit_overhead_ms       NUMERIC(8,2) GENERATED ALWAYS AS (
    CASE WHEN (timestamps->>'request_event_emitted_at') IS NOT NULL AND (timestamps->>'policy_resolved_at') IS NOT NULL
    THEN GREATEST(0,
      EXTRACT(EPOCH FROM ((timestamps->>'request_event_emitted_at')::timestamptz - (timestamps->>'policy_resolved_at')::timestamptz)) * 1000
    ) END
  ) STORED,

  end_to_end_ms         NUMERIC(9,2) GENERATED ALWAYS AS (
    EXTRACT(EPOCH FROM (
      (timestamps->>'response_complete_at')::timestamptz - (timestamps->>'request_received_at')::timestamptz
    )) * 1000
  ) STORED,

  vendor_ttft_ms        NUMERIC(8,2) GENERATED ALWAYS AS (
    EXTRACT(EPOCH FROM (
      (timestamps->>'ttft_at')::timestamptz - (timestamps->>'request_forwarded_at')::timestamptz
    )) * 1000
  ) STORED,

  vendor_generation_ms  NUMERIC(8,2) GENERATED ALWAYS AS (
    EXTRACT(EPOCH FROM (
      (timestamps->>'last_chunk_at')::timestamptz - (timestamps->>'ttft_at')::timestamptz
    )) * 1000
  ) STORED,

  -- SLO-breach materialized fields (see §4.6)
  slo_breached          BOOLEAN GENERATED ALWAYS AS (
    gateway_overhead_ms IS NOT NULL AND gateway_overhead_ms > 50
  ) STORED,

  PRIMARY KEY (event_id, received_at)
);

CREATE INDEX ix_event_timestamps_slo_breached
  ON aiqg.event_timestamps (tenant_id, received_at DESC)
  WHERE slo_breached = TRUE;

SELECT create_hypertable('aiqg.event_timestamps', 'received_at', chunk_time_interval => INTERVAL '1 day');
```

### 2.4 JSONB / CloudEvent payload shape

```json
{
  "request_received_at":         "2026-05-31T18:42:01.013482Z",
  "auth_validated_at":           "2026-05-31T18:42:01.014119Z",
  "headers_parsed_at":           "2026-05-31T18:42:01.014180Z",
  "scan_complete_at":            "2026-05-31T18:42:01.018940Z",
  "policy_resolved_at":          "2026-05-31T18:42:01.020410Z",
  "request_event_emitted_at":    "2026-05-31T18:42:01.024830Z",
  "request_forwarded_at":        "2026-05-31T18:42:01.025240Z",
  "dns_resolved_at":             "2026-05-31T18:42:01.026802Z",
  "tcp_connected_at":            "2026-05-31T18:42:01.030114Z",
  "tls_handshake_complete_at":   "2026-05-31T18:42:01.051982Z",
  "ttfb_at":                     "2026-05-31T18:42:01.348771Z",
  "ttft_at":                     "2026-05-31T18:42:01.412884Z",
  "last_chunk_at":               "2026-05-31T18:42:04.917330Z",
  "response_complete_at":        "2026-05-31T18:42:04.918002Z",
  "chunk_count":                 312,
  "inter_token_latency_p50_ms":  9.78,
  "inter_token_latency_p95_ms":  17.40,
  "inter_token_latency_p99_ms":  31.16
}
```

Reads as (gateway phases): auth 0.64ms → header parse 0.06ms → scan 4.76ms → policy resolve 1.47ms → event emit 4.42ms → forward 0.41ms = **11.76ms pre-forward**, plus 0.67ms egress = **12.43ms gateway overhead**. Note `ttft_at` is now 63ms after `ttfb_at` — the gap between vendor response headers (TTFB) and first real content chunk (TTFT) on a typical Anthropic streaming response.

---

## 3. Fields Reference

### request_received_at
The wall-clock moment the gateway received the first byte of the inbound HTTP request. Set in the ingress middleware before any AIQG logic runs. **Anchor for every downstream delta.** Wall-clock, not monotonic.

### auth_validated_at
Set after `internal/security/aiqg_auth.go` has resolved the `tas_qg_live_*` token to an account and validated it. Difference from `request_received_at` is the time spent in token lookup + cache check. Null when authentication failed and the request was rejected before this checkpoint.

### headers_parsed_at
Set after `internal/middleware/aiqg_headers.go` finishes parsing all `TAS-*` headers (`TAS-Policy`, `TAS-Policy-Bundle`, `TAS-Workflow`, `TAS-Upstream-Authorization`, `TAS-Trace`, `TAS-Dry-Run`) and stripping them from the outbound request. Difference from `auth_validated_at` should be < 1ms; sustained deltas above that signal a regression in the header pipeline.

### scan_complete_at
Set after the Gatekeeper Hyperscan invocation that runs all enabled inbound rule packs against the serialized request:
- `aiqg_workflows.yaml` — workflow classifier (output → [[workflow-classification]] and [[inferred-labels]])
- Existing PII / compliance / injection packs (HIPAA, GDPR, PCI-DSS, NIST AI RMF, ...)
- `aiqg_*` antipattern packs (context bloat, prompt antipatterns, behavioral signals)

This is the dominant variable cost in the gateway front-half. Sub-millisecond on small payloads with small packs; bounded above by Hyperscan's regex-set behavior on large payloads. Difference from `headers_parsed_at` IS `scan_overhead_ms` — call this out on dashboards.

### policy_resolved_at
Set after the route-matcher engine (`internal/policy/resolver.go`) has selected a policy bundle for this request from URL + source + header + workflow match. Null if the request was rejected at auth or if no route matched (request 404'd before reaching policy resolution).

Resolved via Redis cache (`aiqg:resolve:<tenant_id>:<fingerprint>`, 60s TTL) on hit; Neo4j route-rule enumeration on miss. The miss path is the known latency hazard documented in `reviews/architect-review.md` §4 Risk #4 — the cache-warm-on-publish mitigation is tracked there.

### request_event_emitted_at
Set after `internal/events/aiqg_publisher.go` has produced the `com.tas.aiqg.request.v1` CloudEvent to topic `tas.aiqg.request.v1` and the Kafka producer has returned (with `acks=1` semantics on the current single-broker cluster). Null if event emission was skipped (e.g., non-AIQG-mode) or if the Kafka publish failed (the request still succeeds; the failed publish is fire-and-forget per [aiqg-request-lifecycle](../cross-service/flows/aiqg-request-lifecycle.md) §4 — but the null checkpoint shows up in dashboards as "event-emission gap").

### request_forwarded_at
Set immediately before the gateway writes the first byte to the upstream vendor connection. Anchor for vendor-side timing. Null if the request was rejected, dry-run shed, or routed to a stub.

### dns_resolved_at / tcp_connected_at / tls_handshake_complete_at
Captured by `net/http/httptrace.ClientTrace` callbacks attached to the vendor HTTP client. Null if connection was reused from the pool (no fresh DNS/TCP/TLS). Together they decompose the "network" portion of the request.

### ttfb_at
**Time-to-first-byte.** `GotFirstResponseByte` from `net/http/httptrace.ClientTrace`. This fires when the **HTTP response headers** arrive from the vendor — NOT when the first content chunk arrives. It's typically followed by a small SSE preamble (role/heartbeat chunks with no content) before the first real token appears. Subtracting `request_forwarded_at` gives the **vendor queue / pre-think time**.

`ttfb_at` and `ttft_at` are **distinct on purpose**. Earlier drafts of this design conflated them — see `reviews/architect-review.md` §4 Risk #5. Conflating the two systematically under-reports vendor TTFT by 50–200ms and falsely flatters CLEAR Latency scores.

### ttft_at
**Time-to-first-token.** Set the first time a parsed SSE chunk has a **non-empty content delta** (skips role/heartbeat openers and any other zero-content control frames). Implementation-side: stamped from inside the provider's stream loop via `timing.StampFirstContent(ctx)`, NOT from the `httptrace.ClientTrace.GotFirstResponseByte` hook. The provider examines each `stream.Recv()` chunk and triggers the stamp only on the first chunk where the content field is non-empty:

```go
// inside provider StreamCompletion loop — pseudocode
if !firstContentSeen && chunkHasNonEmptyContent(c) {
    timing.StampFirstContent(ctx)   // no-op when ctx is not AIQG-mode
    firstContentSeen = true
}
```

This is the user-perceived "the model started responding" moment. `ttft_at − ttfb_at` is the SSE preamble cost; usually 30–150ms. `ttft_at − request_forwarded_at` is the vendor's true think time and is the dominant component of perceived latency for most workflows (see spec §2.2 example: "9.4s of 12s is vendor TTFT").

### last_chunk_at
Set by `timing.StampChunk(ctx)` on the final `stream.Recv()` call before the stream closes. `last_chunk_at − ttft_at` is the vendor's generation duration. For non-streaming requests where the full body arrives in one chunk, `last_chunk_at ≈ ttft_at`.

### response_complete_at
Set when the gateway has written the final byte back to the customer client and closed the connection. `response_complete_at − last_chunk_at` is the gateway's egress overhead.

### chunk_count
Total number of SSE chunks observed during the stream. For non-streaming requests this is `1`. Used both to compute average inter-token latency and as a sanity check for the percentile fields (percentiles only meaningful when `chunk_count >= 20`).

### inter_token_latency_p50_ms / _p95_ms / _p99_ms
Per-request percentiles of the inter-chunk delta distribution, computed by `TimingCollector.Snapshot()` at request close. Null when `chunk_count < 20` (insufficient sample). Numeric(7,2) — range 0.00–99999.99 ms.

---

## 4. Validation Rules

### 4.1 Monotonic checkpoint ordering

For any single request, the populated checkpoints must satisfy:

```
request_received_at
  <= auth_validated_at
  <= policy_resolved_at
  <= request_forwarded_at
  <= dns_resolved_at      (when fresh connection)
  <= tcp_connected_at
  <= tls_handshake_complete_at
  <= ttfb_at
  <= ttft_at
  <= last_chunk_at
  <= response_complete_at
```

Null checkpoints are skipped in the chain (a null `policy_resolved_at` means the next non-null checkpoint must still be ≥ `auth_validated_at`).

Violations are **logged at WARN with `event_id`, the violating pair, and the negative delta in microseconds**, then the row is written as-is. The row is **not rejected** — losing observability data is worse than admitting a bad measurement. The aggregator (`tas-spark-jobs/aiqg_aggregator`) filters violators out of percentile windows but counts them in a `monotonicity_violation_rate` metric.

### 4.2 Future-timestamp tolerance

Any timestamp more than 100ms in the future relative to the gateway's wall clock at write time is **flagged but not rejected**. Implementation: emit a `aiqg_clock_skew_total{component=...}` Prometheus counter; SRE alert fires at > 0.1% of requests in a 5m window. Likely root cause: pod clock skew across the cluster — flag for chrony/ntpd investigation.

### 4.3 Gateway overhead SLO

Per [source-spec-v0.2.md §3.3](./source-spec-v0.2.md) the gateway commits to **< 50ms p99 gateway overhead**. The `gateway_overhead_ms` generated column drives the SLO breach query.

**Formula scope (revised 2026-06-01).** `gateway_overhead_ms` now measures the TRUE customer-perceived gateway cost — every µs spent in gateway code that isn't waiting on the vendor. This means it captures the front-half phases (`auth_validated_at` → `headers_parsed_at` → `scan_complete_at` → `policy_resolved_at` → `request_event_emitted_at` → `request_forwarded_at`) plus the back-half egress (`last_chunk_at` → `response_complete_at`). The earlier draft of this field measured only auth+egress and hid scan/classify/policy/emit; that change was a finding in `reviews/architect-review.md` §4 and `reviews/sre-review.md`.

**The 50ms target is intentionally aspirational.** Under the corrected formula, the realistic happy-path cost is 15-40ms (cache-hit-everywhere) and the cache-miss cliff is 70-160ms. Keeping the target at 50ms is a **forcing function** — it tells the team that:
1. Cache-warm-on-issuance for `tas_qg_live_*` tokens (gateway cache miss must become a bug, not a routine path)
2. Cache-warm-on-publish for policy bundles + route rules (same reasoning)
3. Authoring and benchmarking the six new Gatekeeper rule packs against p99 representative payloads
4. Kafka `acks=1` produce on the request-event emission path

…must all land before the gateway is expected to hold the 50ms target. We do NOT redefine the SLO down to match unbacked code. We measure honestly and track breaches.

### 4.4 Component non-negativity

Each derived `_ms` column must be ≥ 0 when both source checkpoints are non-null. Negative values are clamped to 0 in the generated expression (PostgreSQL `GREATEST(expr, 0)`) and the row is logged for the monotonicity counter.

### 4.5 Percentile sanity

`inter_token_latency_p99_ms >= inter_token_latency_p95_ms >= inter_token_latency_p50_ms` when all three are non-null. Asserted by a check constraint at write time.

### 4.6 SLO Breach Tracking

Every request whose `gateway_overhead_ms` exceeds the configured target (`Config.AIQG.GatewayOverheadSLO`, default 50ms) is recorded in three places so SREs can both alert and diagnose:

**1. Prometheus counter.** Emitted by the gateway at the close of every AIQG-mode request that breaches:

```
aiqg_slo_breach_total{
  slo="gateway_overhead",
  dominant_phase="<phase>",      # "auth" | "headers" | "scan" | "policy" | "emit" | "forward_prep" | "egress" | "rejected"
  vendor="<vendor>",             # "openai" | "anthropic" | ...
  account_id="<aiqg_account_id>",
  cache_state="<state>"          # "all_hit" | "token_miss" | "policy_miss" | "both_miss"
}
```

`dominant_phase` is whichever of `gateway_overhead_pre_forward_ms` or `gateway_overhead_egress_ms` (and the sub-phases within pre-forward — `auth`, `headers`, `scan`, `policy`, `emit`, `forward_prep`) contributed the largest absolute ms to the overhead in this request. Computed in-process at emission time. `rejected` is used when `request_forwarded_at` is null.

**2. Structured log line** at WARN level to stdout (picked up by Alloy → Loki):

```json
{
  "level": "warn",
  "service": "tas-llm-router",
  "event": "aiqg_slo_breach",
  "request_id": "...",
  "tenant_id": "...",
  "aiqg_account_id": "...",
  "vendor": "anthropic",
  "endpoint": "/anthropic/v1/messages",
  "gateway_overhead_ms": 87.4,
  "slo_target_ms": 50,
  "dominant_phase": "policy",
  "phase_breakdown_ms": {
    "auth": 1.2, "headers": 0.1, "scan": 4.7, "policy": 64.3,
    "emit": 11.4, "forward_prep": 0.3, "egress": 5.4
  },
  "cache_state": "policy_miss"
}
```

**3. Audit log entry** of type `slo_breach_observed` (per [audit-log-entry](./audit-log-entry.md)) with the same payload as the log line but durable and queryable. Severity = `info` by default; reclassified to `warn` if breach rate per tenant exceeds 1% over 5 minutes.

**Sampling.** Under sustained breach the volume of (1)+(2)+(3) could be high. Sampling is controlled by `Config.AIQG.SLOBreachSampleRate` (default `1.0` — every breach emits all three). Set lower in production hotspots to bound Loki ingest and audit-table growth; the Prometheus counter is always emitted at full rate (cardinality-bounded by labels).

**Standing alert.** Defined in `shared-monitoring/prometheus/alerts/aiqg-slos.yml`:

```yaml
- alert: AIQGGatewayOverheadBreachRateHigh
  expr: |
    (
      sum by (account_id) (rate(aiqg_slo_breach_total{slo="gateway_overhead"}[5m]))
      /
      sum by (account_id) (rate(aiqg_request_total[5m]))
    ) > 0.01
  for: 5m
  labels: { severity: warn }
  annotations:
    summary: "AIQG gateway-overhead breach rate > 1% (account_id={{ $labels.account_id }})"
    runbook: "https://github.com/Tributary-ai-services/tas-aiqg/blob/main/runbooks/gateway-overhead-breach.md"
```

**Forensics dashboard panel.** A Grafana panel surfaces the top-N breaching tenants in the last hour, broken down by `dominant_phase` and `cache_state`. A second panel shows the time-series rate per phase. See `shared-monitoring/grafana/dashboards/aiqg-latency-decomposition.json`.

---

## 5. Relationships

```
                     ┌─────────────────────┐
                     │ EventTimestamps     │
                     │ (JSONB + generated  │
                     │  columns)           │
                     └──────────┬──────────┘
                                │ embedded as `timestamps` column on:
              ┌─────────────────┴─────────────────┐
              │                                   │
              ▼                                   ▼
   ┌─────────────────────┐              ┌─────────────────────┐
   │ [[request-event]]   │              │ [[response-event]]  │
   │ tas.aiqg.request.v1 │              │ tas.aiqg.response.v1│
   └──────────┬──────────┘              └──────────┬──────────┘
              │                                    │
              │ tenant_id, account_id              │
              ▼                                    ▼
   ┌────────────────────────────────────────────────────────┐
   │ [[account]]   (Neo4j AIQGAccount, scoping)             │
   └────────────────────────────────────────────────────────┘
              │
              │ rolled up by tas-spark-jobs/aiqg_aggregator
              ▼
   ┌────────────────────────────────────────────────────────┐
   │ [[aggregated-metrics]]                                 │
   │ 1m / 5m / 1h / 1d continuous aggregates                │
   └────────────────────────────────────────────────────────┘
```

Also referenced by [[response-structure]] via `chunk_count` (which must match the number of structured response chunks recorded on the response side).

---

## 6. Lifecycle & State Machines

The structure has a strictly linear lifecycle — it accumulates during a single request and is immutable thereafter:

```
[ CREATED ]   on inbound HTTP request — TimingCollector attached to context
     │
     │  middlewares + provider stream stamp checkpoints onto the collector
     ▼
[ ACCUMULATING ]   one or more checkpoints set; chunk timestamps streaming in
     │
     │  request closes (success, error, or client disconnect)
     ▼
[ SNAPSHOT ]   TimingCollector.Snapshot() computes percentiles + derived
     │
     │  written into AIQG CloudEvent payload
     │  written into JSONB column on event row
     ▼
[ FROZEN ]   no further mutation; aggregator reads but never writes
```

**No mutation after write.** The aggregator reads; the dashboard reads; nothing in the system ever updates `event_timestamps` rows. This is what makes it safe to drive continuous aggregates from the table.

---

## 7. Examples

### 7.1 Full streaming request (every checkpoint populated)

```json
{
  "event_id": "01HX9C8K3F0Z1QN5Y6W7P8R9V",
  "tenant_id": "1cb7a4e2-...",
  "received_at": "2026-05-31T18:42:01.013482Z",
  "timestamps": {
    "request_received_at":         "2026-05-31T18:42:01.013482Z",
    "auth_validated_at":           "2026-05-31T18:42:01.014119Z",
    "headers_parsed_at":           "2026-05-31T18:42:01.014180Z",
    "scan_complete_at":            "2026-05-31T18:42:01.018940Z",
    "policy_resolved_at":          "2026-05-31T18:42:01.020410Z",
    "request_event_emitted_at":    "2026-05-31T18:42:01.024830Z",
    "request_forwarded_at":        "2026-05-31T18:42:01.025240Z",
    "dns_resolved_at":             "2026-05-31T18:42:01.026802Z",
    "tcp_connected_at":            "2026-05-31T18:42:01.030114Z",
    "tls_handshake_complete_at":   "2026-05-31T18:42:01.051982Z",
    "ttfb_at":                     "2026-05-31T18:42:01.348771Z",
    "ttft_at":                     "2026-05-31T18:42:01.412884Z",
    "last_chunk_at":               "2026-05-31T18:42:04.917330Z",
    "response_complete_at":        "2026-05-31T18:42:04.918002Z",
    "chunk_count":                 312,
    "inter_token_latency_p50_ms":  9.78,
    "inter_token_latency_p95_ms":  17.40,
    "inter_token_latency_p99_ms":  31.16
  },
  "gateway_overhead_ms":             12.43,
  "gateway_overhead_pre_forward_ms": 11.76,
  "gateway_overhead_egress_ms":       0.67,
  "scan_overhead_ms":                 4.76,
  "policy_overhead_ms":               1.47,
  "emit_overhead_ms":                 4.42,
  "end_to_end_ms":                 3904.52,
  "vendor_ttft_ms":                 387.64,
  "vendor_generation_ms":          3504.45,
  "slo_breached":                    false
}
```

Reads as: "3.9s end-to-end. 388ms vendor TTFT (vendor headers landed at +323ms; first real content token at +388ms — the 64ms gap is SSE preamble). 3.5s of generation. Gateway added 12.4ms total — dominated by scan (4.8ms) and event emission (4.4ms). Network was 27ms (DNS+TCP+TLS)." This matches the diagnostic narrative in [source-spec-v0.2.md §2.2](./source-spec-v0.2.md): each component is independently actionable, and the gateway's own contribution is now visible by phase.

### 7.2 Request rejected at policy resolution

A `TAS-Policy: enforce-strict` request that violated bundle constraints — rejected before being forwarded. Every checkpoint downstream of `policy_resolved_at` is null EXCEPT `response_complete_at` (the gateway still emitted a 403 back to the customer).

```json
{
  "timestamps": {
    "request_received_at":      "2026-05-31T18:43:11.001000Z",
    "auth_validated_at":        "2026-05-31T18:43:11.001642Z",
    "headers_parsed_at":        "2026-05-31T18:43:11.001712Z",
    "scan_complete_at":         "2026-05-31T18:43:11.001870Z",
    "policy_resolved_at":       "2026-05-31T18:43:11.002193Z",
    "request_event_emitted_at": null,
    "request_forwarded_at":     null,
    "dns_resolved_at":          null,
    "tcp_connected_at":         null,
    "tls_handshake_complete_at": null,
    "ttfb_at":                  null,
    "ttft_at":                  null,
    "last_chunk_at":             null,
    "response_complete_at":     "2026-05-31T18:43:11.002701Z",
    "chunk_count":              0,
    "inter_token_latency_p50_ms": null,
    "inter_token_latency_p95_ms": null,
    "inter_token_latency_p99_ms": null
  }
}
```

Rejected-path formula applies: `gateway_overhead_ms = response_complete_at − request_received_at = 1.70ms`. `dominant_phase = "rejected"` on the breach counter (though here we're well under the SLO target). `request_event_emitted_at` is null because the request never reached the emission step — the policy resolver rejected it. The audit log records a `policy_blocked_request` entry instead of an `aiqg_request_emitted` entry.

### 7.3 Non-streaming request (single-chunk response)

OpenAI `chat/completions` with `stream=false`. The full body arrives in one HTTP response; `ttft_at` and `last_chunk_at` are effectively equal (they're stamped on the same chunk boundary). Inter-token percentiles are null because there's a sample of size 1.

```json
{
  "timestamps": {
    "request_received_at":         "2026-05-31T18:44:00.000000Z",
    "auth_validated_at":           "2026-05-31T18:44:00.000503Z",
    "headers_parsed_at":           "2026-05-31T18:44:00.000545Z",
    "scan_complete_at":            "2026-05-31T18:44:00.001022Z",
    "policy_resolved_at":          "2026-05-31T18:44:00.001141Z",
    "request_event_emitted_at":    "2026-05-31T18:44:00.001392Z",
    "request_forwarded_at":        "2026-05-31T18:44:00.001442Z",
    "dns_resolved_at":             null,
    "tcp_connected_at":            null,
    "tls_handshake_complete_at":   null,
    "ttfb_at":                     "2026-05-31T18:44:02.184220Z",
    "ttft_at":                     "2026-05-31T18:44:02.184220Z",
    "last_chunk_at":               "2026-05-31T18:44:02.184220Z",
    "response_complete_at":        "2026-05-31T18:44:02.185001Z",
    "chunk_count":                 1,
    "inter_token_latency_p50_ms":  null,
    "inter_token_latency_p95_ms":  null,
    "inter_token_latency_p99_ms":  null
  }
}
```

DNS/TCP/TLS are null because the connection came out of the pool (pooled vendor connection — `httptrace` callbacks fire only on fresh connections). `ttfb_at == ttft_at == last_chunk_at` because the full response arrived in a single chunk; the SSE preamble distinction doesn't apply. `gateway_overhead_ms = 1.44 + 0.78 = 2.22ms`.

### 7.4 SQL: p95 latency decomposition per endpoint over last 24h

Matches the Day-1 Report Screen 5 latency-decomposition section.

```sql
SELECT
  re.endpoint,
  re.vendor,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY et.gateway_overhead_ms)   AS p95_gateway_ms,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY et.network_round_trip_ms) AS p95_network_ms,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY et.vendor_ttft_ms)        AS p95_vendor_ttft_ms,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY et.vendor_generation_ms)  AS p95_vendor_gen_ms,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY et.end_to_end_ms)         AS p95_end_to_end_ms,
  count(*) AS request_count
FROM aiqg.event_timestamps et
JOIN aiqg.request_events  re USING (event_id)
WHERE et.received_at >= NOW() - INTERVAL '24 hours'
  AND et.tenant_id = $1
GROUP BY re.endpoint, re.vendor
ORDER BY p95_end_to_end_ms DESC;
```

### 7.5 SQL: gateway-overhead outliers with phase breakdown for SRE triage

```sql
SELECT
  et.event_id,
  et.received_at,
  et.account_id,
  et.gateway_overhead_ms,
  et.gateway_overhead_pre_forward_ms,
  et.gateway_overhead_egress_ms,
  -- Per-phase sub-costs (pre-forward only; egress is its own column)
  EXTRACT(EPOCH FROM ((et.timestamps->>'auth_validated_at')::timestamptz   - (et.timestamps->>'request_received_at')::timestamptz)) * 1000 AS auth_ms,
  EXTRACT(EPOCH FROM ((et.timestamps->>'headers_parsed_at')::timestamptz   - (et.timestamps->>'auth_validated_at')::timestamptz))    * 1000 AS headers_ms,
  et.scan_overhead_ms                                                                                                                       AS scan_ms,
  et.policy_overhead_ms                                                                                                                     AS policy_ms,
  et.emit_overhead_ms                                                                                                                       AS emit_ms,
  EXTRACT(EPOCH FROM ((et.timestamps->>'request_forwarded_at')::timestamptz - (et.timestamps->>'request_event_emitted_at')::timestamptz)) * 1000 AS forward_prep_ms
FROM aiqg.event_timestamps et
WHERE et.received_at >= NOW() - INTERVAL '1 hour'
  AND et.slo_breached = TRUE    -- uses the partial index
ORDER BY et.gateway_overhead_ms DESC
LIMIT 100;
```

This query is the source of the dashboard's "top breaches in the last hour" panel and is the canonical first-hop diagnostic for the `AIQGGatewayOverheadBreachRateHigh` alert.

### 7.6 SQL: SLO breach rate by tenant + dominant phase over last 24h

Drives the Grafana time-series panel and feeds the runbook decision tree.

```sql
WITH breaches AS (
  SELECT
    et.tenant_id,
    et.account_id,
    time_bucket('5 minutes', et.received_at) AS bucket,
    -- Compute dominant phase per breach
    CASE GREATEST(
        COALESCE(et.scan_overhead_ms,         0),
        COALESCE(et.policy_overhead_ms,       0),
        COALESCE(et.emit_overhead_ms,         0),
        COALESCE(et.gateway_overhead_egress_ms, 0)
      )
      WHEN COALESCE(et.scan_overhead_ms,         0) THEN 'scan'
      WHEN COALESCE(et.policy_overhead_ms,       0) THEN 'policy'
      WHEN COALESCE(et.emit_overhead_ms,         0) THEN 'emit'
      WHEN COALESCE(et.gateway_overhead_egress_ms, 0) THEN 'egress'
    END AS dominant_phase
  FROM aiqg.event_timestamps et
  WHERE et.received_at >= NOW() - INTERVAL '24 hours'
    AND et.slo_breached = TRUE
    AND et.tenant_id = $1
)
SELECT bucket, dominant_phase, count(*) AS breach_count
FROM breaches
GROUP BY bucket, dominant_phase
ORDER BY bucket DESC, breach_count DESC;
```

---

## 8. Cross-Service Integration

- **`tas-llm-router/internal/instrumentation/timing.go`** — defines `TimingCollector`, owns the in-memory state during the request, exposes `Snapshot()` returning the `EventTimestamps` Go struct.
- **`tas-llm-router/internal/instrumentation/httptrace.go`** — wraps the vendor HTTP client with `net/http/httptrace.ClientTrace` to stamp DNS/TCP/TLS/TTFB.
- **`tas-llm-router/internal/providers/{openai,anthropic}/provider.go`** — calls `timing.StampChunk(ctx)` inside the `StreamCompletion()` body (no signature or struct change).
- **`tas-llm-router/internal/events/aiqg_v1.go`** — embeds the snapshot into `com.tas.aiqg.request.v1` / `com.tas.aiqg.response.v1` payloads.
- **Kafka topics `tas.aiqg.request.v1` / `tas.aiqg.response.v1`** — carry the embedded JSONB payload.
- **`tas-spark-jobs/aiqg_aggregator`** — consumes the topics, writes raw rows into `aiqg.event_timestamps`, and feeds continuous aggregates feeding [[aggregated-metrics]].
- **`aiqg-dashboard-be/internal/clients/timescale_client.go`** — reads pre-aggregated latency percentiles for the Day-1 report and the ongoing dashboard.
- **`shared-monitoring/grafana/dashboards/aiqg-latency-decomposition.json`** — visualization sink for the same percentile aggregates.

---

## 9. Performance Considerations

### 9.1 Capture-side overhead

`TimingCollector` is a single struct held in the request `context.Context`, with a mutex-guarded slice of chunk deltas. Per-chunk stamp cost is dominated by a single `time.Now()` call plus an `append()` — measured at < 200ns per stamp on the K3s reference node. For a typical 300-chunk streaming response, total capture overhead is < 60µs — well inside the < 50ms p99 gateway-overhead SLO.

The percentile computation at `Snapshot()` time uses an in-place sort on the chunk-delta slice; for `chunk_count < 1000` this is sub-millisecond. For larger streams the cost is still bounded by `O(n log n)` over a few thousand entries.

### 9.2 Storage-side optimizations

- **TimescaleDB hypertable** partitioned by `received_at` with `chunk_time_interval => INTERVAL '1 day'` — drops old chunks via retention policy without expensive deletes.
- **Generated columns hoisted from JSONB** (`gateway_overhead_ms`, `gateway_overhead_pre_forward_ms`, `gateway_overhead_egress_ms`, `scan_overhead_ms`, `policy_overhead_ms`, `emit_overhead_ms`, `end_to_end_ms`, `vendor_ttft_ms`, `vendor_generation_ms`, `slo_breached`) so SLO breach queries and percentile aggregates never need to parse JSONB at read time. Phase-breakdown columns add ~80 bytes/row uncompressed; TimescaleDB native compression collapses the redundancy within a chunk down to a couple of bytes/row.
- **Composite index** `(tenant_id, received_at DESC, gateway_overhead_ms)` for SLO breach queries (§7.5).
- **Partial index** `(tenant_id, received_at DESC) WHERE slo_breached = TRUE` so breach forensic queries hit only the breach-row subset — critical when breach rate is low (the expected steady state) and most rows are non-breach.
- **Continuous aggregate** computing p50/p95/p99 of each component per workflow per 5m window — this is what feeds the latency-decomposition dashboard panel and the Day-1 report. Continuous aggregate refresh policy: every 1m for the trailing 10m, every 5m for the trailing 1h.

### 9.3 Aggregator throughput

A single Spark executor processes ~20K events/sec writing into the hypertable using `COPY` in 5K-row batches. For the MVP target (≤ 50K events/min/customer per [build-vs-reuse.md §7.3](./build-vs-reuse.md)) a 2-executor configuration has 50× headroom.

---

## 10. Migration Strategies

### 10.1 Schema evolution

Because `timestamps` is JSONB, **adding new checkpoints is non-breaking**. A new field added to the Go `EventTimestamps` struct shows up as a new key in the JSONB; old rows simply lack the key and read as null. Generated columns can be added with `ALTER TABLE ... ADD COLUMN ... GENERATED ALWAYS AS (...) STORED` and TimescaleDB backfills them lazily per chunk.

**Removing a checkpoint** is a breaking change and requires a versioned event type bump (`com.tas.aiqg.request.v2`). Not anticipated for v1 lifetime.

### 10.2 Retention

Per [[account]] retention setting (default 30d, configurable to 7d / 30d / 90d / 1y per account-region settings). Retention is enforced by a TimescaleDB retention policy keyed on `received_at`:

```sql
SELECT add_retention_policy('aiqg.event_timestamps', INTERVAL '30 days');
```

Continuous aggregates retain longer (90d for 5m windows, indefinitely for 1d windows) because they're already aggregated and orders-of-magnitude smaller.

### 10.3 Re-platforming to ClickHouse (deferred)

If a customer crosses the 30K events/min/customer threshold per [build-vs-reuse.md §7.1](./build-vs-reuse.md), the structure ports directly to ClickHouse: `Nested` columns for the checkpoint map and materialized columns for the derived `_ms` values. Spark sink swap; no event-schema change.

---

## 11. Common Patterns

### 11.1 Stamping a checkpoint in code

```go
// inside internal/middleware/aiqg_headers.go
collector := timing.NewCollector()
ctx := timing.WithCollector(r.Context(), collector)
collector.Stamp("request_received_at")
// ... continue with handler
```

```go
// inside internal/security/aiqg_auth.go after successful token resolution
timing.FromContext(ctx).Stamp("auth_validated_at")
```

### 11.2 Reading the snapshot at request close

```go
snap := timing.FromContext(ctx).Snapshot()
event := events.AIQGResponseV1{
    EventID:    eventID,
    TenantID:   tenantID,
    Timestamps: snap,        // embedded into CloudEvent payload
    // ... other fields
}
publisher.Publish(ctx, event)
```

### 11.3 Latency-decomposition waterfall (Day-1 Report)

The Day-1 report renders four stacked bars per request percentile, summing to `end_to_end_ms`:

```
| network 27ms | vendor_ttft 334ms | vendor_generation 3568ms | gateway 1ms |
```

The frontend reads pre-aggregated percentiles from the continuous aggregate via `aiqg-dashboard-be` — never raw rows.

---

## 12. Error Handling

| Failure mode | Behavior |
|---|---|
| `TimingCollector` not attached to context (non-AIQG caller) | All `Stamp()` and `StampChunk()` calls are no-ops. Existing behavior preserved. |
| `httptrace` callback fires on a pooled connection | DNS/TCP/TLS checkpoints remain null. Not an error. |
| Vendor closes the stream before any token arrives | `ttfb_at` set, `ttft_at` null. Row is written; aggregator skips it for inter-token percentiles. |
| Customer client disconnects mid-stream | `last_chunk_at` captured for the last received chunk; `response_complete_at` set at the gateway-side connection close. Both are accurate. |
| Negative delta from clock skew | Generated column clamps to 0; counter `aiqg_monotonicity_violation_total` incremented; row still written. |
| JSONB parse failure on read | Dashboard query returns 500; row remains in table; SRE alert via Loki query for `JSONB parse error` log lines. |
| `Snapshot()` panic (programmer error) | Recovered in the deferred-publish path. Event row is written with a marker `timing_capture_error=true` instead of a partial structure. Counter `aiqg_timing_capture_panics_total` tracks this. |

---

## 13. Testing Strategies

### 13.1 Unit tests (Go)

- `timing.StampChunk` updates the collector slice atomically under concurrent calls (test with `-race`).
- `Snapshot()` returns null percentiles when `chunk_count < 20`.
- `Snapshot()` returns monotonic percentiles (`p99 >= p95 >= p50`) for synthetic distributions.
- No-op behavior when collector is not in context: `timing.FromContext(emptyCtx).Stamp("x")` returns without panic.
- Concurrent stamps from `httptrace` callbacks + provider stream do not race.

### 13.2 Contract tests (covered by [build-vs-reuse.md §10](./build-vs-reuse.md))

- §10.3 — `ChatChunk` JSON shape snapshot must not include any new field; guards against accidental struct extension.
- §10.4 — `com.tas.aiqg.response.v1` schema includes a `timestamps` object; baseline schema fixture committed.

### 13.3 Integration tests

- Run a synthetic OpenAI streaming server returning N chunks with controlled delays; assert percentiles in the snapshot match expected values within ±0.5ms.
- Forward a request through the dev gateway, capture the emitted Kafka event, validate against the JSON schema.
- Verify generated columns are computed correctly in TimescaleDB across all four example rows in §7.

### 13.4 Soak / load tests

- 1K req/s for 1h against a stub upstream; assert p99 `gateway_overhead_ms` < 50ms (the SLO).
- Verify Spark aggregator keeps up: lag on `tas.aiqg.response.v1` consumer group < 10s p95.

---

## 14. Related Documentation

- [[build-vs-reuse]] — §1.2 non-breaking-change constraint; §2.3 timing capture pattern; §4.5 SLO alert wiring; §7.1 hot store decision; §10 wire-compat checklist.
- [[source-spec-v0.2]] — §2.2 Latency dimension; §3.3 streaming-native architecture; §3.7 per-request capture; §4 Day-1 report Screen 5.
- [[request-event]] — parent envelope embedding `timestamps`.
- [[response-event]] — parent envelope embedding `timestamps`.
- [[response-structure]] — `chunk_count` must match between the timing structure and the response structure (cross-check).
- [[aggregated-metrics]] — downstream rollups consuming the percentile fields.
- [[account]] — `tenant_id` / `account_id` scoping; retention policy source.
- [Argo Workflows reference](../argo-workflows-reference.md) — workflow steps that traverse the gateway carry through the same timing capture path.
- [TAS LLM Router request format](../tas-llm-router/request-format.md) — annotative note that AIQG-mode captures the full payload separately; the `ChatRequest` schema itself is unchanged.

---

## Changelog

| Version | Date | Author | Notes |
|---|---|---|---|
| v1.0.0 | 2026-05-31 | TAS Platform | Initial spec draft. |
| v1.1.0 | 2026-06-01 | TAS Platform | Apply architect/SRE review findings on `gateway_overhead_ms`. Redefine the formula to `(request_forwarded_at − request_received_at) + (response_complete_at − last_chunk_at)` (forwarded path) or `response_complete_at − request_received_at` (rejected path) — was previously only auth+egress. Add three new checkpoint fields (`headers_parsed_at`, `scan_complete_at`, `request_event_emitted_at`) and five new phase-breakdown generated columns (`gateway_overhead_pre_forward_ms`, `gateway_overhead_egress_ms`, `scan_overhead_ms`, `policy_overhead_ms`, `emit_overhead_ms`). Add `slo_breached` generated boolean + partial index. Tighten `ttft_at` definition to distinguish from `ttfb_at`. Add §4.6 SLO Breach Tracking (Prometheus counter, structured log, audit-log entry, sampling, standing alert, forensics panel). Keep 50ms p99 target unchanged as a forcing function. |
