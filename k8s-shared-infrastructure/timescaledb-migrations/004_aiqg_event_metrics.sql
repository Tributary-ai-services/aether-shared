-- AIQG per-event metrics hypertable + continuous aggregates (tas_events DB).
--
-- This is the source of truth for aiqg.event_metrics, which the Spark
-- aiqg-aggregator writes (Kafka tas.aiqg.events.v1 → here) and aiqg-dashboard-be
-- reads (CLEAR/cost/latency panels + the experiments runner). It was originally
-- created out-of-band and grown via live ALTERs through Phase C/D; this file
-- captures the full current schema so a fresh environment is reproducible from
-- the repo (the migrate job globs *.sql; every statement is idempotent, so it
-- is a safe no-op against the existing populated DB).

CREATE SCHEMA IF NOT EXISTS aiqg;

-- Per-event row. PK (time, tenant_id, response_event_id) makes the Spark
-- upsert (ON CONFLICT DO NOTHING) deterministic under at-least-once delivery.
CREATE TABLE IF NOT EXISTS aiqg.event_metrics (
    time                     TIMESTAMPTZ      NOT NULL,
    tenant_id                TEXT             NOT NULL,
    aiqg_account_id          TEXT,
    vendor                   TEXT,
    model                    TEXT,
    workflow                 TEXT,
    status                   TEXT,
    http_status              INTEGER,
    finish_reason            TEXT,
    clear_composite          INTEGER,
    clear_cost               INTEGER,
    clear_latency            INTEGER,
    clear_efficacy           INTEGER,
    clear_assurance          INTEGER,
    clear_reliability        INTEGER,
    end_to_end_ms            INTEGER,
    prompt_tokens            INTEGER,
    completion_tokens        INTEGER,
    total_tokens             INTEGER,
    total_cost_usd           DOUBLE PRECISION,
    assurance_inbound_count  INTEGER,
    assurance_outbound_count INTEGER,
    nist_secure_resilient    INTEGER,
    nist_privacy_enhanced    INTEGER,
    nist_valid_reliable      INTEGER,
    nist_safe                INTEGER,
    response_event_id        TEXT             NOT NULL,
    request_event_id         TEXT,
    raw                      JSONB,
    ingested_at              TIMESTAMPTZ      NOT NULL DEFAULT now(),
    tags                     JSONB,
    -- Identity attribution (Phase C/C2/C3 — promoted from agent_context).
    agent_id                 TEXT,
    agent_name               TEXT,
    user_id                  TEXT,
    conversation_id          TEXT,
    flow_id                  TEXT,
    principal_id             TEXT,
    client_ip                TEXT,
    identity_source          TEXT,
    source_app               TEXT,
    -- linked-tier flow/step topology (C2a).
    step_id                  TEXT,
    parent_step_id           TEXT,
    flow_step_seq            INTEGER,
    -- Experiments runner attribution (Phase D).
    experiment_id            TEXT,
    experiment_variant       TEXT,
    PRIMARY KEY (time, tenant_id, response_event_id)
);

-- Converge an older event_metrics that predates the attribution/experiment
-- columns (no-op on a fresh table created above).
ALTER TABLE aiqg.event_metrics
    ADD COLUMN IF NOT EXISTS agent_id           TEXT,
    ADD COLUMN IF NOT EXISTS agent_name         TEXT,
    ADD COLUMN IF NOT EXISTS user_id            TEXT,
    ADD COLUMN IF NOT EXISTS conversation_id    TEXT,
    ADD COLUMN IF NOT EXISTS flow_id            TEXT,
    ADD COLUMN IF NOT EXISTS principal_id       TEXT,
    ADD COLUMN IF NOT EXISTS client_ip          TEXT,
    ADD COLUMN IF NOT EXISTS identity_source    TEXT,
    ADD COLUMN IF NOT EXISTS source_app         TEXT,
    ADD COLUMN IF NOT EXISTS step_id            TEXT,
    ADD COLUMN IF NOT EXISTS parent_step_id     TEXT,
    ADD COLUMN IF NOT EXISTS flow_step_seq      INTEGER,
    ADD COLUMN IF NOT EXISTS experiment_id      TEXT,
    ADD COLUMN IF NOT EXISTS experiment_variant TEXT;

SELECT create_hypertable('aiqg.event_metrics', 'time', if_not_exists => TRUE);

-- Read-path indexes (tenant-scoped time-descending; partial on optional dims).
CREATE INDEX IF NOT EXISTS event_metrics_time_idx
    ON aiqg.event_metrics ("time" DESC);
CREATE INDEX IF NOT EXISTS idx_aiqg_em_tenant_time
    ON aiqg.event_metrics (tenant_id, "time" DESC);
CREATE INDEX IF NOT EXISTS idx_aiqg_em_tenant_workflow_time
    ON aiqg.event_metrics (tenant_id, workflow, "time" DESC) WHERE workflow IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_aiqg_em_tenant_vendor_time
    ON aiqg.event_metrics (tenant_id, vendor, "time" DESC) WHERE vendor IS NOT NULL;
