-- Isolation test for 005_aiqg_mcp_read_layer.sql.
--
-- Run against a scratch TimescaleDB that has had 001..005 (and the Spark
-- column migrations) applied, as a superuser:
--   psql -v ON_ERROR_STOP=1 -d tas_events -f 005_aiqg_mcp_read_layer.test.sql
-- Any failed check RAISEs and aborts with a non-zero exit. It never touches a
-- real tenant: it seeds two synthetic UUIDs and deletes them at the end.
--
-- NOT applied by the migrate Job — that Job globs /migrations/*.sql from the
-- ConfigMap, and deploy-timescaledb.sh lists files by name, so this file is
-- never in it.

\set ON_ERROR_STOP 1
\set ta '''11111111-1111-4111-8111-111111111111'''
\set tb '''22222222-2222-4222-8222-222222222222'''

-- Start clean: a run that aborted on a failed check never reached the cleanup
-- at the bottom, and its seed rows would double-count here.
DELETE FROM aiqg.event_metrics
 WHERE tenant_id IN (:ta, :tb);

-- Seed: tenant A has 3 events (2 models), tenant B has 2.
INSERT INTO aiqg.event_metrics
  (time, tenant_id, response_event_id, vendor, model, workflow, status,
   total_cost_usd, total_tokens, end_to_end_ms, user_id, principal_id, client_ip, raw)
VALUES
  (now() - interval '3 hours', :ta, 'a-1', 'anthropic', 'claude-x', 'qa', 'success',        0.10, 100, 900, 'ua', 'pa', '10.0.0.1', '{"secret":"a"}'),
  (now() - interval '3 hours', :ta, 'a-2', 'anthropic', 'claude-x', 'qa', 'vendor_error',   0.00,   0, 300, 'ua', 'pa', '10.0.0.1', '{"secret":"a"}'),
  (now() - interval '3 hours', :ta, 'a-3', 'openai',    'gpt-y',    'qa', 'success',        0.30, 300, 700, 'ua', 'pa', '10.0.0.1', '{"secret":"a"}'),
  (now() - interval '3 hours', :tb, 'b-1', 'openai',    'gpt-y',    'qa', 'success',        5.00, 999, 500, 'ub', 'pb', '10.0.0.2', '{"secret":"b"}'),
  (now() - interval '3 hours', :tb, 'b-2', 'openai',    'gpt-y',    'qa', 'gateway_error',  0.00,   0, 100, 'ub', 'pb', '10.0.0.2', '{"secret":"b"}');

-- Materialise every rollup so the views over them have rows to filter.
CALL refresh_continuous_aggregate('aiqg.event_metrics_1m',       NULL, now());
CALL refresh_continuous_aggregate('aiqg.event_metrics_1h',       NULL, now());
CALL refresh_continuous_aggregate('aiqg.event_metrics_model_1h', NULL, now());

-- The tests run as the reader. It is NOLOGIN in production until a password is
-- set; SET ROLE needs only membership, which a superuser has.
SET ROLE aiqg_mcp_reader;

-- 1. Fail closed: no tenant set → zero rows from every view.
DO $$
DECLARE v text; n bigint;
BEGIN
  FOREACH v IN ARRAY ARRAY['events','events_identity','usage_1m','usage_1h','usage_by_model_1h'] LOOP
    EXECUTE format('SELECT count(*) FROM aiqg_mcp.%I', v) INTO n;
    IF n <> 0 THEN RAISE EXCEPTION 'FAIL fail-closed: % returned % rows with no tenant set', v, n; END IF;
  END LOOP;
  RAISE NOTICE 'PASS 1 no tenant set -> 0 rows from all 5 views';
END $$;

-- 2. Empty string (what a reset GUC reads as) also fails closed.
BEGIN;
SET LOCAL aiqg.tenant_id = '';
DO $$
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM aiqg_mcp.events;
  IF n <> 0 THEN RAISE EXCEPTION 'FAIL empty tenant returned % rows', n; END IF;
  RAISE NOTICE 'PASS 2 empty tenant -> 0 rows';
END $$;
COMMIT;

-- 3. Tenant A sees exactly its own rows, in every view, and none of B's.
BEGIN;
SET LOCAL aiqg.tenant_id = '11111111-1111-4111-8111-111111111111';
DO $$
DECLARE n bigint; cost double precision; ids text;
BEGIN
  SELECT count(*), string_agg(response_event_id, ',' ORDER BY response_event_id) INTO n, ids FROM aiqg_mcp.events;
  IF n <> 3 OR ids <> 'a-1,a-2,a-3' THEN RAISE EXCEPTION 'FAIL events for A: n=% ids=%', n, ids; END IF;

  SELECT count(*) INTO n FROM aiqg_mcp.events_identity WHERE user_id <> 'ua' OR principal_id <> 'pa';
  IF n <> 0 THEN RAISE EXCEPTION 'FAIL events_identity leaked % foreign identities', n; END IF;

  SELECT sum(total_cost_usd) INTO cost FROM aiqg_mcp.usage_1m;
  IF round(cost::numeric, 2) <> 0.40 THEN RAISE EXCEPTION 'FAIL usage_1m cost for A = %, want 0.40', cost; END IF;
  SELECT sum(total_cost_usd) INTO cost FROM aiqg_mcp.usage_1h;
  IF round(cost::numeric, 2) <> 0.40 THEN RAISE EXCEPTION 'FAIL usage_1h cost for A = %, want 0.40', cost; END IF;

  SELECT sum(request_count) INTO n FROM aiqg_mcp.usage_by_model_1h;
  IF n <> 3 THEN RAISE EXCEPTION 'FAIL by-model request_count for A = %, want 3', n; END IF;
  SELECT error_count INTO n FROM aiqg_mcp.usage_by_model_1h WHERE model = 'claude-x';
  IF n <> 1 THEN RAISE EXCEPTION 'FAIL by-model error_count for claude-x = %, want 1', n; END IF;

  RAISE NOTICE 'PASS 3 tenant A sees exactly its 3 events and $0.40 in every view';
END $$;
COMMIT;

-- 4. Switching tenant in a new transaction switches the data; SET LOCAL does
--    not leak across transactions (the pooled-connection case).
BEGIN;
SET LOCAL aiqg.tenant_id = '22222222-2222-4222-8222-222222222222';
DO $$
DECLARE n bigint; cost double precision;
BEGIN
  SELECT count(*), sum(total_cost_usd) INTO n, cost FROM aiqg_mcp.events;
  IF n <> 2 OR round(cost::numeric, 2) <> 5.00 THEN RAISE EXCEPTION 'FAIL tenant B: n=% cost=%', n, cost; END IF;
  RAISE NOTICE 'PASS 4 tenant B sees exactly its 2 events';
END $$;
COMMIT;
DO $$
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM aiqg_mcp.events;
  IF n <> 0 THEN RAISE EXCEPTION 'FAIL SET LOCAL leaked past COMMIT: % rows', n; END IF;
  RAISE NOTICE 'PASS 5 tenant setting does not survive the transaction';
END $$;

-- 6. Withheld columns are not in the views at all, so no query can select them.
DO $$
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM information_schema.columns
   WHERE table_schema = 'aiqg_mcp' AND table_name = 'events'
     AND column_name IN ('raw','client_ip','user_id','principal_id','tenant_id','aiqg_account_id');
  IF n <> 0 THEN RAISE EXCEPTION 'FAIL aiqg_mcp.events exposes % withheld columns', n; END IF;
  SELECT count(*) INTO n FROM information_schema.columns
   WHERE table_schema = 'aiqg_mcp' AND table_name = 'events_identity'
     AND column_name IN ('raw','client_ip','tenant_id');
  IF n <> 0 THEN RAISE EXCEPTION 'FAIL aiqg_mcp.events_identity exposes % withheld columns', n; END IF;
  RAISE NOTICE 'PASS 6 raw, client_ip, tenant_id absent from the views';
END $$;

-- 7. The reader cannot reach the underlying tables, cannot write, cannot create.
--    SET ROLE escalation is NOT tested here: under SET ROLE the session user is
--    still the superuser who ran this file, so switching back is always allowed
--    and the probe would prove nothing. Check it with a real login instead:
--      ALTER ROLE aiqg_mcp_reader LOGIN PASSWORD '<scratch>';
--      psql -U aiqg_mcp_reader -c 'SET ROLE postgres'   -- must be refused
--      ALTER ROLE aiqg_mcp_reader NOLOGIN PASSWORD NULL;
DO $$
DECLARE stmt text; ok boolean;
BEGIN
  FOREACH stmt IN ARRAY ARRAY[
    'SELECT count(*) FROM aiqg.event_metrics',
    'SELECT count(*) FROM aiqg.event_metrics_1h',
    'SELECT count(*) FROM aiqg.event_metrics_model_1h',
    'SELECT count(*) FROM aiqg.identity_baseline',
    'INSERT INTO aiqg_mcp.events (response_event_id) VALUES (''x'')',
    'DELETE FROM aiqg_mcp.events',
    'CREATE TABLE public.x (i int)',
    'CREATE TABLE aiqg_mcp.x (i int)'
  ] LOOP
    ok := true;
    BEGIN
      EXECUTE stmt;
    EXCEPTION WHEN insufficient_privilege OR object_not_in_prerequisite_state OR feature_not_supported OR wrong_object_type THEN
      ok := false;
    END;
    IF ok THEN RAISE EXCEPTION 'FAIL reader was allowed: %', stmt; END IF;
  END LOOP;
  RAISE NOTICE 'PASS 7 reader refused on 4 base tables, writes and CREATE';
END $$;

-- 8. The role's safety settings are in place.
RESET ROLE;
DO $$
DECLARE r record;
BEGIN
  SELECT rolsuper, rolbypassrls, rolcanlogin, rolconnlimit, rolconfig INTO r
    FROM pg_roles WHERE rolname = 'aiqg_mcp_reader';
  IF r.rolsuper OR r.rolbypassrls THEN RAISE EXCEPTION 'FAIL reader is superuser or bypassrls'; END IF;
  IF r.rolconnlimit <> 8 THEN RAISE EXCEPTION 'FAIL reader connection limit = %', r.rolconnlimit; END IF;
  IF NOT (r.rolconfig @> ARRAY['statement_timeout=20s']) THEN RAISE EXCEPTION 'FAIL statement_timeout not set: %', r.rolconfig; END IF;
  RAISE NOTICE 'PASS 8 reader: not superuser, no bypassrls, conn limit 8, statement_timeout 20s (login=%)', r.rolcanlogin;
END $$;

-- Clean up the synthetic tenants.
DELETE FROM aiqg.event_metrics
 WHERE tenant_id IN ('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222');
CALL refresh_continuous_aggregate('aiqg.event_metrics_1m',       NULL, now());
CALL refresh_continuous_aggregate('aiqg.event_metrics_1h',       NULL, now());
CALL refresh_continuous_aggregate('aiqg.event_metrics_model_1h', NULL, now());

\echo 'ALL CHECKS PASSED'
