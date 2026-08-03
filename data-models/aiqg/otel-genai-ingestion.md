# AIQG OTel GenAI Ingestion

---

**Metadata**

```yaml
service: tas-llm-router (header ingestion + precedence) → aiqg-dashboard-be / Spark (consumption)
model: OTelGenAIIngestion (a declared classification/attribution SOURCE, not a data table)
database: none (transport contract); ingested values land in [[inferred-labels]], [[response-event]].agent_context, and the drift fields in [[classification-drift]]
schema_location: tas-llm-router/internal/middleware/aiqg_headers.go (parse + strip)
version: 0.1.0
last_updated: 2026-08-03
status: planned (Plan #8 Phase 1)
spec_refs: OpenTelemetry GenAI semantic conventions (gen_ai.*); source-spec-v0.2.md §3.9 (workflow classification); AIQG-AGENT-FLOW-ATTRIBUTION.md (identity ladder)
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #8 (OTel GenAI Header Ingestion + Classification/Attribution Drift)
```

---

## 1. Overview

### Purpose

Clients that already run OpenTelemetry / APM (Datadog, etc.) emit **GenAI semantic-convention** attributes (`gen_ai.*`) describing what they *believe* each LLM call is — the operation, the agent, the conversation. Today the gateway ignores them and relies solely on its own inference (heuristic workflow `Classify()` + the fingerprinted identity tier). This contract makes `gen_ai.*` a **declared source** that:

1. slots between the explicit `TAS-*` override and the gateway's own inference in both the **workflow** and **agent-attribution** precedence ladders, and
2. is always captured *alongside* the inferred value so the two can be compared — the disagreement is the product (see [[classification-drift]]).

### What this is / is not

- **Is**: a transport + precedence contract. Which `gen_ai.*` keys we read, on what wire format, how they map to the 6-type taxonomy, where they sit in each precedence ladder, and the guarantee they never reach the vendor.
- **Is not**: the drift computation (that is [[classification-drift]]), nor a new storage table. Ingested values reuse existing homes (`inferred_labels`, `agent_context`).

### Honest-confidence stance

`gen_ai.*` is a **high-confidence declared label but still a client assertion** — a customer's explicit `TAS-*` header outranks it, and the gateway's own inference is always computed too. We never *discard* the inferred value when a declared one wins.

---

## 2. Ingested keys

Per the locked decision (2026-08-03), the wire format is **explicit raw HTTP request headers** carrying the `gen_ai.*` attribute names verbatim. (Baggage-carried `gen_ai.*` is a deliberate future add-on — the parser already captures all baggage keys, so it is a one-function follow-on; not in v0.1.)

| Header (verbatim `gen_ai.*` name) | OTel attribute | Feeds | Notes |
|---|---|---|---|
| `gen_ai.operation.name` | operation name | **workflow** ladder → declared `workflow_type` | Coarse OTel op-name; mapped per §3 (partial map + heuristic fallthrough) |
| `gen_ai.agent.id` | agent id | **agent** ladder → declared agent | Stable client-side agent identifier |
| `gen_ai.agent.name` | agent name | **agent** ladder → declared agent name | Human-readable |
| `gen_ai.conversation.id` | conversation id | **agent** ladder → `conversation_id` | Parallel to `TAS-Conversation-Id` / baggage `session.id` |
| `gen_ai.system` | system/provider | context only (observability) | e.g. `anthropic`, `openai`; not used for routing/classification in v0.1 |

**Header-name casing**: HTTP header names are case-insensitive; Go's `http.Header` canonicalizes, but the `.`-bearing `gen_ai.*` names are NOT valid canonical MIME header tokens, so ingestion MUST read them via a case-insensitive raw lookup (iterate the header map, compare lowercased), not `Header.Get("Gen_Ai.Operation.Name")`. See §6 impl note.

