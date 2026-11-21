-- Plan #22 T.6: the tool layer (design org-structure-and-attribution.md §9.5).
--
-- Three objects:
--   aiqg.tool_call_events   raw hypertable, one row per tool call, 90-day retention
--   aiqg.tool_call_1h       hourly continuous aggregate, no retention
--   aiqg.tool_catalogue     snapshot rows of what each server offers (tools/list
--                           at the proxy, the request's `tools` at the gateway),
--                           for the never-called report
--
-- SOURCE. CloudEvent com.tas.aiqg.tool_call.v1 on tas.aiqg.events.v1, emitted
-- by the MCP proxy (tas-mcp, observed_by='proxy') and the LLM gateway
-- (tas-llm-router, observed_by='gateway'). The Spark aiqg-aggregator writes
-- this table (tas-spark-jobs T.7). Event doc: data-models/aiqg/tool-call-event.md.
--
-- ONE ROW PER CALL, NOT PER SIGHTING. A call both sources saw is merged by the
-- writer on (tenant_id, tool_call_id) into one row with observed_by='both' and
-- the proxy's fields winning. The merge is done at write time, so the
-- aggregate below never counts a call twice. A hypertable cannot carry a
-- UNIQUE (tenant_id, tool_call_id) constraint (every unique index must include
-- the partition column, and the two sources stamp different times), so the
-- writer finds the partner row with a bounded time-window lookup instead; see
-- tool_call_merge_idx and tas-spark-jobs jobs/aiqg_aggregator/tool_calls.py.
--
-- LATENCY HISTOGRAM, NOT percentile_agg. The design asks for a p95 sketch via
-- timescaledb_toolkit's percentile_agg. The live database runs the plain
-- timescale/timescaledb:2.13.0-pg15 image, which does not ship the toolkit
-- (checked 2026-11-21: pg_available_extensions lists only timescaledb). The
-- rollup therefore keeps a fixed-bucket histogram: cumulative counts of calls
-- at or under each bound. Counts re-group by plain sum (team, day, server), a
-- reader takes the p95 as the smallest bound whose count reaches 95% of
-- latency_n, and the 5 s bound is exactly the default Speed target (§9.5), so
-- "share of calls under target" is exact rather than interpolated.
--   Bounds (ms): 100, 250, 500, 1000, 2500, 5000, 10000, 30000.
-- The latency is COALESCE(duration_ms, round_trip_ms): the proxy's own
-- measurement when the proxy saw the call, the gateway's round trip (which
-- includes the client) otherwise. observed_by is a group key, so a reader can
-- always tell the two apart. If the toolkit is installed later, add a
-- percentile_agg column in a new aggregate rather than altering this one.
--
-- SUMS, NOT AVERAGES, for the same reason as 008: rows are re-grouped at read
-- time. Every measure that can be absent is a sum plus the count of rows that
-- had it.
--
-- tool_name CARDINALITY. Bounded by the catalogue (§9.5). The "(other) past
-- 200 names per tenant" fold is the writer's job, not this file's.
--
-- BACKFILL: none needed, the table is new. Never refresh the aggregate with a
-- NULL start (the 006 trap applies here too once retention drops chunks).
--
-- Idempotent: IF NOT EXISTS / if_not_exists / OR REPLACE throughout; the
-- migrate Job re-runs every file.

CREATE SCHEMA IF NOT EXISTS aiqg;

-- ---------------------------------------------------------------------------
-- Raw events
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS aiqg.tool_call_events (
    occurred_at        TIMESTAMPTZ NOT NULL,   -- proxy's time when the proxy saw the call
    tenant_id          TEXT        NOT NULL,
    event_id           TEXT        NOT NULL,   -- CloudEvent id of the winning sighting
    gateway_event_id   TEXT,                   -- CloudEvent id of the gateway sighting once merged
    token_id           UUID,                   -- NULL when absent or not a UUID
    owner_sub          TEXT,
    user_id            TEXT,
    agent_id           TEXT,
    source_app         TEXT,
    flow_id            TEXT,
    step_id            TEXT,
    tool_call_id       TEXT,                   -- join key; NULL rows are never merged
    tool_name          TEXT        NOT NULL,
    server_id          TEXT,                   -- NULL = not via the proxy
    observed_by        TEXT        NOT NULL,
    outcome            TEXT        NOT NULL,
    duration_ms        INTEGER,                -- proxy: the tool's own time
    round_trip_ms      INTEGER,                -- gateway: offer-to-echo, includes the client
    result_bytes       BIGINT,
    result_tokens_est  INTEGER,
    reduced_bytes      BIGINT,
    findings           JSONB,                  -- {"<pattern category>": count}; counts only
    scan_skipped       BOOLEAN,
    ingested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tool_call_events_observed_by_chk
        CHECK (observed_by IN ('proxy', 'gateway', 'both')),
    CONSTRAINT tool_call_events_outcome_chk
        CHECK (outcome IN ('ok', 'tool_error', 'timeout', 'unreachable', 'blocked', 'no_result')),
    CONSTRAINT tool_call_events_findings_chk
        CHECK (findings IS NULL OR jsonb_typeof(findings) = 'object'),
    PRIMARY KEY (occurred_at, tenant_id, event_id)
);

