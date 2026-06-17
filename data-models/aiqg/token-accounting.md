# Token Accounting

**Service:** AI Quality Gateway (AIQG)
**Storage:** JSONB column `token_accounting` on `aiqg.response_events` (PostgreSQL/TimescaleDB)
**Status:** v1.0.0 — Initial spec draft
**Last updated:** 2026-05-31

---

## 1. Overview

`token_accounting` is the per-response cost-and-token sub-structure embedded in every [[response-event]]. It is documented as its own data model — separate from the response event envelope — for two reasons:

1. The field set is non-trivial (15+ fields covering token counts, pricing snapshots, computed costs, and waste decomposition).
2. The CLEAR Cost-dimension three-category waste model ([source-spec-v0.2 §2.1](./source-spec-v0.2.md)) lives here: direct payload waste, induced output waste, and genuine post-model waste. Keeping the waste decomposition adjacent to the raw token counts that drove it makes the cost story self-contained.

**Storage decision: JSONB column on `aiqg.response_events`, not a separate table.**

The write path is the gateway's hot path (`tas-llm-router` per [build-vs-reuse §7.2](./build-vs-reuse.md)). One `INSERT` per response is materially simpler than a two-row insert across a parent/child table, and keeps response-event ingestion atomic. Analytics queries that need fast access to specific fields (e.g. `actual_cost_usd` for cost-anomaly dashboards) are served by PostgreSQL generated columns + partial indexes (see §9 Performance). This decision is consistent with the JSONB-as-extension pattern used elsewhere in TAS (`audimodal.files.metadata`, `aether-be` workflow definitions).

