# AIQG Request Structure

---

**Metadata**

```yaml
service: tas-llm-router (AIQG extension; observational capture only)
model: RequestStructure
database: PostgreSQL + TimescaleDB (JSONB column on aiqg_requests hypertable); also embedded in CloudEvent com.tas.aiqg.request.v1 payload
version: 1.0.0
last_updated: 2026-05-31
status: planned (new observational sidecar; non-breaking — no change to tas-llm-router ChatRequest)
spec_refs: source-spec-v0.2.md §2.6 (Input Quality leading indicators), §3.7 (per-request capture fields)
plan_ref: build-vs-reuse.md §1.2 (non-breaking-change constraint), §2.3, §2.7, §2.12
```

---

## 1. Overview

### Purpose
The `request_structure` JSONB blob is the **structural snapshot of an inbound request** taken by the AIQG-mode gateway at request receipt. It is the canonical input-side input to the CLEAR scoring engine (Cost decomposition, Efficacy proxies, Assurance signals) and the source of every "leading indicator" called out in spec §2.6. It captures *shape, not content* by default — token and character counts, hashes, structural flags — so that the highest-value diagnostic signal can be persisted at every traffic volume without retaining raw payload.

### Critical Non-Modification Constraint
**This model does not extend, replace, or modify `tas-llm-router/internal/types.ChatRequest`.** Per build-vs-reuse §1.2 (the AIQG non-breaking-change hard rule), the existing `ChatRequest` Go struct, its JSON marshaling, and every method that consumes or produces it remain byte-identical to today. `RequestStructure` is **observational data captured BY the gateway ABOUT a `ChatRequest`** — a sidecar record published on a new CloudEvent type and persisted to a new TimescaleDB table. Existing callers (`tas-agent-builder`, `aether-be`, `audimodal`, `llm-invocation`) never see this struct, never reference it, and need no changes.

### Ownership
- **Producer**: `tas-llm-router` AIQG-mode middleware (only when the per-request AIQG context is active; non-AIQG traffic emits nothing)
- **Persisted by**: `tas-spark-jobs/aiqg_aggregator` (lands the CloudEvent payload into the `aiqg_requests` hypertable on `postgres-shared`)
- **Read-only consumers**: `aiqg-dashboard-be` (Day-1 report assembly, "Where cost is being destroyed" section, per-workflow analytics), `pkg/clear/` scorers (read structural counts to compute CNA / cost decomposition), Grafana (cost-destruction and input-quality dashboards)

### Lifecycle Summary
- **Static structural fields** (`*_present`, `*_chars`, `*_tokens`, `tool_names`, `tool_definition_count`, `context_block_count`, request-time params) are populated synchronously at request receipt, before forwarding upstream.
- **Input-quality computed signals** (`context_utilization_ratio`, `chunk_integrity_score`, `context_staleness_signal`, `context_contradiction_count`, `prompt_antipattern_tags`, `tool_definition_quality_score`) are populated at response close — they require correlation with the response content and are written back into the same record (see Lifecycle §6).
- **Payload-derived hashes** (`system_prompt_hash`, `context_blocks_summary`) are populated only when `[[account]].payload_retention_mode` permits (see Security §10).

### Key Characteristics
- **Observational only** — no mutation of the upstream request shape
- **Counts over content** — character + estimated token counts are the default unit of capture; raw text is referenced by hash where permitted, never embedded
- **Two-phase write** — structural fields at request receipt, input-quality signals at response close
- **Vendor-aware tokenization** — uses tiktoken (OpenAI) / anthropic-tokenizer (Anthropic) when known, falls back to a char/4 estimate
- **Heuristic context-block detection** — best-effort detection of RAG-style retrieved blocks; customers can override workflow inference via the `TAS-Workflow` header

---

## 2. Schema Definition

### Storage
- **Database**: PostgreSQL + TimescaleDB on `postgres-shared` (database `aiqg`)
- **Table**: `aiqg_requests` (hypertable, time partitioned on `received_at` — see [[request-event]])
- **Column**: `request_structure JSONB NOT NULL`
- **Also in**: CloudEvent payload `com.tas.aiqg.request.v1`, under the top-level key `request_structure`
- **Migration impact**: additive — new column on a new hypertable; no existing schema affected

