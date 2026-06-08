-- TAS Events Pipeline: compression and retention policies.
-- Raw: 30 days (compressed after 3 days)
-- 1m:  90 days
-- 1h:  1 year
-- 1d:  7 years

ALTER TABLE events SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tenant_id, type',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('events',
    INTERVAL '3 days',
    if_not_exists => TRUE
);

SELECT add_retention_policy('events',
    INTERVAL '30 days',
    if_not_exists => TRUE
);

SELECT add_retention_policy('events_1m',
    INTERVAL '90 days',
    if_not_exists => TRUE
);

SELECT add_retention_policy('events_1h',
    INTERVAL '365 days',
    if_not_exists => TRUE
);

SELECT add_retention_policy('events_1d',
    INTERVAL '2555 days',
    if_not_exists => TRUE
);
