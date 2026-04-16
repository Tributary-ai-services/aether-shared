-- TAS Events Pipeline: continuous aggregates for rollup queries.
-- events_1m feeds the Live Streams stats endpoint (events/sec, active sources).
-- events_1h and events_1d support historical dashboard queries.

-- 1-minute rollup (from raw events)
CREATE MATERIALIZED VIEW IF NOT EXISTS events_1m
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 minute', time) AS bucket,
    tenant_id,
    source,
    type,
    COALESCE(severity, 'none') AS severity,
    count(*)                                        AS event_count,
    count(*) FILTER (WHERE severity = 'critical')   AS critical_count,
    count(*) FILTER (WHERE severity = 'high')       AS high_count
FROM events
GROUP BY bucket, tenant_id, source, type, COALESCE(severity, 'none')
WITH NO DATA;

SELECT add_continuous_aggregate_policy('events_1m',
    start_offset  => INTERVAL '10 minutes',
    end_offset    => INTERVAL '1 minute',
    schedule_interval => INTERVAL '30 seconds',
    if_not_exists => TRUE
);

-- 1-hour rollup (from events_1m — hierarchical)
CREATE MATERIALIZED VIEW IF NOT EXISTS events_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', bucket) AS bucket,
    tenant_id,
    source,
    type,
    severity,
    sum(event_count)    AS event_count,
    sum(critical_count) AS critical_count,
    sum(high_count)     AS high_count
FROM events_1m
GROUP BY time_bucket('1 hour', bucket), tenant_id, source, type, severity
WITH NO DATA;

SELECT add_continuous_aggregate_policy('events_1h',
    start_offset  => INTERVAL '3 hours',
    end_offset    => INTERVAL '10 minutes',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists => TRUE
);

-- 1-day rollup (from events_1h — hierarchical)
CREATE MATERIALIZED VIEW IF NOT EXISTS events_1d
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', bucket) AS bucket,
    tenant_id,
    source,
    type,
    severity,
    sum(event_count)    AS event_count,
    sum(critical_count) AS critical_count,
    sum(high_count)     AS high_count
FROM events_1h
GROUP BY time_bucket('1 day', bucket), tenant_id, source, type, severity
WITH NO DATA;

SELECT add_continuous_aggregate_policy('events_1d',
    start_offset  => INTERVAL '3 days',
    end_offset    => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes',
    if_not_exists => TRUE
);
