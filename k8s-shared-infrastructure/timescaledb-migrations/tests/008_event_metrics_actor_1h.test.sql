-- Run against a scratch database after 001..004, tas-spark-jobs/migrations/*.sql
-- (they add the cache and cost columns), then 005..008:
--   psql -v ON_ERROR_STOP=1 -f 008_event_metrics_actor_1h.test.sql
-- Fails (RAISE EXCEPTION) on any mismatch. Cost columns are double precision,
-- so money is compared within 1e-9.
\set ON_ERROR_STOP 1
INSERT INTO aiqg.event_metrics (time, tenant_id, token_id, owner_sub, user_id, source_app, workflow, vendor, model, status,
                                total_cost_usd, total_tokens, end_to_end_ms, clear_composite, assurance_inbound_count, response_event_id)
VALUES
 ('2026-11-01 10:05+00', 't1', '00000000-0000-0000-0000-000000000001', 'kc-a', 'u1', 'claude-code', 'qa', 'anthropic', 'm', 'success', 0.10, 100, 1000, 80, 1, 'r1'),
 ('2026-11-01 10:35+00', 't1', '00000000-0000-0000-0000-000000000001', 'kc-a', 'u1', 'claude-code', 'qa', 'anthropic', 'm', 'vendor_error', 0.20, 200, NULL, NULL, 0, 'r2'),
 ('2026-11-01 10:40+00', 't1', '00000000-0000-0000-0000-000000000002', 'kc-b', 'u2', 'claude-code', 'qa', 'anthropic', 'm', 'success', 0.30, 300, 3000, 60, 2, 'r3'),
 ('2026-11-01 11:10+00', 't1', '00000000-0000-0000-0000-000000000001', 'kc-a', 'u1', 'claude-code', 'qa', 'anthropic', 'm', 'success', 0.40, 400, 2000, 100, 0, 'r4'),
 ('2026-11-01 10:15+00', 't2', NULL, NULL, NULL, NULL, 'qa', 'openai', 'm', 'success', 1.00, 1000, 500, 50, 0, 'r5');
CALL refresh_continuous_aggregate('aiqg.event_metrics_actor_1h', '2026-11-01', '2026-11-02');
DO $$
DECLARE r record; n int;
BEGIN
  SELECT count(*) INTO n FROM aiqg.event_metrics_actor_1h WHERE bucket >= '2026-11-01' AND bucket < '2026-11-02';
  IF n <> 4 THEN RAISE EXCEPTION 'rows = %, want 4 (t1 token1 10h, t1 token2 10h, t1 token1 11h, t2 null 10h)', n; END IF;
  SELECT * INTO r FROM aiqg.event_metrics_actor_1h WHERE tenant_id='t1' AND owner_sub='kc-a' AND bucket='2026-11-01 10:00+00';
  IF r.request_count <> 2 OR r.success_count <> 1 OR r.error_count <> 1 OR abs(r.total_cost_usd - 0.30) > 1e-9 OR r.total_tokens <> 300
     OR r.latency_n <> 1 OR r.latency_ms_sum <> 1000 OR r.clear_n <> 1 OR r.clear_composite_sum <> 80 OR r.inbound_findings <> 1 THEN
    RAISE EXCEPTION 'kc-a 10h row wrong: %', r; END IF;
  -- Billed total equals the tenant total from raw events (design §13).
  IF abs((SELECT sum(total_cost_usd) FROM aiqg.event_metrics_actor_1h WHERE tenant_id='t1' AND bucket >= '2026-11-01' AND bucket < '2026-11-02')
     - (SELECT sum(total_cost_usd) FROM aiqg.event_metrics WHERE tenant_id='t1' AND time >= '2026-11-01' AND time < '2026-11-02')) > 1e-9 THEN
    RAISE EXCEPTION 'tenant total mismatch'; END IF;
  -- NULL actors keep their own row rather than vanishing.
  IF NOT EXISTS (SELECT 1 FROM aiqg.event_metrics_actor_1h WHERE tenant_id='t2' AND token_id IS NULL AND abs(total_cost_usd - 1.00) < 1e-9) THEN
    RAISE EXCEPTION 'NULL-actor row missing'; END IF;
  RAISE NOTICE '008 actor_1h: all checks passed';
END $$;
