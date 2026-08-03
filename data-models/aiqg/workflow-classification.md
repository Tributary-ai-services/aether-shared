# AIQG Workflow Classification

---

**Metadata**

```yaml
service: gatekeeper (rule-pack execution) + tas-llm-router (classifier wrapper) + aiqg-dashboard-be (taxonomy presentation)
model: WorkflowClassification (taxonomy definition — per-request results land in InferredLabels)
database: YAML on disk (Gatekeeper config); per-request results in TimescaleDB JSONB on aiqg_request_events.inferred_labels
schema_location: Gatekeeper/configs/rules/aiqg_workflows.yaml
version: 1.0.0
last_updated: 2026-05-31
status: planned (MVP coarse, Phase 2 fine-grained per build-vs-reuse §9)
spec_refs: source-spec-v0.2.md §3.9 (Workflow Classification — six-type taxonomy + detection signals)
plan_ref: build-vs-reuse.md §2.9 (classifier as Gatekeeper rule pack), §2.10 (match-once-tag-many reuse), §9 (phasing — MVP coarse → Phase 2 fine)
```

---

## 1. Overview

### Purpose
This document defines the **AIQG Workflow Classification taxonomy** — the canonical enumeration of workflow types, the detection signals used to identify each type, and the schema by which the Gatekeeper Hyperscan rule pack `aiqg_workflows.yaml` emits classification decisions.

This is **not** a per-request data table. The six (plus one `unknown`) workflow types are the *meta-definition*; per-request classification results land in [[inferred-labels]].`workflow_type` and are also surfaced as `workflow:*` tags in [[tag-set]]. The dashboards, [[aggregated-metrics]] rollups, and Phase-2 [[route-rule]] `workflow_type` matchers all consume this enumeration.

### Why a single source of truth
Multiple subsystems (the rule pack, the dashboard taxonomy picker, the API policy editor, the report generator, the customer-facing report templates) must agree on:
1. **What workflow types exist** (the closed set: 6 + `unknown`)
2. **How each is detected** (the canonical detection signals)
3. **What sub-metrics apply** to each type (informs which CLEAR weighting profile and which efficacy probes run)
4. **How customer overrides interact** with the heuristic decision

This document is that source of truth. The YAML rule pack is the executable form; this document is the human-readable definition.

### Ownership
- **Taxonomy author**: TAS Platform (this document)
- **Rule pack maintainer**: `tas-llm-router` team (owns `Gatekeeper/configs/rules/aiqg_workflows.yaml`)
- **Per-request writer**: `tas-llm-router` writes the classifier output to [[inferred-labels]] at request receipt (build-vs-reuse §2.9 — `internal/workflow/classifier.go` thin wrapper around the Gatekeeper scanner)
- **Override channel writer**: `tas-llm-router` records `TAS-Workflow` header into [[inferred-labels]].`workflow_classification_source = "customer_override_header"`
- **Consumers**: [[aggregated-metrics]] (scope_type=workflow rollups), [[route-rule]] (Phase 2 matcher), aiqg-dashboard-be (workflow distribution panels), aiqg-ui (taxonomy picker, classification-accuracy panel)

### Key Characteristics
- **Closed set in v1**: six workflow types plus `unknown` — additive only via spec amendment + rule-pack version bump
- **Single label per request**: no multi-classification (highest-confidence wins; below-threshold = `unknown`)
- **Heuristic, not authoritative**: confidence score exposed; customer override always wins
- **Zero added latency**: Hyperscan rule pack runs in the same scan pass as other AIQG rule packs (spec §2.10 "match once, tag many"); sub-millisecond per request
- **No LLM call required**: classification is shape-based, not semantic — preserves the spec §2.10 reuse claim

---

## 2. Schema Definition

### Storage

| Artifact | Storage | Owner |
|---|---|---|
| **Taxonomy definition** (this doc) | Markdown in `aether-shared/data-models/aiqg/` | TAS Platform |
| **Rule pack** (executable) | YAML at `Gatekeeper/configs/rules/aiqg_workflows.yaml` | `tas-llm-router` team |
| **Per-request classification result** | JSONB on `aiqg_request_events.inferred_labels` — see [[inferred-labels]] | `tas-llm-router` |
| **Tag form of result** | `workflow:<type>` tag in [[tag-set]] (`workflow:` prefix) | Gatekeeper scanner |
| **Customer overrides** (account-level default) | `customer_workflow_classification_override` on [[account]] (Phase 2) | aiqg-dashboard-be |
| **Customer overrides** (per-request) | `TAS-Workflow` request header → [[inferred-labels]] | `tas-llm-router` |

### The Six Workflow Types