**Not ingested in v0.1** (documented so downstream doesn't assume them): `gen_ai.request.*` / `gen_ai.usage.*` / `gen_ai.response.*` (we measure those ourselves), `gen_ai.prompt`/`gen_ai.completion` (content — never accept as a side channel).

---

## 3. `gen_ai.operation.name` → `workflow_type` map

OTel's operation-name set is **coarser** than the 6-type AIQG taxonomy, so the map is **partial + heuristic fallthrough**, never a wholesale replacement. Locked mapping (2026-08-03, "agentic-broad + embeddings excluded"):

| `gen_ai.operation.name` | Declared `workflow_type` | Rationale |
|---|---|---|
| `invoke_agent` | `agentic` | agent invocation is unambiguously agentic |
| `execute_tool` | `agentic` | tool execution is agentic |
| `create_agent` | `agentic` | agent lifecycle op |
| `chat` | *(fallthrough)* | too generic to override the heuristic — let `Classify()` decide shape (rag/qa/etc.) |
| `text_completion` | *(fallthrough)* | same — generic completion |
| `generate_content` | *(fallthrough)* | same — generic generation |
| `embeddings` | *(excluded)* | not a chat-completion workload; do **not** emit a declared workflow, do not classify |
| *(any other / unknown op-name)* | *(fallthrough)* | forward-compatible: unknown op-names fall through to the heuristic |

**Fallthrough semantics**: when the mapping yields no confident type, the declared workflow is left empty and the heuristic `Classify()` result is used as the effective workflow — but the raw declared op-name is still recorded for drift (`workflow_declared` may be empty while `workflow_declared_op` carries the raw op-name; see [[classification-drift]] §3).

**Versioning**: this table is a versioned mapping. Changes bump `otel_map_version` (recorded on the event so historical drift stays interpretable).

---

## 4. Precedence ladders

### 4.1 Workflow precedence (effective `workflow_type`)

```
explicit TAS-Workflow header   (source = customer_override_header)
  >  OTel gen_ai.operation.name-derived   (source = otel_declared)
  >  gateway heuristic Classify()   (source = gateway_heuristic)
```

- A valid `TAS-Workflow` header still wins outright (customer intent is authoritative).
- An OTel-derived type (only the mappable op-names in §3) wins over the heuristic.
- `embeddings` and the fallthrough op-names do **not** set a declared type → heuristic decides.
- Implemented by extending the existing `preferredWorkflow(headerVal, classified)` selector to `preferredWorkflow(headerVal, otelVal, classified)` (tas-llm-router `pkg/aiqg/events/builder.go`).

`workflow_classification_source` (see [[workflow-classification]] §3) gains a new enum value **`otel_declared`**.

### 4.2 Agent precedence (identity ladder)

The existing ladder (strongest→weakest): `baggage › asserted (TAS-Agent-*) › trace › linked › fingerprinted › principal › transport › unattributed`. OTel inserts a new **`otel`** rung directly **below `asserted`** (an explicit `TAS-Agent-*` still wins) and **above `trace`**:

```
baggage  >  asserted (TAS-Agent-*)  >  otel (gen_ai.agent.*)  >  trace  >  linked  >  fingerprinted  >  principal  >  transport  >  unattributed
```

- `gen_ai.agent.id` / `gen_ai.agent.name` populate `agent_id` / `agent_name` when no `TAS-Agent-*` header is present; `identity_source = "otel"`.
- `gen_ai.conversation.id` populates `conversation_id` when neither `TAS-Conversation-Id` nor baggage `session.id` is present.
- `identity_source` (see [[response-event]] `agent_context`) gains a new enum value **`otel`**.

---

## 5. Strip-before-vendor guarantee

All ingested `gen_ai.*` request headers MUST be removed before the request is proxied upstream — same discipline as `baggage` and `TAS-*`. They are added to the gateway's strip list (`canonicalHeaderNames`, tas-llm-router `internal/middleware/aiqg_headers.go`). Rationale: they are TAS-internal attribution signals; leaking them to the vendor is both a data-hygiene and a surprise-behavior risk. A unit test asserts none of the `gen_ai.*` names survive on the outbound request.

Note: unlike `traceparent` (a standard tracing header intentionally kept), `gen_ai.*` are **stripped**.

---

## 6. Implementation notes (tas-llm-router)

- **Parse**: in `ParseHeaders` add a small `parseGenAIHeaders(r.Header)` that does a case-insensitive scan for the five `gen_ai.*` names (they contain `.`, so canonical `Header.Get` won't find them). Populate new fields on `AIQGHeaders`: `OTelOperation`, `OTelAgentID`, `OTelAgentName`, `OTelConversationID`, `OTelSystem`.
- **Map**: `operationToWorkflow(op string) (workflowType string, ok bool)` implements §3 (returns `("agentic", true)` for the three agentic ops; `("", false)` for fallthrough/embeddings — caller distinguishes embeddings-excluded from fallthrough only for the raw-op record).
- **Workflow ladder**: extend `preferredWorkflow` to 3-arg; project the mapped OTel workflow through `AIQGHeadersView`.
- **Agent ladder**: add an `otel` `case` in `buildAgentContext` between the `asserted` and `trace` cases; set `identity_source = "otel"`.
- **Strip**: append the five names to `canonicalHeaderNames`.
- **Factoring**: keep `parseGenAIHeaders` source-agnostic (takes a `map[string]string`-like accessor) so a future baggage path feeds the same mapper.

---

## 7. Cross-service integration

- **[[workflow-classification]]** — `workflow_classification_source` enum gains `otel_declared`; precedence §4 documented there too.
- **[[response-event]]** — `agent_context.identity_source` gains `otel`; the declared/inferred/drift fields (§ in [[classification-drift]]) are additive on the event.
- **[[classification-drift]]** — consumes the declared vs inferred pair this contract produces.
- **Spark aggregator** (`tas-spark-jobs/jobs/aiqg_aggregator`) — additive nullable `event_metrics` columns for the drift fields.

---

## 8. Testing

Per adapter/ingestion:
1. **Header parse** — `gen_ai.*` (with dots, mixed case) are read into `AIQGHeaders`.
2. **Op-name map** — table test over all §3 rows incl. unknown-op fallthrough and `embeddings` exclusion.
3. **Workflow precedence** — `TAS-Workflow` beats OTel beats heuristic; fallthrough op → heuristic effective type.
4. **Agent precedence** — `TAS-Agent-*` beats OTel; OTel beats trace/fingerprinted; `identity_source = "otel"` set correctly.
5. **Strip** — no `gen_ai.*` header on the outbound vendor request.
6. **Round-trip** — snake_case JSON keys on the event (declared/inferred/drift) are pinned (see [[classification-drift]]).

---

## 9. Related Documentation

- [[classification-drift]] — the two drift axes this ingestion enables
- [[workflow-classification]] — the 6-type taxonomy + `workflow_classification_source` enum
- [[response-event]] — `agent_context` + event drift fields
- [[inferred-labels]] — where per-request classification lands
- `AIQG-AGENT-FLOW-ATTRIBUTION.md` — the identity ladder this extends