SELECT create_hypertable('aiqg.tool_call_events', 'occurred_at', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS tool_call_events_tenant_time
    ON aiqg.tool_call_events (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS tool_call_events_server_time
    ON aiqg.tool_call_events (tenant_id, server_id, tool_name, occurred_at DESC)
    WHERE server_id IS NOT NULL;
-- The writer's partner lookup: (tenant_id, tool_call_id) inside a time window.
CREATE INDEX IF NOT EXISTS tool_call_merge_idx
    ON aiqg.tool_call_events (tenant_id, tool_call_id, occurred_at)
    WHERE tool_call_id IS NOT NULL;

-- Raw is kept 90 days, like event_metrics (006, G-4).
SELECT add_retention_policy('aiqg.tool_call_events', INTERVAL '90 days', if_not_exists => TRUE);

-- Total finding count of one findings object. IMMUTABLE so the continuous
-- aggregate may call it. Non-numeric values count 0 rather than erroring, so a
-- malformed event cannot wedge the refresh policy.
CREATE OR REPLACE FUNCTION aiqg.findings_total(f JSONB) RETURNS BIGINT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT COALESCE(sum(CASE WHEN jsonb_typeof(v) = 'number' THEN (v #>> '{}')::numeric ELSE 0 END), 0)::bigint
    FROM jsonb_each(CASE WHEN jsonb_typeof(f) = 'object' THEN f ELSE '{}'::jsonb END) AS e(k, v)
$$;

-- ---------------------------------------------------------------------------
-- Hourly rollup
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW IF NOT EXISTS aiqg.tool_call_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', occurred_at)                                   AS bucket,
    tenant_id,
    token_id,
    owner_sub,
    source_app,
    agent_id,
    server_id,
    tool_name,
    observed_by,
    outcome,
    count(*)                                                             AS call_count,
    -- Speed: the proxy's own time, and the gateway's round trip, kept apart.
    sum(duration_ms)                                                     AS duration_ms_sum,
    count(duration_ms)                                                   AS duration_n,
    max(duration_ms)                                                     AS duration_ms_max,
    sum(round_trip_ms)                                                   AS round_trip_ms_sum,
    count(round_trip_ms)                                                 AS round_trip_n,
    max(round_trip_ms)                                                   AS round_trip_ms_max,
    -- Fixed-bucket histogram of COALESCE(duration_ms, round_trip_ms); cumulative.
    count(COALESCE(duration_ms, round_trip_ms))                          AS latency_n,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=   100) AS latency_le_100ms,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=   250) AS latency_le_250ms,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=   500) AS latency_le_500ms,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=  1000) AS latency_le_1s,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=  2500) AS latency_le_2500ms,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <=  5000) AS latency_le_5s,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <= 10000) AS latency_le_10s,
    count(*) FILTER (WHERE COALESCE(duration_ms, round_trip_ms) <= 30000) AS latency_le_30s,
    -- Spend: what tool results add to the next prompt, and what reduction saved.
    sum(result_bytes)                                                    AS result_bytes,
    sum(result_tokens_est)                                               AS result_tokens_est,
    sum(reduced_bytes)                                                   AS reduced_bytes,
    -- Data safety: findings per 1,000 results = findings_total / scanned_n.
    sum(aiqg.findings_total(findings))                                   AS findings_total,
    count(*) FILTER (WHERE aiqg.findings_total(findings) > 0)            AS results_with_findings,
    count(*) FILTER (WHERE scan_skipped IS TRUE)                         AS scan_skipped_count
FROM aiqg.tool_call_events
GROUP BY time_bucket('1 hour', occurred_at), tenant_id, token_id, owner_sub,
         source_app, agent_id, server_id, tool_name, observed_by, outcome
WITH NO DATA;

-- Same window as 005 and 008. A proxy/gateway merge that lands more than 4 h
-- after the call is in raw but not in the rollup until a manual refresh of
-- that window (explicit bounds, never NULL).
SELECT add_continuous_aggregate_policy('aiqg.tool_call_1h',
    start_offset      => INTERVAL '4 hours',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists     => TRUE
);

CREATE INDEX IF NOT EXISTS tool_call_1h_tenant_bucket
    ON aiqg.tool_call_1h (tenant_id, bucket DESC);
CREATE INDEX IF NOT EXISTS tool_call_1h_server_tool
    ON aiqg.tool_call_1h (tenant_id, server_id, tool_name, bucket DESC);

-- ---------------------------------------------------------------------------
-- Tool catalogue snapshots (never-called report, §9.5)
-- ---------------------------------------------------------------------------
-- One row per (tool, server, source) per snapshot. The proxy writes its
-- tools/list result per server (tenant_id NULL: the federation is shared);
-- the gateway writes the tools a request offered (tenant_id set, server_id
-- NULL, since the model's tool list does not say which server serves a tool).
-- schema_bytes is the size of the tool's JSON schema: the input-token cost of
-- offering it on every request. A plain table, not a hypertable: it is small
-- and the nightly job reads the latest snapshot per key.
CREATE TABLE IF NOT EXISTS aiqg.tool_catalogue (
    snapshot_at   TIMESTAMPTZ NOT NULL,
    server_id     TEXT,
    tool_name     TEXT        NOT NULL,
    schema_bytes  INTEGER,
    observed_by   TEXT        NOT NULL,
    tenant_id     TEXT,
    CONSTRAINT tool_catalogue_observed_by_chk CHECK (observed_by IN ('proxy', 'gateway'))
);

-- NULLS NOT DISTINCT (pg15): a re-delivered snapshot with NULL tenant or
-- server is still a duplicate, so the writer can ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX IF NOT EXISTS tool_catalogue_snapshot_key
    ON aiqg.tool_catalogue (snapshot_at, server_id, tool_name, observed_by, tenant_id)
    NULLS NOT DISTINCT;
CREATE INDEX IF NOT EXISTS tool_catalogue_latest
    ON aiqg.tool_catalogue (server_id, tool_name, snapshot_at DESC);