### Top-Level Shape
```json
{
  "system_prompt_present": true,
  "system_prompt_chars": 1842,
  "system_prompt_tokens": 461,
  "system_prompt_hash": "9f2c4ab1e0d7b3a5",
  "user_message_chars": 312,
  "user_message_tokens": 78,
  "conversation_history_turns": 4,
  "conversation_history_tokens": 1024,
  "tool_definition_count": 5,
  "tool_definition_tokens": 612,
  "tool_names": ["search_docs", "fetch_url", "run_sql", "send_email", "create_ticket"],
  "context_block_count": 8,
  "context_block_total_tokens": 18204,
  "context_blocks_summary": null,
  "response_format_requested": "json_schema",
  "json_schema_present": true,
  "max_tokens": 4096,
  "temperature": 0.2,
  "top_p": null,
  "frequency_penalty": null,
  "presence_penalty": null,
  "stop_sequences_count": 0,
  "streaming_requested": true,
  "tool_choice": "auto",
  "context_utilization_ratio": 0.27,
  "chunk_integrity_score": 0.71,
  "context_staleness_signal": "mild",
  "context_contradiction_count": 1,
  "prompt_antipattern_tags": ["conflicting_instructions", "missing_escape_condition"],
  "tool_definition_quality_score": 0.62
}
```

### Fields — Structural (captured at request receipt)

#### System Prompt
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `system_prompt_present` | bool | Yes | — | True if the request contains any `role:system` message. |
| `system_prompt_chars` | int | Yes | `0` | Character count of concatenated system prompt content. Cheaper to compute than tokenization; used as a fast sanity check. |
| `system_prompt_tokens` | int | Yes | `0` | Estimated token count for the system prompt. Vendor-tokenizer when known; otherwise char/4. |
| `system_prompt_hash` | string (16 hex chars) | No | `null` | First 16 hex chars of `sha256(normalized_system_prompt)`. Used for dedup and churn detection across a tenant's traffic. Populated only when `[[account]].payload_retention_mode != off`. |

#### User Message
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `user_message_chars` | int | Yes | `0` | Character count of the final `role:user` message. |
| `user_message_tokens` | int | Yes | `0` | Estimated tokens for the final `role:user` message. |

#### Conversation History
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `conversation_history_turns` | int | Yes | `0` | Number of prior message turns (assistant + user pairs prior to the current user turn). Tool messages count as half-turns; rounded down. |
| `conversation_history_tokens` | int | Yes | `0` | Estimated cumulative tokens across all prior turns. |

#### Tool Definitions
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `tool_definition_count` | int | Yes | `0` | Number of tools declared in the OpenAI `tools[]` / Anthropic `tools[]` field. |
| `tool_definition_tokens` | int | Yes | `0` | Estimated tokens for the full serialized tool schemas. |
| `tool_names` | string[] | Yes | `[]` | Names of declared tools (`tools[].function.name` for OpenAI; `tools[].name` for Anthropic). Names only — not full JSONSchema — to keep cardinality bounded and avoid leaking customer schema design. Useful for workflow classification + diagnostics. |

#### Context Blocks (Heuristic RAG Detection)
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `context_block_count` | int | Yes | `0` | Number of RAG-style retrieved context blocks detected. Heuristic: triple-backtick delimited, XML tags like `<context>...</context>`, or custom markers (`---DOCUMENT---`, `### Source`). |
| `context_block_total_tokens` | int | Yes | `0` | Estimated cumulative tokens across detected context blocks. |
| `context_blocks_summary` | JSON array \| null | No | `null` | Per-block summary `{tokens: int, head_hash: string, tail_hash: string}` (first/last 80 chars hashed). Populated only on the 5–10% sampled retention case (spec §3.8) AND only when `[[account]].payload_retention_mode = sampled` or `full`. Otherwise `null`. |