The taxonomy is the closed set below (per spec §3.9). Each type has:
- A canonical enum value (lowercase snake_case — the literal string written to [[inferred-labels]].`workflow_type` and as the `workflow:*` tag suffix)
- A natural-language description
- A set of Hyperscan detection signals (the executable form lives in `aiqg_workflows.yaml`)
- Its key sub-metrics (which CLEAR dimensional probes apply)
- The typical CLEAR weighting concerns (informs the default scoring profile per workflow)

### 1. `single_turn_qa`

- **Description**: Short, single-turn question-answer interactions. The classic chat-completion call with no conversation history, no tool definitions, and short input/output.
- **Detection signals** (Hyperscan rule pack — all must match):
  - `system_prompt_chars < 2000` AND
  - `conversation_history_turns == 0` AND
  - `tool_definition_count == 0` AND
  - `user_message_chars < 2000` AND
  - response expected as text (not structured / not `response_format=json_object`)
- **Key sub-metrics**: cost per request, structural validity, hedge phrase rate
- **Typical CLEAR weighting concerns**: Latency dominant (user is waiting); cost secondary

### 2. `rag`

- **Description**: Retrieval-augmented generation — request contains injected context blocks pulled from a vector store, document corpus, or knowledge base.
- **Detection signals**:
  - Presence of context-block delimiters in user message: triple-backtick fenced blocks (≥1000 chars), OR XML-style `<context>`, `<document>`, `<chunk>`, `<documents>` (Anthropic convention), OR custom markers `---DOCUMENT---`, `---SOURCE---`, `[CONTEXT]`
  - OR system prompt contains directive patterns: "use the following context", "based on the provided documents", "answer using only the information below", "cite your sources"
  - OR total context tokens (user_message + system_prompt context regions) > 2000
