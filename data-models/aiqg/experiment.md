# Experiment (AIQG Experiments Runner)

Data model for the AIQG **Experiments runner** — split a cohort of live LLM
traffic across variants and measure the outcome. Authoritative design:
[`tas-llm-router/docs/AIQG-EXPERIMENTS-RUNNER.md`](https://github.com/Tributary-ai-services/tas-llm-router/blob/main/docs/AIQG-EXPERIMENTS-RUNNER.md).

Owned by **aiqg-dashboard-be** (PostgreSQL, `aiqg` schema). The gateway
(**tas-llm-router**) reads active experiments via `/internal/experiments` and
assigns/reroutes; the **Spark aggregator** lands per-variant metrics in
TimescaleDB.

## Tables (migration `011_experiments.sql`)

```
aiqg.experiment
  id            UUID PK
  tenant_id     UUID            -- isolation boundary; experiments never cross tenants
  name          TEXT
  description   TEXT
  status        TEXT            -- draft|dry_run|running|paused|completed|archived (CHECK)
  cohort        JSONB           -- eligibility matcher (below)
  assignment    JSONB           -- sticky-key config (below)
  guardrails    JSONB           -- max_traffic_pct + stop_on + min_samples (below)
  starts_at     TIMESTAMPTZ     -- optional schedule window
  ends_at       TIMESTAMPTZ
  created_by    TEXT            -- Keycloak sub
  created_at    TIMESTAMPTZ
  updated_at    TIMESTAMPTZ

aiqg.experiment_variant
  id            UUID PK
  experiment_id UUID FK → experiment(id) ON DELETE CASCADE
  key           TEXT            -- "control" = baseline (empty override); UNIQUE(experiment_id,key)
  weight        INT             -- split share; weights across an experiment sum to 100
  override      JSONB           -- what this arm changes (below)

aiqg.experiment_transition          -- audit trail of every lifecycle move
  id, experiment_id, from_status, to_status, actor, reason, at
```

## JSONB shapes (gateway-interpreted)

```jsonc
// cohort — empty fields match anything; set fields AND across, OR within a list
{ "url_path": "/v1/chat",            // substring match
  "source_app": ["checkout"],         // TAS-Source-App / token source_app
  "model": ["gpt-4o"],                // REQUESTED model (pre-override)
  "workflow_type": ["rag"] }          // classified workflow

// assignment — sticky bucketing
{ "key_source": "conversation",       // conversation|user|flow|principal|ip|request
  "salt": "v1" }                      // decorrelates assignment across experiments

// guardrails
{ "max_traffic_pct": 5,               // eligible share; rest left untouched (≤ tenant ceiling 50)
  "min_samples": 100,                 // both arms need ≥ this before auto-stop can trip
  "stop_on": { "error_delta": 0.02,   // pause if variant err > control + 2pp
               "latency_factor": 1.5, //   or variant p95 > control × 1.5
               "cost_factor": 2.0 } } //   or variant avg cost > control × 2

// variant.override — a TYPED SET OF AXES; an experiment varies one (rarely more)
{ "model": "gpt-4o-mini",             // model swap (canonical) — reroutes via model→provider
  "params": { "temperature": 0.2, "top_p": 1, "max_tokens": 256 } }
// (system_prompt / prompt_template_id / policy_bundle_id / gatekeeper_profile axes: design §3, not all wired)
```

## Lifecycle (§8)

```
draft → dry_run → running → paused → completed → archived
```
- **dry_run** — gateway ASSIGNS variants + stamps `experiment_id`/`experiment_variant`
  on events, but does NOT reroute (everyone routes to control). Safe pre-flight.
- **running** — live split; the variant's override is applied before `Router.Route`,
  within `max_traffic_pct`; auto-stop armed.
- **paused** — all traffic → control (manual transition or auto-stop trip).
- `dry_run → running` is the only transition that changes production routing.
- Cohort/assignment/variants are **immutable once past draft/dry_run** (a live split's
  control vs variant populations must come from one claimed population).

## Assignment (deterministic, sticky)

```
key     = first non-empty of the key_source ladder, ending at a per-request id
bucket  = crc32(experiment_id + ":" + salt + ":" + key) % 10000
eligible band = max_traffic_pct × 100 buckets; bucket ≥ band → untouched (route normally)
variant = walk cumulative variant weights across the eligible band
```
Same identity → same variant for the experiment's life (no mid-conversation flips).
v1 **collision rule**: the first matching cohort (by `created_at`) claims a request;
it participates in that experiment only (claim-on-match).

## Attribution on events

A claimed request's response event carries `experiment_id` + `experiment_variant`
(promoted to the Loki line and projected into `aiqg.event_metrics` columns by the
Spark aggregator). Per-variant results — requests, error rate, avg cost, p95
latency, avg CLEAR composite — come from `GROUP BY experiment_variant` over
`event_metrics` (`GET /api/v1/experiments/{id}/results`), and feed the auto-stop
evaluator.

## API (aiqg-dashboard-be)

```
GET    /api/v1/experiments                  list (tenant-scoped)
POST   /api/v1/experiments                  create (draft)
GET    /api/v1/experiments/{id}             detail
PATCH  /api/v1/experiments/{id}             edit (cohort/variants only in draft/dry_run)
POST   /api/v1/experiments/{id}/transition  { to, reason }  (§8 state machine enforced)
DELETE /api/v1/experiments/{id}             archive (soft)
GET    /api/v1/experiments/{id}/results     per-variant metrics (?days=, default 7)
GET    /api/v1/experiments/{id}/transitions audit trail
GET    /internal/experiments?tenant=…       gateway cache-load (Internal-Auth)
```
Validation: ≥2 variants, exactly one `control`, weights sum to 100, `max_traffic_pct`
≤ tenant ceiling.

## Related

- [`response-event.md`](./response-event.md) — `experiment_id`/`experiment_variant` fields.
- [`aggregated-metrics.md`](./aggregated-metrics.md) — the per-variant rollup dimension.
- [`route-rule.md`](./route-rule.md) — cohort matchers + `priority` model reused conceptually.
- [`response-feedback.md`](./response-feedback.md) — explicit-feedback gold signal joined per variant (quality objectives).
