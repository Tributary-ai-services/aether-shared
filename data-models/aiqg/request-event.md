# AIQG Request Event

**Service:** tas-llm-router (AIQG mode) → Kafka → aiqg_aggregator (Spark) → TimescaleDB
**Storage:** Dual-face — CloudEvent on Kafka topic `tas.aiqg.request.v1`, row in TimescaleDB hypertable `aiqg.request_events`
**Document version:** v1.0.0
**Last updated:** 2026-05-31
**Status:** Initial spec draft

---

## 1. Overview

The **AIQG Request Event** is the core per-request capture envelope emitted by `tas-llm-router` when it operates in AI Quality Gateway (AIQG) mode. It is the foundational record that pairs with a corresponding [[response-event]] to form the unit of measurement for every CLEAR scoring computation, every dashboard query, and every audit trail entry in the AIQG product.

This event answers the question: *"What did the customer ask, on whose behalf, against which vendor, under which policy, at exactly what moments in time?"* It is emitted at request close (after the response stream completes or the request fails), not at request open — emission timing is described in §5.

### 1.1 Two storage faces

The same logical event exists in two stores, by design:

| Face | Where | Schema | Purpose |
|---|---|---|---|
| **CloudEvent on Kafka** | topic `tas.aiqg.request.v1` | CloudEvents 1.0 envelope wrapping the data payload below | Wire format; the gateway emits this at request close. Subscribed by `aiqg_aggregator` (Spark) and any future downstream consumers. |
| **TimescaleDB hypertable** | `aiqg.request_events` in the `aiqg` database on `postgres-shared` | The data payload flattened into columns | Analytics store. Populated by `aiqg_aggregator`. Backs dashboard queries, ad-hoc analysis, and customer exports. |

The CloudEvent's `data` payload and the TimescaleDB row are the **same field set** modulo encoding differences (JSON in Kafka, native Postgres types in TimescaleDB). The field reference in §2 is normative for both.

### 1.2 Why a new event type (not an extension)

