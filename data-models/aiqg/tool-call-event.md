# AIQG Tool Call Event

---

**Metadata**

```yaml
service: tas-mcp (MCP proxy) + tas-llm-router (LLM gateway)
model: ToolCallEvent
wire_form: CloudEvent
cloudevent_type: com.tas.aiqg.tool_call.v1 (+ com.tas.aiqg.tool_catalogue.v1, §2.4)
kafka_topic: tas.aiqg.events.v1
storage: TimescaleDB hypertable aiqg.tool_call_events (+ aiqg.tool_call_1h, aiqg.tool_catalogue)
partition_key: occurred_at
primary_key: (occurred_at, tenant_id, event_id)
migration: k8s-shared-infrastructure/timescaledb-migrations/009_tool_call.sql
writer: tas-spark-jobs jobs/aiqg_aggregator/tool_calls.py (+ tool_catalogue.py)
version: 1.1.0
last_updated: 2026-11-21
status: schema + writers built (Plan #22 T.6/T.7; tool_catalogue writer since tas-spark-jobs 0.5.0); emitters in progress (T.2 proxy, T.5 gateway)
```

---

## 1. Overview

### Purpose

One event per tool call an agent makes. LLM vendor calls are measured for speed, failures, cost, quality and data safety; tool calls were measured for none of these, so a failed or slow tool call, sensitive data in a tool result, and a tool that is offered but never called were all invisible (design `org-structure-and-attribution.md` §9.5, D16).

### Two sources, one event

| Source | `observed_by` | Sees | Misses |
|---|---|---|---|
| MCP proxy (`tas-mcp` federation) | `proxy` | every call routed through it: server, tool, its own duration, outcome, result size, reduction, boundary-scan findings on the result | tools the client runs itself, and any agent that calls a server directly |
| LLM gateway (`tas-llm-router`) | `gateway` | every call the model makes (`tool_calls` in a response), proxy or not; the round trip until the next request echoes the `tool_call_id`; calls whose result never came back (`no_result`) | why a call failed, how long the tool itself took, anything about the server |

