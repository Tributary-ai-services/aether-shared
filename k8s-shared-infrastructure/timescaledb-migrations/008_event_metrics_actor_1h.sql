-- Plan #22 4.1: the hourly rollup by actor (design org-structure-and-attribution.md §9.1).
--
-- Every org view (Billed, Reported under, Involving, Served; design §8) reads
-- this aggregate rather than raw events. It keeps one row per hour per
-- combination of WHO sent the request (token_id + owner_sub, the
-- authenticated identity; user_id and agent_id, what the caller asserted) and
-- WHAT it was (source_app, workflow, vendor, model). Teams are NOT stored:
-- a token's or a person's team is resolved at read time from aiqg-dashboard-be's
-- history tables (token_placement, team_member_history), so a back-dated
-- placement re-attributes past hours without recomputing anything here.
--
-- NO user_id FOLD. user_id is caller-set and unbounded, so the design asked
-- for a cardinality measurement first (§14.2 Q3). Measured 2026-11-21 over 14
-- days: the busiest tenant peaked at 7 groups per hour. The aggregate keeps
-- raw user_id; actor_1h_rows_per_hour below is the guard that says when a fold
-- is needed.
--
-- SUMS, NOT AVERAGES. Rows are re-grouped at read time (by team, by day), and
-- an average of averages is wrong. Every quality column is a sum plus the
-- count of rows that had a value, so readers compute sum/count after grouping.
--
-- NO RETENTION. Kept indefinitely, like the other rollups. Raw event_metrics
-- has 90-day retention (006), so this is the only long-term per-actor record.
--
-- BACKFILL is not in this file, because the migrate Job re-runs every file.
-- Run once after the first apply, in 7-day windows with explicit bounds and
-- never with a NULL start: refreshing a range whose raw chunks were dropped
-- by retention would recompute it from nothing.
--   CALL refresh_continuous_aggregate('aiqg.event_metrics_actor_1h', <from>, <from> + 7 days);
--
-- Idempotent: IF NOT EXISTS / if_not_exists / OR REPLACE throughout.

CREATE MATERIALIZED VIEW IF NOT EXISTS aiqg.event_metrics_actor_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time)                                          AS bucket,
    tenant_id,
    token_id,
    owner_sub,
    user_id,
    agent_id,
    source_app,
    workflow,
    vendor,
    model,
    count(*)                                                             AS request_count,
    count(*) FILTER (WHERE status = 'success')                           AS success_count,
    count(*) FILTER (WHERE status IS DISTINCT FROM 'success')            AS error_count,
    sum(total_cost_usd)                                                  AS total_cost_usd,
    sum(cache_aware_total_cost_usd)                                      AS cache_aware_cost_usd,
    sum(prompt_tokens)                                                   AS prompt_tokens,
    sum(completion_tokens)                                               AS completion_tokens,
    sum(total_tokens)                                                    AS total_tokens,
    sum(cache_read_tokens)                                               AS cache_read_tokens,
    sum(cache_creation_tokens)                                           AS cache_creation_tokens,
    sum(end_to_end_ms)                                                   AS latency_ms_sum,
    count(end_to_end_ms)                                                 AS latency_n,
    max(end_to_end_ms)                                                   AS latency_ms_max,
    sum(clear_composite)                                                 AS clear_composite_sum,
    sum(clear_cost)                                                      AS clear_cost_sum,
    sum(clear_latency)                                                   AS clear_latency_sum,
    sum(clear_efficacy)                                                  AS clear_efficacy_sum,
    sum(clear_assurance)                                                 AS clear_assurance_sum,
    sum(clear_reliability)                                               AS clear_reliability_sum,
    count(clear_composite)                                               AS clear_n,
    sum(assurance_inbound_count)                                         AS inbound_findings,
    sum(assurance_outbound_count)                                        AS outbound_findings
FROM aiqg.event_metrics
GROUP BY time_bucket('1 hour', time), tenant_id, token_id, owner_sub, user_id,
         agent_id, source_app, workflow, vendor, model
WITH NO DATA;

-- Same window as the model rollup (005): late events up to 4 hours old are
-- folded in; the newest hour is left to real-time aggregation.
SELECT add_continuous_aggregate_policy('aiqg.event_metrics_actor_1h',
    start_offset      => INTERVAL '4 hours',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists     => TRUE
);

CREATE INDEX IF NOT EXISTS event_metrics_actor_1h_tenant_bucket
    ON aiqg.event_metrics_actor_1h (tenant_id, bucket DESC);
CREATE INDEX IF NOT EXISTS event_metrics_actor_1h_token
    ON aiqg.event_metrics_actor_1h (tenant_id, token_id, bucket DESC);

-- The cardinality guard (§14.2 Q3). aiqg-dashboard-be exports the max of
-- `rows` over the last day as a gauge, with an alert at 5,000 (about 700x the
-- 2026-11-21 peak). When it fires, design the user_id fold against the
-- traffic that caused it.
CREATE OR REPLACE VIEW aiqg.actor_1h_rows_per_hour AS
SELECT bucket, tenant_id, count(*) AS rows
FROM aiqg.event_metrics_actor_1h
WHERE bucket > now() - INTERVAL '7 days'
GROUP BY bucket, tenant_id;
