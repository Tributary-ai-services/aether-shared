# AIQG Metrics — Computation Reference

**Status:** v1 (starting point) · **Date:** 2026-06-24

How every AIQG metric is computed, end to end. For each metric: what it means, its
inputs, the **exact formula**, when it's `nil`/absent, where it's computed, and how
it rolls up into the dashboard reports.

> Scope: the metric *computation*. Schemas live in [`token-accounting.md`](./token-accounting.md),
> [`response-event.md`](./response-event.md), [`aggregated-metrics.md`](./aggregated-metrics.md);
> the NIST/tag taxonomy in [`tag-set.md`](./tag-set.md) and [`policy-pattern.md`](./policy-pattern.md).

---

## 0. The pipeline (where computation happens)

```
 request close (tas-llm-router)
   │  pkg/clear.Compute()  → CLEAR dimension scores (per request)
   │  pkg/clear.ActualCost()/DecomposeCost() → cost + cost-waste decomposition
   │  middleware.MapPatternToNIST() → NIST characteristic counts (from Gatekeeper findings)
   ▼
 ResponseEvent  (pkg/aiqg/events/builder.go)
   ├─► Loki   (emitter.go promotes metric fields as log fields/labels) ── fallback read path
   └─► Kafka  tas.aiqg.events.v1 (full envelope)
              ▼
        Spark aggregator (tas-spark-jobs/jobs/aiqg_aggregator)
              ▼
        TimescaleDB  aiqg.event_metrics (per-event) + event_metrics_1m/1h (continuous aggregates)
              ▼
        aiqg-dashboard-be (timescale/client.go, handlers/reports.go) ── TS-first, Loki fallback
              ▼
        Reports (Health Report, CLEAR Scorecard, Cost, NIST, Reliability, …)
```

**Two rules that recur below:**
- **`nil` ≠ 0.** Every CLEAR dimension is a nullable pointer. `nil` = "not scored / no signal" (excluded from averages and from the composite); `0` = "scored, and it's bad." Distinguish them everywhere.
- **Scores are computed once, at the gateway.** Spark and the dashboard **aggregate** pre-computed scores; they never re-derive them. A formula change bumps `clear.Version` (`clear-v0.2-cost-decomp`) so a re-score job can find stale rows.

---

## 1. CLEAR dimension scores (per request, 0–100)

All five live in `tas-llm-router/pkg/clear/`. `Compute(Input)` (`clear.go:127`) runs each
scorer and then the composite. Bands (spec §2.2): **Healthy ≥ 75 · Marginal 50–74 · Failing < 50**
(Assurance uses stricter tiers — see §1.4).

### 1.1 Latency — `scoreLatency` (`clear.go:155`)
- **Input:** `EndToEndMs` (wall-clock, from the timing snapshot), `Workflow`, `HTTPStatus`.
- **Formula:** `score = 100 − 50 × (actual / target − 1)`, clamped to `[0,100]`.
  - `target` = workflow SLA (ms): `single_turn_qa`/unknown 3000 · `classification_extraction` 5000 · `rag` 5000 · `summarization` 15000 · `code_generation` 30000 · `agentic` 30000 (`slaMsForWorkflow`).
  - Anchors: `actual=target → 100` · `1.5×target → 75` · `2×target → 50` · `≥3×target → 0`.
- **nil when:** `EndToEndMs` not stamped (request never completed) **or** `HTTPStatus == 0` (gateway-blocked → latency meaningless).

### 1.2 Cost — `scoreCost` (`cost.go:152`)
- **Input:** `Vendor`, `Model`, `PromptTokens`, `CompletionTokens`, `HTTPStatus`.
- **Step 1 — dollar cost (`DollarCost`, `cost.go:75`):** `cost_usd = (prompt/1000)×inputRate + (completion/1000)×outputRate`, where rates are USD/1k tokens from the in-binary `modelPricing` table (`PricingVersion = pricing-v2026-06-05`). Unknown `vendor:model` ⇒ unpriced.
- **Step 2 — score (log curve):** `score = 100 − 25 × log10(cost_usd × 10000)`, clamped to `[0,100]`.
  - Anchors: `$0.0001 → 100` · `$0.001 → 75` · `$0.01 → 50` · `$0.10 → 25` · `$1.00+ → 0` (25 points per decade).
- **nil when:** `HTTPStatus == 0`, both token counts zero/absent, or pricing unknown for the model.

### 1.3 Efficacy — `scoreEfficacy` (`efficacy.go:29`)
- **Input:** vendor `FinishReason`, `HTTPStatus`. (MVP: finish-reason only; structural-validity/groundedness sub-metrics deferred.)
- **Formula:** normalize the vendor's finish reason (`normalizeFinishReason`), then map:
  - `stop` / `tool_calls` → **100** (clean completion or tool-use success)
  - `length` → **60** (truncated at max_tokens)
  - `content_filter` → **0** (vendor blocked output)
  - Normalization: Anthropic `end_turn`/`stop_sequence`→`stop`, `max_tokens`→`length`, `tool_use`→`tool_calls`; OpenAI passthrough.
- **nil when:** `HTTPStatus == 0`, `FinishReason == ""`, or an unknown reason after normalization (so dashboards flag it for a code update).

