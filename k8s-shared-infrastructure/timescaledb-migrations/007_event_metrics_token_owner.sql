-- Plan #22 WP1.3: record which token, and whose, sent each event.
--
-- token_id is the aiqg.token row (Postgres) that authenticated the request;
-- owner_sub is the Keycloak `sub` of the person who issued it (aiqg-dashboard-be
-- migration 052). Together they are the authenticated "who" behind a request
-- (design org-structure-and-attribution.md §4.1). user_id stays what the caller
-- asserted through baggage; the two are kept side by side and the person is
-- resolved at read time, never written here.
--
-- Both are nullable and additive: rows written before the gateway stamps them
-- (tas-llm-router WP1.5) and events from unowned tokens stay NULL. The Spark
-- aiqg-aggregator maps them from the response event (tas-spark-jobs WP1.4);
-- this file must be applied before that job deploys, or its INSERT names
-- columns that do not exist.
--
-- No index: nothing filters raw events on these yet. The P4 rollup
-- event_metrics_actor_1h groups on them and is where reads will go.
--
-- Idempotent (ADD COLUMN IF NOT EXISTS): the migrate Job re-runs every file.

ALTER TABLE aiqg.event_metrics
    ADD COLUMN IF NOT EXISTS token_id  UUID,
    ADD COLUMN IF NOT EXISTS owner_sub TEXT;