#### Response-Format Request
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `response_format_requested` | enum (`text`, `json_object`, `json_schema`, `null`) | Yes | `text` | Mirrors OpenAI `response_format.type` / Anthropic structured-output toggle. `null` when unset. |
| `json_schema_present` | bool | Yes | `false` | True when `response_format = json_schema` and a non-empty `schema` object was supplied. |

#### Request-Time Sampling Params
| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `max_tokens` | int \| null | No | `null` | As requested by the client. `null` if unset. |
| `temperature` | numeric \| null | No | `null` | Request-time value; `null` if unset (provider default applies upstream). |
| `top_p` | numeric \| null | No | `null` | Request-time value. |
| `frequency_penalty` | numeric \| null | No | `null` | Request-time value (OpenAI). |
| `presence_penalty` | numeric \| null | No | `null` | Request-time value (OpenAI). |
| `stop_sequences_count` | int \| null | No | `null` | Length of `stop` array; `null` when unset. Values not retained — only the count. |
| `streaming_requested` | bool | Yes | `false` | True when `stream: true` was set on the inbound request. |
| `tool_choice` | string \| null | No | `null` | Lowercase enum: `auto`, `none`, `required`, or a specific tool name. `null` when unset. |

### Fields — Input-Quality Computed Signals (populated at response close, per spec §2.6)

These are the **leading indicators that feed Cost / Efficacy / Assurance scoring**. They are not available at request receipt because most require correlation with the response content. They are written back into the same record once the response stream closes.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `context_utilization_ratio` | numeric(3,2) \| null | No | `null` | Fraction of `context_block_total_tokens` attributable to output (spec §2.6 "Context utilization ratio"). Computed at response close via output-to-context attribution sampling. `null` when `context_block_count = 0` or not yet computed. Range `[0.00, 1.00]`. |
| `chunk_integrity_score` | numeric(3,2) \| null | No | `null` | Heuristic `[0.00, 1.00]` based on chunk boundary signals: sentence-completeness, orphan-fragment ratio, balanced delimiters. Computed by `aiqg_context_antipatterns.yaml` rule pack. `null` when `context_block_count = 0`. |
| `context_staleness_signal` | enum (`none`, `mild`, `strong`) \| null | No | `null` | Based on detected date markers in context vs. temporal markers in the query (spec §2.6 "Context staleness"). `null` when no temporal markers detected. |
| `context_contradiction_count` | int \| null | No | `null` | Number of contradictions flagged by the content scanner across detected context blocks (spec §2.6 "Context contradiction rate"). `null` when `context_block_count < 2`. |
| `prompt_antipattern_tags` | string[] | Yes | `[]` | Output of the `aiqg_prompt_antipatterns.yaml` rule pack: `conflicting_instructions`, `missing_escape_condition`, `injection_vulnerable`, `vague_directive`, etc. Empty array when no anti-patterns detected. |
| `tool_definition_quality_score` | numeric(3,2) \| null | No | `null` | Heuristic `[0.00, 1.00]` for agentic workflows: schema completeness × disambiguation × description-quality (spec §2.6 "Tool definition quality"). `null` when `tool_definition_count = 0`. |

### Generated Columns / Computed Helpers
On the `aiqg_requests` table:
```sql
total_input_tokens INT GENERATED ALWAYS AS (
  COALESCE((request_structure->>'system_prompt_tokens')::int, 0)
  + COALESCE((request_structure->>'user_message_tokens')::int, 0)
  + COALESCE((request_structure->>'conversation_history_tokens')::int, 0)
  + COALESCE((request_structure->>'tool_definition_tokens')::int, 0)
  + COALESCE((request_structure->>'context_block_total_tokens')::int, 0)
) STORED;
```
Drives the headline "Direct payload waste" cost-destruction row on the Day-1 report.

---

## 3. Relationships

Conceptual references (this is a JSONB sidecar, not a graph node — relationships are by foreign key or by `tenant_id` join):