- **Key sub-metrics**: `context_utilization_ratio`, `groundedness`, `chunk_integrity_score`, `citation_present`
- **Typical CLEAR weighting concerns**: Cost dominant (context bloat is the #1 RAG cost driver); efficacy via groundedness

### 3. `agentic`

- **Description**: Tool-using agentic workflows — the LLM is given tool definitions and is expected to invoke them, often across multiple turns.
- **Detection signals**:
  - `tool_definition_count >= 1` AND
    - (`tool_choice in {auto, required}` OR explicit tool name in `tool_choice`)
  - OR response includes `tool_calls`
  - OR conversation_history contains prior `tool_results` / `role: tool` messages
- **Key sub-metrics**: `tool_call_accuracy`, `trajectory_validity` (multi-turn loop integrity), `tool_definition_quality_score`
- **Typical CLEAR weighting concerns**: Reliability dominant (pass@k matters for multi-step trajectories where any single failure kills the chain); cost via tool-loop length

### 4. `summarization`

- **Description**: Long-input, short-output summarization. Distinct from RAG in that the input is the source-of-truth document itself, not retrieved context fragments.
- **Detection signals**:
  - `user_message_tokens + conversation_history_tokens > 10000` AND
  - `tool_definition_count == 0` AND
  - `max_tokens` (if set) < `input_tokens / 4`
  - AND/OR system prompt contains pattern: "summarize", "summary", "tl;dr", "key points", "in N sentences", "executive summary"
- **Key sub-metrics**: `context_utilization_ratio` (most input must contribute to output), `coverage_estimate`
- **Typical CLEAR weighting concerns**: Cost dominant (large input); efficacy via coverage / faithfulness

### 5. `code_generation`

- **Description**: Code authoring or modification — generating new code, modifying existing code, or transforming code between languages.
- **Detection signals**:
  - Code-fence patterns in `user_message` or `system_prompt` (triple-backtick with language hint: ` ```python`, ` ```javascript`, ` ```go`, ` ```sql`, ` ```typescript`, etc.)
  - OR system prompt contains language hints: "you are a python expert", "write JavaScript", "generate SQL", "implement in Go", "refactor this code"
  - OR response contains code-fence patterns (post-hoc reinforcement signal)
- **Key sub-metrics**: `parse_validity`, `language_detection`, `code_block_count`
- **Typical CLEAR weighting concerns**: Efficacy (parse-rate) dominant; latency less critical (developer waits, accepts longer responses)

### 6. `classification_extraction`

- **Description**: Structured output with a schema — classification labels, entity extraction, form-filling, named-entity recognition.
- **Detection signals**:
  - `response_format_requested in {json_object, json_schema}` AND `json_schema_present == true`
  - OR system prompt describes a fixed label set: "classify as one of: A, B, C", "return one of [...]"
  - OR system prompt describes a schema: "extract the following fields: ..."
  - OR repetitive request shape (same system prompt across many requests within a tenant, varying user input only) — Phase 2 signal
- **Key sub-metrics**: `json_schema_conformance`, `label_drift`, `schema_validity_rate`
- **Typical CLEAR weighting concerns**: Efficacy (conformance) dominant; assurance for safety classifications (toxicity, intent, PII categorization)

### 7. `unknown`

- **Description**: Classifier confidence is below the threshold (default 0.6). The request shape didn't match any single type strongly enough.
- **Detection signals**: nothing matches strongly — no rule's confidence exceeded threshold
- **Behavior**:
  - Surfaced **explicitly** in dashboards as "uncertain" (the spec §3.9 "Classification accuracy is itself surfaced" requirement)
  - Customer can override via `TAS-Workflow` request header
  - Sub-metrics fall back to the universal set (cost, latency, structural validity, hedge rate)
  - The diagnostic report explicitly calls out unknown rate per workflow scope

### Classifier Output Schema

The YAML rule pack emits, per request, a structured object that lands in [[inferred-labels]]:

```json
{
  "workflow_type": "rag",
  "workflow_classification_confidence": 0.87,
  "workflow_classification_source": "gateway_heuristic",
  "workflow_classification_matched_signals": [
    "rag.context_block_delimiter_xml",
    "rag.system_prompt_directive",
    "rag.total_context_tokens_gt_2000"
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `workflow_type` | enum string | One of the 7 enum values defined above |
| `workflow_classification_confidence` | float (0.0–1.0) | Max of matched signal scores |
| `workflow_classification_source` | enum | `gateway_heuristic` \| `customer_override_header` \| `otel_declared` \| `customer_account_default` |
| `workflow_classification_matched_signals` | string[] | Debug trail — which named rule patterns fired |

---

## 3. Fields Reference

Per-request classifier output fields, as written to [[inferred-labels]]:

| Field | Type | Required | Description | Example |
|---|---|---|---|---|
| `workflow_type` | enum string | Yes | One of `single_turn_qa`, `rag`, `agentic`, `summarization`, `code_generation`, `classification_extraction`, `unknown` | `"rag"` |
| `workflow_classification_confidence` | float | Yes | Max of matched-rule confidences in [0.0, 1.0]; for customer overrides, set to `1.0` | `0.87` |
| `workflow_classification_source` | enum | Yes | `gateway_heuristic` \| `customer_override_header` \| `otel_declared` \| `customer_account_default` | `"gateway_heuristic"` |
| `workflow_classification_matched_signals` | string[] | Optional (omitted for overrides) | Names of rule patterns that fired, in the form `<workflow_type>.<signal_name>` | `["rag.context_block_delimiter_xml"]` |
| `workflow_classification_version` | string | Yes | Version of `aiqg_workflows.yaml` that produced the decision | `"1.0.0"` |

Per-type sub-metric mapping (used by CLEAR scorer to know which probes to apply):

| Workflow type | Cost probes | Latency probes | Efficacy probes | Assurance probes | Reliability probes |
|---|---|---|---|---|---|
| `single_turn_qa` | standard | TTFT, total | structural_validity, hedge_rate | standard | consistency_proxy |
| `rag` | + context_utilization_ratio | TTFT, total | + groundedness, + citation_present, chunk_integrity_score | standard | consistency_proxy |
| `agentic` | + tool_loop_cost | TTFT, total + tool_roundtrip | tool_call_accuracy | + tool_definition_quality_score | + trajectory_validity, pass@k |
| `summarization` | + context_utilization_ratio | total | + coverage_estimate, structural_validity | standard | consistency_proxy |
| `code_generation` | standard | total | + parse_validity, language_detection | + injection (code-eval risk) | consistency_proxy |
| `classification_extraction` | standard | TTFT, total | + json_schema_conformance, + label_drift | + safety_classification_assurance | + schema_validity_rate (pass@k) |
| `unknown` | standard | standard | structural_validity, hedge_rate | standard | consistency_proxy |

---

## 4. Validation Rules

The classifier output MUST satisfy:

1. **Exactly one `workflow_type` per request.** No multi-classification in v1. If multiple type-rules match, pick the one with the highest summed confidence across its matched signals; ties broken by enum-declaration order (above).
2. **Confidence below threshold → `unknown`.** Default threshold is `0.6`. Below threshold, `workflow_type = "unknown"`, even if some signals matched.
3. **Customer override always wins.** If the request carries `TAS-Workflow: <type>` and `<type>` is in the enum, set:
   - `workflow_type = <type>`
   - `workflow_classification_confidence = 1.0`
   - `workflow_classification_source = "customer_override_header"`
   - `workflow_classification_matched_signals` is omitted (null)
4. **OTel-declared beats heuristic, loses to customer override.** If no valid `TAS-Workflow` override is present but the request carries `gen_ai.operation.name` (see [[otel-genai-ingestion]]) that maps to a workflow type (`invoke_agent`/`execute_tool`/`create_agent` → `agentic`), set:
   - `workflow_type = <mapped type>`
   - `workflow_classification_confidence = 1.0`
   - `workflow_classification_source = "otel_declared"`

   Op-names that fall through (`chat`/`text_completion`/`generate_content`/unknown) or are excluded (`embeddings`) do **not** set a declared type — fall through to heuristic. The raw op-name and the heuristic result are still recorded for classification drift ([[classification-drift]]). Precedence: `customer_override_header` > `otel_declared` > `gateway_heuristic`.
5. **Invalid override → log + fall through.** If the `TAS-Workflow` header value is not in the enum, log a warning, drop the override, and fall through to (OTel-declared, then heuristic) classification. Tag the event with `workflow:override_invalid` for visibility.
6. **`workflow_classification_version` MUST always be set** (the rule-pack version) — required for reproducibility of historical aggregates.
7. **No-match guarantee.** Even if zero rules match, the classifier emits `unknown` with `confidence = 0.0` and empty `matched_signals`. There is no nullable case.

---

## 5. Relationships

| Relationship | Direction | Target | Cardinality | Notes |
|---|---|---|---|---|
| Classifier writes result to | → | [[inferred-labels]] | 1:1 per request | Embedded fields, not a join |
| Surfaces as tag in | → | [[tag-set]] | 1:1 (`workflow:*` prefix) | `workflow:rag`, `workflow:agentic`, etc. |
| Reads request shape from | ← | [[request-structure]] | 1:1 | Token counts, tool defs, code fences, schema presence |
| Reads response shape from | ← | [[response-structure]] | 1:1 | Post-hoc reinforcement (tool_calls present, code fences in output) |
| Per-type rollups consumed by | → | [[aggregated-metrics]] | 1:N | `scope_type=workflow` rollups grouped by `workflow_type` |
| Matched by Phase-2 routing rule | → | [[route-rule]] | N:1 | Route rules MAY filter on `workflow_type` |
| Override channel from request | ← | [[request-event]] | 1:1 | `TAS-Workflow` header captured to event row |
| Account-level override default | ← | [[account]] | N:1 | Phase 2 — `workflow_classification_override` setting |
| Customer-supplied workflow name | ← | [[request-event]] | 1:1 | Spec §3.6 `TAS-Workflow` header is the override mechanism |

---

## 6. Lifecycle & State Machines

### Per-Request Classifier Lifecycle

```
                  +---------------------------+
                  | Request arrives at        |
                  | tas-llm-router            |
                  +---------------------------+
                              |
                  +-----------v-----------+
                  | TAS-Workflow header   |
                  | present AND valid?    |
                  +-----------+-----------+
                              |
                Yes           |          No
            +-----------------+-----------------+
            |                                   |
            v                                   v
+---------------------+         +---------------------------+
| source=             |         | Run aiqg_workflows.yaml   |
|   customer_override |         | via Gatekeeper scanner    |
| confidence=1.0      |         | (single Hyperscan pass)   |
+---------------------+         +---------------------------+
            |                                   |
            |                       +-----------v-----------+
            |                       | max(matched.confidence)|
            |                       | >= 0.6 ?              |
            |                       +-----------+-----------+
            |                                   |
            |                       Yes         |       No
            |                  +----------------+----------------+
            |                  |                                 |
            |                  v                                 v
            |        +---------------------+      +---------------------------+
            |        | workflow_type=      |      | workflow_type=unknown     |
            |        |   highest-conf type |      | confidence=max (or 0)     |
            |        | source=             |      | source=gateway_heuristic  |
            |        |   gateway_heuristic |      +---------------------------+
            |        +---------------------+                 |
            |                  |                             |
            +------------------+-----------------------------+
                               |
                               v
                  +---------------------------+
                  | Write to inferred_labels  |
                  | Emit workflow:<type> tag  |
                  | Continue to CLEAR scorer  |
                  +---------------------------+
```

### Taxonomy Lifecycle (Document-Level)

- **v1.0.0 (this document, 2026-05-31)**: Six types + `unknown`. Closed set.
- **Phase 2 (per build-vs-reuse §9)**: Fine-grained sub-types within each parent type (e.g., `rag.standard`, `rag.multi_doc`, `rag.long_context_rag`); multi-label classification (Phase 3 candidate).
- **Adding a type**: Requires spec amendment + new rule-pack version + dashboard UI update + customer-comm window. Never silently.
- **Removing a type**: Never. Deprecate via "stop emitting" but keep the value accepted for historical replay/queries.

### Rule-Pack Lifecycle

| Stage | Owner | Artifact |
|---|---|---|
| Draft | TAS Platform | Update this document |
| Encode | `tas-llm-router` team | Update `aiqg_workflows.yaml`; bump `version` |
| Test | `tas-llm-router` team | Run classifier against captured request corpus; verify confusion matrix |
| Ship | Gatekeeper deploy | Rule pack loaded at scanner startup |
| Pin | aiqg-dashboard-be | Tenants on Phase-2 plans MAY pin to a specific rule-pack version via [[account]] |

---

## 7. API Examples

### YAML rule-pack snippet (the `rag` detection in `aiqg_workflows.yaml`)

```yaml
version: 1.0.0
ruleset: aiqg_workflows
description: AIQG workflow-type classifier rule pack (six-type taxonomy + unknown)
classification_confidence_threshold: 0.6
workflows:
  - type: rag
    rules:
      - name: rag.context_block_delimiter_xml
        pattern: '<(context|document|chunk|documents)>'
        scope: user_message
        confidence: 0.85
        case_insensitive: true
      - name: rag.context_block_delimiter_custom
        pattern: '---(DOCUMENT|SOURCE|CONTEXT)---'
        scope: user_message
        confidence: 0.85
      - name: rag.context_block_fenced_long
        # triple-backtick fence with body >=1000 chars
        pattern: '```[\s\S]{1000,}```'
        scope: user_message
        confidence: 0.7
      - name: rag.system_prompt_directive
        pattern: '(use the following context|based on the provided document|answer using only the information|cite your sources)'
        scope: system_prompt
        confidence: 0.75
        case_insensitive: true
      - name: rag.total_context_tokens_gt_2000
        kind: numeric_threshold
        signal: total_context_tokens
        operator: '>'
        value: 2000
        confidence: 0.6
    aggregation: max
```

### SQL — workflow distribution & confusion analysis for one tenant over 7 days

This SQL drives the "Classification accuracy" UI panel (spec §3.9 last paragraph).

```sql
-- Workflow distribution + unknown rate + customer-override rate for tenant_id=$1, last 7d
SELECT
  inferred_labels->>'workflow_type' AS workflow_type,
  inferred_labels->>'workflow_classification_source' AS source,
  COUNT(*) AS event_count,
  AVG((inferred_labels->>'workflow_classification_confidence')::numeric) AS avg_confidence
FROM aiqg_request_events
WHERE tenant_id = $1
  AND received_at >= NOW() - INTERVAL '7 days'
GROUP BY 1, 2
ORDER BY event_count DESC;
```

### SQL — drives the "unknown rate" alarm

```sql
-- Returns the fraction of requests classified as unknown over the last hour
-- Alert when > 20% (suggests rule-pack drift or new traffic shape)
SELECT
  COUNT(*) FILTER (WHERE inferred_labels->>'workflow_type' = 'unknown')::float
    / NULLIF(COUNT(*), 0) AS unknown_fraction
FROM aiqg_request_events
WHERE tenant_id = $1
  AND received_at >= NOW() - INTERVAL '1 hour';
```

### API JSON — dashboard endpoint returns the taxonomy for the policy editor

Phase 2 — the [[route-rule]] policy editor's `workflow_type` matcher dropdown calls this endpoint to render the picker. By returning the taxonomy from a live endpoint (rather than hard-coding in the UI), the dashboard auto-updates when a new type ships.

```
GET /api/v1/aiqg/taxonomy/workflow-classification
```

```json
{
  "version": "1.0.0",
  "confidence_threshold": 0.6,
  "types": [
    {
      "value": "single_turn_qa",
      "label": "Single-turn Q&A",
      "description": "Short single-turn question-answer interactions",
      "key_sub_metrics": ["cost_per_request", "structural_validity", "hedge_phrase_rate"],
      "clear_weighting_hint": "latency_dominant"
    },
    {
      "value": "rag",
      "label": "Retrieval-Augmented Generation",
      "description": "Request contains injected context blocks",
      "key_sub_metrics": ["context_utilization_ratio", "groundedness", "chunk_integrity_score", "citation_present"],
      "clear_weighting_hint": "cost_dominant"
    },
    {
      "value": "agentic",
      "label": "Agentic / Tool Use",
      "description": "Tool-using agentic workflows",
      "key_sub_metrics": ["tool_call_accuracy", "trajectory_validity", "tool_definition_quality_score"],
      "clear_weighting_hint": "reliability_dominant"
    },
    {
      "value": "summarization",
      "label": "Long-context Summarization",
      "description": "Long-input short-output summarization",
      "key_sub_metrics": ["context_utilization_ratio", "coverage_estimate"],
      "clear_weighting_hint": "cost_dominant"
    },
    {
      "value": "code_generation",
      "label": "Code Generation",
      "description": "Code authoring or modification",
      "key_sub_metrics": ["parse_validity", "language_detection", "code_block_count"],
      "clear_weighting_hint": "efficacy_dominant"
    },
    {
      "value": "classification_extraction",
      "label": "Classification / Extraction",
      "description": "Structured output with schema or fixed label set",
      "key_sub_metrics": ["json_schema_conformance", "label_drift", "schema_validity_rate"],
      "clear_weighting_hint": "efficacy_dominant"
    },
    {
      "value": "unknown",
      "label": "Unknown (uncertain)",
      "description": "Classifier confidence below 0.6 threshold",
      "key_sub_metrics": ["cost_per_request", "structural_validity", "hedge_phrase_rate"],
      "clear_weighting_hint": "universal_default"
    }
  ]
}
```

### Cypher — classifier-version pinning history (Phase 2)

When Phase 2 introduces per-account version pinning, the audit history of which version a tenant ran against is stored as relationships on the [[account]] node in Neo4j:

```cypher
// Find the active classifier-pack version for an account, with full pin history
MATCH (a:Account {id: $accountId})-[r:PINNED_TO_CLASSIFIER_VERSION]->(v:ClassifierVersion)
RETURN v.version AS version, r.pinned_at AS pinned_at, r.unpinned_at AS unpinned_at
ORDER BY r.pinned_at DESC;
```

---

## 8. Cross-Service Integration

| Service | How it interacts with this taxonomy |
|---|---|
| `tas-llm-router` | Loads `aiqg_workflows.yaml` at startup; runs the classifier per request (build-vs-reuse §2.9) and writes [[inferred-labels]].`workflow_type` + emits the `workflow:*` tag |
| Gatekeeper scanner | Executes the rule pack as part of the single Hyperscan pass (spec §3.10 "match once, tag many") — no separate scan cost |
| `aiqg-dashboard-be` | Serves the `/api/v1/aiqg/taxonomy/workflow-classification` endpoint; renders the "Classification accuracy" panel; surfaces `unknown` rate as a first-class metric in reports |
| `tas-spark-jobs/aiqg_aggregator` | Rolls up CLEAR scores by `workflow_type` into [[aggregated-metrics]] (`scope_type=workflow`) |
| `aiqg-ui` | Workflow distribution panel; taxonomy picker in the route-rule editor (Phase 2); per-workflow CLEAR weighting profiles in the customer report |
| Customer applications | MAY send `TAS-Workflow: <type>` to override the heuristic (spec §3.6 + this doc §4) |
| CLEAR scorer (`pkg/clear/`) | Reads `workflow_type` to select which sub-metric probes to run (§3 Fields Reference table) and which weighting profile to apply |

---

## 9. Performance Considerations

- **Latency**: Sub-millisecond classification per request — the rule pack runs *inside* Gatekeeper's existing Hyperscan pass, sharing scan cost with all other AIQG/Gatekeeper rule packs. There is **zero added LLM call** for classification — this is the key spec §2.10 reuse claim.
- **Confidence aggregation**: Simple `max()` over matched-signal confidences per workflow type. O(rules) per request, but in practice all rules fire in a single Hyperscan automaton — true cost is O(input_length) once across all packs.
- **Rule-pack memory**: One compiled Hyperscan database per pack, loaded once at scanner startup; ~MBs, negligible vs. existing Gatekeeper footprint.
- **Caching**: The classifier output is part of the [[inferred-labels]] JSONB column — no separate cache layer. The hot-path retry lookup in Redis ([[inferred-labels]] §1) does not need workflow_type because retry detection uses request fingerprints.
- **Dashboard query cost**: Workflow distribution queries hit the GIN index on `tags @> '["workflow:<type>"]'::jsonb` ([[tag-set]] §2) — same index path as other tag queries; no separate index needed.
- **Hot-path safety**: Classifier completion is required before the request is forwarded to the upstream LLM provider. P99 budget is <1ms; circuit-breaker fallback is "emit `unknown` with confidence 0.0, log, continue" — never block the request.

---

## 10. Migration Strategies

### Adding a new workflow type (Phase 2+)
1. Append the new type to this document with full template fields (description, signals, sub-metrics, CLEAR weighting)
2. Bump `aiqg_workflows.yaml` to a new minor version (e.g., 1.1.0)
3. Add rules for the new type to the YAML pack — additive only
4. Update the dashboard taxonomy endpoint (§7) to include the new type
5. Update CLEAR scorer's per-type sub-metric mapping (§3) to register new probes if any
6. Customer notification: 30-day window before existing tag filters that may inadvertently catch the new type take effect
7. Deploy: backward-compatible (existing tenants see new type appear in dashboards naturally)

### Removing a workflow type (DISCOURAGED)
Never. Always deprecate-but-accept:
- Stop emitting the type from the classifier (no new requests classified as it)
- Continue accepting it in `TAS-Workflow` override (back-compat)
- Continue accepting it in historical queries / replay
- Mark deprecated in this document with deprecation date + replacement

### Schema migration of the classifier output
- The classifier output JSON shape is additive — new optional fields can be appended at any time
- Removing a field requires a major-version bump of `aiqg_workflows.yaml` AND a back-compat migration on [[inferred-labels]] (default the removed field to its historical-value contract)

### Customer override migration
- The `TAS-Workflow` header is permanent contract (spec §3.6). Never break it.
- New override channels (e.g., Phase 2 [[account]].`workflow_classification_override`) are additive; precedence is **per-request header > account default > heuristic**.

### Rule-pack version pinning (Phase 2)
- Tenants on enterprise plans MAY pin to a specific rule-pack version via [[account]] settings
- Pinning is recorded as a Neo4j relationship (`PINNED_TO_CLASSIFIER_VERSION`) for audit
- When TAS Platform retires an old version (e.g., 0.x), tenants pinned to it are notified 90 days in advance

---

## 11. Common Patterns

### Pattern: Customer overrides classification because heuristic is unreliable for their domain
- Customer's domain (e.g., legal review) doesn't fit the six types cleanly
- Customer sends `TAS-Workflow: classification_extraction` to force the closest type
- Dashboards show their traffic correctly bucketed; their CLEAR scoring uses the right sub-metric set

### Pattern: Mixed workflow (RAG + tool use)
- Request has *both* context blocks (RAG) and tool definitions (agentic)
- Current v1 behavior: highest-confidence match wins. Typically `agentic` wins because `tool_definition_count >= 1` is a strong signal.
- Acknowledged limitation (see §15). Phase-3 multi-label classification will properly tag both.
- Customer workaround: `TAS-Workflow: rag` if they care more about groundedness than tool accuracy

### Pattern: Repetitive structured-output workloads (Phase 2 signal)
- Same system prompt across thousands of requests in a tenant, varying only user input
- Phase 2 will detect "structural repetition" as a classification_extraction signal
- v1 falls back to JSON-schema-presence signal, which catches most but not all of these

### Pattern: Dashboard surfaces classification accuracy
- Per spec §3.9 final paragraph, classification accuracy itself is a first-class metric
- "Classification accuracy" panel shows: % of requests with `workflow_classification_confidence >= 0.6` (confident); % `unknown`; % `customer_override_header`
- Customer takes action: low confident-rate AND low override-rate suggests they should adopt the override header for their workload

### Pattern: New traffic shape detected
- Sudden spike in `unknown` rate alerts ops; signals that customer traffic has shifted shape (e.g., new feature launched in their app)
- Investigation: dump 100 sample `unknown` requests; review shape; propose new rule or new workflow type for the next rule-pack version

---

## 12. Error Handling

| Error | Detection | Response | Surface |
|---|---|---|---|
| Classifier scanner unavailable | Hyperscan call returns error | Set `workflow_type=unknown`, `confidence=0.0`, source=`gateway_heuristic`, log warning | `workflow:scanner_unavailable` tag emitted; metric `aiqg_classifier_failures_total` increments |
| Invalid `TAS-Workflow` header value | Value not in enum | Drop override, fall through to heuristic, log warning | `workflow:override_invalid` tag; metric `aiqg_workflow_override_invalid_total` |
| Rule-pack load failed at startup | YAML parse error or schema validation failure | `tas-llm-router` fails to start (fail-fast); circuit-breaker engages | Alert on `aiqg_classifier_pack_load_failure` |
| Confidence aggregation overflow | Numeric edge case (shouldn't happen but defended) | Clamp to [0.0, 1.0] | Logged once at debug level |
| Conflicting per-request override and account default | Both set | Per-request wins (per §4 rule 3 precedence) | Recorded normally; no error |
| Customer sends override for `unknown` | `TAS-Workflow: unknown` | Accept it — useful for testing the unknown code path | Recorded with source=`customer_override_header` |

---

## 13. Testing Strategies

### Unit tests (per rule)
- For each rule in `aiqg_workflows.yaml`, provide a positive and negative test fixture
- Assert the rule's confidence value matches the spec
- Run via `go test ./internal/workflow/...` in `tas-llm-router`

### Corpus regression tests
- Curated corpus of N labeled requests (initially N=200; grow to N=2000 in Phase 2)
- Each labeled with the true workflow type + a brief justification
- CI runs the classifier against the corpus; report confusion matrix per pull request
- Block merge if any individual type's recall drops > 5 percentage points vs. last release

### Confusion-matrix monitoring (production)
- Daily Spark job that samples 1% of requests + their classifier outputs, plus all `TAS-Workflow`-overridden requests where the override disagreed with the heuristic
- Builds a confusion matrix: heuristic-class × override-class
- Surfaces top mis-classified shapes for inspection by the rule-pack maintainer

### Override-disagreement panel (production)
- Dashboard shows the top 10 (heuristic_type, override_type) pairs over the last 7d per tenant
- Drives prioritization of which rules need refinement

### Performance test
- Synthetic load test: 10,000 requests/sec into the scanner with the rule pack loaded
- Assert P99 added latency < 1ms attributable to the workflow rule pack
- Captured in `tas-llm-router` perf-test harness alongside other Gatekeeper rule pack benchmarks

---

## 14. Related Documentation

- [[inferred-labels]] — the data model where per-request classification results land
- [[tag-set]] — the `workflow:*` tag namespace produced by classification
- [[request-event]] — the event row that stores the `TAS-Workflow` override header value
- [[request-structure]] — the canonical fields (token counts, tool defs, fences, schemas) that the classifier reads
- [[response-structure]] — post-hoc reinforcement signals (tool_calls present, code in output)
- [[aggregated-metrics]] — `scope_type=workflow` rollups grouped by `workflow_type`
- [[route-rule]] — Phase 2 policy matcher on `workflow_type`
- [[account]] — Phase 2 storage of account-level `workflow_classification_override`
- [[response-event]] — paired event for response-side enrichment
- [[otel-genai-ingestion]] — OTel `gen_ai.*` as a declared classification source (adds the `otel_declared` source + precedence rung)
- [[classification-drift]] — declared-vs-inferred drift built on this taxonomy
- `source-spec-v0.2.md` §3.9 — the canonical six-type spec
- `source-spec-v0.2.md` §3.6 — `TAS-Workflow` request header definition
- `source-spec-v0.2.md` §3.10 — "match once, tag many" architecture
- `build-vs-reuse.md` §2.9 — classifier as a Gatekeeper rule pack
- `build-vs-reuse.md` §2.10 — Gatekeeper rule-pack reuse (the central reuse lever)
- `build-vs-reuse.md` §9 — phasing: MVP coarse → Phase 2 fine-grained

---

## 15. Known Issues

- **Heuristic classification has false positives and false negatives.** MVP solution is the customer override via `TAS-Workflow` header. Phase 2 will add LLM-judged classification on a sampled subset to drive rule-pack refinement.
- **Mixed-workflow requests** (e.g., RAG with tool use, or summarization with embedded code) currently choose a single type — typically `agentic` because `tool_definition_count >= 1` is a high-confidence signal. Multi-label classification is deferred to Phase 3. Customer workaround: override with the type whose sub-metrics they care about most.
- **Vendor-specific detection variability**: Anthropic's `<documents>` tag convention differs from OpenAI's typical fenced-block convention. Rule pack handles both for `rag`, but newer or proprietary conventions may slip through until rules are added. Mitigation: monitor the `unknown` rate; investigate spikes.
- **`code_generation` overlap with `classification_extraction`**: A request asking for structured-JSON output describing code (e.g., a code-review summary) may match both type-rules. Currently `classification_extraction` wins via the `response_format=json_schema` strong signal. Document as expected; revisit if false-positive rate becomes material.
- **No classifier-version pinning in MVP**: All tenants run the same rule-pack version. A breaking change to rule semantics (rather than additive) would shift historical aggregates. Mitigation: rule-pack changes are additive-only in v1.x; semantic shifts require major-version bump + opt-in.
- **`unknown` is a real class, not an error**: Dashboards and reports must treat `unknown` as a normal value (the spec §3.9 explicitly calls this out). A tenant with high `unknown` rate is a *signal*, not a *bug*.

---

## Security

- Detection rules can't include PII patterns — they observe content **shape** (delimiters, structural patterns, token counts), not content semantics. No PII can leak through classification.
- No LLM call required for classification, so no third-party data exposure path from classification itself (matches the spec §2.10 reuse claim).
- The `TAS-Workflow` override header is validated against the enum at receipt — invalid values are dropped and never written to storage as `workflow_type`.
- Rule-pack files are signed and loaded via Gatekeeper's existing HMAC-attested scan cache infrastructure ([[tag-set]] §Security); no untrusted rule packs can be loaded at runtime.
- Per-tenant rule-pack overrides (Phase 2 enterprise feature) require account-admin role and are audit-logged.

---

## Changelog

- **v1.1.0 — 2026-08-03** — add `otel_declared` classification source + precedence rung (`customer_override_header` > `otel_declared` > `gateway_heuristic`); Validation Rule 4 (OTel-declared mapping) inserted; cross-links to [[otel-genai-ingestion]] + [[classification-drift]] (Plan #8 Phase 0) — TAS Platform
- **v1.0.0 — 2026-05-31** — initial spec draft — TAS Platform
