-- TAS Events Pipeline: raw events hypertable
-- Stores CloudEvents 1.0 messages from all TAS activity + compliance topics.

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS events (
    time           TIMESTAMPTZ       NOT NULL,
    event_id       TEXT              NOT NULL,
    tenant_id      TEXT              NOT NULL DEFAULT '',
    source         TEXT              NOT NULL,
    type           TEXT              NOT NULL,
    subject        TEXT              NULL,
    space_id       TEXT              NULL,
    user_id        TEXT              NULL,
    request_id     TEXT              NULL,
    severity       TEXT              NULL,
    frameworks     TEXT[]            NULL,
    data           JSONB             NULL,
    raw            JSONB             NOT NULL,
    ingested_at    TIMESTAMPTZ       NOT NULL DEFAULT now(),
    PRIMARY KEY (time, tenant_id, event_id)
);

SELECT create_hypertable(
    'events', 'time',
    partitioning_column => 'tenant_id',
    number_partitions => 4,
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

CREATE INDEX IF NOT EXISTS idx_events_tenant_time
    ON events (tenant_id, time DESC);

CREATE INDEX IF NOT EXISTS idx_events_tenant_type_time
    ON events (tenant_id, type, time DESC);

CREATE INDEX IF NOT EXISTS idx_events_tenant_sev_time
    ON events (tenant_id, severity, time DESC)
    WHERE severity IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_events_subject
    ON events (subject) WHERE subject IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_events_frameworks_gin
    ON events USING GIN (frameworks);

CREATE INDEX IF NOT EXISTS idx_events_data_gin
    ON events USING GIN (data jsonb_path_ops);