- **Embedded in** `[[request-event]]` — `request_structure` is a column on the `aiqg_requests` hypertable; the row's primary key (`request_id`) is the join.
- **Paired with** `[[response-structure]]` — output side of the same `request_id`. The pair is what `pkg/clear/` reads to compute CLEAR scores.
- **Classified by** `[[workflow-classification]]` — the workflow classifier reads structural fields (`tool_definition_count`, `context_block_count`, `conversation_history_turns`, `user_message_tokens`) to assign the six-type workflow taxonomy.
- **Tagged by** `[[tag-set]]` — Gatekeeper-style tags from the AIQG rule packs (`aiqg_context_antipatterns.yaml`, `aiqg_prompt_antipatterns.yaml`) are stamped onto the same request event and reference `request_structure` field values in their tag metadata.
- **Inferred against** `[[inferred-labels]]` — `retry_of_previous`, `abandonment`, and `hedge` labels combine structural signals with response-side observation.
- **Closed by** `[[response-event]]` — the response close handler is what populates `context_utilization_ratio` back into this struct.
- **Bound to** `[[account]]` (via `tenant_id` on the parent `aiqg_requests` row) — controls `payload_retention_mode`, which governs whether `system_prompt_hash` and `context_blocks_summary` are populated.

---

## 4. Validation Rules

### Synchronous (at request receipt)
1. All counts must be `>= 0`. Negative values reject the event (logged, dropped, alert fired).
2. Sanity check: `*_tokens >= floor(*_chars / 4)` for all paired char/token fields. A token count below the char/4 floor indicates a tokenizer bug and is logged at WARN; the record is still persisted with a `validation_warning` tag.
3. `tool_definition_count == len(tool_names)`. Mismatch rejects the event.
4. `system_prompt_hash` is `null` OR exactly 16 lowercase hex characters.
5. `response_format_requested` is one of the enum values or `null`.
6. `tool_choice`, when non-null, is one of `auto`, `none`, `required`, or matches a value in `tool_names`.
7. `temperature`, `top_p`, `frequency_penalty`, `presence_penalty` ranges, when set, follow vendor norms (e.g., temperature `[0.0, 2.0]`). Out-of-range values are passed through (vendor will reject) but tagged `param_out_of_vendor_range`.
8. `max_tokens`, when set, `> 0` and `<= 200000` (sanity ceiling).
9. `streaming_requested` must equal the `stream` field of the inbound JSON payload.

### Two-Phase (at response close)
10. `context_utilization_ratio` in `[0.00, 1.00]` when non-null. `null` is permitted (and required when `context_block_count = 0`).
11. `chunk_integrity_score` in `[0.00, 1.00]` when non-null.
12. `context_staleness_signal` is one of `none`, `mild`, `strong`, or `null`.
13. `context_contradiction_count >= 0` when non-null.
14. `tool_definition_quality_score` in `[0.00, 1.00]` when non-null; `null` required when `tool_definition_count = 0`.
15. `prompt_antipattern_tags` values must come from the union of identifiers declared in `aiqg_prompt_antipatterns.yaml`. Unknown tags are dropped at the aggregator with a WARN log.

### Hash / Retention Invariants
16. `system_prompt_hash` is populated only when `[[account]].payload_retention_mode IN ('sampled','full')`. When `payload_retention_mode = off`, this field MUST be `null`.
17. `context_blocks_summary` is populated only when `payload_retention_mode IN ('sampled','full')` AND the request is in the sampling stratum. Otherwise `null`.

---

## 5. Lifecycle & State Machines

```
[client request received by tas-llm-router AIQG-mode middleware]
                  │
                  ▼
   ┌──────────────────────────────────────┐
   │ Phase 1 — Synchronous capture        │
   │ (executes in <1ms; no LLM call)      │
   │                                      │
   │  - count chars / estimate tokens     │
   │  - detect context blocks (heuristic) │
   │  - extract tool_names                │
   │  - hash system prompt (if permitted) │
   │  - read request-time params          │
   │  - run aiqg_prompt_antipatterns.yaml │
   │    via Gatekeeper Hyperscan engine   │
   │    (zero added latency)              │
   └──────────────┬───────────────────────┘
                  │
                  ▼
   ┌──────────────────────────────────────┐
   │ Request forwarded upstream to vendor │
   │ (provider-side; AIQG out of band)    │
   └──────────────┬───────────────────────┘
                  │
                  ▼ (response stream completes)
                  │
   ┌──────────────────────────────────────┐
   │ Phase 2 — Response-close enrichment  │
   │                                      │
   │  - context_utilization_ratio         │
   │    (output→context attribution)      │
   │  - chunk_integrity_score             │
   │  - context_staleness_signal          │
   │  - context_contradiction_count       │
   │  - tool_definition_quality_score     │
   │                                      │
   │ Sampled scoring (5–10%) per spec §3.8│
   └──────────────┬───────────────────────┘
                  │
                  ▼
   ┌──────────────────────────────────────┐
   │ Single CloudEvent emitted on         │
   │  com.tas.aiqg.request.v1             │
   │ (Kafka topic tas.aiqg.request.v1)    │
   └──────────────┬───────────────────────┘
                  │
                  ▼
   tas-spark-jobs/aiqg_aggregator lands
   the row into aiqg_requests hypertable
```