CREATE INDEX IF NOT EXISTS event_metrics_agent_idx
    ON aiqg.event_metrics (tenant_id, agent_id, "time" DESC) WHERE agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS event_metrics_flow_idx
    ON aiqg.event_metrics (tenant_id, flow_id, "time" DESC) WHERE flow_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS event_metrics_experiment_idx
    ON aiqg.event_metrics (experiment_id, experiment_variant, "time" DESC) WHERE experiment_id IS NOT NULL;

-- 1-minute rollup (from raw events), grouped by tenant/workflow/vendor.
CREATE MATERIALIZED VIEW IF NOT EXISTS aiqg.event_metrics_1m
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 minute', time) AS bucket,
    tenant_id,
    workflow,
    vendor,
    count(*)                                                              AS request_count,
    avg(clear_composite)   FILTER (WHERE clear_composite   IS NOT NULL)   AS avg_composite,
    avg(clear_cost)        FILTER (WHERE clear_cost        IS NOT NULL)   AS avg_cost,
    avg(clear_latency)     FILTER (WHERE clear_latency     IS NOT NULL)   AS avg_latency,
    avg(clear_efficacy)    FILTER (WHERE clear_efficacy    IS NOT NULL)   AS avg_efficacy,
    avg(clear_assurance)   FILTER (WHERE clear_assurance   IS NOT NULL)   AS avg_assurance,
    avg(clear_reliability) FILTER (WHERE clear_reliability IS NOT NULL)   AS avg_reliability,
    sum(total_cost_usd)                                                   AS total_cost_usd,
    sum(total_tokens)                                                     AS total_tokens,
    min(end_to_end_ms)     FILTER (WHERE end_to_end_ms     IS NOT NULL)   AS min_latency_ms,
    avg(end_to_end_ms)     FILTER (WHERE end_to_end_ms     IS NOT NULL)   AS avg_latency_ms,
    max(end_to_end_ms)     FILTER (WHERE end_to_end_ms     IS NOT NULL)   AS max_latency_ms,
    sum(assurance_inbound_count)                                          AS inbound_findings,
    sum(assurance_outbound_count)                                         AS outbound_findings,
    sum(nist_secure_resilient)                                            AS nist_secure_resilient,
    sum(nist_privacy_enhanced)                                            AS nist_privacy_enhanced,
    sum(nist_valid_reliable)                                              AS nist_valid_reliable,
    sum(nist_safe)                                                        AS nist_safe
FROM aiqg.event_metrics
GROUP BY bucket, tenant_id, workflow, vendor
WITH NO DATA;

SELECT add_continuous_aggregate_policy('aiqg.event_metrics_1m',
    start_offset      => INTERVAL '5 minutes',
    end_offset        => INTERVAL '30 seconds',
    schedule_interval => INTERVAL '30 seconds',
    if_not_exists     => TRUE
);

-- 1-hour rollup (hierarchical, from the 1m aggregate). Scores are
-- request-count-weighted averages so the rollup arithmetic stays stable.
CREATE MATERIALIZED VIEW IF NOT EXISTS aiqg.event_metrics_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', bucket) AS bucket,
    tenant_id,
    workflow,
    vendor,
    sum(request_count)                                                       AS request_count,
    sum(avg_composite   * request_count) / NULLIF(sum(request_count), 0)     AS avg_composite,
    sum(avg_cost        * request_count) / NULLIF(sum(request_count), 0)     AS avg_cost,
    sum(avg_latency     * request_count) / NULLIF(sum(request_count), 0)     AS avg_latency,
    sum(avg_efficacy    * request_count) / NULLIF(sum(request_count), 0)     AS avg_efficacy,
    sum(avg_assurance   * request_count) / NULLIF(sum(request_count), 0)     AS avg_assurance,
    sum(avg_reliability * request_count) / NULLIF(sum(request_count), 0)     AS avg_reliability,
    sum(total_cost_usd)                                                      AS total_cost_usd,
    sum(total_tokens)                                                        AS total_tokens,
    min(min_latency_ms)                                                      AS min_latency_ms,
    sum(avg_latency_ms  * request_count) / NULLIF(sum(request_count), 0)     AS avg_latency_ms,
    max(max_latency_ms)                                                      AS max_latency_ms,
    sum(inbound_findings)                                                    AS inbound_findings,
    sum(outbound_findings)                                                   AS outbound_findings,
    sum(nist_secure_resilient)                                               AS nist_secure_resilient,
    sum(nist_privacy_enhanced)                                               AS nist_privacy_enhanced,
    sum(nist_valid_reliable)                                                 AS nist_valid_reliable,
    sum(nist_safe)                                                           AS nist_safe
FROM aiqg.event_metrics_1m
GROUP BY bucket, tenant_id, workflow, vendor
WITH NO DATA;

SELECT add_continuous_aggregate_policy('aiqg.event_metrics_1h',
    start_offset      => INTERVAL '4 hours',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists     => TRUE
);
