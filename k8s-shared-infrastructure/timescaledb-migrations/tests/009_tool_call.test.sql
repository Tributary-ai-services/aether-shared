-- Run against a scratch database after 001..004, tas-spark-jobs/migrations/*.sql
-- (they add the cache and cost columns 005 and 008 read), then 005..009:
--   psql -v ON_ERROR_STOP=1 -f 009_tool_call.test.sql
-- Fails (RAISE EXCEPTION) on any mismatch. Writes only rows for tenants tt-*
-- and removes them at the end, so it can be re-run.
\set ON_ERROR_STOP 1

-- Constraints reject what the contract does not allow.
DO $$
BEGIN
  BEGIN
    INSERT INTO aiqg.tool_call_events (occurred_at, tenant_id, event_id, tool_name, observed_by, outcome)
    VALUES ('2026-11-01 10:00+00', 'tt-bad', 'e-bad1', 'x', 'client', 'ok');
    RAISE EXCEPTION 'observed_by=client was accepted';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO aiqg.tool_call_events (occurred_at, tenant_id, event_id, tool_name, observed_by, outcome)
    VALUES ('2026-11-01 10:00+00', 'tt-bad', 'e-bad2', 'x', 'proxy', 'success');
    RAISE EXCEPTION 'outcome=success was accepted';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO aiqg.tool_call_events (occurred_at, tenant_id, event_id, tool_name, observed_by, outcome, findings)
    VALUES ('2026-11-01 10:00+00', 'tt-bad', 'e-bad3', 'x', 'proxy', 'ok', '[1,2]');
    RAISE EXCEPTION 'findings array was accepted';
  EXCEPTION WHEN check_violation THEN NULL; END;
END $$;

-- findings_total: sums numeric values, ignores the rest, NULL/empty = 0.
DO $$
BEGIN
  IF aiqg.findings_total('{"email": 2, "ssn": 1}') <> 3 THEN RAISE EXCEPTION 'findings_total sum'; END IF;
  IF aiqg.findings_total('{"email": "two"}') <> 0 THEN RAISE EXCEPTION 'findings_total non-numeric'; END IF;
  IF aiqg.findings_total('{}') <> 0 OR aiqg.findings_total(NULL) <> 0 THEN RAISE EXCEPTION 'findings_total empty'; END IF;
END $$;

INSERT INTO aiqg.tool_call_events (occurred_at, tenant_id, event_id, token_id, owner_sub, source_app, agent_id,
    tool_call_id, tool_name, server_id, observed_by, outcome, duration_ms, round_trip_ms,
    result_bytes, result_tokens_est, reduced_bytes, findings, scan_skipped)
VALUES
 -- tt1 / napkin / draw, proxy, ok x3 in 10h: latencies 80, 900, 6000
 ('2026-11-01 10:01+00', 'tt1', 'e1', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c1', 'draw', 'napkin-mcp', 'proxy', 'ok',   80, NULL, 1000, 250, 400, '{"email": 2}', false),
 ('2026-11-01 10:02+00', 'tt1', 'e2', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c2', 'draw', 'napkin-mcp', 'proxy', 'ok',  900, NULL, 3000, 750, NULL, NULL, true),
 ('2026-11-01 10:03+00', 'tt1', 'e3', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c3', 'draw', 'napkin-mcp', 'proxy', 'ok', 6000, NULL,  500, 125, NULL, '{"email": 1, "ssn": 1}', false),
 -- same key, timeout: its own row
 ('2026-11-01 10:04+00', 'tt1', 'e4', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c4', 'draw', 'napkin-mcp', 'proxy', 'timeout', 30000, NULL, NULL, NULL, NULL, NULL, NULL),
 -- merged row (proxy + gateway): duration from proxy, round trip from gateway
 ('2026-11-01 10:05+00', 'tt1', 'e5', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c5', 'draw', 'napkin-mcp', 'both', 'ok', 200, 1500, 100, 25, NULL, NULL, false),
 -- gateway only (not via the proxy): no server, round trip is the latency
 ('2026-11-01 10:06+00', 'tt1', 'e6', NULL, NULL, 'agent-builder', 'ag-1',
  'c6', 'local_fn', NULL, 'gateway', 'no_result', NULL, 2000, NULL, NULL, NULL, NULL, NULL),
 -- next hour
 ('2026-11-01 11:00+00', 'tt1', 'e7', '00000000-0000-0000-0000-000000000001', 'kc-a', 'claude-code', NULL,
  'c7', 'draw', 'napkin-mcp', 'proxy', 'ok', 100, NULL, 10, 3, NULL, NULL, false),
 -- another tenant, unattributed caller
 ('2026-11-01 10:30+00', 'tt2', 'e8', NULL, NULL, NULL, NULL,
  NULL, 'query', 'postgres-mcp', 'proxy', 'unreachable', NULL, NULL, NULL, NULL, NULL, NULL, NULL);

CALL refresh_continuous_aggregate('aiqg.tool_call_1h', '2026-11-01', '2026-11-02');

DO $$
DECLARE r record; n int;
BEGIN
  SELECT count(*) INTO n FROM aiqg.tool_call_1h WHERE tenant_id LIKE 'tt%' AND bucket >= '2026-11-01' AND bucket < '2026-11-02';
  IF n <> 6 THEN RAISE EXCEPTION 'rows = %, want 6 (proxy ok, proxy timeout, both, gateway, tt1 11h, tt2)', n; END IF;

  SELECT * INTO r FROM aiqg.tool_call_1h
   WHERE tenant_id='tt1' AND bucket='2026-11-01 10:00+00' AND observed_by='proxy' AND outcome='ok';
  IF r.call_count <> 3 OR r.duration_n <> 3 OR r.duration_ms_sum <> 6980 OR r.duration_ms_max <> 6000
     OR r.round_trip_n <> 0 OR r.latency_n <> 3 THEN
    RAISE EXCEPTION 'proxy ok counts wrong: %', r; END IF;
  -- cumulative histogram: 80 | 900 | 6000
  IF r.latency_le_100ms <> 1 OR r.latency_le_250ms <> 1 OR r.latency_le_500ms <> 1 OR r.latency_le_1s <> 2
     OR r.latency_le_2500ms <> 2 OR r.latency_le_5s <> 2 OR r.latency_le_10s <> 3 OR r.latency_le_30s <> 3 THEN
    RAISE EXCEPTION 'histogram wrong: %', r; END IF;
  IF r.result_bytes <> 4500 OR r.result_tokens_est <> 1125 OR r.reduced_bytes <> 400 THEN
    RAISE EXCEPTION 'bytes/tokens wrong: %', r; END IF;
  IF r.findings_total <> 4 OR r.results_with_findings <> 2 OR r.scan_skipped_count <> 1 THEN
    RAISE EXCEPTION 'findings wrong: %', r; END IF;

  -- 30 s is inside the last bound; nothing above it is lost from latency_n.
  SELECT * INTO r FROM aiqg.tool_call_1h WHERE tenant_id='tt1' AND outcome='timeout';
  IF r.call_count <> 1 OR r.latency_le_10s <> 0 OR r.latency_le_30s <> 1 THEN RAISE EXCEPTION 'timeout row wrong: %', r; END IF;

  -- merged row: histogram uses the proxy's duration, round trip kept apart.
  SELECT * INTO r FROM aiqg.tool_call_1h WHERE tenant_id='tt1' AND observed_by='both';
  IF r.call_count <> 1 OR r.duration_ms_sum <> 200 OR r.round_trip_ms_sum <> 1500 OR r.latency_le_250ms <> 1 THEN
    RAISE EXCEPTION 'both row wrong: %', r; END IF;

  -- gateway-only: NULL server kept as its own group, latency from round trip.
  SELECT * INTO r FROM aiqg.tool_call_1h WHERE tenant_id='tt1' AND observed_by='gateway';
  IF r.server_id IS NOT NULL OR r.outcome <> 'no_result' OR r.latency_n <> 1 OR r.latency_le_2500ms <> 1
     OR r.latency_le_1s <> 0 OR r.findings_total <> 0 THEN
    RAISE EXCEPTION 'gateway row wrong: %', r; END IF;

  -- NULL actors keep their own row.
  IF NOT EXISTS (SELECT 1 FROM aiqg.tool_call_1h WHERE tenant_id='tt2' AND token_id IS NULL
                 AND outcome='unreachable' AND call_count=1 AND latency_n=0) THEN
    RAISE EXCEPTION 'NULL-actor row missing'; END IF;

  -- rollup total equals raw.
  IF (SELECT sum(call_count) FROM aiqg.tool_call_1h WHERE tenant_id='tt1' AND bucket >= '2026-11-01' AND bucket < '2026-11-02')
     <> (SELECT count(*) FROM aiqg.tool_call_events WHERE tenant_id='tt1') THEN
    RAISE EXCEPTION 'tenant total mismatch'; END IF;
END $$;

-- Catalogue: NULL tenant/server snapshots dedupe.
INSERT INTO aiqg.tool_catalogue (snapshot_at, server_id, tool_name, schema_bytes, observed_by, tenant_id)
VALUES ('2026-11-01 10:00+00', 'napkin-mcp', 'draw', 812, 'proxy', NULL),
       ('2026-11-01 10:00+00', NULL, 'local_fn', 120, 'gateway', 'tt1');
INSERT INTO aiqg.tool_catalogue (snapshot_at, server_id, tool_name, schema_bytes, observed_by, tenant_id)
VALUES ('2026-11-01 10:00+00', 'napkin-mcp', 'draw', 812, 'proxy', NULL),
       ('2026-11-01 10:00+00', NULL, 'local_fn', 120, 'gateway', 'tt1')
ON CONFLICT DO NOTHING;
DO $$
BEGIN
  IF (SELECT count(*) FROM aiqg.tool_catalogue WHERE snapshot_at = '2026-11-01 10:00+00'
        AND tool_name IN ('draw', 'local_fn')) <> 2 THEN
    RAISE EXCEPTION 'catalogue duplicate snapshot was kept'; END IF;
END $$;

-- Never-called shape: offered in the catalogue, absent from calls.
INSERT INTO aiqg.tool_catalogue (snapshot_at, server_id, tool_name, schema_bytes, observed_by, tenant_id)
VALUES ('2026-11-01 10:00+00', 'napkin-mcp', 'erase', 640, 'proxy', NULL) ON CONFLICT DO NOTHING;
DO $$
DECLARE n int;
BEGIN
  SELECT count(*) INTO n FROM aiqg.tool_catalogue c
   WHERE c.observed_by = 'proxy' AND c.server_id = 'napkin-mcp' AND c.snapshot_at = '2026-11-01 10:00+00'
     AND NOT EXISTS (SELECT 1 FROM aiqg.tool_call_1h h
                      WHERE h.server_id = c.server_id AND h.tool_name = c.tool_name
                        AND h.bucket >= '2026-11-01' AND h.bucket < '2026-11-02');
  IF n <> 1 THEN RAISE EXCEPTION 'never-called = %, want 1 (erase)', n; END IF;
  RAISE NOTICE '009 tool_call: all checks passed';
END $$;

-- Clean up so a re-run starts empty.
DELETE FROM aiqg.tool_call_events WHERE tenant_id LIKE 'tt%';
DELETE FROM aiqg.tool_catalogue WHERE snapshot_at = '2026-11-01 10:00+00' AND tool_name IN ('draw', 'local_fn', 'erase');
CALL refresh_continuous_aggregate('aiqg.tool_call_1h', '2026-11-01', '2026-11-02');