The `request_structure` JSON is **complete** by the time the CloudEvent is emitted; the aggregator never updates a row in place. Late-arriving input-quality signals from out-of-band batch scoring (Phase 3 enrichment) are written to a separate `aiqg_request_enrichment` table keyed by `request_id`, preserving append-only semantics on the hypertable.

---

## 6. Examples

### Example A — RAG workflow (the prototypical cost-destruction case)
```json
{
  "system_prompt_present": true,
  "system_prompt_chars": 4210,
  "system_prompt_tokens": 1052,
  "system_prompt_hash": "3b9c1a8e0d2f4671",
  "user_message_chars": 96,
  "user_message_tokens": 24,
  "conversation_history_turns": 0,
  "conversation_history_tokens": 0,
  "tool_definition_count": 0,
  "tool_definition_tokens": 0,
  "tool_names": [],
  "context_block_count": 14,
  "context_block_total_tokens": 18204,
  "context_blocks_summary": null,
  "response_format_requested": "text",
  "json_schema_present": false,
  "max_tokens": 1024,
  "temperature": 0.0,
  "top_p": null,
  "frequency_penalty": null,
  "presence_penalty": null,
  "stop_sequences_count": 0,
  "streaming_requested": true,
  "tool_choice": null,

  "context_utilization_ratio": 0.27,
  "chunk_integrity_score": 0.62,
  "context_staleness_signal": "mild",
  "context_contradiction_count": 2,
  "prompt_antipattern_tags": ["vague_directive"],
  "tool_definition_quality_score": null
}
```
Diagnostic story: 18K context tokens injected; only 27% utilized; 2 contradictions; chunk fragmentation. Lands in the Day-1 report's "Where cost is being destroyed" section as **direct payload waste** with the customer's "RAG context bloat" call-out.

### Example B — Pure chat (no context, no tools)
```json
{
  "system_prompt_present": true,
  "system_prompt_chars": 320,
  "system_prompt_tokens": 80,
  "system_prompt_hash": null,
  "user_message_chars": 142,
  "user_message_tokens": 36,
  "conversation_history_turns": 2,
  "conversation_history_tokens": 184,
  "tool_definition_count": 0,
  "tool_definition_tokens": 0,
  "tool_names": [],
  "context_block_count": 0,
  "context_block_total_tokens": 0,
  "context_blocks_summary": null,
  "response_format_requested": "text",
  "json_schema_present": false,
  "max_tokens": null,
  "temperature": 0.7,
  "top_p": null,
  "frequency_penalty": null,
  "presence_penalty": null,
  "stop_sequences_count": 0,
  "streaming_requested": true,
  "tool_choice": null,

  "context_utilization_ratio": null,
  "chunk_integrity_score": null,
  "context_staleness_signal": null,
  "context_contradiction_count": null,
  "prompt_antipattern_tags": [],
  "tool_definition_quality_score": null
}
```
All input-quality signals null because there's nothing to score upstream. `payload_retention_mode = off` shown by the null `system_prompt_hash`.