A call both sources saw is stored as **one row** with `observed_by = 'both'`. See [§5 Lifecycle](#5-lifecycle).

### Key Characteristics

- Same topic as the response event (`tas.aiqg.events.v1`); the Spark aiqg-aggregator routes by CloudEvent `type`, in its own streaming query (consumer group `spark-aiqg-aggregator-tool-calls`, checkpoint `/tool_calls/`). `aiqg.tool_catalogue` has a third query (consumer group `spark-aiqg-aggregator-tool-catalogue`, checkpoint `/tool_catalogue/`, §2.4). The job stops when any query fails, so a tool-call or catalogue failure restarts the job loudly instead of dropping rows without notice.
- Same identity fields as LLM events (design §4): the authenticated `token_id` + `owner_sub`, and the caller-asserted `user_id`, `agent_id`, `source_app`, `flow_id`, `step_id`.
- `findings` carries **counts only**, never the matched value.
- A tool call has no vendor cost of its own. Its cost is `result_tokens_est`, the tokens its result adds to the next prompt, priced at that request's model rate.

---

## 2. Schema Definition

### 2.1 CloudEvent Envelope

```yaml
specversion: "1.0"
type: com.tas.aiqg.tool_call.v1
source: urn:tas:service:tas-mcp | urn:tas:service:tas-llm-router
id: <unique per sighting>           # becomes event_id / gateway_event_id; required
time: <emit time>                   # fallback for occurred_at
datacontenttype: application/json
data: <ToolCallEvent JSON, §2.2>
```

### 2.2 `data` fields

Field names are snake_case on the wire and in the table.

| Field | Type | Required | Source | Description |
|---|---|---|---|---|
| `tenant_id` | text | Yes (gateway) | both | Token tenant. A proxy call with no valid token has none and is stored as `(unattributed)` (operator-only view, design §9.5); a gateway event without it is dropped. |
| `token_id` | UUID text | No | both | `aiqg.token` row that authenticated the caller. A value that is not a UUID is stored NULL rather than failing the batch (same rule as `event_metrics`, tas-spark-jobs#11). |
| `owner_sub` | text | No | both | Keycloak `sub` of the token's issuer. |
| `user_id`, `agent_id`, `source_app`, `flow_id`, `step_id` | text | No | both | Caller-asserted identity (W3C `baggage` / TAS headers). |
| `tool_call_id` | text | No | both | The model's tool-call id. **The join key** between the two sources. NULL rows are never merged. |
| `tool_name` | text | Yes | both | Tool name. Events without it are dropped. |
| `server_id` | text | No | proxy | MCP server id. NULL means the call did not go through the proxy. |
| `observed_by` | enum | Yes | — | `proxy` \| `gateway` on the wire. `both` is written only by the aggregator. |
| `outcome` | enum | Yes | both | `ok` \| `tool_error` \| `timeout` \| `unreachable` \| `blocked` \| `no_result`. Anything else is dropped. `no_result` comes only from the gateway. |
| `duration_ms` | int | No | proxy | The tool's own time, measured at the proxy. |
| `round_trip_ms` | int | No | gateway | From the response that made the call to the request that echoed its result; includes the client. |
| `result_bytes` | bigint | No | proxy (gateway may estimate) | Size of the result. |
| `result_tokens_est` | int | No | both | Estimated tokens the result adds to the next prompt. |
| `reduced_bytes` | bigint | No | proxy | Bytes removed by payload reduction. |
| `findings` | object | No | proxy (gateway for direct calls) | `{"<pattern category>": count}` from the boundary scan. Non-object values are stored NULL. |
| `scan_skipped` | bool | No | both | True when a valid scan attestation (GK-5) let the scanner skip this content. |
| `occurred_at` | RFC 3339 | No | both | When the call happened. Falls back to the CloudEvent `time`, then the Kafka timestamp. |

### 2.3 TimescaleDB

`aiqg.tool_call_events` holds the fields above, plus:

| Column | Description |
|---|---|
| `event_id` | CloudEvent id of the winning sighting (the proxy's, once merged). Part of the PK. |
| `gateway_event_id` | CloudEvent id of the gateway's sighting, when there is one. Lets a re-delivered gateway event be recognised after a merge. |
| `ingested_at` | Write time. |

CHECK constraints enforce the `observed_by` and `outcome` sets and that `findings` is an object. Raw rows are kept **90 days** (G-4).

`aiqg.tool_call_1h` is the hourly continuous aggregate, grouped by `(bucket, tenant_id, token_id, owner_sub, source_app, agent_id, server_id, tool_name, observed_by, outcome)`. No retention.

| Column | Meaning |
|---|---|
| `call_count` | calls |
| `duration_ms_sum`, `duration_n`, `duration_ms_max` | the proxy's duration |
| `round_trip_ms_sum`, `round_trip_n`, `round_trip_ms_max` | the gateway's round trip |
| `latency_n`, `latency_le_100ms` … `latency_le_30s` | cumulative histogram of `COALESCE(duration_ms, round_trip_ms)`, bounds 100, 250, 500, 1000, 2500, 5000, 10000, 30000 ms |
| `result_bytes`, `result_tokens_est`, `reduced_bytes` | sums |
| `findings_total`, `results_with_findings` | sum of all finding counts; rows with at least one |
| `scan_skipped_count` | rows with `scan_skipped = true` |

**Why a histogram and not `percentile_agg`.** The design asks for a p95 sketch via timescaledb_toolkit. The live database runs `timescale/timescaledb:2.13.0-pg15`, which does not ship the toolkit (checked 2026-11-21). The histogram re-groups by plain sum. p95 is the smallest bound whose cumulative count reaches `0.95 × latency_n`. A call over 30 s counts in `latency_n` but in no bucket. The 5 s bound is the default Speed target, so "share of calls under target" is `latency_le_5s / latency_n`, exact.

`aiqg.tool_catalogue` stores snapshot rows `(snapshot_at, server_id, tool_name, schema_bytes, observed_by, tenant_id)`. The proxy writes its `tools/list` per server (`tenant_id` NULL). The gateway writes the tools a request offered (`server_id` NULL). The snapshot key is unique with `NULLS NOT DISTINCT`, so a re-delivered snapshot is a no-op. How the rows are produced is in §2.4.

### 2.4 Tool catalogue sources

Two sources on `tas.aiqg.events.v1` feed `aiqg.tool_catalogue`. The writer is `tas-spark-jobs jobs/aiqg_aggregator/tool_catalogue.py` (since 0.5.0), in its own streaming query.

**MCP proxy: CloudEvent `com.tas.aiqg.tool_catalogue.v1`**, one event per server per hour:

```json
{
  "specversion": "1.0",
  "type": "com.tas.aiqg.tool_catalogue.v1",
  "source": "urn:tas:service:tas-mcp",
  "id": "<unique>",
  "time": "2026-11-21T10:00:03Z",
  "data": {
    "snapshot_at": "2026-11-21T10:00:00Z",
    "server_id": "napkin-mcp",
    "observed_by": "proxy",
    "tenant_id": null,
    "tools": [{"name": "draw", "schema_bytes": 310}, {"name": "erase", "schema_bytes": 120}]
  }
}
```

One row per element of `tools`. `server_id` from the event, `tenant_id` NULL (the federation is shared).

**LLM gateway: `tools_offered` on `com.tas.aiqg.response.v1`** (see [[response-event]] §2 Offered Toolset). One row per element, `snapshot_at` = the response's CloudEvent `time`, `server_id` NULL, `observed_by = 'gateway'`, `tenant_id` from the event. The gateway sends `tools_offered` once per toolset per tenant per hour per replica; the per-replica repeats collapse on the key. The response event's projection to `aiqg.event_metrics` is unaffected.

**Shared rules.**

- `snapshot_at` is truncated to the **UTC hour** for both sources, in SQL with an explicit `'UTC'`, so the database session's time zone cannot move it. A conforming proxy `snapshot_at` is already an hour, so this is a no-op there.
- `INSERT ... ON CONFLICT DO NOTHING` on the key `(snapshot_at, server_id, tool_name, observed_by, tenant_id)`. Re-delivery, retried micro-batches and several gateway replicas all collapse to one row. **The first write of a key wins**: a different `schema_bytes` for the same tool in the same hour is not recorded until the next hour's snapshot.
- Why its own query and not part of the response query: the response query's projection and its transaction stay unchanged, and the proxy half is not a response event. The cost is one more Kafka consumer on the topic.

---

## 3. Relationships

- **Upstream:** the proxy's federation call path (T.2) and the gateway's response `tool_calls` + flow linker (T.5, `builder.go:291-346`).
- **Siblings:** [[response-event]]: the gateway sighting's `flow_id` / `step_id` match the response event that made the call.
- **Downstream:** aiqg-dashboard-be `/api/v1/tools*` (T.8) reads `tool_call_1h` and `tool_catalogue`. Team placement joins (design §9.1) apply to `token_id` / `owner_sub` unchanged.

---

## 4. Validation Rules

- Stored as tenant `(unattributed)`: a proxy event with no `tenant_id` (an untokened call, served in observe mode). Since tas-spark-jobs 0.4.1.
- Dropped by the aggregator, never stored: a gateway event with no `tenant_id`, no `tool_name`, no CloudEvent `id`, `observed_by` other than `proxy`/`gateway`, `outcome` outside the six values.
- Stored with a NULL: non-UUID `token_id`, non-object `findings`, empty strings in optional text fields.

`aiqg.tool_catalogue` (§2.4):

- Dropped: a tool with no name (or a blank one) or a NULL array element; `observed_by` other than `proxy`/`gateway`; a proxy event with no `server_id`; a gateway response with no `tenant_id`; `tools` / `tools_offered` that is not an array.
- Stored with a NULL: `schema_bytes` that is negative, over the INTEGER range, or not a number.
- A proxy `snapshot_at` that is missing or unparsable falls back to the CloudEvent `time`, then the Kafka timestamp, before truncation.
- No malformed row fails a batch.

---

## 5. Lifecycle

### Merging the two sightings (write time)

The aggregator merges at write time, in the same transaction as the insert:

1. For each incoming row with a `tool_call_id`, find stored rows with the same `(tenant_id, tool_call_id)` within **1 day**.
2. Merge all sightings into one row. For each column, **the proxy's value wins** and the gateway's fills a gap (`round_trip_ms` always comes from the gateway; `findings` and `scan_skipped` from the proxy, so a finding is counted once). `occurred_at` and `event_id` are the proxy's. `observed_by = 'both'`.
3. Delete the stored partner rows and insert the merged row.

**Why not read time.** A continuous aggregate cannot collapse two raw rows into one, so two rows per call would double every count.

**Why not `ON CONFLICT`.** A unique index on a hypertable must include the partition column, and the two sources stamp different times.

**Exceptions:**
- **Outcome.** The proxy's outcome wins, except that a gateway `no_result` overrides a proxy `ok`. The proxy did deliver the result, but only the gateway can see that it never reached the model. A proxy failure (`tool_error`, `timeout`, …) is kept, since it explains the missing result.
- **Reused ids are not merged.** If one `(tenant_id, tool_call_id)` has more than one distinct proxy event or more than one distinct gateway event, nothing is merged and the rows are stored as they are. Some clients reuse ids (local models emit `call_0` every turn), and merging those would lose calls. **Emitters should send one event per call per source.**
- **Late merges.** The refresh policy reaches back 4 hours. A merge that lands later than that is in raw, but `tool_call_1h` shows both rows until that window is refreshed by hand (explicit bounds, never NULL).

Re-delivery is idempotent. A re-sent sighting merges into the stored row and produces the same row.

---

## 6. Examples

```json
{
  "specversion": "1.0",
  "type": "com.tas.aiqg.tool_call.v1",
  "source": "urn:tas:service:tas-mcp",
  "id": "5d1f6c0e-2f7b-4f43-9d55-0c3f1f6f7a10",
  "time": "2026-11-21T10:00:00.412Z",
  "datacontenttype": "application/json",
  "data": {
    "tenant_id": "a689c0b2-02ca-46d1-9916-f9a30c00222a",
    "token_id": "8f29197c-818b-4b69-bf59-200f5c3228f5",
    "owner_sub": "kc-sub-123",
    "source_app": "claude-code",
    "tool_call_id": "toolu_01AbC",
    "tool_name": "draw",
    "server_id": "napkin-mcp",
    "observed_by": "proxy",
    "outcome": "ok",
    "duration_ms": 412,
    "result_bytes": 2048,
    "result_tokens_est": 512,
    "reduced_bytes": 0,
    "findings": {"email": 1},
    "scan_skipped": false,
    "occurred_at": "2026-11-21T10:00:00Z"
  }
}
```

p95 per server over a day:

```sql
WITH h AS (
  SELECT server_id, sum(latency_n) n,
         sum(latency_le_100ms) b100, sum(latency_le_250ms) b250, sum(latency_le_500ms) b500,
         sum(latency_le_1s) b1000, sum(latency_le_2500ms) b2500, sum(latency_le_5s) b5000,
         sum(latency_le_10s) b10000, sum(latency_le_30s) b30000
  FROM aiqg.tool_call_1h
  WHERE tenant_id = $1 AND bucket >= now() - interval '1 day'
  GROUP BY server_id)
SELECT server_id,
       CASE WHEN b100 >= 0.95*n THEN 100 WHEN b250 >= 0.95*n THEN 250 WHEN b500 >= 0.95*n THEN 500
            WHEN b1000 >= 0.95*n THEN 1000 WHEN b2500 >= 0.95*n THEN 2500 WHEN b5000 >= 0.95*n THEN 5000
            WHEN b10000 >= 0.95*n THEN 10000 WHEN b30000 >= 0.95*n THEN 30000 END AS p95_ms_upper_bound,
       b5000::float / NULLIF(n, 0) AS share_under_5s
FROM h;
```

---

## 7. Tenant Isolation, Security

Every row carries `tenant_id`, and the merge never crosses tenants. `findings` holds counts by category only. The matched text never leaves the scanner. Proxy catalogue rows have `tenant_id` NULL because the federation is shared. Readers scope them through the calls the tenant made, or through placement of the `mcp_server` member (T.8).

---

## 8. Deployment Order

1. Apply `009_tool_call.sql` (via `scripts/deploy-timescaledb.sh`).
2. Deploy the tas-spark-jobs image that contains `tool_calls.py` (and, from 0.5.0, `tool_catalogue.py`). If the job deploys before the tables exist, its tool-call or catalogue query fails on its first batch, and because the job waits on any query, the whole job restarts.
3. Enable the emitters (T.2, T.5).

---

## 9. Changelog

| Version | Date | Change |
|---|---|---|
| 1.0.0 | 2026-11-21 | Initial: event contract, `tool_call_events` / `tool_call_1h` / `tool_catalogue` (migration 009), write-time proxy+gateway merge (Plan #22 T.6/T.7) |
| 1.1.0 | 2026-11-21 | `tool_catalogue` writer (tas-spark-jobs 0.5.0): proxy `com.tas.aiqg.tool_catalogue.v1` and gateway `tools_offered`, UTC-hour snapshots, `ON CONFLICT DO NOTHING` (§2.4) |
