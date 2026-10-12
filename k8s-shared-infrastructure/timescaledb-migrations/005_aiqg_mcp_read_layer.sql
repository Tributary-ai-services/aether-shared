-- AIQG customer data MCP server, Phase 0: the read layer (tas_events DB).
--
-- The MCP server (aiqg-data-mcp) reads a tenant's data on behalf of that
-- tenant's AI client. This file gives it a schema of views and a role that can
-- read ONLY those views, so tenant isolation is enforced by the database
-- instead of by every query remembering its WHERE clause.
--
-- WHY VIEWS THAT FILTER, NOT ROW-LEVEL SECURITY
-- Tested against timescale/timescaledb:2.13.0-pg15, the version in production:
--   * RLS cannot be enabled on a continuous aggregate ("ALTER action ENABLE ROW
--     SECURITY ... not supported for views"), and the rollups are what most
--     tools read.
--   * A view runs with its OWNER's privileges. These views are owned by
--     tas_events_rw, which is a superuser with BYPASSRLS, so RLS on the
--     underlying hypertable would be skipped anyway.
-- So the tenant predicate lives in every view, keyed on a transaction-local
-- setting the server sets from the token:
--     BEGIN; SET LOCAL aiqg.tenant_id = '<uuid>'; SELECT ...; COMMIT;
--
-- FAIL CLOSED
-- aiqg_mcp.current_tenant() returns NULL when the setting is missing or empty,
-- and `tenant_id = NULL` matches nothing. Forgetting SET LOCAL returns zero
-- rows, never every tenant's rows.
--
-- security_barrier stops a caller-supplied predicate (e.g. a function with
-- side effects) from being evaluated before the tenant filter and leaking rows.
--
-- WHAT IS DELIBERATELY LEFT OUT OF aiqg_mcp.events
--   raw            the whole CloudEvent; get_event serves a curated projection
--   client_ip      never exposed
--   user_id, principal_id   exposed only by events_identity (data:identity)
--   aiqg_account_id         internal
--
-- Every statement is idempotent: the migration Job re-runs every *.sql file.

CREATE SCHEMA IF NOT EXISTS aiqg_mcp;

-- The tenant for this transaction, or NULL. current_setting(..., true) returns
-- NULL for an unset GUC and '' once a GUC has been set and reset in the
-- session, so both are normalised to NULL.
CREATE OR REPLACE FUNCTION aiqg_mcp.current_tenant() RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE
AS $$ SELECT NULLIF(current_setting('aiqg.tenant_id', true), '') $$;

-- Per-request events, metadata only.
CREATE OR REPLACE VIEW aiqg_mcp.events WITH (security_barrier = true) AS
SELECT time, response_event_id, request_event_id,
       vendor, model, workflow, status, http_status, finish_reason,
       clear_composite, clear_cost, clear_latency, clear_efficacy,
       clear_assurance, clear_reliability,
       end_to_end_ms, prompt_tokens, completion_tokens, total_tokens,
       total_cost_usd, cache_creation_tokens, cache_read_tokens,
       cache_aware_total_cost_usd,
       assurance_inbound_count, assurance_outbound_count,
       nist_secure_resilient, nist_privacy_enhanced, nist_valid_reliable, nist_safe,
       agent_id, agent_name, conversation_id, flow_id, step_id, parent_step_id,
       flow_step_seq, source_app, identity_source, reduction_mode,
       experiment_id, experiment_variant
FROM aiqg.event_metrics
WHERE tenant_id = aiqg_mcp.current_tenant();

-- The same rows plus the person-level identifiers. The server reads this view
-- instead of aiqg_mcp.events only for a token carrying data:identity.
CREATE OR REPLACE VIEW aiqg_mcp.events_identity WITH (security_barrier = true) AS
SELECT time, response_event_id, user_id, principal_id
FROM aiqg.event_metrics
WHERE tenant_id = aiqg_mcp.current_tenant();

-- The two existing rollups, tenant-filtered. tenant_id itself is dropped:
-- the caller already knows its own tenant.
CREATE OR REPLACE VIEW aiqg_mcp.usage_1m WITH (security_barrier = true) AS
SELECT bucket, workflow, vendor, request_count,
       avg_composite, avg_cost, avg_latency, avg_efficacy, avg_assurance, avg_reliability,
       total_cost_usd, total_tokens, min_latency_ms, avg_latency_ms, max_latency_ms,
       inbound_findings, outbound_findings,
       nist_secure_resilient, nist_privacy_enhanced, nist_valid_reliable, nist_safe
FROM aiqg.event_metrics_1m
WHERE tenant_id = aiqg_mcp.current_tenant();

CREATE OR REPLACE VIEW aiqg_mcp.usage_1h WITH (security_barrier = true) AS
SELECT bucket, workflow, vendor, request_count,
       avg_composite, avg_cost, avg_latency, avg_efficacy, avg_assurance, avg_reliability,
       total_cost_usd, total_tokens, min_latency_ms, avg_latency_ms, max_latency_ms,
       inbound_findings, outbound_findings,
       nist_secure_resilient, nist_privacy_enhanced, nist_valid_reliable, nist_safe
FROM aiqg.event_metrics_1h
WHERE tenant_id = aiqg_mcp.current_tenant();

-- By-model hourly rollup. The existing rollups are grouped by workflow and
-- vendor only, so "cost by model" would otherwise scan raw events.
--
-- GROUP BY repeats the time_bucket expression rather than naming the alias:
-- in a GROUP BY, a bare `bucket` is ambiguous between the output alias and an
-- input column of that name, and TimescaleDB rejects the view ("must include a
-- valid time bucket function"). That is the bug fixed in 004's 1h rollup.
-- Here the input has no `bucket` column, but the explicit form is kept so the
-- two definitions read the same.
--
-- Error counts are split by status (success / gateway_error / vendor_error as
-- of 2026-10) so a dashboard can show error rate without a raw scan.
CREATE MATERIALIZED VIEW IF NOT EXISTS aiqg.event_metrics_model_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time)                                          AS bucket,
    tenant_id,
    vendor,
    model,
    count(*)                                                             AS request_count,
    count(*) FILTER (WHERE status = 'success')                           AS success_count,
    count(*) FILTER (WHERE status IS DISTINCT FROM 'success')            AS error_count,
    sum(total_cost_usd)                                                  AS total_cost_usd,
    sum(prompt_tokens)                                                   AS prompt_tokens,
    sum(completion_tokens)                                               AS completion_tokens,
    sum(total_tokens)                                                    AS total_tokens,
    sum(cache_read_tokens)                                               AS cache_read_tokens,
    avg(end_to_end_ms)   FILTER (WHERE end_to_end_ms   IS NOT NULL)      AS avg_latency_ms,
    max(end_to_end_ms)   FILTER (WHERE end_to_end_ms   IS NOT NULL)      AS max_latency_ms,
    avg(clear_composite) FILTER (WHERE clear_composite IS NOT NULL)      AS avg_composite,
    avg(clear_efficacy)  FILTER (WHERE clear_efficacy  IS NOT NULL)      AS avg_efficacy
FROM aiqg.event_metrics
GROUP BY time_bucket('1 hour', time), tenant_id, vendor, model
WITH NO DATA;

SELECT add_continuous_aggregate_policy('aiqg.event_metrics_model_1h',
    start_offset      => INTERVAL '4 hours',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists     => TRUE
);

CREATE OR REPLACE VIEW aiqg_mcp.usage_by_model_1h WITH (security_barrier = true) AS
SELECT bucket, vendor, model, request_count, success_count, error_count,
       total_cost_usd, prompt_tokens, completion_tokens, total_tokens, cache_read_tokens,
       avg_latency_ms, max_latency_ms, avg_composite, avg_efficacy
FROM aiqg.event_metrics_model_1h
WHERE tenant_id = aiqg_mcp.current_tenant();

-- The read-only role. Created NOLOGIN here: the password is set out of band
-- (`ALTER ROLE aiqg_mcp_reader LOGIN PASSWORD ...` from a Secret), so no
-- credential ever lands in this repo or in the migrations ConfigMap.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'aiqg_mcp_reader') THEN
        CREATE ROLE aiqg_mcp_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
            NOINHERIT NOREPLICATION NOBYPASSRLS;
    END IF;
END
$$;

-- It can reach the aiqg_mcp schema and nothing else. It gets no USAGE on aiqg,
-- so the views work (they run as their owner) while a direct
-- SELECT on aiqg.event_metrics is refused. The rows themselves come from the
-- views, which run as their owner.
GRANT USAGE ON SCHEMA aiqg_mcp TO aiqg_mcp_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA aiqg_mcp TO aiqg_mcp_reader;
GRANT EXECUTE ON FUNCTION aiqg_mcp.current_tenant() TO aiqg_mcp_reader;
REVOKE ALL ON SCHEMA aiqg FROM aiqg_mcp_reader;
REVOKE CREATE ON SCHEMA public FROM aiqg_mcp_reader;

-- Bound what one bad query can cost. The server also sets its own timeouts;
-- these hold even if it forgets.
ALTER ROLE aiqg_mcp_reader SET statement_timeout = '20s';
ALTER ROLE aiqg_mcp_reader SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE aiqg_mcp_reader CONNECTION LIMIT 8;