### Example C — Agentic / tool-use
```json
{
  "system_prompt_present": true,
  "system_prompt_chars": 1842,
  "system_prompt_tokens": 461,
  "system_prompt_hash": "9f2c4ab1e0d7b3a5",
  "user_message_chars": 312,
  "user_message_tokens": 78,
  "conversation_history_turns": 4,
  "conversation_history_tokens": 1024,
  "tool_definition_count": 5,
  "tool_definition_tokens": 612,
  "tool_names": ["search_docs", "fetch_url", "run_sql", "send_email", "create_ticket"],
  "context_block_count": 0,
  "context_block_total_tokens": 0,
  "context_blocks_summary": null,
  "response_format_requested": "text",
  "json_schema_present": false,
  "max_tokens": 4096,
  "temperature": 0.2,
  "top_p": null,
  "frequency_penalty": null,
  "presence_penalty": null,
  "stop_sequences_count": 0,
  "streaming_requested": true,
  "tool_choice": "auto",

  "context_utilization_ratio": null,
  "chunk_integrity_score": null,
  "context_staleness_signal": null,
  "context_contradiction_count": null,
  "prompt_antipattern_tags": ["conflicting_instructions"],
  "tool_definition_quality_score": 0.62
}
```
Drives tool-quality diagnostics + the agentic-workflow row in the dashboard.

### Example D — SQL: average context bloat per tenant per workflow over 7 days (drives the Day-1 "Where cost is being destroyed" section)
```sql
SELECT
  r.tenant_id,
  w.workflow_type,
  AVG((r.request_structure->>'context_block_total_tokens')::int)  AS avg_ctx_tokens,
  AVG((r.request_structure->>'context_utilization_ratio')::numeric) AS avg_utilization,
  -- direct payload waste estimate, $:
  SUM(
    ((r.request_structure->>'context_block_total_tokens')::int
     * (1.0 - COALESCE((r.request_structure->>'context_utilization_ratio')::numeric, 0))
    ) * p.input_token_price
  ) AS estimated_direct_payload_waste_usd
FROM aiqg_requests r
JOIN aiqg_workflow_classifications w USING (request_id)
JOIN aiqg_pricing                   p USING (vendor, model)
WHERE r.received_at >= NOW() - INTERVAL '7 days'
  AND (r.request_structure->>'context_block_count')::int > 0
GROUP BY r.tenant_id, w.workflow_type
ORDER BY estimated_direct_payload_waste_usd DESC;
```

---

## 7. Cross-Service Integration

| Service | Role | Surface |
|---|---|---|
| `tas-llm-router` (AIQG mode) | Producer | `internal/aiqg/request_structure.go` populates the struct; emitted as part of `com.tas.aiqg.request.v1` |
| Gatekeeper | Rule-pack engine | `aiqg_context_antipatterns.yaml`, `aiqg_prompt_antipatterns.yaml` populate `prompt_antipattern_tags`, `chunk_integrity_score`, `context_contradiction_count` |
| `tas-spark-jobs/aiqg_aggregator` | Persistence | Writes JSONB column to `aiqg_requests` hypertable; never re-derives fields |
| `aiqg-dashboard-be` | Read-only consumer | Day-1 report assembly via `internal/services/report_service.go`; queries against `aiqg_requests` JSONB GIN indexes |
| `pkg/clear/` (in `tas-llm-router`) | Read-only consumer | Cost decomposer reads `*_tokens` fields; Efficacy scorer reads `prompt_antipattern_tags`; Input-Quality leading indicators feed CLEAR composite (spec §2.6) |
| `aiqg-ui` | Indirect consumer | Renders the "Where cost is being destroyed" + "Input quality" sections of the Day-1 report by calling `aiqg-dashboard-be` |
| `[[token-accounting]]` | Reconciliation | `actual_cost` from vendor billing is reconciled against `total_input_tokens` (generated column) — discrepancies surface as a "prompt-caching reconciliation" line item |

**ID mapping**: `request_id` is the join across every consumer. The `tenant_id` partition key on `aiqg_requests` ensures tenant isolation at the storage layer.

---

## 8. Tenancy