**Relationship to CLEAR scoring:** Fields in this model are inputs to the CLEAR Cost-dimension scorer (`pkg/clear/cost.go` per [build-vs-reuse §2.12](./build-vs-reuse.md#212-21-25--clear-dimension-measurement)). The scorer reads `actual_cost_usd` + waste decomposition and emits CNA (Cost-Normalized Accuracy) and CPS (Cost Per Success) metrics. The scoring runs gateway-side at request close per [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go).

**Non-breaking-change constraint** ([build-vs-reuse §1.2](./build-vs-reuse.md#12-non-breaking-change-constraint)): the existing `tas-llm-router/internal/pricing.EstimateCost()` function is **not** modified. A new sibling function `pkg/clear/cost.ActualCost(usage, pricingTable) Cost` computes the actual post-response cost from real vendor usage fields. `estimated_cost_usd` in this schema is the EstimateCost return value captured pre-request; `actual_cost_usd` is the new ActualCost return value computed at response close. Both are persisted so cost-variance analytics work.

---

## 2. Schema Definition

`token_accounting` is a JSONB document. The canonical JSON shape (with PostgreSQL/TypeScript type annotations) is:

```jsonc
{
  // — Token counts (vendor-reported usage) —
  "input_tokens": 0,                          // int  >= 0
  "output_tokens": 0,                         // int  >= 0
  "cached_input_tokens": 0,                   // int  >= 0
  "cache_creation_input_tokens": 0,           // int  >= 0  (Anthropic cache write)
  "tool_tokens": 0,                           // int  >= 0
  "reasoning_tokens": null,                   // int|null  (OpenAI o-series only)

  // — Pricing snapshot (price-table reproducibility) —
  "vendor_pricing_version": "openai-2026-05-15",  // string
  "input_unit_price_usd": "0.00250000",           // numeric(12,8) per 1k tokens
  "output_unit_price_usd": "0.01000000",          // numeric(12,8) per 1k tokens
  "cached_input_unit_price_usd": "0.00125000",    // numeric(12,8)|null

  // — Computed cost (pre-request estimate vs. post-response actual) —
  "estimated_cost_usd": "0.012500",           // numeric(10,6)  from EstimateCost()
  "actual_cost_usd":    "0.013200",           // numeric(10,6)  from ActualCost()
  "actual_cost_source": "vendor",             // enum: "vendor" | "estimated"
  // cost_variance_usd is a generated column (see §9)

  // — Waste decomposition (CLEAR Cost dimension, three-category) —
  "direct_payload_waste_tokens": 1247,        // int  >= 0  | null
  "direct_payload_waste_usd":    "0.003117",  // numeric    | null
  "induced_output_waste_estimated_usd": "0.001800", // numeric | null
  "genuine_post_model_waste_usd":       null,       // numeric | null

  // — Scoring provenance —
  "scoring_version": "clear-1.0.0"            // string, see build-vs-reuse §7.2
}
```

### 2.1 Containing table DDL (TimescaleDB)

```sql
-- Hypertable: response_events  (defined in detail under [[response-event]])
CREATE TABLE aiqg.response_events (
  response_id        UUID PRIMARY KEY,
  request_id         UUID NOT NULL,
  account_id         UUID NOT NULL,
  tenant_id          UUID NOT NULL,
  occurred_at        TIMESTAMPTZ NOT NULL,
  -- ... other response-event fields ...
  token_accounting   JSONB NOT NULL,

  -- Generated columns for hot analytic paths
  actual_cost_usd    NUMERIC(10,6) GENERATED ALWAYS AS
                       ((token_accounting->>'actual_cost_usd')::NUMERIC(10,6)) STORED,
  estimated_cost_usd NUMERIC(10,6) GENERATED ALWAYS AS
                       ((token_accounting->>'estimated_cost_usd')::NUMERIC(10,6)) STORED,
  cost_variance_usd  NUMERIC(10,6) GENERATED ALWAYS AS
                       ( ((token_accounting->>'actual_cost_usd')::NUMERIC(10,6))
                       - ((token_accounting->>'estimated_cost_usd')::NUMERIC(10,6))
                       ) STORED
);

SELECT create_hypertable('aiqg.response_events', 'occurred_at',
                        chunk_time_interval => INTERVAL '1 day');

-- See §9 for index DDL
```

---

## 3. Fields Reference

### 3.1 Token counts

| Field | Type | Required | Description |
|---|---|---|---|
| `input_tokens` | int | yes | Total input tokens billed by the vendor (system + user + history + tool defs + retrieved context). Mirrors `usage.prompt_tokens` from OpenAI / `usage.input_tokens` from Anthropic. |
| `output_tokens` | int | yes | Tokens generated by the model. `usage.completion_tokens` (OpenAI) / `usage.output_tokens` (Anthropic). |
| `cached_input_tokens` | int | yes | Input tokens served from vendor cache (prompt caching). OpenAI: `usage.prompt_tokens_details.cached_tokens`. Anthropic: `usage.cache_read_input_tokens`. Default 0 when not applicable. |
| `cache_creation_input_tokens` | int | yes | Tokens billed to **write** to the vendor cache (Anthropic-specific: `usage.cache_creation_input_tokens`). 0 for OpenAI. |
| `tool_tokens` | int | yes | Subset of input/output tokens attributable to tool definitions, tool calls, and tool results. Computed by the gateway via Hyperscan tagging on the request structure (not directly reported by all vendors). |
| `reasoning_tokens` | int \| null | no | OpenAI o-series reasoning tokens (`usage.completion_tokens_details.reasoning_tokens`). `null` for non-reasoning models. |

**Why `cached_input_tokens` is its own field, not subtracted from `input_tokens`:** vendor APIs report `input_tokens` as the *total* including cache hits, and bill cache hits at a discounted rate. The CLEAR Cost scorer needs both the total token consumption (for context-utilization metrics) and the cached subset (for actual-cost computation), so they are stored separately.

### 3.2 Pricing snapshot

| Field | Type | Required | Description |
|---|---|---|---|
| `vendor_pricing_version` | string | yes | Identifier of the price-table snapshot used to compute `actual_cost_usd`. Format: `<vendor>-<YYYY-MM-DD>` (e.g. `openai-2026-05-15`, `anthropic-2026-04-22`). Pricing tables are config-driven and additive (see §11 Migration). |
| `input_unit_price_usd` | numeric(12,8) | yes | Per-1k-token input rate applied at compute time, sourced from the table identified by `vendor_pricing_version`. |
| `output_unit_price_usd` | numeric(12,8) | yes | Per-1k-token output rate. |
| `cached_input_unit_price_usd` | numeric(12,8) \| null | no | Per-1k-token rate for cache hits (typically 10–25% of `input_unit_price_usd`). `null` when no cache hits occurred for this request. |

**numeric(12,8) precision rationale:** vendor list prices range from ~$0.00015/1k (Haiku input) to ~$0.075/1k (GPT-4 output 32k context). 8 decimal places preserves full precision across this range and leaves headroom for future fractional-cent pricing tiers.

### 3.3 Computed cost

| Field | Type | Required | Description |
|---|---|---|---|
| `estimated_cost_usd` | numeric(10,6) | yes | What `tas-llm-router/internal/pricing.EstimateCost()` returned **pre-request**, based on input-token estimation + the routing decision's target model. Do **not** modify EstimateCost — see [build-vs-reuse §1.2 / §7.5](./build-vs-reuse.md#12-non-breaking-change-constraint). |
| `actual_cost_usd` | numeric(10,6) | yes | Computed **post-response** from real `usage.*` fields, by the new `pkg/clear/cost.ActualCost(usage Usage, pricing PricingTable) Cost` function added per [build-vs-reuse §1.2 surface table](./build-vs-reuse.md#12-non-breaking-change-constraint). |
| `actual_cost_source` | enum | yes | `"vendor"` when `usage.*` came back from the vendor and `ActualCost()` used real counts. `"estimated"` when the vendor omitted usage on a streamed response and the gateway fell back to client-side token estimation (see [§13 Known Issues](#13-known-issues)). |
| `cost_variance_usd` | numeric(10,6) | derived | **PostgreSQL generated column** on the containing table: `actual_cost_usd - estimated_cost_usd`. Positive = under-estimate, negative = over-estimate. Used for cost-anomaly dashboards. |

**numeric(10,6) precision rationale:** maximum plausible single-request cost is ~$10 (large agentic tool-use loop with multiple GPT-4 32k turns). 4 decimal places fully cover sub-cent precision; 6 leaves room for batched aggregation without precision loss.

### 3.4 Waste decomposition (CLEAR Cost three-category model) — Contract v1 (projected)

Populated by the CLEAR cost scorer at request close ([build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go)), implemented in `tas-llm-router/pkg/clear/cost_decomposer.go` (`DecomposeCost`). Sources the three categories defined in [source-spec-v0.2 §2.1](./source-spec-v0.2.md).

**Contract v1 is the *projected* basis** (cheap inline heuristic — no embeddings, no network, <1ms; runs on 100% of priced traffic). The embed+cosine attribution method is the *measured* basis and moves to **Contract v2** (policy-gated / experiment, see [[experiment]]). Only emitted on **priced** traffic (`actual.Priced`); never fabricated on unpriced.

| Field | Type | Description |
|---|---|---|
| `reduction_mode` | string | `"projected"` in v1 (v2 adds `shadow`/`active`). Filters projected-vs-measured rollups. |
| `actual_cost_usd` | numeric | Billed total — the invariant denominator (`= total_cost_usd` in v1). |
| `actual_cost_source` | string | `"vendor_usage"` (vendor-reported counts) \| `"computed"`. |
| `context_efficiency_ratio` | numeric \| null | CER proxy `clamp(r/(r+0.5))`, `r = completion/prompt`, ×0.7 when inbound bloat findings present. Nullable: `0.0` (all context wasted) is meaningful vs absent. |
| `projected_direct_payload_waste_tokens` / `_usd` | int / numeric | Input tokens/dollars projected droppable = `prompt·(1−CER)` priced at the input rate. |
| `direct_payload_waste_tokens` / `_usd` | int / numeric | **Documented alias = the projected basis** (Contract v1 D1-B; *not* COALESCE). Kept so the existing bound-invariant CHECK + dashboards keep working — relabel "projected" in UI. |
| `projected_reduction_relevance_usd` (+ `_confidence`) | numeric / string | Relevance/top-K standalone savings ≈ direct payload waste. Confidence `medium`. |
| `projected_reduction_slm_usd` (+ `_confidence`) | numeric / string | SLM-rewrite standalone savings (~25% prompt compression prior). Confidence `low`. |
| `projected_reduction_combined_usd` | numeric | Compound of the two: `inputCost·(1−(1−relFrac)(1−0.25))`. |
| `induced_output_waste_estimated_usd` | numeric | **0 in v1** (retry linkage is Contract v2). |
| `genuine_post_model_waste_usd` | numeric | Spend no reduction could save: full cost on `status≥400`/`content_filter`; output cost on high/critical outbound findings; else 0. |
| `gateway_addressable_pct` | numeric | `(direct + induced) / actual_cost_usd × 100`. |

**Bound invariant (#6):** `direct + induced + genuine ≤ actual_cost_usd`, enforced by a clamp in `DecomposeCost` (genuine first, then direct against the remaining budget) — the Go `TestDecompose_BoundInvariant` is the canary for the eventual SQL CHECK.

### 3.4.1 Contract v2 — measured reduction (Phase 2, shadow/active)

Populated only when the **real** Gatekeeper extractor runs on a request (`reduction_mode` ∈ {`shadow`, `active`}; see [[extraction-policy]]). Under Contract v1 (`projected`) these are absent. Defined here so emitter/consumers are ready; the gateway execution slice populates them.

| Field | Type | Description |
|---|---|---|
| `reduction_mode` | string | now also emits `shadow` (measured, not applied) / `active` (applied). |
| `reduction_sampled` | bool | request was in the `sample_rate` shadow sample (i.e. the extractor actually ran). |
| `actual_direct_payload_reduction_tokens` / `_usd` | int / numeric | **measured** input reduction from the real extractor (vs the v1 *projected* `direct_payload_waste_*`). |
| `actual_reduction_relevance_usd` | numeric | measured saving from the relevance/top-K step. |
| `actual_reduction_slm_usd` | numeric | measured saving from the SLM step. |
| `reduction_efficacy_delta` | numeric \| null | CLEAR Efficacy change vs the un-reduced baseline (shadow eval). Nullable: measured-0 vs not-measured. |
| `reduction_assurance_delta` | numeric \| null | CLEAR Assurance change vs baseline. Same null semantics. |

Projected-vs-actual reconciliation lives on the event (compare the v1 `projected_*` with these). `scoring_version` bumps when the gateway starts populating these (the execution slice).

### 3.5 Scoring provenance

| Field | Type | Required | Description |
|---|---|---|---|
| `scoring_version` | string | yes | Identifier of the CLEAR scorer that wrote the decomposition. Current: **`clear-v0.2-cost-decomp`** (`pkg/clear/clear.go` `Version`). Bumped when a formula changes so Spark re-scores old rows — see [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go). |
| `model_pricing_version` | string | yes | Pricing table identifier (current `pricing-v2026-06-05`). Distinct from `scoring_version` — rates change independently of the formula. |

---

## 4. Validation Rules

Enforced in `pkg/clear/cost.go::ValidateTokenAccounting()` before persistence:

1. **Token counts non-negative.** `input_tokens, output_tokens, cached_input_tokens, cache_creation_input_tokens, tool_tokens >= 0`. `reasoning_tokens >= 0` when not null.
2. **Cache subset bound.** `cached_input_tokens <= input_tokens` — cache hits are a subset of total input.
3. **Cost non-negative.** `estimated_cost_usd >= 0` AND `actual_cost_usd >= 0`. Negative cost is never valid (refunds happen at the billing layer, not the per-request layer).
4. **Pricing required when cost > 0.** If `actual_cost_usd > 0`, then `vendor_pricing_version`, `input_unit_price_usd`, and `output_unit_price_usd` MUST be non-null. Zero-cost requests (free-tier, error-before-vendor-call) may omit pricing fields.
5. **Cached pricing required when cache hits occurred.** If `cached_input_tokens > 0`, then `cached_input_unit_price_usd` MUST be non-null.
6. **Waste decomposition bounded by actual cost (CRITICAL INVARIANT):**
   ```
   COALESCE(direct_payload_waste_usd, 0)
   + COALESCE(induced_output_waste_estimated_usd, 0)
   + COALESCE(genuine_post_model_waste_usd, 0)
   <= actual_cost_usd
   ```
   Waste estimates are *conservative*: the three categories never sum to more than the cost actually incurred. In MVP, individual components may be `null` (not computed) — those default to 0 in the sum but are reported as `null` (not 0) in dashboards so users distinguish "no waste detected" from "waste not measured."
7. **Source enum.** `actual_cost_source IN ('vendor', 'estimated')`.
8. **Scoring version present.** `scoring_version` MUST be set on any row where any waste field is non-null (so historical re-scoring can identify which scorer wrote which fields).

The waste-bound invariant is also enforced as a CHECK constraint at the database level via a generated column:

```sql
ALTER TABLE aiqg.response_events
  ADD CONSTRAINT waste_bounded_by_actual_cost CHECK (
    COALESCE((token_accounting->>'direct_payload_waste_usd')::NUMERIC, 0)
    + COALESCE((token_accounting->>'induced_output_waste_estimated_usd')::NUMERIC, 0)
    + COALESCE((token_accounting->>'genuine_post_model_waste_usd')::NUMERIC, 0)
    <= COALESCE((token_accounting->>'actual_cost_usd')::NUMERIC, 0) + 0.000001
    -- epsilon allows for float rounding in the gateway-side scorer
  );
```

---

## 5. Relationships

```
                 ┌──────────────────────┐
                 │ [[request-event]]    │
                 │   request_id (PK)    │
                 └──────────┬───────────┘
                            │ 1:1
                            ▼
                 ┌──────────────────────┐         ┌───────────────────────┐
                 │ [[response-event]]   │ embeds  │  token_accounting     │
                 │   response_id (PK)   │────────►│  (this model, JSONB)  │
                 │   request_id (FK)    │         └───────────────────────┘
                 │   token_accounting   │                    │
                 └──────────┬───────────┘                    │ inputs to
                            │                                ▼
                            │                     ┌─────────────────────┐
                            │ 1:1                 │ [[aggregated-       │
                            ▼                     │  metrics]] cost     │
                 ┌──────────────────────┐         │ rollups (1m/1h/1d)  │
                 │ [[event-timestamps]] │         └─────────────────────┘
                 │ [[inferred-labels]]  │
                 │ [[tag-set]]          │
                 └──────────────────────┘

                 ┌──────────────────────┐
                 │ [[account]]          │ owns all response_events via
                 │   account_id (PK)    │ account_id FK on response_events
                 │   tenant_id          │
                 └──────────────────────┘
```

- **1:1 with [[response-event]]** — every response event has exactly one `token_accounting` JSONB (it's a column, not a relation).
- **Reads from [[inferred-labels]]** — the `retry_of_previous` flag drives the `induced_output_waste_estimated_usd` heuristic (§3.4).
- **Feeds [[aggregated-metrics]]** — the Spark aggregator (`tas-spark-jobs/aiqg_aggregator`) rolls `actual_cost_usd`, `cost_variance_usd`, and the three waste fields into 1m / 1h / 1d hypertables for dashboard queries.
- **Scoped by [[account]]** — `account_id` and `tenant_id` on the parent `response_events` row gate all reads (§10 Security).

---

## 6. Lifecycle & State Machines

`token_accounting` has no state machine of its own — it is written once at response close and never mutated. The lifecycle is the lifecycle of the enclosing `response_events` row:

```
   request arrives at gateway
            │
            ▼
   ┌──────────────────────────┐
   │ EstimateCost() called    │  → estimated_cost_usd, pricing snapshot captured
   └────────────┬─────────────┘
                │
                ▼
   forward request to vendor
                │
                ▼
   stream response chunks
                │
                ▼
   ┌──────────────────────────┐
   │ stream complete:         │
   │  - read usage.* fields   │  → input/output/cached/tool token counts
   │  - ActualCost() called   │  → actual_cost_usd, actual_cost_source
   │  - CLEAR cost scorer     │  → waste decomposition + scoring_version
   └────────────┬─────────────┘
                │
                ▼
   ┌──────────────────────────┐
   │ ValidateTokenAccounting()│  → invariant checks (§4)
   └────────────┬─────────────┘
                │
                ▼
   INSERT into aiqg.response_events  (one atomic write)
                │
                ▼
   publish CloudEvent → tas.aiqg.response.v1 (Kafka)
                │
                ▼
   tas-spark-jobs/aiqg_aggregator consumes → rolls into aggregated-metrics
                │
                ▼
   eventually subject to retention policy (account.retention_days)
```

**Re-scoring (out-of-band, rare):** If a CLEAR formula update requires recomputing historical waste fields, a one-off Spark job reads raw signals from `response_events` + `request_events`, computes new waste fields tagged with a new `scoring_version`, and writes them to a sibling table `aiqg.token_accounting_rescored` (NOT in-place — the original row's `scoring_version` must remain stable for audit). See [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go).

---

## 7. Examples

### 7.1 Successful chat completion (OpenAI gpt-4o-mini, no cache, no waste)

```json
{
  "input_tokens": 248,
  "output_tokens": 412,
  "cached_input_tokens": 0,
  "cache_creation_input_tokens": 0,
  "tool_tokens": 0,
  "reasoning_tokens": null,

  "vendor_pricing_version": "openai-2026-05-15",
  "input_unit_price_usd":  "0.00015000",
  "output_unit_price_usd": "0.00060000",
  "cached_input_unit_price_usd": null,

  "estimated_cost_usd": "0.000280",
  "actual_cost_usd":    "0.000284",
  "actual_cost_source": "vendor",

  "direct_payload_waste_tokens": null,
  "direct_payload_waste_usd": null,
  "induced_output_waste_estimated_usd": null,
  "genuine_post_model_waste_usd": null,

  "scoring_version": "clear-1.0.0"
}
```

Read: clean Q&A, no context blocks present so direct-payload-waste is N/A (`null`, not `0`). Cost variance `+$0.000004` is within rounding tolerance.

### 7.2 Anthropic prompt caching (Claude 3.5 Sonnet, cache hit on system prompt)

```json
{
  "input_tokens": 8420,
  "output_tokens": 612,
  "cached_input_tokens": 7800,
  "cache_creation_input_tokens": 0,
  "tool_tokens": 0,
  "reasoning_tokens": null,

  "vendor_pricing_version": "anthropic-2026-04-22",
  "input_unit_price_usd":         "0.00300000",
  "output_unit_price_usd":        "0.01500000",
  "cached_input_unit_price_usd":  "0.00030000",

  "estimated_cost_usd": "0.027440",
  "actual_cost_usd":    "0.013800",
  "actual_cost_source": "vendor",

  "direct_payload_waste_tokens": null,
  "direct_payload_waste_usd": null,
  "induced_output_waste_estimated_usd": null,
  "genuine_post_model_waste_usd": null,

  "scoring_version": "clear-1.0.0"
}
```

Read: a 7800-token system prompt hit the cache at 10% of base rate. Estimate over-shot by $0.0136 because `EstimateCost()` is cache-agnostic by design (pre-request, we can't know if the vendor will serve from cache). This negative variance is the *intended* signal for "your cache is working."

### 7.3 RAG workflow with bloated context (full waste decomposition)

```json
{
  "input_tokens": 18420,
  "output_tokens": 287,
  "cached_input_tokens": 0,
  "cache_creation_input_tokens": 0,
  "tool_tokens": 0,
  "reasoning_tokens": null,

  "vendor_pricing_version": "openai-2026-05-15",
  "input_unit_price_usd":  "0.00250000",
  "output_unit_price_usd": "0.01000000",
  "cached_input_unit_price_usd": null,

  "estimated_cost_usd": "0.048920",
  "actual_cost_usd":    "0.048920",
  "actual_cost_source": "vendor",

  "direct_payload_waste_tokens": 13100,
  "direct_payload_waste_usd":    "0.032750",
  "induced_output_waste_estimated_usd": null,
  "genuine_post_model_waste_usd":       null,

  "scoring_version": "clear-1.0.0"
}
```

Read: 18420 input tokens (RAG context-laden), but only ~5320 attributed to the output by embedding-similarity sampling. The other 13100 tokens (~71% of input) were retrieved-but-unused context — the canonical "direct payload waste" pattern. $0.032 of the $0.049 spend is gateway-addressable per [source-spec-v0.2 §2.1](./source-spec-v0.2.md). This is the screenshot Day-1 reports are built around.

### 7.4 Streaming response with vendor usage omission (fallback path)

```json
{
  "input_tokens": 412,
  "output_tokens": 187,
  "cached_input_tokens": 0,
  "cache_creation_input_tokens": 0,
  "tool_tokens": 0,
  "reasoning_tokens": null,

  "vendor_pricing_version": "openai-2026-05-15",
  "input_unit_price_usd":  "0.00250000",
  "output_unit_price_usd": "0.01000000",
  "cached_input_unit_price_usd": null,

  "estimated_cost_usd": "0.002900",
  "actual_cost_usd":    "0.002900",
  "actual_cost_source": "estimated",

  "direct_payload_waste_tokens": null,
  "direct_payload_waste_usd": null,
  "induced_output_waste_estimated_usd": null,
  "genuine_post_model_waste_usd": null,

  "scoring_version": "clear-1.0.0"
}
```

Read: the vendor's streaming response did not include a final `usage` block (a known issue on some legacy `/v1/completions` endpoints — see §13). The gateway fell back to client-side token counting via tiktoken. `actual_cost_source = "estimated"` flags this row so cost-accuracy dashboards can filter it out when computing variance metrics.

### 7.5 Aggregation SQL — cost per workflow type for a tenant, last 7 days

```sql
-- "Where is this tenant's AI spend going, broken out by workflow + waste category?"
SELECT
  il.workflow_type,
  COUNT(*) AS request_count,
  ROUND(SUM(re.actual_cost_usd)::NUMERIC, 2)                              AS total_cost_usd,
  ROUND(SUM((re.token_accounting->>'direct_payload_waste_usd')::NUMERIC)::NUMERIC, 2)
                                                                          AS direct_waste_usd,
  ROUND(SUM((re.token_accounting->>'induced_output_waste_estimated_usd')::NUMERIC)::NUMERIC, 2)
                                                                          AS induced_waste_usd,
  ROUND(SUM((re.token_accounting->>'genuine_post_model_waste_usd')::NUMERIC)::NUMERIC, 2)
                                                                          AS genuine_waste_usd,
  ROUND(
    SUM(
        COALESCE((re.token_accounting->>'direct_payload_waste_usd')::NUMERIC, 0)
      + COALESCE((re.token_accounting->>'induced_output_waste_estimated_usd')::NUMERIC, 0)
    ) / NULLIF(SUM(re.actual_cost_usd), 0) * 100,
    1
  )                                                                       AS pct_addressable_waste
FROM aiqg.response_events re
JOIN aiqg.inferred_labels  il ON il.response_id = re.response_id
WHERE re.tenant_id    = $1
  AND re.occurred_at >= NOW() - INTERVAL '7 days'
GROUP BY il.workflow_type
ORDER BY total_cost_usd DESC;
```

This is the query that backs Screen 5 §2 ("Where cost is being destroyed") of the Day-1 report ([source-spec-v0.2 §4.3](./source-spec-v0.2.md)).

---

## 8. Cross-Service Integration

| Producer | Consumer | Mechanism | Notes |
|---|---|---|---|
| `tas-llm-router` `pkg/clear/cost.go::ActualCost()` | `aiqg.response_events.token_accounting` | Direct PostgreSQL `INSERT` via `aiqg-dashboard-be`'s persistence layer (or direct write — TBD per [build-vs-reuse §4.2](./build-vs-reuse.md#42-new-repo-aiqg-dashboard-be-go)) | Atomic with response-event insert |
| `tas-llm-router` `internal/events/aiqg_v1.go` | Kafka topic `tas.aiqg.response.v1` | CloudEvents 1.0 envelope; `token_accounting` is a top-level field on the event payload | Per [build-vs-reuse §2.7](./build-vs-reuse.md#27-37--per-request-capture) |
| `tas-spark-jobs/aiqg_aggregator` | `aiqg.aggregated_metrics_1m/1h/1d` | Structured Streaming reads `tas.aiqg.response.v1`; rolls cost + waste fields into TimescaleDB continuous aggregates | Per [build-vs-reuse §4.4](./build-vs-reuse.md#44-extend-tas-spark-jobs) |
| `aiqg-dashboard-be` | `aiqg-ui` Day-1 report | REST `GET /api/v1/reports/day1?tenant_id=...` returns aggregated waste decomposition | Per [build-vs-reuse §4.2](./build-vs-reuse.md#42-new-repo-aiqg-dashboard-be-go) |
| Operations | Grafana dashboard `aiqg-cost-destruction.json` | Direct TimescaleDB query against `aggregated_metrics_1h` | Per [build-vs-reuse §4.5](./build-vs-reuse.md#45-extend-shared-monitoring) |

**ID-mapping context:** `tenant_id` on the response event chains back to Keycloak realm → Aether Space → AIQG Account per [build-vs-reuse §4.6](./build-vs-reuse.md#46-update-existing-claudemd-files). Cross-service cost analytics that need to display tenant names join `account_id` → `aiqg.accounts` → Aether Space.

---

## 9. Performance Considerations

**Write path** (gateway hot path):
- One `INSERT` per response, JSONB serialization adds ~50µs vs. plain columns. Acceptable given the response-event throughput target (10k/s p99 per [build-vs-reuse §7.3](./build-vs-reuse.md#73-path-a-enforcement)).
- The waste-bound CHECK constraint adds ~5µs (numeric comparisons on already-parsed JSONB). Negligible.

**Read path** (analytics):

Generated columns + indexes for the hot query paths:

```sql
-- 1. Cost dashboards: "where is spend going" — heaviest query class
CREATE INDEX idx_response_events_actual_cost
  ON aiqg.response_events (tenant_id, occurred_at DESC, actual_cost_usd)
  WHERE actual_cost_usd > 0;

-- 2. Cost-anomaly detection: outliers in estimate vs. actual variance
CREATE INDEX idx_response_events_cost_variance
  ON aiqg.response_events (tenant_id, occurred_at DESC)
  WHERE ABS(cost_variance_usd) > 0.01;
-- partial index — only the rows that actually anomalous (>1¢ off) are indexed

-- 3. Waste-decomposition rollups: by workflow type joined from inferred-labels
--    The aggregator job pre-rolls these; the index supports ad-hoc queries.
CREATE INDEX idx_response_events_direct_waste
  ON aiqg.response_events (tenant_id, occurred_at DESC)
  INCLUDE (token_accounting)
  WHERE (token_accounting->>'direct_payload_waste_usd')::NUMERIC > 0;
```

**Continuous aggregates** (TimescaleDB) materialize the §7.5 query at 1m / 1h / 1d granularity, so dashboard reads never scan raw `response_events`. The aggregator drops per-row JSONB and keeps only the numeric cost / waste fields, so the aggregate tables are ~5% the size of raw.

**JSONB GIN indexing** is **not** recommended on `token_accounting` — the field set is fixed (no key-existence queries), and individual fields that need fast access are surfaced as generated columns above. A GIN index would inflate write cost without query benefit.

---

## 10. Security

- **Tenant scoping** — every read MUST be filtered by `tenant_id`, gated by `aiqg-dashboard-be`'s Keycloak JWT realm-`aether` middleware. There is no row-level security at the PostgreSQL layer; the application-layer scope is the boundary.
- **PII risk: low.** Token counts and costs are non-PII numerics. `vendor_pricing_version` is a public price-table identifier. No request/response payloads are in this model.
- **Financial data sensitivity: high.** Per-tenant cost is competitive intelligence. Access controls:
  - Only the tenant's own users (Aether Space members per [build-vs-reuse §4.6](./build-vs-reuse.md#46-update-existing-claudemd-files)) may read their own rows.
  - TAS internal operators read aggregated cross-tenant data via Grafana with separate Keycloak role `aiqg-ops`.
  - All reads to per-tenant cost data MUST emit an [[audit-log-entry]] with `action = "cost_data_read"`, `tenant_id`, `actor_user_id`, `query_window`. This is gateway-side; raw SQL access via DB admin tools is logged at the PostgreSQL `log_statement = 'all'` level on `aiqg` database connections.
- **Account-level retention:** [[account]].`retention_days` governs how long `response_events` (including `token_accounting`) is kept. Default 90 days for paid tier, 30 days for free tier. Retention is enforced by a TimescaleDB drop-chunks policy keyed off `occurred_at`.
- **Region residency:** `tenant_id` is bound to an `account.region` ∈ {`us-east`, `us-west`, `eu-west`} at signup. Cross-region replication of `response_events` is disabled — each region has its own `aiqg` database. See [source-spec-v0.2 §3.11](./source-spec-v0.2.md).

---

## 11. Migration Strategies

### 11.1 Vendor price-table updates (the common case)

Pricing tables are **config-driven** and **additive**. A new vendor price list is a new entry in `tas-llm-router/configs/pricing/` keyed by `vendor_pricing_version` (e.g. `openai-2026-06-01.yaml`). Historical rows keep their original `vendor_pricing_version` and remain reproducible.

**Rule (enforced by code review):** never modify a price-table file once committed. Always add a new dated version. Re-pricing historical traffic requires a one-off Spark job that writes to the rescored sibling table per §6.

### 11.2 Schema additions (new fields)

JSONB makes additions trivial: a new field (e.g. `cache_read_latency_ms`) appears in new rows, defaults to `null` in old rows, and the application code reads with `COALESCE`. No DDL required.

### 11.3 Schema deletions (rare — discouraged)

Removing a field from the JSONB shape requires:
1. Stop writing the field (deploy gateway change).
2. Wait `account.retention_days` for old rows to drop out.
3. Optionally backfill `NULL` over the field in surviving rows with `jsonb_set(token_accounting, '{<field>}', 'null')`.

In practice we never delete — JSONB has no penalty for unused-but-present fields, and audit trails matter.

### 11.4 Generated column changes

If the generated-column formula needs to change (e.g. to handle currency conversion), use `ALTER TABLE ... DROP COLUMN cost_variance_usd; ALTER TABLE ... ADD COLUMN cost_variance_usd ... GENERATED ALWAYS AS (...) STORED;`. TimescaleDB rewrites the affected chunks lazily. Plan for ~5 minutes downtime on a 100M-row hypertable.

### 11.5 Scoring-version migrations

When the CLEAR cost scorer formula changes:
1. Bump `clear-X.Y.Z` semver.
2. New rows get the new version automatically.
3. Old rows retain their old version — never silently re-scored in place.
4. If historical re-scoring is requested by a customer, run the dedicated Spark job and surface both versions in the report ("computed with clear-1.0.0; re-scored as clear-1.1.0 shows ...").

---

## 12. Common Patterns

### 12.1 Querying "addressable waste this period"

```sql
SELECT
  SUM(
      COALESCE((token_accounting->>'direct_payload_waste_usd')::NUMERIC, 0)
    + COALESCE((token_accounting->>'induced_output_waste_estimated_usd')::NUMERIC, 0)
  ) AS gateway_addressable_usd,
  SUM(actual_cost_usd) AS total_cost_usd
FROM aiqg.response_events
WHERE tenant_id = $1
  AND occurred_at >= $2 AND occurred_at < $3;
```

This produces the headline "$X of $Y is gateway-addressable" figure on Screen 5.

### 12.2 Detecting EstimateCost drift

```sql
-- Top routes where the estimator is systematically off by >5%
SELECT
  endpoint,
  COUNT(*) AS n,
  ROUND(AVG(cost_variance_usd / NULLIF(estimated_cost_usd, 0))::NUMERIC, 4) AS mean_pct_variance,
  ROUND(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY ABS(cost_variance_usd))::NUMERIC, 6) AS p95_abs_variance
FROM aiqg.response_events
WHERE occurred_at >= NOW() - INTERVAL '24 hours'
  AND estimated_cost_usd > 0.001
GROUP BY endpoint
HAVING ABS(AVG(cost_variance_usd / NULLIF(estimated_cost_usd, 0))) > 0.05
ORDER BY n DESC;
```

Signals when `EstimateCost()` needs recalibration *without* touching its return values (it would still ship the old answer; we just route Prometheus alerts off the variance).

### 12.3 Per-model cost-per-1k-tokens reconciliation

```sql
-- Sanity check: do the unit prices in our pricing snapshot match what we billed?
SELECT
  model,
  vendor_pricing_version,
  ROUND(AVG(actual_cost_usd / NULLIF(input_tokens + output_tokens, 0) * 1000)::NUMERIC, 6) AS observed_per_1k,
  ROUND(AVG(
    (input_unit_price_usd  * input_tokens
   + output_unit_price_usd * output_tokens) / NULLIF(input_tokens + output_tokens, 0)
  )::NUMERIC, 6) AS expected_per_1k
FROM aiqg.response_events re, LATERAL (
  SELECT
    (token_accounting->>'vendor_pricing_version')      AS vendor_pricing_version,
    (token_accounting->>'input_unit_price_usd')::NUMERIC  AS input_unit_price_usd,
    (token_accounting->>'output_unit_price_usd')::NUMERIC AS output_unit_price_usd,
    (token_accounting->>'input_tokens')::INT  AS input_tokens,
    (token_accounting->>'output_tokens')::INT AS output_tokens
) ta
WHERE occurred_at >= NOW() - INTERVAL '1 hour'
GROUP BY model, vendor_pricing_version;
```

---

## 13. Known Issues

| # | Issue | Workaround | Status |
|---|---|---|---|
| 1 | **Vendor mid-period price changes.** Vendors occasionally update pricing without advance notice. Historical cost reconstruction must use the price table that was in effect at request time. | `vendor_pricing_version` captures the snapshot used. Migration §11.1 documents the additive-only convention. | Mitigated by design. |
| 2 | **Vendor omits `usage` on some streamed responses.** OpenAI legacy `/v1/completions`, certain Anthropic streaming edge cases, and Bedrock invoke-with-response-stream sometimes return the final chunk without a usage block. | Gateway falls back to client-side token counting via `tiktoken` (OpenAI) / Anthropic's tokenizer. `actual_cost_source = "estimated"` flags the row. Dashboards filter or call out the estimated subset. | Mitigated; Phase 2 may add per-vendor retry-with-usage-request. |
| 3 | **MVP waste-decomposition heuristics are coarse.** Direct-payload-waste via embedding similarity has ~15% false-positive rate on highly cohesive contexts; induced-output-waste estimation depends on retry detection in [[inferred-labels]] which has its own accuracy limits. | Phase 2 introduces LLM-as-judge sampling at 5–10% rate per [source-spec-v0.2 §3.8](./source-spec-v0.2.md) to refine. `scoring_version` makes the refinement traceable per-row. | MVP-acceptable; refined Phase 2. |
| 4 | **Tool-call token attribution is gateway-computed, not vendor-reported.** No vendor reports "of these N input tokens, M were tool definitions." The gateway counts tool-related tokens by Hyperscan-tagging the request structure before forwarding. | Best-effort; documented as approximate. Variance vs. ground-truth measured at ~3% in MVP test set. | Acceptable; revisit if customers query directly. |
| 5 | **Reasoning tokens (OpenAI o-series) are billed at output rate but conceptually different.** They're not visible in the response stream and inflate output_token count without corresponding output text. | Stored separately in `reasoning_tokens` so dashboards can break them out. Cost is computed correctly because OpenAI reports them within `completion_tokens`. | Disclosed in docs; no fix needed. |
| 6 | **`actual_cost_usd` precision rounding at extremely small per-token rates.** Embedding models price at $0.00002/1k (Haiku-tier); a 1-token request rounds to $0.00000002 which fits numeric(10,6) only as `0.000000` (truncated). | Aggregation at the day-level is unaffected. Per-request display below sub-cent precision is documented as truncated. | Disclosed; revisit if sub-cent precision becomes a customer ask. |
| 7 | **Cache_creation_input_tokens billing is one-time but tied to a specific cache key.** A cache write that gets evicted before any read produces "billed but useless" cost. The gateway has no visibility into vendor cache lifecycle. | Anomaly is detectable retrospectively: rows with high `cache_creation_input_tokens` but zero subsequent `cached_input_tokens` from the same prefix indicate eviction. Phase 2 dashboard surfaces this. | Acceptable for MVP. |

---

## 14. Cross-References & Related Documentation

### Within AIQG
- [[response-event]] — parent table; this model is a JSONB column on it.
- [[request-event]] — companion to response-event; identifies the source request for cost attribution.
- [[event-timestamps]] — sibling JSONB column on response-event for latency decomposition.
- [[inferred-labels]] — sibling JSONB column; supplies `retry_of_previous` flag that drives induced-waste heuristic.
- [[tag-set]] — sibling JSONB column; supplies CLEAR Assurance tags (no overlap with cost).
- [[aggregated-metrics]] — downstream rollups of fields in this model.
- [[account]] — owns the response events via `tenant_id`; governs retention and region.
- [[audit-log-entry]] — receives `cost_data_read` events when this model is queried.

### Spec & architecture
- [source-spec-v0.2.md §2.1](./source-spec-v0.2.md) — CLEAR Cost dimension definition; three-category waste model.
- [source-spec-v0.2.md §3.7](./source-spec-v0.2.md) — per-request capture field categories.
- [build-vs-reuse.md §1.2](./build-vs-reuse.md#12-non-breaking-change-constraint) — non-breaking-change constraint on `EstimateCost()`; addition of `ActualCost()`.
- [build-vs-reuse.md §2.12](./build-vs-reuse.md#212-21-25--clear-dimension-measurement) — CLEAR dimension measurement and `pkg/clear/` package layout.
- [build-vs-reuse.md §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go) — decision: scoring runs gateway-side; rescoring strategy.
- [build-vs-reuse.md §7.5](./build-vs-reuse.md#75-spec-6-inherited-open-question-triage) — composite-weighting and threshold decisions.

### TAS-wide
- `tas-llm-router/internal/pricing/` — `EstimateCost()` source (unchanged).
- `tas-llm-router/pkg/clear/cost.go` — new home for `ActualCost()` and waste scorer.
- `aether-shared/data-models/tas-llm-router/request-format.md` — request structure that drives token counting.
- `aether-shared/data-models/tas-llm-router/response-format.md` — response structure containing the vendor `usage` field this model reads.
- `aether-shared/data-models/cross-service/mappings/id-mapping-chain.md` — `tenant_id` → Aether Space → Keycloak realm chain.

---

## Changelog

| Version | Date | Author | Change |
|---|---|---|---|
| v1.0.0 | 2026-05-31 | TAS Platform | Initial spec draft |