### 1.4 Assurance — `scoreAssurance` (`assurance.go:39`)
- **Input:** `AssuranceScanRan`, inbound + outbound Gatekeeper findings bucketed by severity.
- **Formula:** bucket on the **worst** severity present across either direction (not a count — one critical finding shouldn't be diluted):
  - no findings → **100** · only `low` → **95** · any `medium` → **80** · any `high` → **50** · any `critical` → **0**.
- **nil when:** no scan ran (`AssuranceScanRan == false`). Distinct from "scan ran, found nothing" = **100**.
- **Stricter bands** (asymmetric consequences): Healthy ≥ 90 · Marginal 75–89 · Failing < 75.

### 1.5 Reliability — `scoreReliability` (`reliability.go:32`)
- **Input:** `AttemptCount`, `FallbackUsed`, `HTTPStatus`. (MVP: a gateway-fulfillment proxy for the spec's pass@k.)
- **Formula:** base by attempt count — `1 → 100`, `2 → 75`, `3 → 50`, `≥4 → 0`; then **−25 if `FallbackUsed`** (clamped ≥ 0).
- **nil when:** `HTTPStatus == 0` (no attempt made) or `AttemptCount ≤ 0` (signal not captured).
- **Honest framing:** measures the gateway's ability to deliver a vendor response, *not* cross-run model consistency.

### 1.6 Composite — `composite` (`clear.go:215`)
- **Formula:** **equal-weight integer mean of the non-`nil` dimensions** — `sum(present dims) / count(present dims)` (integer division). `WeightsApplied = "equal-weight-non-nil"`.
- **nil when:** zero dimensions scored.
- Per-account weighting is a future slice (`account.scoring_weights`); today every present dimension counts 1/N.

---

## 2. Cost & token accounting (per request)

Computed in `pkg/aiqg/events/builder.go` from the vendor response; stored in the
`token_accounting` JSONB ([`token-accounting.md`](./token-accounting.md)).

- **Token counts:** `prompt_tokens`, `completion_tokens`, `total_tokens` — taken from vendor usage (or gateway estimate; `actual_cost_source` records which).
- **Billed cost:** `input_cost_usd`, `output_cost_usd`, `total_cost_usd` via `clear.LookupPricing` + the per-1k arithmetic in §1.2. `actual_cost_usd` (= billed total in Contract v1) is the **invariant denominator** the waste decomposition is bounded against.

---

## 3. Cost decomposition (per priced request) — `DecomposeCost` (`cost_decomposer.go:61`)

Projected (Contract v1) breakdown of the billed cost into source-spec §1.4/§2.1 categories,
plus per-method projected payload-reduction savings. Cheap inline heuristic (<1ms, no network).
**Only runs when the request is priced** (never fabricate waste on unpriced traffic).

- **Context-Efficiency Ratio (CER):** `r = completion / max(prompt,1)`; `CER = r / (r + 0.5)`; **×0.7 if inbound bloat/instruction-stuffing was flagged**; clamped `[0,1]`. Interpreting: fraction of input context that "earned its keep." `relFrac = 1 − CER` = projected-droppable fraction.
- **Direct payload waste:** `direct_usd = input_cost_usd × relFrac`; `direct_tokens = round(prompt × relFrac)`.
- **Per-method projected reductions:**
  - `relevance_usd = direct_usd` (relevance/top-K drops the droppable context) — confidence `medium`.
  - `slm_usd = input_cost_usd × 0.25` (assumed SLM-rewrite compression) — confidence `low`.
  - `combined_usd = input_cost_usd × (1 − (1−relFrac)(1−0.25))` (relevance ∘ SLM compounded).
- **Induced output waste:** `0` in v1 (retry linkage is Contract v2).
- **Genuine post-model waste** (no reduction could have saved it): whole `total_cost_usd` when `HTTP ≥ 400` or `finish_reason == content_filter`; else `output_cost_usd` when there were high/critical **outbound** findings; else 0.
- **Bound clamp (invariant #6):** `genuine ≤ budget`, then `direct ≤ budget − genuine` (tokens rescaled to match); per-method ≤ `input_cost_usd`. Guarantees `direct + induced + genuine ≤ actual.TotalUSD`.
- **Gateway-addressable %:** `(direct_usd + induced_usd) / total_cost_usd × 100`.

> Measured/actual reductions (`actual_reduction_*`, efficacy/assurance deltas) are **Contract v2** —
> emitted only when the real extractor runs in shadow/active mode, not by this heuristic.

---

## 4. NIST trustworthiness counts (per request)

Not a score — **counts of Gatekeeper findings mapped to the 4 NIST AI-RMF characteristics.**
Mapping `MapPatternToNIST` (`internal/middleware/nist_classifier.go`); each `pattern_id` →
exactly one characteristic:

| Characteristic (`nist_*` field) | Example pattern_ids |
|---|---|
| `secure_resilient` | `aiqg-role-claim`, `injection-prompt`, `injection-sql`, `injection-xss` |
| `privacy_enhanced` | `cred-*` (api-key, aws, azure, gcp, jwt, oauth, private-key, connection-string), `pii-*` |
| `valid_reliable` | `aiqg-bloated-context`, `aiqg-refusal`, `aiqg-repetition`, `aiqg-hallucination-hedge`, `aiqg-malformed-output`, `aiqg-citation-marker`, `aiqg-vague-prompt`, `aiqg-instruction-stuffing`, `aiqg-unbounded-loop` |
| `safe` | `aiqg-harm-request`, `aiqg-credential-solicitation`, `aiqg-explicit-jailbreak` |

- **Computation:** for each finding the gateway increments the mapped characteristic (`StampNISTFindings`); **inbound + outbound are summed** (distinct events). Emitted as `assurance.nist_findings` → denormalized to `nist_secure_resilient` / `nist_privacy_enhanced` / `nist_valid_reliable` / `nist_safe` integer columns.
- **Report = sum over the window** (counts, not averages). They surface as the Health Report's "Trustworthiness" row and the NIST AI RMF report.

---

## 5. Assurance findings (per request)
- `assurance_inbound_count` / `assurance_outbound_count` — total Gatekeeper findings on the request / response (always emitted, even when 0). `worst_severity` rides along for filtering.
- Per-`pattern_id` counts ride in the `tags` JSONB (and Loki `tag_<id>` fields) — the slicing source for §7 (avoidable cost, groundedness).

---

## 6. Timing decomposition (per request, ms)
From the gateway `TimingCollector` snapshot, promoted to Loki fields (not in the TS continuous aggregates):
`gateway_ingress_ms`, `gateway_overhead_ms`, `gateway_egress_ms`, `vendor_ttfb_ms`, `vendor_ttft_ms`,
`vendor_generation_ms`, `median_inter_token_ms`, and the headline `end_to_end_ms` (the only one stored as a TS column → feeds Latency §1.1 + the p50/p95 percentiles).

---

## 7. Derived report metrics (computed in the dashboard, per window)

These don't exist per-event; the dashboard derives them by scanning `aiqg.event_metrics`
(TS-first; Loki fallback). All windows are `[now − Ndays, now]`.

### 7.1 Headline scalars — `QueryPreviewScalars` (`timescale/client.go:624`)
One aggregate scan: `count(*)` (requests), `sum(total_tokens)`, `sum(total_cost_usd)`,
`avg(clear_*)` for each of the 6 dimensions, `percentile_cont(0.5|0.95) WITHIN GROUP (ORDER BY end_to_end_ms)`
(p50/p95 latency), `sum(assurance_inbound/outbound_count)`, `sum(nist_*)`.

### 7.2 Avoidable cost — `avoidable_cost.go`
Cost attributable to fixable failure modes. Per category, `sum(total_cost_usd)` over events whose
`tags` JSONB contains **any** of the category's pattern_ids (`tags ?| array[...]`):
- **refusal** = `aiqg-refusal` · **bloat** = `aiqg-bloated-context`,`aiqg-instruction-stuffing` · **hedging** = `aiqg-hallucination-hedge`,`aiqg-repetition` · **vague input** = `aiqg-vague-prompt`,`aiqg-unbounded-loop`.
- `avoidable_usd = min(Σ category costs, total_cost_usd)` (cap avoids double-counting multi-match events); `avoidable_pct = avoidable_usd / total_cost_usd × 100`; each category also reports `pct_of_total`.

### 7.3 Groundedness (RAG only) — `groundedness.go`
Over `workflow = 'rag'` events: `rag_count`, `cited_count` (events with `aiqg-citation-marker > 0`),
`total_citations = sum(aiqg-citation-marker)`. Derived: `uncited = rag − cited`,
`cited_pct = cited/rag × 100`, `avg_citations_per_cited = total_citations / cited`. `nil` when no RAG traffic.

### 7.4 Drift (period-over-period) — `drift.go`
Compares the current window to the immediately prior equal window. Metrics: composite (request-count-weighted avg), total cost (sum), p95 latency (Loki — percentiles aren't in the cont-aggs). `delta% = (current − previous) / previous × 100`; direction up/down/flat at a ±1% threshold.

### 7.5 Projected savings — `QueryProjectedSavings` (`client.go:687`)
Sums the Contract-v1 decomposition fields (`projected_direct_payload_waste_usd`, `projected_reduction_{relevance,slm,combined}_usd`, `genuine_*`, `induced_*`) over events where `reduction_mode` is set, JSONPath-extracted from `raw->'data'->'token_accounting'`.

---

## 8. Aggregation rules (how per-event becomes per-window) — VERIFIED 2026-06-24

Verified against the live TimescaleDB cont-aggregate definitions + the read paths in
`internal/timescale/client.go`. DDL: `aether-shared/k8s-shared-infrastructure/timescaledb-migrations/004_aiqg_event_metrics.sql`.

- **`aiqg.event_metrics`** — one row per response. PK `(time, tenant_id, response_event_id)`, `ON CONFLICT DO NOTHING` → idempotent under at-least-once Kafka. Spark **projects pre-computed fields**; it never recomputes scores. `clear_*` and `end_to_end_ms` are stored as `integer`.
- **`event_metrics_1m`** (cont-agg, `GROUP BY time_bucket('1m'), tenant_id, workflow, vendor`):
  - `request_count = count(*)`
  - `avg_<dim> = avg(clear_<dim>) FILTER (WHERE clear_<dim> IS NOT NULL)` for composite/cost/latency/efficacy/assurance/reliability — **nil-excluded**, so unscored dims don't drag the mean.
  - `total_cost_usd = sum(...)`, `total_tokens = sum(...)`.
  - `{min,avg,max}_latency_ms = {min,avg,max}(end_to_end_ms) FILTER (WHERE … IS NOT NULL)`.
  - `inbound_findings/outbound_findings = sum(assurance_*_count)`; `nist_* = sum(nist_*)`.
- **`event_metrics_1h`** (rolls up `event_metrics_1m`): every `avg_*` is **request-count-weighted** — `sum(avg_X × request_count) / NULLIF(sum(request_count), 0)` — so the hourly mean equals the true mean across the minute buckets. `min_latency_ms = min(min_latency_ms)`, `max_latency_ms = max(max_latency_ms)`; cost/tokens/findings/NIST are plain `sum`.
- **Percentiles are NOT in the cont-aggs.** p50/p95/p99 latency always hit the **raw** hypertable via `percentile_cont(q) WITHIN GROUP (ORDER BY end_to_end_ms::float8)` (or Loki `quantile_over_time`).

**Which TS query reads which table** (the load-bearing nuance):

| Reads cont-agg (`_1m`/`_1h`) | Reads raw `event_metrics` |
|---|---|
| `QueryCLEARSeries` (CLEAR-over-time chart) · `QueryCostSeries` (cost-over-time) · `QueryDriftScalars` (drift composite + cost, weighted) | **everything else** — `QueryPreviewScalars` (headlines), `QueryLatencySeries`/percentiles, `QueryAvoidableCost`, `QueryGroundedness`, `QueryVendorBreakdown`, `QueryPeriodSpend`, `QueryProjectedSavings`, `QueryReductionReconciliation`/`Validation`, `QueryAgentRollups`, `QueryFlowRollups`, `QueryStatusCounts`, `QueryExperimentResults`, `QueryTagCounts`, `QueryEvents`/`ByID` |

- `QueryCLEARSeries`/`QueryCostSeries` pick `_1m` for sub-day windows, `_1h` for longer (≈60× fewer rows).
- **TS-first, Loki-fallback:** every report read prefers TimescaleDB; on TS error / unconfigured / empty window it falls back to LogQL over the promoted Loki fields (§1.3). Loki is the **only** source for the latency-decomposition stages (§6) and drift's p95 (percentiles aren't in cont-aggs).

---

## 9. Gotchas worth knowing
- **A `nil` dimension vanishes** — it's omitted from JSON, excluded from the composite, and excluded from `avg()` (the `FILTER (WHERE … IS NOT NULL)`). A flat/empty chart usually means "inputs not plumbed," not "scored zero."
- **Cost score vs cost dollars are different things.** `clear_cost` is a 0–100 log-curve *score*; `total_cost_usd` is the billed dollars. Reports show both.
- **NIST + avoidable-cost + groundedness are count/tag-driven**, so they depend on Gatekeeper having scanned (assurance ran) and on the `tags` JSONB being populated by Spark.
- **Pricing is an in-binary table** (`pkg/clear/cost.go modelPricing`, `PricingVersion`). Unknown models → no Cost score and no dollar cost (not zero — absent).
- **Imported traces (Plan #12)** reuse this exact scorer (`aiqg-import/clear`, vendored) but write an **isolated** `aiqg.imported_event_metrics` table; latency is always `nil` (timing unavailable from logs), so their composite leans on Cost + Efficacy.

---

## 10. Data source per metric (Loki · TimescaleDB · both)

The **default read pattern is TS-first with a Loki fallback** (= "both"): the dashboard reads
TimescaleDB, and on TS error / unconfigured / empty-window it falls back to LogQL over the
fields promoted to Loki (§1.3). The same per-event signal exists in *both* stores; this table is
about which **read path** serves each report. Exceptions (single-source) are called out.

| Metric / report | Source | How |
|---|---|---|
| Headline scalars — request count, tokens, total cost, CLEAR averages (6), latency **p50/p95**, assurance in/out counts, NIST sums | **Both** | `QueryPreviewScalars` (raw hypertable) → concurrent Loki path on TS error/empty |
| CLEAR-over-time series · Cost-over-time series | **Both** | TS cont-aggs `event_metrics_1m`/`_1h` → Loki fallback |
| Status distribution · Provider/vendor breakdown · Tag (findings-by-pattern) counts | **Both** | `QueryStatusCounts` / `QueryVendorBreakdown` / `QueryTagCounts` (tags JSONB) → Loki |
| Events list / detail | **Both** | `QueryEvents` / `QueryEventByID` → Loki feed |
| Avoidable cost · Groundedness (RAG) | **Both** | TS (`tags ?|`, workflow filter) → Loki |
| Projected savings · Reduction reconciliation | **Both** | TS raw-JSONB (`raw->'data'->'token_accounting'`) → Loki |
| Agent rollups · Flow rollups | **Both** | TS-first (agents: only when ≥1 attributed row) → Loki line-capped (≤5000) |
| Drift — composite, cost | **TS** (cont-agg, request-count-weighted) | + Loki fallback |
| Drift — **p95 latency** | **Loki only** | percentiles aren't in the cont-aggs |
| Latency decomposition stages — `gateway_ingress/egress/overhead_ms`, `vendor_ttfb/ttft/generation_ms`, `median_inter_token_ms` | **Loki only** | not denormalized as TS columns; the handler reads Loki by design (panel-refresh latency is fine) |
| Experiment per-variant results + verdict (§11) | **TS only** | no TS ⇒ `source="none"` (zeros, UI still renders); Loki used only for a supplementary status query |
| Experiment quality signals — judge score, pairwise win-rate (§11.7) | **Postgres** (`response_feedback`) | scored by the LLM judge in the gateway, stored as feedback rows; aggregated `avg(value)`/`count(*)` per variant — neither Loki nor the event-metrics TS |
| Imported-trace metrics (Plan #12) | **TS only** | isolated `aiqg.imported_event_metrics` table; no Loki path |

> **Why "both" for most:** Spark batch lag (≈30s–minutes) means a just-emitted event reaches Loki
> before TimescaleDB, so Loki is the resilience/freshness fallback; TS is the fast, percentile-capable
> primary once the row lands. **Loki-only** items are fields never denormalized into TS columns (timing
> stages) or operations TS can't do on a cont-agg (p95). **TS-only** items need columnar joins/group-bys
> Loki can't serve at interactive speed (per-variant stats; the isolated imported table).

---

## 11. Experiment per-variant results, verdict & significance

A/B experiment scoring lives in `aiqg-dashboard-be/internal/handlers/{verdict,significance}.go`
+ `timescale/client.go QueryExperimentResults`. Per-variant metrics are aggregated from the raw
hypertable; the verdict is a non-inferiority gate + objective z-test.

### 11.1 Per-variant metrics — `QueryExperimentResults` (raw `event_metrics`, `GROUP BY experiment_variant`)
Per arm over the window: `requests = count(*)`; `error_rate = avg(CASE WHEN status IN ('vendor_error','gateway_error') THEN 1 ELSE 0 END)`; `avg_cost = avg(total_cost_usd)`, `cost_sd = stddev_samp(total_cost_usd)`; `avg_latency = avg(end_to_end_ms)`, `latency_sd = stddev_samp(end_to_end_ms)`, `p95_ms = percentile_cont(0.95)`; `avg_composite/efficacy/assurance = avg(clear_*)`; `avg_reduction_usd = avg(COALESCE(raw->'data'->'token_accounting'->>'actual_direct_payload_reduction_usd', 0))`. The handler overlays LLM-as-judge + pairwise shadow-eval quality signals from the feedback store.

### 11.2 Gates (`verdict.go`)
- `minVerdictSamples = 30` — fewer requests in **either** arm ⇒ status `insufficient` (abstain).
- `qualityMargin = 0.05` — non-inferiority tolerance (quality deltas are normalized to 0–1; −5pp = the floor).
- `objectiveWinPct = 0.05` — the variant must beat control by ≥5% (relative) on the objective to count as a win.

### 11.3 Objective & "better" — per the experiment's objective (`cost` default · `latency` · `savings`)
`objDelta = pctDelta(variant_mean, control_mean) = (variant − control)/control`. Lower is better for all three (cost/latency/savings test the cost or latency mean). `savings` tests on **cost mean** (cost-basis), not the savings amount.

### 11.4 Quality non-inferiority (strongest signal wins)
`QualityDelta` = (strongest present): **pairwise** shadow-eval `win_rate − 0.5` › **judge** `judge_score − control_judge` › **CLEAR composite** `(variant − control)/100`. Non-inferior iff `QualityDelta ≥ −0.05`. For extraction experiments, two extra gates fire **only when both arms have signal (avg > 0)**: efficacy `(Δ)/100 ≥ −0.05` and assurance `(Δ)/100 ≥ −0.05`. Any gate below −0.05 ⇒ **reject**.

### 11.5 Significance — two-sample z-test on the objective mean (`significance.go`)
`se = sqrt(sdA²/nA + sdB²/nB)` (Welch SE); `z = (meanV − meanC)/se`; two-sided `p = 2·(1 − Φ(|z|))` with `Φ(x)=½·erfc(−x/√2)`; 95% CI `= diff ± 1.96·se` (reported as a fraction of control's mean). Classification: `p < 0.05 → "clear"` · `p ≥ 0.05 → "directional"` · test can't run (n<2 or se ambiguous) → `"insufficient"`.

### 11.6 Per-variant status & experiment recommendation
Per variant: any quality/efficacy/assurance regression < −5pp ⇒ **reject**; else `objDelta ≤ −5%` ⇒ **promote**; `objDelta ≥ +5%` ⇒ **hold** (loses); within ±5% ⇒ **hold** (flat). Experiment-level: a variant is *promotable* only if status=promote **and** significance=`clear` (p<0.05) → recommend "promote X"; multiple ⇒ "pick the largest objective win"; any insufficient arm ⇒ "keep collecting"; else "no clear winner — keep control".

### 11.7 The two quality signals (LLM-judge & pairwise) — how they're produced
§11.4's quality ladder consumes two signals that are **not** computed in the metrics pipeline — they're scored by an LLM judge in `tas-llm-router` and stored as rows in the dashboard's **Postgres `aiqg.response_feedback`** table (not the event-metrics TS / Loki), then aggregated per variant.

**LLM-as-judge (pointwise)** — `tas-llm-router/pkg/aiqg/judge/judge.go` + `internal/server/judge.go`:
- Runs **async off the hot path** on a deterministic sample (`JudgeSamplePct`, CRC32 on event id); a no-op when `JUDGE_MODEL` is unset.
- The judge LLM scores the response against a **workflow-specific rubric** (`RubricVersion=v1`): e.g. `rag` → {faithfulness, answer_relevance} · `code_generation` → {solves_the_prompt, idiomatic} · `single_turn_qa` → {correctness, helpfulness, completeness} · `summarization` → {faithfulness, key_point_coverage, concision} · `agentic` → {goal_completion, correct_tool_use}. Each dimension ∈ [0,1]; **`overall` = the JSON `overall` field or the mean of dimensions**, clamped [0,1]. A parse failure ⇒ **abstain** (excluded, not guessed).
- The gateway POSTs `overall` → dashboard `POST /internal/judge` → a `response_feedback` row `signal_type='judge'`, `value=overall`, with `experiment_id`/`experiment_variant` denormalized.
- **Per variant** (`SignalAggByVariant`): `judge_score = avg(value)`, `judge_samples = count(*)` over `signal_type='judge'` `GROUP BY variant`.

**Pairwise shadow-eval (head-to-head)** — `judge/pairwise.go` + `judgeRunner.shadowEval`:
- Separately sampled (`shadowPct`) on **control-arm** traffic in dry_run/running experiments. Replays the control prompt through each non-control variant's config, then the judge **compares control vs variant for the same prompt** — paired, so it controls for prompt variance. Position bias removed by a deterministic A/B swap (CRC32 on eventId+variant).
- Result `VariantPreference ∈ {0.0 control-better, 0.5 tie, 1.0 variant-better}` (abstain on parse fail) → POST `/internal/judge` `signal_type='judge_pairwise'`, `value=VariantPreference`, on the variant key.
- **Per variant:** `pairwise_win_rate = avg(value)` (`> 0.5` ⇒ beats control head-to-head), `pairwise_samples = count(*)`.

So both are just `avg(value)`/`count(*)` over the relevant `response_feedback` rows; the verdict then prefers pairwise › judge › CLEAR composite (§11.4).

> **Operational status — verified 2026-06-25 (dev cluster); point-in-time, re-check when the deployment changes.**
> - **Pointwise judge: RUNNING** on the AIQG gateway `llm-router-aiqg` — `AIQG_JUDGE_MODEL=claude-haiku-4-5-20251001`, `AIQG_JUDGE_SAMPLE_PCT=20`. Confirmed by live data in `aiqg.response_feedback`: 55 `judge` rows (18 in the trailing 7 days, latest 2026-06-22), workflow-tagged, rubric `v1`, scores ~0.81 avg.
> - **Pairwise shadow-eval: OFF** — `AIQG_SHADOW_EVAL_PCT=0`. Only 4 stale `judge_pairwise` rows (all 2026-06-14, none recent).
> - **Consequence:** with no recent pairwise samples, the verdict's active quality signal today is the **pointwise judge** (§11.4 falls through pairwise → judge → CLEAR composite). The judge scores only AIQG-attributed, non-streaming responses, so it covers the AIQG gateway path, not the internal `llm-router`.

### 11.8 Judge calibration (is the judge trustworthy?) — `GET /judge/calibration`
Reference-free agreement between the judge and **human** feedback on the same response (`JudgeCalibrationPairs` joins `signal_type='judge'` rows to human rows on `response_event_id`). Human signals normalized to [0,1]: thumb/accept_reject `(value+1)/2` · rating(1–5) `(value−1)/4` · task_success unchanged. Then: **agreement rate** = fraction where both ≥ 0.5 or both < 0.5; **bias** = `mean(judge) − mean(human)` (+ ⇒ lenient judge); **MAE** = `mean(|judge − human|)`; computed overall and per workflow.

---

## 12. Reduction reconciliation (projected-vs-measured calibration)

`handlers/reduction_reconciliation.go` + `timescale/client.go QueryReductionReconciliation`. Compares the
always-on **projected** payload-reduction heuristic (§3) against the **measured** Contract-v2 savings when
the real extractor ran. Filter: `reduction_mode IN ('shadow','active')` (sampled traffic only).

- **Summed per window** (raw hypertable, JSONB-extracted from `raw->'data'->'token_accounting'`): projected `projected_reduction_{relevance,slm,combined}_usd`; measured `actual_reduction_{relevance,slm}_usd` and `actual_direct_payload_reduction_usd`.
- **Per-method pairs** (`reconPair`): `relevance` = projected vs actual relevance; `slm` = projected vs actual SLM; `combined` = `projected_reduction_combined_usd` vs `actual_direct_payload_reduction_usd` (measured direct reduction).
- **Calibration ratio** = `actual_usd / projected_usd` per pair (nil/omitted when `projected_usd == 0`). `> 1` ⇒ heuristic **under**-projected (real savings exceeded the estimate); `< 1` ⇒ over-projected; `≈ 1` ⇒ well calibrated.
- **Empty guard:** `sampled_requests == 0` ⇒ returns nil (no card). **TS-first, Loki-fallback** (`reduction_mode=~"shadow|active"`), `source` = `"timescale"|"loki"`.

---

## 13. Per-agent & per-flow rollups

`timescale/client.go QueryAgentRollups`/`QueryFlowRollups` over the raw hypertable; `handlers/agents.go`/`flows.go`.
Both are TS-first with a Loki line-capped (≤5000-event) fallback.

### 13.1 Agent rollups — `GROUP BY` the `by` dimension
- `by`: **agent** (`COALESCE(NULLIF(agent_name,''),NULLIF(agent_id,''),'(unattributed)')`) · **user** (`user_id`) · **client_ip** (`client_ip`). `by=source_app` is **Loki-only** (no TS column historically — note: `source_app` now exists on the table, but the rollup path still routes source_app to Loki).
- Aggregates: `packets = count(*)`; `flows = count(DISTINCT NULLIF(COALESCE(flow_id,conversation_id),''))`; `sum(total_tokens)`, `sum(total_cost_usd)`; `errors = count(*) FILTER (status NOT IN ('success','policy_blocked'))`; `blocked = count(*) FILTER (status='policy_blocked')`; **`flagged = count(*) FILTER (assurance_inbound_count + assurance_outbound_count > 0)`**; `first/last = min/max(time)`; `vendors/types = string_agg(DISTINCT …)`; `identity_source = mode() WITHIN GROUP (ORDER BY identity_source)` (the dominant tier). Ordered by `packets DESC`.
- **Auto-cutover:** uses TS only if the result has ≥1 *attributed* row (`Key ≠ '(unattributed)'`); otherwise falls back to Loki (covers the pre-Spark-backfill window).

### 13.2 Flow rollups — `GROUP BY COALESCE(NULLIF(flow_id,''), conversation_id)`
- `field` = which key (`flow_id`|`conversation_id`); `identity` = first non-empty of `max(user_id)`→`agent_name`→`agent_id`→`principal_id`→`client_ip`→`'—'`.
- Aggregates: `steps = count(*)`; `sum(total_tokens)`, `sum(total_cost_usd)`; errors/blocked/flagged (same definitions as agents); span from `min/max(time)`; `vendors/types = string_agg(DISTINCT …)`. Optional exact-match filters: `agent_id`/`user_id`/`client_ip`. Ordered by `last DESC`.
- **Cutover:** uses TS when the result is non-empty; else Loki.

> "Flagged" everywhere = an event with **any** Gatekeeper finding (`assurance_inbound_count + assurance_outbound_count > 0`) — same definition in the TS `FILTER` and the Loki in-memory aggregation.

---

## 14. Source map

| Area | Files |
|---|---|
| CLEAR dimension formulas (§1) | `tas-llm-router/pkg/clear/{clear,cost,efficacy,assurance,reliability}.go` |
| Cost decomposition (§3) | `tas-llm-router/pkg/clear/cost_decomposer.go` |
| Event build + emit (§0,§1.3) | `tas-llm-router/pkg/aiqg/events/{builder,emitter,event}.go` |
| NIST mapping/stamping (§4) | `tas-llm-router/internal/middleware/{nist_classifier,aiqg_routing}.go` |
| Spark rollup (§8) | `tas-spark-jobs/jobs/aiqg_aggregator/{main,schema}.py` |
| TS continuous aggregates (§8) | `aether-shared/k8s-shared-infrastructure/timescaledb-migrations/004_aiqg_event_metrics.sql` (verified live) |
| Headline / derived reports (§7) | `aiqg-dashboard-be/internal/timescale/client.go`, `internal/handlers/{reports,drift,avoidable_cost,groundedness,metrics}.go` |
| Experiment verdict + significance (§11) | `aiqg-dashboard-be/internal/handlers/{verdict,significance,experiments}.go` + `timescale/client.go QueryExperimentResults` |
| Reduction reconciliation (§12) | `aiqg-dashboard-be/internal/handlers/reduction_reconciliation.go` + `timescale/client.go QueryReductionReconciliation` |
| Agent/flow rollups (§13) | `aiqg-dashboard-be/internal/handlers/{agents,flows,rollup}.go` + `timescale/client.go Query{Agent,Flow}Rollups` |

---

**Verification status (2026-06-24):** §1–3 transcribed directly from `pkg/clear`. §8 aggregation
verified against the **live** TimescaleDB cont-aggregate definitions (`event_metrics_1m`/`1h`) and the
read-path map confirmed against `client.go`. §11–13 from a code sweep of `verdict.go`/`significance.go`,
`reduction_reconciliation.go`, and `Query{Agent,Flow,Experiment}*`. Pricing rates (§1.2) and the cont-agg
rollup policies are point-in-time — re-confirm `PricingVersion` / `clear.Version` when those bump.

---

## 15. Worked numeric examples

One concrete substitution per metric. All scores are integers (`int16`); intermediate math is real-valued, truncated at the end (`Score(x)` truncates toward zero).

### 15.1 Latency (§1.1)
A `rag` request (target = 5000 ms), actual end-to-end = **6000 ms**:
`100 − 50×(6000/5000 − 1) = 100 − 50×0.20 = 100 − 10 = 90` → **90 (Healthy)**.
A `code_generation` request (target 30000 ms) taking **45000 ms**: `100 − 50×(1.5−1) = 75` → **75** (the Healthy↔Marginal edge).

### 15.2 Cost dollars + Cost score (§1.2)
`gpt-4o` (in 0.00250, out 0.01000 per 1k), prompt 1000 / completion 500:
`DollarCost = (1000/1000)×0.00250 + (500/1000)×0.01000 = 0.00250 + 0.00500 = $0.00750`.
Score: `100 − 25×log10(0.00750 × 10000) = 100 − 25×log10(75) = 100 − 25×1.8751 = 100 − 46.88 = 53.1` → **53 (Marginal)**.

### 15.3 Efficacy (§1.3)
Anthropic `finish_reason = "end_turn"` → normalizes to `stop` → **100**. `"max_tokens"` → `length` → **60**. `"refusal"`-style `content_filter` → **0**.

### 15.4 Assurance (§1.4)
Findings = {inbound: 1 medium, 2 low; outbound: 0}. Worst severity = **medium** → **80**. Add one outbound `critical` → worst = critical → **0** (one critical floors it regardless of the clean checks).

### 15.5 Reliability (§1.5)
`attempt_count=2, fallback_used=false` → base **75**. Same with `fallback_used=true` → `75 − 25 =` **50**. `attempt_count=1, fallback=false` → **100**.

### 15.6 Composite (§1.6)
All five present — Cost 53, Latency 90, Efficacy 100, Assurance 80, Reliability 75:
`(53+90+100+80+75)/5 = 398/5 = 79.6` → integer division → **79**.
Only two present (no scan, no timing, single attempt) — Cost 53, Efficacy 100: `(53+100)/2 = 76` → **76**. (The nils simply don't participate.)

### 15.7 Cost decomposition (§3)
`gpt-4o`, prompt 2000 / completion 200, HTTP 200, no findings:
- `input_cost = (2000/1000)×0.00250 = $0.00500`; `output_cost = (200/1000)×0.01000 = $0.00200`; **total = $0.00700**.
- `r = 200/2000 = 0.10`; `CER = 0.10/(0.10+0.5) = 0.1667`; `relFrac = 0.8333`.
- `direct_usd = 0.00500 × 0.8333 = $0.004167`; `direct_tokens = round(2000×0.8333) = 1667`.
- `relevance_usd = $0.004167`; `slm_usd = 0.00500×0.25 = $0.00125`; `combined_usd = 0.00500×(1 − 0.1667×0.75) = 0.00500×0.875 = $0.004375`.
- `induced = 0`, `genuine = 0`; **`gateway_addressable_pct = 0.004167/0.00700 = 59.5%`**.

*Bloat variant* (`InboundBloatFindings > 0`): `CER = 0.1667×0.7 = 0.1167` → `relFrac = 0.8833` → `direct = $0.004417` → addressable **63.1%** (flagged padding ⇒ more droppable).
*Failure variant* (`HTTP 500`): `genuine = total = $0.00700`; bound clamp ⇒ `direct = min(0.004167, 0.00700−0.00700) = $0` → addressable **0%** (nothing reduction could have saved).

### 15.8 NIST (§4)
A request with inbound `pii-email` + inbound `injection-prompt`, and outbound `aiqg-hallucination-hedge`:
`nist_privacy_enhanced=1`, `nist_secure_resilient=1`, `nist_valid_reliable=1`, `nist_safe=0`; `assurance_inbound_count=2`, `assurance_outbound_count=1`.

### 15.9 Avoidable cost (§7.2)
Window `total_cost_usd = $10.00`; tagged sums — refusal $0.50, bloat $1.20, hedging $0.30, vague $0.00:
`avoidable = min(0.50+1.20+0.30+0.00, 10.00) = $2.00`; `avoidable_pct = 20%`; per-category `pct_of_total`: bloat 12%, refusal 5%, hedging 3%.

### 15.10 Groundedness (§7.3)
`rag_count=100`, `cited=80`, `total_citations=240`: `uncited = 20`, `cited_pct = 80%`, `avg_citations_per_cited = 240/80 = 3.0`.

### 15.11 Drift (§7.4)
Composite current 82 vs previous 78: `(82−78)/78 × 100 = +5.13%` → **up**. Total cost $12 vs $10: **+20% up**.

### 15.12 1m→1h weighted rollup (§8)
Two minute-buckets: A = 10 requests @ avg_composite 80, B = 30 @ 90.
1h: `(80×10 + 90×30)/(10+30) = 3500/40 = 87.5`. (A naive `(80+90)/2 = 85` would be wrong — the weighting keeps the rollup equal to the true mean.)

### 15.13 Experiment verdict + significance (§11)
Control: n=100, avg_cost $0.010, cost_sd $0.004, composite 85. Variant: n=100, avg_cost $0.009, cost_sd $0.004, composite 84. Objective = cost.
- `objDelta = (0.009−0.010)/0.010 = −0.10` → **10% cheaper** (≤ −5% win).
- Quality: `(84−85)/100 = −0.01 ≥ −0.05` → **non-inferior**. Per-variant status = **promote**.
- z-test: `se = √(0.004²/100 + 0.004²/100) = √(3.2e-7) = 0.0005657`; `z = −0.001/0.0005657 = −1.77`; `p = 2×(1−Φ(1.77)) ≈ 0.077` → **directional** (p ≥ 0.05).
- ⇒ **NOT promotable** (promotion needs `significance = clear`); experiment recommendation: *"no clear winner yet — keep control."*

Same effect size at **n=400** each: `se = √(0.004²/400 ×2) = 0.0002828`; `z = −3.54`; `p ≈ 0.0004` → **clear**; 95% CI on the cost delta `= −0.001 ± 1.96×0.0002828 = [−15.5%, −4.5%]` of control → entirely negative ⇒ **promote the variant**. (4× the samples flips the *same* 10% win from "directional" to a confident "promote.")

### 15.14 Reduction reconciliation (§12)
Relevance: projected $12.50, measured $13.40 → ratio `13.40/12.50 = 1.072` (heuristic **under-projected** by 7.2%). SLM: $8.20 vs $7.95 → 0.969 (over-projected). Combined: $20.70 vs measured-direct $21.35 → 1.031.

### 15.15 Agent/flow rollup (§13)
Agent "Coder" over the window: 50 events (`packets`), 4 distinct `flow_id`s (`flows=4`), `sum(total_cost_usd)=$0.42`, 2 events with status ∉ {success, policy_blocked} (`errors=2`), 3 events with `assurance_inbound+outbound > 0` (`flagged=3`), `mode(identity_source) = "linked"` (dominant tier). A flow with 5 steps spanning 10:00:00→10:00:42 reports `steps=5`, `span≈42s`.

### 15.16 Judge & pairwise quality signals (§11.7)
**Pairwise** — variant V has 40 head-to-head comparisons: 24 variant-wins (1.0), 6 ties (0.5), 10 control-wins (0.0). `pairwise_win_rate = (24×1 + 6×0.5 + 10×0)/40 = 27/40 = 0.675`; `pairwise_samples = 40`. Verdict quality delta = `0.675 − 0.5 = +0.175` (≥ −0.05 → non-inferior; actually a quality *gain*).
**Judge** (only used if no pairwise) — V `judge_score = avg = 0.82` over 120 judged rows, control `0.80`: delta = `0.82 − 0.80 = +0.02` → non-inferior. Since `pairwise_samples > 0`, the verdict uses pairwise and ignores this.
**Calibration** — judge mean 0.78 vs human-normalized mean 0.72 over the paired responses: `bias = +0.06` (judge slightly lenient); if 0.85 of pairs land on the same side of 0.5, `agreement = 85%`.