- **Partition key**: `tenant_id` on the `aiqg_requests` hypertable
- **Row-level isolation**: enforced at the `aiqg-dashboard-be` query layer via the Keycloak JWT's `tenant_id` claim; no cross-tenant read paths exist
- **Hash collisions across tenants**: `system_prompt_hash` is a 16-hex truncation of SHA256, so within-tenant churn detection is reliable, but collisions are possible across tenants. This is by design — dedup is intentionally scoped to a single tenant
- **Retention scoping**: `[[account]].payload_retention_mode` is read once at account level; the AIQG-mode middleware caches it in process to avoid a hot-path read

---

## 9. Performance

- **Synchronous capture budget**: < 1ms per request (Phase 1). All structural fields are direct reads + length calculations + Hyperscan rule-pack evaluation. No LLM call. No external I/O.
- **Tokenizer cost**: tiktoken / anthropic-tokenizer ~100µs per 1K tokens; for large RAG contexts (~20K tokens) this is the dominant cost in Phase 1, capped at ~2ms.
- **Storage shape**: JSONB column on a TimescaleDB hypertable. Typical row size: 600–1200 bytes uncompressed; ~150–300 bytes with `toast_tuple_target` + Timescale compression on chunks older than 7 days.
- **Indexes**:
  - **GIN index on `tool_names`** — `CREATE INDEX aiqg_req_tools_gin ON aiqg_requests USING GIN ((request_structure->'tool_names'))`. Required for tool-usage analytics ("which tenants are calling `run_sql`?").
  - **GIN index on `prompt_antipattern_tags`** — same pattern; drives the anti-pattern frequency dashboard.
  - **B-tree on `total_input_tokens`** (generated column) — drives top-N cost-destruction queries.
  - **B-tree on `(tenant_id, received_at DESC)`** — already present on the hypertable; covers per-tenant time-window queries.
- **Continuous aggregates** (TimescaleDB): `aiqg_request_structure_1m`, `aiqg_request_structure_1h`, `aiqg_request_structure_1d`, each rolling up mean / p95 / sum of token counts and counting anti-pattern tag occurrences by tenant × workflow. Dashboard queries hit aggregates, never raw rows.
- **Hot-path read avoidance**: `aiqg-dashboard-be` Day-1 report runs entirely against the 1h continuous aggregate; raw `aiqg_requests` is queried only for drill-down.

---

## 10. Security

- **Default no payload retention**: `system_prompt_hash` is null and `context_blocks_summary` is null when `[[account]].payload_retention_mode = off`. The structural fields (counts, tokens, names, request-time params) are *not* considered payload — they are metadata about shape, not content — and are always persisted.
- **`tool_names` as metadata**: tool names are persisted by default because they are required for the workflow classifier and customer-facing diagnostics. Customers who consider tool names sensitive can configure name-redaction at the account level (deferred to Phase 2; would replace names with stable hashes).
- **Hash-only payload references**: where retention is permitted, the gateway stores 16-hex-char SHA256 prefixes — not full hashes, not raw text. Reverse lookups against retained payloads (the 5–10% sampled retention case) go through Databunker per spec §3.11.
- **Anti-pattern rule packs run in-engine**: `aiqg_prompt_antipatterns.yaml` runs through Gatekeeper's Hyperscan engine. **No LLM call in the hot path** — this is critical to keeping Phase 1 capture under 1ms.
- **No injection surface**: the JSONB column never executes anything; it is read by SQL aggregates only. The `tool_names` GIN index is the only operator-exposed surface and is bounded by the per-request `tool_definition_count` validation.
- **Tag whitelist**: `prompt_antipattern_tags` values are validated against the rule pack's declared identifier set. Unknown tags are dropped — preventing rule-pack drift from polluting the analytic surface.
- **Auditability**: every population path writes a `[[audit-log-entry]]` row when retention mode disagrees with what was written (e.g., a hash present on an `off` account is treated as a bug, alarmed, and surfaces in audit).

---

## 11. Migration

