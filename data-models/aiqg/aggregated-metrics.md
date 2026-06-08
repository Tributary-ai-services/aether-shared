# Aggregated Metrics

**Service:** AI Quality Gateway (AIQG)
**Storage:** TimescaleDB hypertables (`aiqg.metrics_1m`, `aiqg.metrics_5m`, `aiqg.metrics_1h`, `aiqg.metrics_1d`) on the shared PostgreSQL/TimescaleDB instance
**Producer:** `aiqg_aggregator` Spark Structured Streaming job (per [build-vs-reuse §4.4](./build-vs-reuse.md#44-spark-job-for-rollups))
**Status:** v1.0.0 — Initial spec draft
**Last updated:** 2026-05-31

---

## 1. Overview

`aggregated-metrics` is the family of pre-computed rollups that powers all AIQG dashboards, the Day-1 report ([source-spec-v0.2 §4.4](./source-spec-v0.2.md)), drift alerts, and tenant-facing CLEAR summaries. The model is **read-optimized**: every dashboard query MUST hit one of the four hypertables (`metrics_1m`, `metrics_5m`, `metrics_1h`, `metrics_1d`) instead of scanning raw `aiqg.response_events`.

**Critical contract — the aggregator does not score, it aggregates.** Per [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go), CLEAR scoring runs **gateway-side at request close** inside `tas-llm-router`. The Spark job reads the already-computed `clear_cost_score`, `clear_latency_score`, `clear_efficacy_score`, `clear_assurance_score`, `clear_reliability_score`, and `clear_composite_score` fields off [[response-event]] and rolls them up. It does **not** re-derive scores, does **not** re-price requests, and does **not** re-classify waste — that work is already done and stored. Re-derivation would (a) require duplicating the scorer in PySpark, (b) introduce score drift between dashboards and per-request audit views, and (c) break the "scoring provenance" guarantee via [[token-accounting]].`scoring_version`.

**Five CLEAR dimensions** ([source-spec-v0.2 §2](./source-spec-v0.2.md)) — every rollup row carries averages and distribution percentiles for **Cost, Latency, Efficacy, Assurance, Reliability** plus the **Composite**. These are the columns that drive the dashboard.

**Why TimescaleDB, not ClickHouse / Druid / Pinot:**

1. Already deployed on the shared PostgreSQL instance — no new infra to provision (per [build-vs-reuse §7.1](./build-vs-reuse.md#71-aggregate-storage--decided-timescaledb)).
2. Continuous aggregates eliminate the need for a separate Spark job per window — only the `metrics_1m` table is Spark-fed; 5m/1h/1d are derived inside TimescaleDB on a refresh schedule.
3. Hypertable partitioning + native compression delivers sub-100ms p95 on 90 days of 1h-grain data.
4. Standard PostgreSQL clients work — Grafana, the Day-1 report tool, and ad-hoc `psql` all "just work" without a separate analytics driver.

**Relationship to raw events:** [[request-event]] + [[response-event]] are the source of truth for forensics, audit, and re-scoring. Aggregated metrics are the source of truth for **dashboards and trend analysis**. Never join a dashboard query against the raw events tables — go through an aggregate.

---

## 2. Schema Definition

Four hypertables, identical column shape, different bucket sizes. All partitioned on `bucket_start` (timestamptz). The canonical row shape (using `metrics_1m` as the exemplar) is:

```sql
CREATE TABLE aiqg.metrics_1m (
  -- — Bucket identity (composite PK) —
  bucket_start              TIMESTAMPTZ      NOT NULL,    -- minute-aligned UTC
  tenant_id                 UUID             NOT NULL,
  scope_type                aiqg.scope_kind  NOT NULL,    -- enum, see §3.1
  scope_key                 TEXT             NOT NULL,    -- '' (empty) for account-level scope

  -- — Volume counters —
  request_count             INTEGER          NOT NULL DEFAULT 0,
  success_count             INTEGER          NOT NULL DEFAULT 0,
  error_count               INTEGER          NOT NULL DEFAULT 0,
  policy_blocked_count      INTEGER          NOT NULL DEFAULT 0,
  streaming_count           INTEGER          NOT NULL DEFAULT 0,

  -- — Token totals —
  total_input_tokens        BIGINT           NOT NULL DEFAULT 0,
  total_output_tokens       BIGINT           NOT NULL DEFAULT 0,

  -- — Cost totals (USD) —
  total_actual_cost_usd                   NUMERIC(12,6) NOT NULL DEFAULT 0,
  total_direct_payload_waste_usd          NUMERIC(12,6) NOT NULL DEFAULT 0,
  total_induced_output_waste_usd          NUMERIC(12,6) NOT NULL DEFAULT 0,
  total_genuine_post_model_waste_usd      NUMERIC(12,6) NOT NULL DEFAULT 0,

  -- — Latency percentile sketches (t-digest, materialized via TimescaleDB toolkit) —
  end_to_end_ms_pct          tdigest        NOT NULL,
  gateway_overhead_ms_pct    tdigest        NOT NULL,
  vendor_ttft_ms_pct         tdigest        NOT NULL,
  vendor_generation_ms_pct   tdigest        NOT NULL,
  network_round_trip_ms_pct  tdigest        NOT NULL,

  -- — CLEAR scores (already computed gateway-side, only aggregated here) —
  clear_cost_avg          NUMERIC(5,4)      NOT NULL DEFAULT 0,    -- [0.0, 1.0]
  clear_latency_avg       NUMERIC(5,4)      NOT NULL DEFAULT 0,
  clear_efficacy_avg      NUMERIC(5,4)      NOT NULL DEFAULT 0,
  clear_assurance_avg     NUMERIC(5,4)      NOT NULL DEFAULT 0,
  clear_reliability_avg   NUMERIC(5,4)      NOT NULL DEFAULT 0,
  clear_composite_avg     NUMERIC(5,4)      NOT NULL DEFAULT 0,

  clear_cost_pct          tdigest           NOT NULL,
  clear_latency_pct       tdigest           NOT NULL,
  clear_efficacy_pct      tdigest           NOT NULL,
  clear_assurance_pct     tdigest           NOT NULL,
  clear_reliability_pct   tdigest           NOT NULL,
  clear_composite_pct     tdigest           NOT NULL,

  -- — Tag distribution (top-N inferred labels in this bucket) —
  tag_counts              JSONB             NOT NULL DEFAULT '{}',

  -- — Quality / safety signal counters —
  validity_passed_count           INTEGER   NOT NULL DEFAULT 0,
  hedge_phrase_dense_count        INTEGER   NOT NULL DEFAULT 0,
  retry_in_window_count           INTEGER   NOT NULL DEFAULT 0,
  nist_violations_count           INTEGER   NOT NULL DEFAULT 0,
  pii_in_output_count             INTEGER   NOT NULL DEFAULT 0,

  -- — Provenance —
  scoring_version         TEXT              NOT NULL,    -- majority value in bucket

  PRIMARY KEY (bucket_start, tenant_id, scope_type, scope_key)
);

SELECT create_hypertable(
  'aiqg.metrics_1m', 'bucket_start',
  chunk_time_interval => INTERVAL '6 hours'
);

CREATE INDEX ON aiqg.metrics_1m (tenant_id, scope_type, scope_key, bucket_start DESC);
```

`metrics_5m`, `metrics_1h`, `metrics_1d` have identical columns and different `chunk_time_interval` (1 day, 7 days, 30 days respectively).

Latency p50/p95/p99 are exposed via the `approx_percentile()` accessor over the `tdigest` columns — Grafana panels call `approx_percentile(0.95, end_to_end_ms_pct)` directly without materializing the percentile in the table (one sketch supports any quantile).

---

## 3. Fields Reference

### 3.1 Bucket identity

| Field | Type | Required | Description |
|---|---|---|---|
| `bucket_start` | timestamptz | yes | UTC, aligned to the window size (1m table: minute-aligned; 1h table: hour-aligned). Validation enforced — see §4. |
| `tenant_id` | UUID | yes | Tenant owner of the bucketed rows. Sourced from [[account]].tenant_id on each event. Every query MUST filter on this. |
| `scope_type` | enum | yes | One of `account`, `workflow`, `route`, `model`, `endpoint`, `source_app`. Determines what `scope_key` is keyed on. Additive — new values appended without breaking existing rows. |
| `scope_key` | text | yes | The value of the scope dimension. Examples below. Empty string `''` (never NULL) for `scope_type='account'` so the composite PK works. |

**`scope_type` / `scope_key` examples:**

| `scope_type` | `scope_key` | Source field on [[response-event]] |
|---|---|---|
| `account` | `''` | (no sub-dimension — entire tenant) |
| `workflow` | `rag`, `summarization`, `chat` | `inferred_labels.workflow_classification` |
| `route` | `production_strict`, `dev_loose` | `routing.route_name` |
| `model` | `gpt-4o-mini`, `claude-3-5-sonnet` | `model` (denormalized on [[response-event]]) |
| `endpoint` | `chat.completions`, `messages` | `routing.endpoint` |
| `source_app` | `aether-be`, `tas-agent-builder` | `client_metadata.source_app` |

One request fans out into multiple rows (one per `scope_type`) — see §11 for the partial-aggregate trick used to avoid double-counting in cross-scope queries.

### 3.2 Volume counters

| Field | Type | Required | Description |
|---|---|---|---|
| `request_count` | int | yes | Total responses observed in the bucket for this scope. |
| `success_count` | int | yes | Rows where [[response-event]].status = `success`. |
| `error_count` | int | yes | Rows where status = `error` (vendor 5xx, gateway internal error, timeout). |
| `policy_blocked_count` | int | yes | Rows where status = `policy_blocked` (refused by [[policy-bundle]] before vendor call). |
| `streaming_count` | int | yes | Rows where `streaming = true` on the response event. |

**Invariant:** `request_count = success_count + error_count + policy_blocked_count` — the three are mutually exclusive. Enforced in §4.

### 3.3 Token totals

| Field | Type | Required | Description |
|---|---|---|---|
| `total_input_tokens` | bigint | yes | Sum of [[token-accounting]].input_tokens across the bucket. |
| `total_output_tokens` | bigint | yes | Sum of [[token-accounting]].output_tokens. |

Token decomposition (cached vs. fresh vs. tool) is **not** rolled up — those are forensic fields and live only on [[token-accounting]]. Rolling them up would inflate the schema without dashboard payoff in MVP.

### 3.4 Cost totals (USD)

| Field | Type | Required | Description |
|---|---|---|---|
| `total_actual_cost_usd` | numeric(12,6) | yes | Sum of [[token-accounting]].actual_cost_usd. The headline "spend" number. |
| `total_direct_payload_waste_usd` | numeric(12,6) | yes | Sum of [[token-accounting]].direct_payload_waste_usd (NULLs treated as 0 in sum — the source field is null when not measured, see [[token-accounting]] §3.4). |
| `total_induced_output_waste_usd` | numeric(12,6) | yes | Sum of [[token-accounting]].induced_output_waste_estimated_usd. |
| `total_genuine_post_model_waste_usd` | numeric(12,6) | yes | Sum of [[token-accounting]].genuine_post_model_waste_usd. |

**Invariant:** waste decomposition sum ≤ `total_actual_cost_usd` (mirrors the per-row CLEAR invariant in [[token-accounting]] §4 rule 6).

### 3.5 Latency percentile sketches

t-digest sketches (`tdigest` type from the [TimescaleDB Toolkit](https://docs.timescale.com/use-timescale/latest/hyperfunctions/percentile-approximation/)) for the five timing fields tracked on [[event-timestamps]]:

| Sketch column | Underlying field |
|---|---|
| `end_to_end_ms_pct` | `event_timestamps.end_to_end_ms` |
| `gateway_overhead_ms_pct` | `event_timestamps.gateway_overhead_ms` |
| `vendor_ttft_ms_pct` | `event_timestamps.vendor_ttft_ms` |
| `vendor_generation_ms_pct` | `event_timestamps.vendor_generation_ms` |
| `network_round_trip_ms_pct` | `event_timestamps.network_round_trip_ms` |

**Why t-digest, not pre-materialized p50/p95/p99 columns:** materialized percentiles force the dashboard to pick the quantile set at write time. With t-digest, a Grafana panel can ask for p50, p90, p95, p99, p99.9 against the same sketch — and continuous aggregates from 1m → 1h merge the sketches losslessly (`rollup(end_to_end_ms_pct)`).

### 3.6 CLEAR scores

The six CLEAR fields ([source-spec-v0.2 §2](./source-spec-v0.2.md)) — **already computed gateway-side and stored on [[response-event]]**. The aggregator reads and rolls up; it does not re-derive.

| Field (avg) | Field (sketch) | Source on [[response-event]] |
|---|---|---|
| `clear_cost_avg` | `clear_cost_pct` | `clear_cost_score` |
| `clear_latency_avg` | `clear_latency_pct` | `clear_latency_score` |
| `clear_efficacy_avg` | `clear_efficacy_pct` | `clear_efficacy_score` |
| `clear_assurance_avg` | `clear_assurance_pct` | `clear_assurance_score` |
| `clear_reliability_avg` | `clear_reliability_pct` | `clear_reliability_score` |
| `clear_composite_avg` | `clear_composite_pct` | `clear_composite_score` |

All scores are normalized to `[0.0, 1.0]` upstream — the aggregator preserves the range.

### 3.7 Tag distribution

| Field | Type | Required | Description |
|---|---|---|---|
| `tag_counts` | JSONB | yes | Top-N tag frequencies in this bucket, keyed by tag string from [[tag-set]]. N defaults to 50 (configurable). Schema: `{"workflow:rag": 1240, "antipattern:context_bloat": 318, "nist:transparency:violation": 7}`. Empty `{}` when no tags fired. |

The top-N truncation is per-bucket, applied in the Spark job *before* write, not in the query. This bounds JSONB size at ~10KB per row even with high-cardinality tag spaces (e.g. a tenant with 1k distinct workflow patterns).

### 3.8 Quality / safety signal counters

Pre-computed scalar counts of the signals dashboards need to chart but don't want to recompute from tag JSON.

| Field | Type | Required | Source |
|---|---|---|---|
| `validity_passed_count` | int | yes | Sum of 1-when-true on [[response-event]].validity_passed. |
| `hedge_phrase_dense_count` | int | yes | Count where [[inferred-labels]] tagged `efficacy:hedge_dense`. |
| `retry_in_window_count` | int | yes | Count where [[inferred-labels]].retry_of_previous = true. |
| `nist_violations_count` | int | yes | Count of rows whose tag set contains at least one `nist:*:violation` tag (per [[tag-set]] NIST family). |
| `pii_in_output_count` | int | yes | Count where [[inferred-labels]] flagged `assurance:pii_in_output`. |

### 3.9 Provenance

| Field | Type | Required | Description |
|---|---|---|---|
| `scoring_version` | text | yes | The CLEAR scorer version that wrote the scores being aggregated. When a bucket contains a mix (during rolling deploy), the majority value is stored — minority responses are still aggregated, the value just labels the bucket for the operator. Format: `clear-<semver>` (e.g. `clear-1.0.0`). |

---

## 4. Validation Rules

Enforced in the Spark job's pre-write check (`aiqg_aggregator/src/validate.py::validate_bucket()`) AND as CHECK constraints at the database level.

1. **Bucket alignment.** `bucket_start` MUST be aligned to its hypertable's window:
   - `metrics_1m`: `EXTRACT(SECOND FROM bucket_start) = 0 AND EXTRACT(MICROSECONDS FROM bucket_start) = 0`
   - `metrics_5m`: minute mod 5 = 0 AND second = 0
   - `metrics_1h`: minute = 0 AND second = 0
   - `metrics_1d`: hour = 0 AND minute = 0 AND second = 0
2. **Counter exclusivity.** `request_count = success_count + error_count + policy_blocked_count`.
3. **Streaming subset.** `streaming_count <= request_count` — streaming is a flag on a subset of requests.
4. **Token totals non-negative.** All `total_*_tokens >= 0`.
5. **Cost non-negative.** `total_actual_cost_usd >= 0` and each `total_*_waste_usd >= 0`.
6. **Waste decomposition bounded (mirrors [[token-accounting]] §4 rule 6):**
   ```
   total_direct_payload_waste_usd
   + total_induced_output_waste_usd
   + total_genuine_post_model_waste_usd
   <= total_actual_cost_usd + 0.000001    -- epsilon for rounding
   ```
7. **CLEAR avg range.** Each `clear_*_avg` ∈ `[0.0, 1.0]`.
8. **Signal counters bound.** Each of `validity_passed_count`, `hedge_phrase_dense_count`, `retry_in_window_count`, `nist_violations_count`, `pii_in_output_count` ≤ `request_count`.
9. **Tenant required.** `tenant_id IS NOT NULL`. Rows without a tenant are dropped in the aggregator with a metric (`aiqg_aggregator.dropped_no_tenant_total`) — never silently swallowed.
10. **Scope key non-null.** `scope_key` is `''` (empty string) for account-level rows, never SQL NULL — the composite PK requires non-null.

DB-level CHECK constraints encode rules 2, 4, 5, 6, 7 directly. Rules 1, 8, 9, 10 are gated by the aggregator before insert.

---

## 5. Relationships

```
   ┌──────────────────────────────┐
   │ tas.aiqg.request.v1 (Kafka)  │
   │   = [[request-event]]        │
   └───────────────┬──────────────┘
                   │ join on request_event_id
                   ▼
   ┌──────────────────────────────┐
   │ tas.aiqg.response.v1 (Kafka) │
   │   = [[response-event]]       │
   │   includes [[token-accounting]],
   │   [[event-timestamps]],
   │   [[inferred-labels]],
   │   clear_*_score fields       │
   └───────────────┬──────────────┘
                   │ tumbling 1-min window
                   │ + watermark 2 min
                   ▼
   ┌──────────────────────────────┐
   │ aiqg_aggregator (Spark)      │
   │ — reads pre-computed scores  │
   │ — does NOT re-derive         │
   └───────────────┬──────────────┘
                   │ upsert (idempotency key)
                   ▼
   ┌──────────────────────────────┐         ┌──────────────────────┐
   │ aiqg.metrics_1m              │ continuous │ aiqg.metrics_5m   │
   │ (hypertable, 7d hot, 30d cold)│ aggregate │  aiqg.metrics_1h   │
   │                              │ refresh    │  aiqg.metrics_1d   │
   └───────────────┬──────────────┘            └─────────┬──────────┘
                   │                                     │
                   │ read by Grafana, Day-1 report,      │
                   │ drift alerts, tenant CLEAR summary  │
                   ▼                                     ▼
              [[report-snapshot]]  freezes selected aggregates into
              immutable per-tenant report rows for compliance handoff
```

**Upstream:** [[request-event]], [[response-event]], [[token-accounting]], [[event-timestamps]], [[inferred-labels]], [[tag-set]], [[workflow-classification]] (via inferred labels), [[account]].

**Downstream:** [[report-snapshot]] — the Day-1 report ([source-spec-v0.2 §4.4](./source-spec-v0.2.md)) reads `metrics_1h` / `metrics_1d` and freezes a snapshot into an immutable row.

---

## 6. Lifecycle & State Machines

A bucket row's lifecycle:

```
            ┌────────────────┐
   Kafka    │ first event for│
   ─event──▶│ (tenant, scope,│──── Spark micro-batch ───▶┌──────────────┐
            │ bucket_start)  │   (60s trigger interval)  │  upserted to │
            │  arrives       │                           │  metrics_1m  │
            └────────────────┘                           └──────┬───────┘
                                                                │
                  ┌────── more events for the same bucket ──────┤
                  │ (within 2-min watermark)                    │
                  │   ▶ Spark upsert keyed on PK                │
                  ▼                                             │
            ┌────────────────┐                                  │
            │ row updated    │◀─────────────────────────────────┘
            │ in place       │
            └────────┬───────┘
                     │
              watermark expires (T + 2 min)
                     │
                     ▼
            ┌────────────────┐
            │ row FROZEN     │── continuous aggregate ──▶ rolls up into
            │ in metrics_1m  │   refresh policy             metrics_5m, _1h, _1d
            └────────┬───────┘
                     │
              chunk_time_interval boundary
                     │
                     ▼
            ┌────────────────┐
            │ chunk compresses│  (after 24h for metrics_1m)
            │ in place        │
            └────────┬────────┘
                     │
              retention policy
                     │
                     ▼
            ┌────────────────┐
            │ chunk dropped  │  (after 7d for metrics_1m, 90d for _1h, forever for _1d)
            └────────────────┘
```

**Retention policy per table:**

| Table | Compression starts | Retention drop |
|---|---|---|
| `metrics_1m` | 24 h | 7 d |
| `metrics_5m` | 24 h | 30 d |
| `metrics_1h` | 7 d | 90 d |
| `metrics_1d` | 30 d | never |

`metrics_1d` is the long-term archive — one row per tenant per scope per day is cheap (single-digit GB per year per large tenant) and powers year-over-year trend charts.

**Continuous aggregate refresh schedule:**

| Derived table | Source | Refresh interval | Lag |
|---|---|---|---|
| `metrics_5m` | `metrics_1m` | every 1 min | up to 1 min behind real time |
| `metrics_1h` | `metrics_1m` | every 5 min | up to 5 min behind |
| `metrics_1d` | `metrics_1m` | every 1 h | up to 1 h behind |

The lag is intentional — dashboards over `metrics_1h` are explicitly "trailing", not "real time". For real-time views, query `metrics_1m` directly (or `metrics_5m` if the bucket window doesn't matter).

---

## 7. API Examples

There is no REST API for aggregated metrics — consumers (Grafana, Day-1 report, drift detector) query TimescaleDB directly via SQL.

### 7.1 Spend by workflow over last 7 days (Day-1 report §2 "Where cost is being destroyed")

```sql
SELECT
  scope_key                AS workflow,
  SUM(total_actual_cost_usd)             AS spend_usd,
  SUM(total_direct_payload_waste_usd)    AS payload_waste_usd,
  SUM(total_induced_output_waste_usd)    AS induced_waste_usd,
  SUM(total_genuine_post_model_waste_usd) AS post_model_waste_usd,
  SUM(request_count)       AS requests
FROM aiqg.metrics_1h
WHERE tenant_id   = $1
  AND scope_type  = 'workflow'
  AND bucket_start >= NOW() - INTERVAL '7 days'
GROUP BY scope_key
ORDER BY spend_usd DESC
LIMIT 20;
```

### 7.2 P95 latency decomposition over last 24 h (Day-1 report §3 "Latency decomposition")

```sql
SELECT
  approx_percentile(0.95, rollup(end_to_end_ms_pct))         AS p95_end_to_end_ms,
  approx_percentile(0.95, rollup(gateway_overhead_ms_pct))   AS p95_gateway_ms,
  approx_percentile(0.95, rollup(vendor_ttft_ms_pct))        AS p95_vendor_ttft_ms,
  approx_percentile(0.95, rollup(vendor_generation_ms_pct))  AS p95_vendor_gen_ms,
  approx_percentile(0.95, rollup(network_round_trip_ms_pct)) AS p95_network_ms
FROM aiqg.metrics_1h
WHERE tenant_id   = $1
  AND scope_type  = 'account'
  AND bucket_start >= NOW() - INTERVAL '24 hours';
```

`rollup()` is the TimescaleDB Toolkit hyperfunction that merges multiple `tdigest` sketches losslessly — the result percentile is as accurate as if we had queried the raw `end_to_end_ms` column across all responses.

### 7.3 Tag distribution top-10 for tenant X this week

```sql
WITH expanded AS (
  SELECT key AS tag, SUM((value)::int) AS occurrences
  FROM aiqg.metrics_1h, LATERAL jsonb_each_text(tag_counts)
  WHERE tenant_id = $1
    AND scope_type = 'account'
    AND bucket_start >= NOW() - INTERVAL '7 days'
  GROUP BY tag
)
SELECT tag, occurrences
FROM expanded
ORDER BY occurrences DESC
LIMIT 10;
```

### 7.4 Drift detection — current bucket vs trailing 30-day average

```sql
WITH recent AS (
  SELECT clear_composite_avg
  FROM aiqg.metrics_1h
  WHERE tenant_id   = $1
    AND scope_type  = 'account'
    AND bucket_start = date_trunc('hour', NOW()) - INTERVAL '1 hour'
),
baseline AS (
  SELECT AVG(clear_composite_avg) AS baseline_avg
  FROM aiqg.metrics_1d
  WHERE tenant_id   = $1
    AND scope_type  = 'account'
    AND bucket_start >= NOW() - INTERVAL '30 days'
    AND bucket_start <  NOW() - INTERVAL '1 day'
)
SELECT
  recent.clear_composite_avg                            AS current_score,
  baseline.baseline_avg                                 AS baseline_score,
  (recent.clear_composite_avg - baseline.baseline_avg)  AS delta,
  CASE
    WHEN ABS(recent.clear_composite_avg - baseline.baseline_avg) > 0.10
    THEN 'ALERT'
    ELSE 'OK'
  END AS status
FROM recent CROSS JOIN baseline;
```

The 10% threshold is configurable per tenant via the drift detector's config; this query is the canonical implementation.

---

## 8. Cross-Service Integration

| Consumer | How it reads | What it reads |
|---|---|---|
| **Grafana** (TAS dashboards) | PostgreSQL data source pointed at TimescaleDB | `metrics_1h`, `metrics_1d` exclusively. Panels MUST NOT query raw `aiqg.response_events`. |
| **Day-1 Ongoing Report** ([source-spec-v0.2 §4.4](./source-spec-v0.2.md)) | Direct `psql` from the report generator | `metrics_1h` for "this week", `metrics_1d` for longer windows. Snapshot result rows into [[report-snapshot]] for immutability. |
| **Drift detector** | Polls every 5 min | `metrics_1h` (current bucket) vs `metrics_1d` (baseline). |
| **Tenant CLEAR API** (`GET /v1/tenants/:id/clear`) | `tas-llm-router` gateway exposes a read endpoint | `metrics_1d` for the requested window. Caches results 60s in Redis. |
| **Billing reconciliation** | Out-of-band export tool | **Does NOT** read aggregates — joins on [[response-event]] directly for audit-perfect cost figures (see §13 Known Issues). |

**The aggregator is read by the platform; the gateway is read by tenants.** Tenant-facing reads always pass through the gateway's API, never directly against TimescaleDB.

---

## 9. Performance Considerations

**Workhorse index:** `(tenant_id, scope_type, scope_key, bucket_start DESC)` covers virtually every dashboard query and supports both range scans and grouped aggregates.

**Why dashboards never hit `metrics_1m`:** at peak, the 1m table can hold ~1M rows per day per tenant (depending on scope cardinality). A 24h-window scan over 1m is 100ms+ even with the index. Always go through `metrics_1h` (~24 rows/day/scope) for trailing-day views or `metrics_1d` (~1 row/day/scope) for week+ views.

**`metrics_1m` is for forensics only:** "What happened at 14:23 last Tuesday for tenant X, workflow rag?" That's a single index seek — sub-10ms.

**Sketch merging cost:** `rollup(end_to_end_ms_pct)` over 168 1h buckets (one week) merges 168 t-digests. Each sketch is ~5KB compressed; the merge is CPU-bound, runs in ~20ms on shared PostgreSQL. The accuracy at p99 is ~99% of true (per TimescaleDB Toolkit benchmarks).

**Compression ratio:** TimescaleDB native compression on the 1m hypertable delivers ~25× space reduction (chunks compressed after 24h). 1h and 1d tables compress to ~10× because the row count is already low.

**Write rate target:** Spark writes ~5–50 rows/sec to `metrics_1m` depending on tenant fanout. The upsert with idempotency-key conflict resolution sustains 200 rows/sec on the shared PostgreSQL instance — comfortable headroom.

**Avoid:**
- `GROUP BY` on raw [[response-event]] from any dashboard — always go through an aggregate. (Code review enforces this — see §13.)
- Queries without `tenant_id` filter — they table-scan all tenants and starve other queries.
- Bucket-wise `DISTINCT` on `tag_counts` keys — JSONB expansion is expensive; pre-aggregate via the §7.3 pattern.

---

## 10. Security Considerations

**Tenant isolation:** every aggregate row is keyed on `tenant_id`. The application layer MUST include `tenant_id = ?` in every query. **Phase 2** will enforce this via PostgreSQL row-level security (`CREATE POLICY` keyed on a session var set per-request); MVP relies on app-layer correctness audited via code review.

**No PII in aggregates.** The aggregator writes counts and aggregate measurements only — no prompt text, no completion text, no PII fields. [[inferred-labels]].pii_in_output is reflected as a *count* (`pii_in_output_count`), not as the raw labels.

**Scope-key cardinality attack surface:** a malicious tenant could try to explode `scope_key` cardinality (e.g. by feeding random workflow names) to bloat their bucket count. Mitigated by:

1. The Spark job clamps unique `scope_key` values per (tenant, scope_type, hour) at 1000 — overflow buckets into a synthetic `__overflow__` key.
2. Per-tenant quotas on workflow-classification cardinality enforced upstream in [[inferred-labels]].

**Access control:**

- Direct DB access (`psql`, Grafana) restricted to platform engineers via PostgreSQL roles.
- Tenant-facing reads go through the gateway's authenticated API — never expose TimescaleDB to tenants.

---

## 11. Migration Strategies

**Adding a new aggregate column** (e.g. a new CLEAR sub-dimension or a new signal counter): pure additive.

```sql
ALTER TABLE aiqg.metrics_1m ADD COLUMN new_metric_count INTEGER NOT NULL DEFAULT 0;
-- Continuous aggregates picking up the new column require recreate:
DROP MATERIALIZED VIEW aiqg.metrics_1h;
CREATE MATERIALIZED VIEW aiqg.metrics_1h ... ;  -- include new_metric_count in SELECT
```

Continuous aggregate recreation backfills from `metrics_1m` automatically (within the source-table retention window).

**Adding a new `scope_type` value:** additive — extend the enum and start writing rows.

```sql
ALTER TYPE aiqg.scope_kind ADD VALUE 'environment';
```

Existing queries that filter `scope_type IN ('workflow', 'route', ...)` continue to work; only queries that want the new value need updating.

**Changing bucket sizes:** **DO NOT** change the `time_bucket()` width of an existing hypertable — it breaks all historical rows. Instead, create a new hypertable at the new width (e.g. `metrics_10m`) and run both in parallel. Deprecate the old once consumers migrate.

**Removing a column:** breaking change for every dashboard that selects it. **Never remove in MVP/Phase 1**. Deprecation path: stop populating (default value), let dashboards migrate, then remove in a major version bump.

**Pricing-table or scoring-version changes:** handled upstream in [[token-accounting]].vendor_pricing_version and [[response-event]].scoring_version. Aggregates carry the majority `scoring_version` of the bucket — if a tenant cares about strict version isolation, they must filter `WHERE scoring_version = 'clear-1.0.0'` (which is then a per-bucket gate, not a per-event gate).

**Cross-scope double-counting:** queries that sum `total_actual_cost_usd` across multiple `scope_type` values double-count. Always filter to **one** `scope_type` per query. A future refactor (Phase 2) may introduce explicit `tenant-only` rows + `delta` rows on other scopes, but MVP keeps the simpler "duplicate rows per scope" model.

---

## 12. Common Patterns

### 12.1 Cohort comparison: route A vs route B

```sql
SELECT
  scope_key                          AS route,
  AVG(clear_composite_avg)           AS avg_clear,
  SUM(total_actual_cost_usd)         AS spend,
  SUM(request_count)                 AS requests,
  approx_percentile(0.95, rollup(end_to_end_ms_pct)) AS p95_latency_ms
FROM aiqg.metrics_1h
WHERE tenant_id = $1
  AND scope_type = 'route'
  AND scope_key IN ('production_strict', 'production_loose')
  AND bucket_start >= NOW() - INTERVAL '7 days'
GROUP BY scope_key;
```

### 12.2 Top 5 antipatterns this week

```sql
SELECT key AS antipattern, SUM((value)::int) AS hits
FROM aiqg.metrics_1h, LATERAL jsonb_each_text(tag_counts)
WHERE tenant_id = $1
  AND scope_type = 'account'
  AND key LIKE 'antipattern:%'
  AND bucket_start >= NOW() - INTERVAL '7 days'
GROUP BY key
ORDER BY hits DESC
LIMIT 5;
```

### 12.3 Waste-to-spend ratio time series

```sql
SELECT
  bucket_start,
  total_actual_cost_usd,
  total_direct_payload_waste_usd
    + total_induced_output_waste_usd
    + total_genuine_post_model_waste_usd                  AS total_waste_usd,
  CASE
    WHEN total_actual_cost_usd > 0
    THEN (total_direct_payload_waste_usd
          + total_induced_output_waste_usd
          + total_genuine_post_model_waste_usd) / total_actual_cost_usd
    ELSE 0
  END                                                     AS waste_ratio
FROM aiqg.metrics_1h
WHERE tenant_id = $1
  AND scope_type = 'account'
  AND bucket_start >= NOW() - INTERVAL '7 days'
ORDER BY bucket_start;
```

### 12.4 NIST violations leaderboard

```sql
SELECT
  scope_key                            AS workflow,
  SUM(nist_violations_count)           AS violations,
  SUM(request_count)                   AS requests,
  SUM(nist_violations_count)::FLOAT
    / NULLIF(SUM(request_count), 0)    AS violation_rate
FROM aiqg.metrics_1h
WHERE tenant_id = $1
  AND scope_type = 'workflow'
  AND bucket_start >= NOW() - INTERVAL '7 days'
GROUP BY scope_key
HAVING SUM(nist_violations_count) > 0
ORDER BY violation_rate DESC;
```

---

## 13. Known Issues

1. **Late-arriving events past the 2-min watermark are dropped.** Matches existing `tas-spark-jobs/events_aggregator` behavior ([build-vs-reuse §4.4](./build-vs-reuse.md#44-spark-job-for-rollups)). For audit-perfect cost reconciliation (e.g. month-end billing), query [[response-event]] directly — do not trust `metrics_1d` for accounting handoff. Magnitude in practice: <0.05% of events at p95 (measured against the existing `events_aggregator` job).
2. **`scoring_version` mixing in a bucket** happens during a rolling deploy of `tas-llm-router`. The bucket records the majority value; minority responses are still aggregated but the version label is approximate. Query layer should treat `scoring_version` as opaque and not mix versions in the same chart series.
3. **Spark micro-batch trigger interval** is 60s. So a bucket for "14:23 UTC" is not visible in `metrics_1m` until ~14:25 (60s trigger + 2-min watermark). Document this lag in any "real-time" dashboard or it will confuse operators.
4. **`tag_counts` top-N truncation is lossy.** A tag that fires 5 times in a bucket where the 50th most popular tag fires 6 times is dropped. Mitigation: query [[response-event]].tags directly for forensic tag analysis; aggregates are for **distribution**, not **completeness**.
5. **No back-fill for added columns:** when a column is added (per §11), historical rows have the default value, not the true value. Drift charts spanning the migration boundary will show a step function — annotate the deploy on the dashboard or filter to post-deploy ranges.
6. **t-digest accuracy degrades at extreme tails** (p99.9+). For SLO violation tracking where exact p99.9 matters, supplement with an exact percentile query against the raw response events for the specific window of interest.
7. **Cross-scope sums double-count by design** (see §11). Code review of new Grafana panels must reject any query that sums a measure across `scope_type` values.

---

## 14. Related Documentation

- [[request-event]] — upstream Kafka event the aggregator joins for routing/client context.
- [[response-event]] — upstream Kafka event carrying the pre-computed `clear_*_score` fields the aggregator reads.
- [[token-accounting]] — source of `actual_cost_usd` and waste-decomposition fields rolled up here.
- [[event-timestamps]] — source of the five latency fields whose t-digest sketches live in this model.
- [[tag-set]] — universe of tags whose top-N distribution is rolled up into `tag_counts`.
- [[inferred-labels]] — source of `workflow_classification`, `retry_of_previous`, hedge/PII signals counted here.
- [[workflow-classification]] — `scope_type='workflow'` scope rows are keyed on this label.
- [[report-snapshot]] — downstream model that freezes selected aggregate rows into immutable per-tenant reports for compliance handoff.
- [[account]] — `tenant_id` is the FK across all aggregate rows.
- [build-vs-reuse §4.4](./build-vs-reuse.md#44-spark-job-for-rollups) — decision record for the Spark job that populates `metrics_1m`.
- [build-vs-reuse §7.1](./build-vs-reuse.md#71-aggregate-storage--decided-timescaledb) — decision record for choosing TimescaleDB.
- [build-vs-reuse §7.2](./build-vs-reuse.md#72-clear-scoring-location--decided-gateway-side-go) — the gateway-side scoring decision that constrains the aggregator to "aggregate, don't re-derive".
- [build-vs-reuse §2.12](./build-vs-reuse.md#212-21-25--clear-dimension-measurement) — CLEAR dimension scorer locations.
- [source-spec-v0.2 §2](./source-spec-v0.2.md) — definitive description of the five CLEAR dimensions.
- [source-spec-v0.2 §4.4](./source-spec-v0.2.md) — the ongoing dashboard / Day-1 report this model exists to serve.

---

## Changelog

- `v1.0.0 — 2026-05-31 — initial spec draft — TAS Platform`