Per the [non-breaking-change constraint](./build-vs-reuse.md#12-non-breaking-change-constraint), existing CloudEvent types `com.tas.activity.llm.request` and `com.tas.activity.llm.response` on topic `tas.activity.llm` MUST NOT change shape. Existing internal-routing callers (tas-agent-builder, aether-be, audimodal, llm-invocation) depend on those events as they are today. The AIQG product therefore ships an entirely new event type on an entirely new topic. Both event streams flow concurrently from the same `tas-llm-router` binary; AIQG events are emitted only when the request enters AIQG mode (see [build-vs-reuse §2.2](./build-vs-reuse.md#22-32--path-a-authentication)).

### 1.3 Role in the AIQG pipeline

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │  customer HTTP request → tas-llm-router (AIQG mode)                  │
  │                            │                                         │
  │                            ├─ middleware: parse TAS-* headers,       │
  │                            │   build aiqg request context            │
  │                            │                                         │
  │                            ├─ forward to vendor (Path A pass-through)│
  │                            │                                         │
  │                            ├─ instrumentation: chunk timing,         │
  │                            │   token accounting, scanning            │
  │                            │                                         │
  │                            └─ at request close:                      │
  │                                  emit com.tas.aiqg.request.v1   ─────┼──→ Kafka tas.aiqg.request.v1
  │                                  emit com.tas.aiqg.response.v1  ─────┼──→ Kafka tas.aiqg.response.v1
  └──────────────────────────────────────────────────────────────────────┘
                                                                          │
                                                                          ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │  aiqg_aggregator (Spark Structured Streaming)                        │
  │   - read tas.aiqg.request.v1 + tas.aiqg.response.v1                  │
  │   - join on request_event_id                                         │
  │   - upsert into aiqg.request_events + aiqg.response_events           │
  │   - roll into aiqg.metrics_1m / aiqg.metrics_1h continuous aggregates│
  └──────────────────────────────────────────────────────────────────────┘
```

### 1.4 Scope of this document

This document specifies the request event only. The paired response event is documented in [[response-event]]. Embedded substructures (token accounting, timestamps, request structure summary, inferred labels, tag set, policy bundle reference, audit log references) are each documented as their own model and cross-referenced from §2.

---

## 2. Schema Definition

### 2.1 Field reference

| Field | Type | Required | Nullable | Indexed | Notes |
|---|---|---|---|---|---|
| `request_event_id` | UUID | yes | no | PK | Generated by gateway at request open. Stable across emission, storage, and dashboard URLs. |
| `tenant_id` | UUID | yes | no | yes | Resolved from the `tas_qg_live_*` token via [[account]]. Hot index for tenant-scoped dashboard queries. |
| `aiqg_account_id` | UUID | yes | no | yes | FK → [[account]]`.aiqg_account_id`. 1:1 with `tenant_id` but stored explicitly for join convenience. |
| `received_at` | timestamptz | yes | no | partition column | Hypertable partition column. Gateway-wall-clock time at first byte of inbound request. |
| `vendor` | enum | yes | no | yes | One of `openai`, `anthropic`. Reserved: `bedrock`, `vertex`, `azure_openai`. See §4.2. |
| `endpoint` | text | yes | no | yes | Inbound request path, normalized. E.g. `/openai/v1/chat/completions`, `/anthropic/v1/messages`. |
| `model` | text | yes | no | yes | Vendor model identifier as it appeared in the request body. E.g. `gpt-4o-mini`, `claude-3-7-sonnet-20250219`. |
| `source_app` | text | no | yes | yes | Derived from the TAS-Auth token's `source_app` claim **or** the customer-supplied `TAS-Source-App` header (header wins if both present). Free-form, max 128 chars. |
| `source_ip` | inet | yes | no | no | Client IP after trusted-proxy header normalization. May be IPv4 or IPv6. |
| `region` | text | yes | no | yes | Region where this request was processed. One of `us-east`, `us-west`, `eu`. Set by deployment, not request. |
| `tas_auth_token_id` | UUID | yes | no | yes | The customer-issued `tas_qg_live_*` token used. FK → AIQG token table (managed by `aiqg-dashboard-be`). |
| `request_id` | text | no | yes | yes | Idempotency / correlation key. Set to vendor request id (e.g. OpenAI's `x-request-id` response header) when known. Falls back to a gateway-generated nonce when the vendor does not return one. |
| `client_request_id` | text | no | yes | yes | Customer-supplied `X-Request-ID` header value, captured verbatim. |
| `correlated_response_event_id` | UUID | no | yes | yes | FK → [[response-event]]`.response_event_id`. NULL until the paired response event is observed. Populated either by the gateway at emission (if it emits both events atomically) or by Spark at join time. |
| `streaming` | bool | yes | no | no | True if the request was `stream: true` (OpenAI) or `stream: true` / SSE (Anthropic). |
| `is_aiqg_mode` | bool | yes | no | no | Always `true` for events in this stream. Field exists so the row is self-describing and so future schema extensions that add non-AIQG events to this topic are detectable. |
| `request_structure` | jsonb | yes | no | no | Embedded summary of inbound payload — see [[request-structure]]. Token-bounded summary; not the raw payload. |
| `event_timestamps` | jsonb | yes | no | no | Embedded timing block — see [[event-timestamps]]. Contains DNS/TLS/TCP/forwarded/TTFB/TTFT/last-chunk/complete timestamps in microsecond precision. |
| `inferred_labels` | jsonb | yes | no | no | Embedded inference results — see [[inferred-labels]]. Workflow type, retry-of-previous, abandonment, hedge flags. |
| `tag_set` | jsonb | yes | no | no | Gatekeeper scan output — see [[tag-set]]. Quality, policy, NIST AI RMF tags applied to this request. |
| `policy_bundle_id` | UUID | no | yes | yes | FK → [[policy-bundle]]`.policy_bundle_id`. The applied bundle at the moment the request was processed. NULL means no bundle was matched and pass-through default applied. |
| `policy_bundle_version` | int | no | yes | no | Version of the policy bundle that was applied. Stored alongside `policy_bundle_id` because bundles are versioned objects. |
| `audit_log_refs` | uuid[] | no | yes | no | Array of FK → [[audit-log-entry]]`.audit_log_entry_id`. References to immutable audit-log rows generated for this request (one per policy action taken). |
| `dry_run` | bool | yes | no | yes | True if the request carried `TAS-Dry-Run: 1`. Dry-run requests still emit events; the policy decisions in `tag_set` and `audit_log_refs` describe what *would have* been done. |
| `trace_returned` | bool | yes | no | no | True if `TAS-Trace: 1` was set and the gateway returned the captured event to the caller as a response header (`TAS-Trace-Result`, base64). |
| `scoring_version` | text | yes | no | no | The `pkg/clear` version string in effect at request close. Pins this event to a specific scoring code path (see [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go)). |
| `gateway_version` | text | yes | no | no | The `tas-llm-router` build version. For drift attribution. |
| `lifecycle_state` | enum | yes | no | yes | One of `received`, `validated`, `policy_resolved`, `forwarded`, `paired_with_response`, `archived`. See §5. |

### 2.2 Vendor enum extension policy

The `vendor` column is a Postgres enum. Adding a new vendor (e.g. `bedrock`) requires:

1. `ALTER TYPE aiqg.vendor ADD VALUE 'bedrock';` (non-locking in Postgres 12+).
2. Update the CloudEvent schema JSON to include the new value in the `enum` list.
3. Bump the AIQG event type minor version is **not** required — the field is open-ended by design.

Reserved values that are not yet implemented: `bedrock`, `vertex`, `azure_openai`. Adding them now would prevent typos in early-adopter code but they will only start appearing in events once the corresponding provider lands in `tas-llm-router`.

### 2.3 Embedded substructures

Five fields hold structured data. Each is documented as its own model so the request event itself stays a relatively flat envelope:

| Field | Model | Why embedded vs. separate table |
|---|---|---|
| `request_structure` | [[request-structure]] | Always 1:1 with the request. Querying request structure independently of the request is meaningless. Embedded as jsonb for storage locality. |
| `event_timestamps` | [[event-timestamps]] | Always 1:1; tightly bound to the request lifecycle. Embedded for the same reason. |
| `inferred_labels` | [[inferred-labels]] | Always 1:1; computed at request close. Embedded. |
| `tag_set` | [[tag-set]] | Always 1:1 (the tag set for *this* request). Variable cardinality of tags within the set is handled inside the jsonb document. Embedded. |
| `audit_log_refs` | [[audit-log-entry]] | 1:N — one request can produce multiple audit log entries. Stored as a UUID array; audit log rows live in their own immutable table. |

### 2.4 What is **not** captured

Explicitly excluded from this event:

- **Raw `Authorization` header.** Never stored. Per [build-vs-reuse §7.3](./build-vs-reuse.md#73-path-a-enforcement--decided-strict), the gateway treats the customer's vendor key as transit-only data.
- **Raw request body.** The full payload may be retained only if [[account]]`.payload_retention_mode` opts in — and when retained it is stored in MinIO under a separate object path keyed by `request_event_id`, not in TimescaleDB. The `request_structure` field is always a **summary**, never the raw payload.
- **Raw response body.** Lives on [[response-event]], not here, and follows the same retention-mode rules.
- **PII inside `source_app`.** The header/claim path is documented to platform teams as identifier-only. PII detection in `source_app` raises a Gatekeeper finding (see [[tag-set]]).

---

## 3. Relationships

```
                                       ┌────────────────────────────┐
                                       │  account                   │
                                       │  (aether-be / Neo4j)       │
                                       │  - aiqg_account_id (PK)    │
                                       │  - tenant_id (UQ)          │
                                       │  - payload_retention_mode  │
                                       │  - region                  │
                                       └─────────────┬──────────────┘
                                                     │ 1
                                                     │
                                                     │ N
                            ┌────────────────────────▼────────────────────────┐
                            │  request_event                                  │
                            │  - request_event_id (PK)                        │
                            │  - tenant_id (FK → account.tenant_id)           │
                            │  - aiqg_account_id (FK → account)               │
                            │  - tas_auth_token_id (FK → aiqg_token)          │
                            │  - policy_bundle_id (FK → policy_bundle)        │
                            │  - correlated_response_event_id (FK 0..1)       │
                            │  - audit_log_refs (FK array)                    │
                            └─────────┬───────────────────┬───────────────────┘
                                      │                   │
                                      │ 0..1 paired       │ N referenced
                                      │ at close          │
                                      ▼                   ▼
                       ┌──────────────────────┐  ┌────────────────────────┐
                       │  response_event      │  │  audit_log_entry       │
                       │  - response_event_id │  │  - audit_log_entry_id  │
                       │  - request_event_id  │  │  - request_event_id    │
                       │    (FK back)         │  │  - action_taken        │
                       │  - token_accounting  │  │  - bundle_version      │
                       │  - response_structure│  │  - immutable           │
                       │  - clear_scores      │  └────────────────────────┘
                       └──────────────────────┘
```

**Relationship summary:**

| From | Cardinality | To | Enforced |
|---|---|---|---|
| `request_event` | N → 1 | [[account]] | FK on `aiqg_account_id`; also denormalized `tenant_id` for query speed |
| `request_event` | 1 → 0..1 | [[response-event]] | `correlated_response_event_id` populated post-hoc; NULL for failed-before-response requests |
| `request_event` | N → 0..1 | [[policy-bundle]] | `policy_bundle_id` + `policy_bundle_version` (composite) |
| `request_event` | N → 1 | AIQG token (managed in `aiqg-dashboard-be`) | `tas_auth_token_id` |
| `request_event` | 1 → N | [[audit-log-entry]] | `audit_log_refs` array; audit rows also carry `request_event_id` for reverse lookup |
| [[aggregated-metrics]] | rolled from N | `request_event` | Spark continuous aggregates over (`tenant_id`, `received_at` bucket, `endpoint`, `model`, [[workflow-classification]].`workflow_type`) |

**Cross-store boundary:** the `account` entity lives in Neo4j (managed by `aether-be` + `aiqg-dashboard-be`); the `request_event` lives in TimescaleDB. There is no foreign-key constraint across that boundary — application-level integrity is maintained by the gateway resolving `aiqg_account_id` at request open and refusing to forward if resolution fails (returns 401).

---

## 4. Validation Rules

### 4.1 Gateway-side validation (at emission)

The gateway MUST emit only events that satisfy:

1. `request_event_id` is a v4 UUID, generated by the gateway, unique across the gateway's lifetime.
2. `tenant_id` and `aiqg_account_id` are non-null; the request was not forwarded if these could not be resolved.
3. `received_at` is in UTC and within ±5 seconds of the gateway host clock. (Drift larger than this indicates clock skew; the gateway logs a warning and uses NTP-corrected time.)
4. `vendor` is one of the implemented values; `endpoint` matches a known route table entry; `model` is a non-empty string.
5. `source_ip` parses as a valid IPv4 or IPv6 address.
6. `region` matches the deployment region; cross-region forwarding is forbidden by [[account]]`.region` enforcement.
7. `streaming` reflects the actual request shape, not the request body's claim. (If the customer requested non-streaming but the gateway streamed for any reason, this field reports the actual behavior.)
8. `event_timestamps` contains at minimum `received_at`, `forwarded_at`, `complete_at`. Vendor timing fields (`ttfb_at`, `ttft_at`, `last_chunk_at`) are populated when applicable; failed-before-forward requests omit them.
9. `lifecycle_state` is one of the values in §5 and follows the allowed transitions.
10. `scoring_version` and `gateway_version` are non-empty version strings.

### 4.2 Aggregator-side validation (at insert into TimescaleDB)

The Spark aggregator MUST reject (dead-letter) events that:

1. Fail schema validation against the committed CloudEvent JSON schema for `com.tas.aiqg.request.v1`.
2. Carry a `received_at` more than 7 days old (these are replayed-from-history events that should not enter the live hypertable; they go to a separate `aiqg.request_events_replay` table).
3. Carry a duplicate `request_event_id` already present in the hypertable for the same `received_at` chunk. Duplicates are silently dropped after logging.
4. Reference an `aiqg_account_id` that does not exist in the account directory. (This should be impossible if the gateway is healthy; treated as a poison-message indicator.)

Dead-lettered events go to topic `tas.aiqg.request.v1.dlq` and are alerted on by Prometheus.

### 4.3 Application-side validation (at dashboard query time)

The `aiqg-dashboard-be` enforces tenant-scoping on every query:

```sql
SELECT … FROM aiqg.request_events
 WHERE tenant_id = $tenant_id_from_jwt
   AND received_at BETWEEN $from AND $to;
```

Any query that does not include `tenant_id =` in the WHERE clause is rejected at the repository layer before hitting Postgres.

---

## 5. Lifecycle & State Transitions

A request event progresses through six lifecycle states. The state is stored explicitly in the `lifecycle_state` column so that partially-completed requests (e.g. crashed mid-forward) can be reconciled.

```
   received ──► validated ──► policy_resolved ──► forwarded ──► paired_with_response ──► archived
       │              │                  │              │
       │              │                  │              └──► (timeout / vendor error)
       │              │                  │                     paired_with_response (response_event records error)
       │              │                  │
       │              │                  └──► (policy block)
       │              │                         paired_with_response (response_event records 4xx/5xx synthetic)
       │              │
       │              └──► (validation failure)
       │                     paired_with_response (response_event records 4xx synthetic)
       │
       └──► (auth failure)
              event NOT emitted; counted only in metrics
```

### 5.1 State definitions

| State | When set | Set by |
|---|---|---|
| `received` | First byte of inbound request parsed; `request_event_id` allocated. | Gateway middleware. |
| `validated` | TAS-* headers parsed, TAS-Auth resolved to an account, request body schema-checked. | Auth + header middleware. |
| `policy_resolved` | Matching route rule + policy bundle resolved; pre-flight Gatekeeper scan complete; dry-run check evaluated. | Policy resolver. |
| `forwarded` | Vendor connection established (DNS/TLS/TCP done); HTTP request sent upstream. | Provider layer. |
| `paired_with_response` | Either the corresponding response event has been emitted (success path) or a synthetic response event recording the failure has been emitted. | Provider layer (success) or error handler (failure). |
| `archived` | Retention window elapsed; event is moved to compressed Timescale chunks (≥7 days old). | TimescaleDB compression policy, not application code. |

### 5.2 Allowed transitions

Only forward transitions are allowed. The state monotonically increases. A request that fails at any state still emits an event with `lifecycle_state = paired_with_response` after a synthetic failure-response event is emitted — there is no `failed` terminal state because the existence of a paired response event (carrying the error) makes the failure observable.

### 5.3 Emission timing

The CloudEvent is emitted to Kafka **once**, at the moment the request reaches `paired_with_response`. This means:

- Successful requests emit after the response stream completes.
- Vendor errors emit after the gateway has decided to give up retrying.
- Policy-blocked requests emit after the block decision is logged.
- Pre-validation auth failures (no valid TAS-Auth) do **not** emit an event — they are counted in Prometheus auth-failure metrics only, because there is no resolved tenant to attribute the event to.

This single-emission rule simplifies the Spark join: every `request_event` will eventually have exactly one `response_event` (real or synthetic). No `correlated_response_event_id IS NULL` rows should persist beyond the gateway's max request timeout.

### 5.4 Retention and archival

- **Hot:** raw events live in TimescaleDB hypertable chunks for 7 days uncompressed.
- **Warm:** chunks older than 7 days are compressed in place via Timescale's native columnar compression (typically 10–20× reduction).
- **Cold:** chunks older than [[account]]`.retention_days` (default 90 days) are dropped via Timescale retention policy. Customers on enterprise plans can extend retention to 365 days.
- **Customer export:** at any time before archival, customers can export their slice via the dashboard's "Export" action, which streams a Parquet file to MinIO and emails a signed download link.

---

## 6. Examples

### 6.1 CloudEvent envelope (Kafka wire format)

A successful streaming chat completion request:

```json
{
  "specversion": "1.0",
  "id": "evt_01J5N3K7Q9X2YZ4A8B6C2D0E1F",
  "source": "/tas-llm-router/aiqg",
  "type": "com.tas.aiqg.request.v1",
  "time": "2026-05-31T14:22:08.412731Z",
  "datacontenttype": "application/json",
  "subject": "request_event_id:7c9e6b2a-5d1f-4e3a-9b8c-2a1f4d3e6c7b",
  "data": {
    "request_event_id": "7c9e6b2a-5d1f-4e3a-9b8c-2a1f4d3e6c7b",
    "tenant_id": "a1b2c3d4-e5f6-4789-9abc-0123456789ab",
    "aiqg_account_id": "f0e1d2c3-b4a5-4968-8576-1f2e3d4c5b6a",
    "received_at": "2026-05-31T14:22:08.412731Z",
    "vendor": "openai",
    "endpoint": "/openai/v1/chat/completions",
    "model": "gpt-4o-mini",
    "source_app": "acme-customer-support-bot",
    "source_ip": "203.0.113.42",
    "region": "us-east",
    "tas_auth_token_id": "11111111-2222-3333-4444-555555555555",
    "request_id": "req_abc123def456",
    "client_request_id": "acme-correlation-9281",
    "correlated_response_event_id": "8d0f7c3b-6e2a-4f4b-ac9d-3b2a5e4f7d8c",
    "streaming": true,
    "is_aiqg_mode": true,
    "request_structure": {
      "summary_ref": "see request-structure model",
      "system_prompt_tokens": 412,
      "user_message_tokens": 87,
      "history_turn_count": 4,
      "history_tokens": 1842,
      "tool_definitions_count": 0,
      "context_block_count": 3,
      "context_block_tokens": 7240,
      "total_input_tokens": 9581
    },
    "event_timestamps": {
      "summary_ref": "see event-timestamps model",
      "received_at": "2026-05-31T14:22:08.412731Z",
      "dns_done_at": "2026-05-31T14:22:08.418102Z",
      "tcp_connect_at": "2026-05-31T14:22:08.421440Z",
      "tls_handshake_done_at": "2026-05-31T14:22:08.443201Z",
      "forwarded_at": "2026-05-31T14:22:08.449880Z",
      "ttfb_at": "2026-05-31T14:22:08.812003Z",
      "ttft_at": "2026-05-31T14:22:08.831112Z",
      "last_chunk_at": "2026-05-31T14:22:11.204550Z",
      "complete_at": "2026-05-31T14:22:11.207801Z"
    },
    "inferred_labels": {
      "summary_ref": "see inferred-labels model",
      "workflow_type": "rag",
      "retry_of_previous": false,
      "abandonment_signal": false,
      "hedge_phrase_present": null
    },
    "tag_set": {
      "summary_ref": "see tag-set model",
      "quality_tags": ["context_bloat_suspected"],
      "policy_tags": [],
      "nist_tags": ["privacy_enhanced:clean"],
      "anti_pattern_tags": []
    },
    "policy_bundle_id": "22222222-3333-4444-5555-666666666666",
    "policy_bundle_version": 3,
    "audit_log_refs": [],
    "dry_run": false,
    "trace_returned": false,
    "scoring_version": "clear-1.0.0",
    "gateway_version": "tas-llm-router-2.4.0+aiqg",
    "lifecycle_state": "paired_with_response"
  }
}
```

### 6.2 TimescaleDB hypertable DDL

```sql
-- Database: aiqg (separate database on postgres-shared, not in tas_shared)

CREATE SCHEMA IF NOT EXISTS aiqg;

CREATE TYPE aiqg.vendor AS ENUM (
  'openai',
  'anthropic'
  -- 'bedrock', 'vertex', 'azure_openai' added via ALTER TYPE when implemented
);

CREATE TYPE aiqg.lifecycle_state AS ENUM (
  'received',
  'validated',
  'policy_resolved',
  'forwarded',
  'paired_with_response',
  'archived'
);

CREATE TABLE aiqg.request_events (
  request_event_id              UUID         NOT NULL,
  tenant_id                     UUID         NOT NULL,
  aiqg_account_id               UUID         NOT NULL,
  received_at                   TIMESTAMPTZ  NOT NULL,
  vendor                        aiqg.vendor  NOT NULL,
  endpoint                      TEXT         NOT NULL,
  model                         TEXT         NOT NULL,
  source_app                    TEXT,
  source_ip                     INET         NOT NULL,
  region                        TEXT         NOT NULL,
  tas_auth_token_id             UUID         NOT NULL,
  request_id                    TEXT,
  client_request_id             TEXT,
  correlated_response_event_id  UUID,
  streaming                     BOOLEAN      NOT NULL,
  is_aiqg_mode                  BOOLEAN      NOT NULL DEFAULT TRUE,
  request_structure             JSONB        NOT NULL,
  event_timestamps              JSONB        NOT NULL,
  inferred_labels               JSONB        NOT NULL,
  tag_set                       JSONB        NOT NULL,
  policy_bundle_id              UUID,
  policy_bundle_version         INTEGER,
  audit_log_refs                UUID[],
  dry_run                       BOOLEAN      NOT NULL DEFAULT FALSE,
  trace_returned                BOOLEAN      NOT NULL DEFAULT FALSE,
  scoring_version               TEXT         NOT NULL,
  gateway_version               TEXT         NOT NULL,
  lifecycle_state               aiqg.lifecycle_state NOT NULL,
  PRIMARY KEY (request_event_id, received_at)
);

-- Convert to hypertable, chunk every hour
SELECT create_hypertable(
  'aiqg.request_events',
  'received_at',
  chunk_time_interval => INTERVAL '1 hour',
  if_not_exists       => TRUE
);

-- Indexes for the dominant access patterns
CREATE INDEX idx_req_tenant_time
  ON aiqg.request_events (tenant_id, received_at DESC);

CREATE INDEX idx_req_event_id
  ON aiqg.request_events (request_event_id);

CREATE INDEX idx_req_account_endpoint_time
  ON aiqg.request_events (aiqg_account_id, endpoint, received_at DESC);

CREATE INDEX idx_req_correlated_response
  ON aiqg.request_events (correlated_response_event_id)
  WHERE correlated_response_event_id IS NOT NULL;

-- Compression policy: compress chunks older than 7 days
ALTER TABLE aiqg.request_events SET (
  timescaledb.compress,
  timescaledb.compress_orderby   = 'received_at DESC',
  timescaledb.compress_segmentby = 'tenant_id, vendor, endpoint'
);

SELECT add_compression_policy('aiqg.request_events', INTERVAL '7 days');

-- Retention policy: drop chunks older than 90 days (default; overridden per-account)
SELECT add_retention_policy('aiqg.request_events', INTERVAL '90 days');
```

### 6.3 Sample INSERT (from Spark aggregator)

```sql
INSERT INTO aiqg.request_events (
  request_event_id, tenant_id, aiqg_account_id, received_at,
  vendor, endpoint, model, source_app, source_ip, region,
  tas_auth_token_id, request_id, client_request_id,
  correlated_response_event_id, streaming, is_aiqg_mode,
  request_structure, event_timestamps, inferred_labels, tag_set,
  policy_bundle_id, policy_bundle_version, audit_log_refs,
  dry_run, trace_returned, scoring_version, gateway_version,
  lifecycle_state
) VALUES (
  '7c9e6b2a-5d1f-4e3a-9b8c-2a1f4d3e6c7b',
  'a1b2c3d4-e5f6-4789-9abc-0123456789ab',
  'f0e1d2c3-b4a5-4968-8576-1f2e3d4c5b6a',
  '2026-05-31T14:22:08.412731Z',
  'openai',
  '/openai/v1/chat/completions',
  'gpt-4o-mini',
  'acme-customer-support-bot',
  '203.0.113.42'::inet,
  'us-east',
  '11111111-2222-3333-4444-555555555555',
  'req_abc123def456',
  'acme-correlation-9281',
  '8d0f7c3b-6e2a-4f4b-ac9d-3b2a5e4f7d8c',
  TRUE,
  TRUE,
  '{"system_prompt_tokens":412,"user_message_tokens":87,"history_turn_count":4,"history_tokens":1842,"tool_definitions_count":0,"context_block_count":3,"context_block_tokens":7240,"total_input_tokens":9581}'::jsonb,
  '{"received_at":"2026-05-31T14:22:08.412731Z","forwarded_at":"2026-05-31T14:22:08.449880Z","ttfb_at":"2026-05-31T14:22:08.812003Z","ttft_at":"2026-05-31T14:22:08.831112Z","last_chunk_at":"2026-05-31T14:22:11.204550Z","complete_at":"2026-05-31T14:22:11.207801Z"}'::jsonb,
  '{"workflow_type":"rag","retry_of_previous":false,"abandonment_signal":false}'::jsonb,
  '{"quality_tags":["context_bloat_suspected"],"policy_tags":[],"nist_tags":["privacy_enhanced:clean"]}'::jsonb,
  '22222222-3333-4444-5555-666666666666',
  3,
  ARRAY[]::uuid[],
  FALSE,
  FALSE,
  'clear-1.0.0',
  'tas-llm-router-2.4.0+aiqg',
  'paired_with_response'
);
```

### 6.4 Dashboard query — last 50 requests for tenant X

```sql
SELECT
  request_event_id,
  received_at,
  vendor,
  endpoint,
  model,
  source_app,
  streaming,
  (inferred_labels ->> 'workflow_type')                          AS workflow_type,
  (event_timestamps ->> 'complete_at')::timestamptz
    - received_at                                                AS total_duration,
  (request_structure ->> 'total_input_tokens')::int              AS input_tokens,
  lifecycle_state,
  correlated_response_event_id IS NOT NULL                       AS response_paired
FROM aiqg.request_events
WHERE tenant_id   = 'a1b2c3d4-e5f6-4789-9abc-0123456789ab'
  AND received_at >= NOW() - INTERVAL '24 hours'
ORDER BY received_at DESC
LIMIT 50;
```

This query is index-served by `idx_req_tenant_time` and typically returns in <50ms even when the hypertable contains hundreds of millions of rows, because Timescale prunes to the most recent chunks.

### 6.5 Cypher — N/A

This model lives in TimescaleDB, not Neo4j. The only Neo4j-side touchpoint is the [[account]] node referenced by `aiqg_account_id`; that lookup happens in `aiqg-dashboard-be` and is not a Cypher concern for this table.

---

## 7. Cross-Service References

| Service | Role | Touchpoint |
|---|---|---|
| `tas-llm-router` (AIQG mode) | Producer | Emits `com.tas.aiqg.request.v1` at request close. |
| Kafka (`kafka-shared.tas-shared.svc`) | Transport | Topic `tas.aiqg.request.v1`, snappy compression, retention 7 days for replay/debug. |
| `tas-spark-jobs/aiqg_aggregator` | Consumer | Reads request + response topics, joins on `request_event_id`, upserts into TimescaleDB, rolls into [[aggregated-metrics]]. |
| `postgres-shared` (TimescaleDB) | Store | Database `aiqg`, schema `aiqg`, hypertable `request_events`. |
| `aiqg-dashboard-be` | Reader | Tenant-scoped queries from the dashboard and report generator; never writes to this table. |
| `aether-be` / Neo4j | Account directory | Resolves `tenant_id` ↔ `aiqg_account_id` via [[account]] nodes. Lookup happens at gateway request open, not at read time. |
| `Gatekeeper` | Tagger | Scan output is embedded in `tag_set` at gateway-side request close. No separate table write. |
| `Loki` (via Alloy) | Logs | Operational logs about emission (failures, drift) are scraped from gateway stdout; not the events themselves. |
| `Prometheus` | Metrics | Counters: `aiqg_request_events_emitted_total{vendor,endpoint,lifecycle_state}`, `aiqg_request_events_dropped_total{reason}`. Histogram: `aiqg_request_event_emission_duration_seconds`. |

---

## 8. Tenant & Space Isolation

### 8.1 Mapping

AIQG aligns with the TAS space-based multi-tenancy model:

- TAS **Space** → AIQG **account** (1:1)
- Space's `tenant_id` is denormalized onto every `request_event` row as `tenant_id`
- `aiqg_account_id` is the explicit AIQG-side identifier; it is unique per account and is what the dashboard's JWT carries

See [[account]] for the full mapping and the rule that adding `aiqg_account_id` to the Space node is **additive only** (does not change Space's existing schema).

### 8.2 Isolation enforcement

Three layers enforce tenant isolation:

1. **At ingest:** the gateway resolves the `TAS-Auth` token to exactly one `aiqg_account_id`. If resolution fails, the request is rejected with 401 and no event is emitted. If the token is valid but the account is suspended, the request is rejected with 403.
2. **At storage:** every row carries `tenant_id` and `aiqg_account_id` as NOT NULL columns. Index `idx_req_tenant_time` makes tenant-scoped queries cheap.
3. **At read:** the `aiqg-dashboard-be` repository layer rejects any query that does not contain a `tenant_id =` predicate matching the JWT's claim. The `iam` package enforces this at the API boundary.

### 8.3 Cross-tenant queries

Cross-tenant queries are forbidden from the dashboard path. The only legitimate cross-tenant access is:

- Internal operator dashboards in Grafana (read TimescaleDB as the `aiqg_ops_reader` role; queries are not tenant-scoped but are visible only inside the SRE Grafana folder).
- Spark aggregation jobs (read all tenants by design to compute global aggregates; write only into pre-aggregated tables that are themselves tenant-scoped on read).
- Customer support escalation (uses a special "impersonation" mode in `aiqg-dashboard-be` that issues a short-lived JWT for a specific tenant; the impersonation is itself audited).

### 8.4 Region isolation

`region` is set by the deployment processing the request. Cross-region forwarding is blocked at the ingress level — a customer with `account.region = 'eu'` is routed to the EU gateway deployment and that deployment's gateway will refuse to forward if `region` does not match. This means `region` on the row is always equal to the deployment region and is a fast filter for residency-scoped queries.

---

## 9. Performance Considerations

### 9.1 Hypertable chunking

- **Chunk interval:** 1 hour. Rationale: most dashboard queries fall inside a few-hour window; 1h chunks let the planner prune aggressively. With ~10K events/hour/customer at MVP scale, a chunk holds ~10K rows, well below Timescale's recommended chunk-size guidance.
- **Chunk-time-interval scaling:** if event volume grows past ~100K events/hour/customer sustained, revisit to 15-minute chunks. The trigger condition is documented in [build-vs-reuse §7.1](./build-vs-reuse.md#71-hot-analytics-store--decided-timescaledb).

### 9.2 Compression

- **Trigger:** chunks ≥7 days old are compressed in place by Timescale's background worker.
- **Segmentby:** `(tenant_id, vendor, endpoint)` — most queries filter on tenant; this keeps tenant data physically co-located within compressed chunks.
- **Orderby:** `received_at DESC` — keeps recent rows clustered.
- **Expected ratio:** 10–20× size reduction based on Timescale's own benchmarks for similar workloads (highly repetitive enum + UUID columns, jsonb compression).

### 9.3 Indexing strategy

| Index | Cardinality | Use case |
|---|---|---|
| PK `(request_event_id, received_at)` | high | Point lookups (e.g. drill-in from dashboard) |
| `idx_req_tenant_time` | high | "Last N events for tenant X" — the dominant dashboard query |
| `idx_req_event_id` | high | Point lookups across all chunks (for support / replay) |
| `idx_req_account_endpoint_time` | medium | Per-endpoint analytics for a single account |
| `idx_req_correlated_response` partial | low | Reverse lookup from a response event back to its request (debugging only) |

JSONB fields are **not** indexed individually for MVP. Common queries on jsonb internals (e.g. "all RAG requests for tenant X") are served from [[aggregated-metrics]], which materializes the relevant slices. If ad-hoc jsonb queries become hot, GIN indexes on specific paths can be added incrementally.

### 9.4 Write path

The Spark aggregator writes in micro-batches (every 5 seconds, ~50K rows per batch at peak). Writes use `INSERT … ON CONFLICT DO NOTHING` keyed on `(request_event_id, received_at)` to make replay idempotent.

Throughput target: 100K events/min sustained per Postgres replica before write latency degrades. Current single-instance `postgres-shared` comfortably handles 30K events/min based on existing `tas-spark-jobs/events_aggregator` load.

### 9.5 Read path

| Query pattern | Latency target (p95) | Notes |
|---|---|---|
| "Show me the last 50 events for tenant X" | <50ms | Index-served, chunk-pruned |
| "Show me all events in the last hour for tenant X" | <100ms | One chunk |
| "Show me events in the last 7 days for tenant X" | <500ms | Up to 168 chunks; consider serving from aggregates if exceeded |
| "Show me a specific event by ID" | <20ms | PK lookup |
| Trigger to revisit: dashboard p95 > 500ms | — | Per [build-vs-reuse §7.1](./build-vs-reuse.md#71-hot-analytics-store--decided-timescaledb), this triggers a ClickHouse evaluation |

### 9.6 Kafka topic sizing

- **Partitions:** 16 (matches the `kafka-shared` cluster default for high-volume topics; allows up to 16 parallel Spark executors)
- **Retention:** 7 days. The events are durable in TimescaleDB long-term; Kafka retention is for replay/debug only.
- **Compression:** snappy (matches existing `tas.activity.*` topics)
- **Cleanup policy:** delete (not compact — these are append-only events)

---

## 10. Security & Compliance

### 10.1 Sensitive-field handling

| Field | Sensitivity | Handling |
|---|---|---|
| `Authorization` header | maximum | **Never stored.** Per [build-vs-reuse §7.3](./build-vs-reuse.md#73-path-a-enforcement--decided-strict), Path A requires the customer's vendor key to transit only. No field on this event exposes it. |
| raw request body | high | Never stored on this row. Optionally persisted in MinIO under separate retention controlled by [[account]]`.payload_retention_mode`. |
| `source_ip` | medium (PII in some jurisdictions) | Stored. Customers in EU residency can configure source IP masking (last octet zeroed) at the gateway via [[account]] settings. Phase 2. |
| `client_request_id` | low-medium | Stored verbatim. Customer-supplied; customer's responsibility not to put PII here. Gatekeeper PII scan runs on this field anyway. |
| `source_app` | low | Stored verbatim. Documented as identifier-only. PII detection by Gatekeeper. |
| `request_structure` | low | Token counts and structural metadata only; never raw text. |
| `inferred_labels`, `tag_set` | low | Aggregated booleans and string enums; never raw text. |

### 10.2 Compliance frameworks

| Framework | Treatment |
|---|---|
| **GDPR** | Data minimization: only structural / metric fields by default. Customer data subjects can be exported and erased via the dashboard's "Data export" and "Data deletion" actions. Right-to-be-forgotten triggers a tombstone insert + retroactive deletion of matching rows across hot and warm chunks. |
| **HIPAA** | When a customer enables payload retention with PHI data, the gateway routes payload storage through Databunker for tokenization before MinIO write. The request event itself never contains PHI fields. |
| **PCI-DSS** | Cardholder data in customer payloads is detected by the existing `pci.yaml` Gatekeeper rule pack and (optionally) blocked or redacted by policy. The request event records the detection in `tag_set` but never the cardholder data itself. |
| **SOC 2** | The audit trail (this event + paired response + [[audit-log-entry]] rows) provides the immutable record SOC 2 requires for AI-system operations. |
| **NIST AI RMF** | The `tag_set` field maps directly to the seven trustworthiness characteristics. See [[tag-set]]. |
| **EU AI Act** | Same mapping path as NIST AI RMF; the `tag_set` carries EU AI Act risk-category tags when relevant. |

### 10.3 Audit trail

Every policy action taken against a request produces an immutable row in [[audit-log-entry]], whose ID is captured in `audit_log_refs` on this event. The audit table is append-only and is not subject to the standard retention policy — it lives for the full account retention window plus 30 days, ensuring an audit record outlives the underlying event by enough margin to handle dispute windows.

### 10.4 Access control

- **Gateway → Kafka:** the gateway's service account has `produce` on `tas.aiqg.*` topics only.
- **Spark → Kafka:** consume-only on `tas.aiqg.request.v1` + `tas.aiqg.response.v1`; produce on `tas.aiqg.findings.v1` (a derived stream).
- **Spark → TimescaleDB:** write role `aiqg_ingest`; can INSERT but not DELETE on `aiqg.request_events` (deletes go through the Timescale retention worker).
- **`aiqg-dashboard-be` → TimescaleDB:** read role `aiqg_reader_tenant_scoped`; the row-level security policy enforces `tenant_id = current_setting('app.current_tenant')::uuid` so even a buggy query cannot leak cross-tenant data.
- **SRE operators:** read role `aiqg_ops_reader`; cross-tenant read in support of incident response; queries are logged.

### 10.5 Threat model highlights

| Threat | Mitigation |
|---|---|
| Token replay | `tas_qg_live_*` tokens are rotateable; each row's `tas_auth_token_id` makes "what was used when" auditable; revoked tokens are rejected at the gateway. |
| Cross-tenant query injection | Postgres RLS + application-layer enforcement; integration tests assert isolation. |
| Kafka consumer leak | Topic ACLs restrict consume to specific service accounts; no public consumer groups. |
| Replay of historical events | Aggregator rejects events with `received_at` >7 days old (see §4.2). |
| Gateway clock skew | NTP enforced on gateway hosts; events with timestamp drift >5s are logged and corrected to host clock; large drift triggers a Prometheus alert. |

---

## 11. Migration History

| Version | Date | Change | Author |
|---|---|---|---|
| v1.0.0 | 2026-05-31 | Initial spec draft. New table, new CloudEvent type, new Kafka topic. Wholly additive — no prior schema exists. | TAS Platform |

### 11.1 Forward compatibility

Future schema changes to `aiqg.request_events` follow these rules to remain backward-compatible with already-emitted events:

- **Adding columns:** allowed; new columns must be nullable or have a default. Aggregator + dashboard tolerate missing fields in older CloudEvent payloads.
- **Adding vendor enum values:** allowed via `ALTER TYPE ADD VALUE`.
- **Adding jsonb subfields inside `request_structure` / `event_timestamps` / `inferred_labels` / `tag_set`:** allowed at any time; readers tolerate missing keys.
- **Removing columns:** **not allowed** without a major version bump on the CloudEvent type (`com.tas.aiqg.request.v2`). The old type must keep producing events for a deprecation window of at least 90 days.
- **Renaming columns / changing types:** treated as remove+add; requires a major version bump.

The `scoring_version` field allows the [[response-event]] CLEAR scores to evolve independently of this schema; events from an older `scoring_version` remain valid rows.

---

## 12. Known Issues & Limitations

### 12.1 Clock skew between gateway pods

If multiple gateway pods serve the same customer simultaneously, their wall clocks must be within 50ms for chunked timing to be coherent. NTP keeps replicas within ~10ms in practice on the K3s host, but if `chrony` drift exceeds 50ms a Prometheus alert fires. This is documented and tolerated; the impact is that latency-decomposition timing for a single request may show small inconsistencies, never that two requests collide on `request_event_id`.

### 12.2 No partial-emission semantics

A gateway crash between request open and response close means no event is emitted at all. The request is observable only in Loki logs and Prometheus counters. There is no "received but never paired" row in TimescaleDB. This is a deliberate trade-off: avoiding partial rows simplifies the Spark join and dashboard queries.

### 12.3 `source_ip` accuracy depends on trust chain

The gateway uses `X-Forwarded-For` parsing with a configured trusted-proxy list (NGINX ingress + any in-front load balancer). Mis-configuration produces incorrect `source_ip` values. The deployment manifest pins the trusted-proxy CIDR; changes require a config update.

### 12.4 jsonb field schemas are documented elsewhere

`request_structure`, `event_timestamps`, `inferred_labels`, and `tag_set` are jsonb columns. Their internal schemas live in [[request-structure]], [[event-timestamps]], [[inferred-labels]], and [[tag-set]] respectively. The column type itself does not enforce the schema; the aggregator validates against committed JSON schemas before insert. A schema mismatch dead-letters the event.

### 12.5 `correlated_response_event_id` populated post-hoc

Most events arrive at TimescaleDB with `correlated_response_event_id` already filled in by the gateway (since the gateway emits both events at request close). However, in rare cases (out-of-order Kafka delivery, partial-failure replay), the request event lands first and the response event lands later. The aggregator's micro-batch logic does an `UPDATE … SET correlated_response_event_id` to fill in the FK on second arrival. The window between insert and update is bounded by the micro-batch interval (~5 seconds at p99).

### 12.6 Embedding policy bundle version, not full bundle

We store `policy_bundle_id` + `policy_bundle_version` but not the full bundle definition. To reconstruct the exact policy applied to a historical request, the bundle definition must still exist in [[policy-bundle]] storage. Bundle versions are never deleted (only deactivated), so this is reliable, but it does mean the event row is not fully self-describing for compliance audits — it relies on the policy-bundle table being retained.

### 12.7 No row-level encryption

Postgres TDE at the volume level is in place via the K3s storage class. Per-column or per-row encryption is **not** applied — performance cost is significant and the threat model (tenant-scoped RLS + Postgres ACL) is judged sufficient. Customers with stricter requirements use payload-retention-disabled mode, which ensures no sensitive customer data lands in TimescaleDB at all.

---

## 13. Related Documentation

### 13.1 AIQG models cross-referenced from this document

- [[account]] — the AIQG account that owns this event; defines retention, region, quotas.
- [[response-event]] — the paired response event for every successful request.
- [[token-accounting]] — input/output/cached/tool token breakdown; lives on the response event but counted-against-budget here.
- [[event-timestamps]] — schema of the `event_timestamps` jsonb field.
- [[request-structure]] — schema of the `request_structure` jsonb field.
- [[response-structure]] — paired response-side structure (on the response event).
- [[inferred-labels]] — schema of the `inferred_labels` jsonb field.
- [[tag-set]] — schema of the `tag_set` jsonb field.
- [[policy-bundle]] — the named, versioned policy collection referenced by `policy_bundle_id`.
- [[audit-log-entry]] — immutable audit rows referenced by `audit_log_refs`.
- [[aggregated-metrics]] — Spark-computed rollups over this table.
- [[workflow-classification]] — taxonomy of `inferred_labels.workflow_type` values.

### 13.2 Architectural background

- [build-vs-reuse.md](./build-vs-reuse.md) — overall AIQG build/reuse plan; §1.2 non-breaking constraint; §2.3 chunk timing; §2.7 event-emission plan; §3 topology; §7 decisions.
- [source-spec-v0.2.md](./source-spec-v0.2.md) — AIQG v0.2 spec; §3.7 "What the Gateway Captures Per Request" maps directly to the field set here.

### 13.3 Cross-service references

- `aether-shared/data-models/cross-service/diagrams/platform-erd.md` — platform-wide ERD; AIQG entities will be added in a follow-up PR.
- `aether-shared/data-models/cross-service/mappings/id-mapping-chain.md` — Keycloak → Aether → AudiModal → DeepLake chain; AIQG provisioning chain is an additive section per [build-vs-reuse §4.6](./build-vs-reuse.md#46-update-existing-claudemd-files).
- `aether-shared/data-models/argo-events-reference.md` — for any future Argo workflow that consumes `tas.aiqg.request.v1` events as a trigger.

### 13.4 Operational references

- `tas-spark-jobs/aiqg_aggregator/` — the Spark job that reads this topic and writes this table (planned).
- `shared-monitoring/grafana/dashboards/aiqg-clear-composite.json` — primary dashboard backed by aggregates over this table (planned).
- `shared-monitoring/prometheus/alerts/aiqg-slos.yml` — alert rules including DLQ depth on `tas.aiqg.request.v1.dlq` (planned).

---

## 14. Changelog

| Version | Date | Author | Notes |
|---|---|---|---|
| v1.0.0 | 2026-05-31 | TAS Platform | Initial spec draft. Defines the dual-face (CloudEvent + TimescaleDB) request envelope, full field reference, lifecycle states, Kafka and Timescale wire formats, examples, and the non-breaking-change guarantees against existing `com.tas.activity.llm.*` events. |