- **Forward**: new `aiqg_requests` hypertable in the `aiqg` database. Migration is `CREATE TABLE ... CREATE INDEX ... SELECT create_hypertable(...)`. No existing object touched.
- **Backward (rollback)**: drop the `aiqg` database. `tas-llm-router` falls back to non-AIQG mode by default when its `Config.AIQG` block is empty (build-vs-reuse §1.2 hard rule) — existing traffic unaffected.
- **Schema evolution within v1.x**: adding new optional fields to the JSONB blob requires no DDL — the JSONB column accepts the new keys, validation rule §15 logs unknown values until the rule-pack identifier set is updated.
- **Breaking schema change (hypothetical v2)**: would be additive — a new `request_structure_v2` JSONB column, written in parallel with v1 for at least one full report cycle (24h), then v1 marked deprecated. Existing reports continue resolving against v1 until the consumer migrates.
- **CLEAR scoring version pinning**: changing the formula does *not* require migrating `request_structure` data — re-scoring is a stateless replay over the existing rows (build-vs-reuse §7.2 tradeoff).

---

## 12. Known Issues & Edge Cases

1. **Heuristic context-block detection is imperfect.** False positives (code blocks misclassified as RAG context) and false negatives (custom retrieval delimiters the gateway doesn't recognize) are real. Customer mitigation: the `TAS-Workflow` header overrides workflow classification for the request. Roadmap: per-tenant custom delimiter rules in Phase 2.
2. **Token counts vs. vendor billing for prompt caching.** When the customer is using OpenAI / Anthropic prompt caching, estimated token counts diverge from billed tokens. Resolution: reconcile against `[[token-accounting]].actual_cost`; surface the delta in the Day-1 report rather than hiding it.
3. **`conversation_history_turns` semantics for tool-use loops.** Tool messages (`role: tool` / `role: function_result`) count as half-turns. This is a deliberate choice — fully counting them inflates the turn count for agentic workflows. Documented limitation; will revisit in Phase 2 if customer feedback warrants.
4. **`system_prompt_hash` cardinality**: 16 hex chars = 2^64 namespace. For a single tenant emitting 10M requests / day, the expected collision rate is negligible (<10^-9 / day). Across tenants, collisions are deliberate non-information (see §8 Tenancy).
5. **Vendor-tokenizer drift.** Vendors update tokenizers without notice (OpenAI cl100k_base → o200k_base for GPT-4o was a real example). Mitigation: pin tokenizer versions per `(vendor, model)` tuple in `pkg/tokenizer/`; bump versions in lock-step with vendor model releases; emit a `tokenizer_pinned_version` field on `[[token-accounting]]` (not on `RequestStructure` — keeps structural fields stable).
6. **Anthropic vs. OpenAI tool schema differences.** Tool names live under `tools[].function.name` (OpenAI) vs. `tools[].name` (Anthropic). Adapter normalizes both into `tool_names`.
7. **Streaming-context-blocks**: if a customer streams the *request* (rare but allowed by Anthropic), context-block detection runs against the buffered prelude only. Documented in the rule pack as "may undercount on chunked-request streaming."

---

## 13. Related Documentation

- [[request-event]] — parent envelope; this struct is its JSONB column
- [[response-structure]] — output-side counterpart
- [[response-event]] — emits the close signal that triggers Phase 2 enrichment
- [[workflow-classification]] — consumes structural fields to assign workflow type
- [[tag-set]] — quality / policy / NIST tags applied alongside this struct
- [[inferred-labels]] — `retry_of_previous`, `abandonment`, `hedge` (response-side companions)
- [[token-accounting]] — reconciliation against vendor billing
- [[account]] — `payload_retention_mode` gates hash and summary fields
- [[audit-log-entry]] — retention violations surface here
- `source-spec-v0.2.md` §2.6 — Input Quality leading indicators (canonical definitions)
- `source-spec-v0.2.md` §3.7 — per-request capture fields (this struct realizes the "Request structure" row)
- `build-vs-reuse.md` §1.2 — non-breaking-change constraint (the reason this is a sidecar, not a `ChatRequest` extension)
- `build-vs-reuse.md` §2.3, §2.7 — capture mechanics
- `build-vs-reuse.md` §2.12 — CLEAR scoring inputs
- `tas-llm-router/internal/types.ChatRequest` — the upstream type this struct *observes* but does not modify

---

## 14. Changelog

| Version | Date | Author | Notes |
|---|---|---|---|
| v1.0.0 | 2026-05-31 | TAS Platform | initial spec draft |
