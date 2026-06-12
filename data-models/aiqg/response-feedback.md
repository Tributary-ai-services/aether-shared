# AIQG Response Feedback

---

**Metadata**

```yaml
service: aiqg-dashboard-be
model: Feedback
database: PostgreSQL (shared instance, schema `aiqg`)
table: aiqg.response_feedback
version: 1.0.0
last_updated: 2026-06-12
status: implemented (migration 010; non-breaking addition)
spec_refs: tas-llm-router/docs/AIQG-EXPERIMENTS-RUNNER.md §6.5, §14 item 6
plan_ref: source-spec-v0.2.md §5.4 (outcome webhook integration)
```

---

## 1. Overview

### Purpose
`aiqg.response_feedback` stores **explicit, app-reported outcome signals** (thumb, rating, task success, reward, …) correlated to AIQG response events. It is the **gold tier** of the quality-metric ladder defined in the experiments-runner design: structural → behavioral → **explicit feedback** → LLM-as-judge. Built as a standalone capability that experiments *consume* — it also augments CLEAR Efficacy and the AI Quality report independently of any experiment.

### Ownership
- **Owning service**: `aiqg-dashboard-be` (ingest + read)
- **Writers**: customer apps via `POST /api/v1/feedback` (AIQG `tas_qg_live_*` token or dashboard Keycloak JWT)
- **Read-only consumers**: experiments results queries (per-variant GROUP BY), Efficacy augmentation, AI Quality report (future)

### Key Characteristics
- **Correlation, two modes** — `response_event_id` (exact) or `client_request_id` (app-friendly; the gateway request event already records it)
- **Best-effort resolution at ingest** — the referenced event is looked up in Loki; a miss stores the row with `resolved = false` rather than rejecting (feedback is late-arriving by nature)
- **Experiment denormalization** — `experiment_id` + `variant` are copied from the resolved event at ingest so per-variant aggregation is a plain GROUP BY (no read-time join). NULL until the experiments runner ships events carrying them.
- **Verdicts mature over time** — a thumb is immediate, "ticket resolved" may land days later; consumers must tolerate late rows
- **Hygiene** — tenant-scoped, per-tenant rate-limited (in-memory token bucket per replica), `metadata` optional and capped at 8 KiB because it may carry PII
- **Non-breaking** — new table + new endpoint only; no existing schema, topic, or endpoint modified

---

## 2. Schema Definition

```sql
CREATE TABLE aiqg.response_feedback (
    feedback_id        UUID PRIMARY KEY,
    tenant_id          UUID NOT NULL,
    response_event_id  TEXT,                      -- exact correlation
    client_request_id  TEXT,                      -- app-friendly correlation
    signal_type        TEXT NOT NULL CHECK (signal_type IN
        ('thumb','accept_reject','rating','task_success',
         'reward','edit_distance','custom')),
    value              DOUBLE PRECISION NOT NULL, -- normalized, see §4
    source_app         TEXT,
    occurred_at        TIMESTAMPTZ NOT NULL,
    experiment_id      TEXT,                      -- denormalized at ingest
    variant            TEXT,                      -- denormalized at ingest
    resolved           BOOLEAN NOT NULL DEFAULT FALSE,
    metadata           JSONB,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (response_event_id IS NOT NULL OR client_request_id IS NOT NULL)
);
```

Indexes: `(tenant_id, created_at DESC)`; partial on `(tenant_id, response_event_id)`, `(tenant_id, client_request_id)`, and `(experiment_id, variant)`.

---

## 3. Fields Reference

| Field | Type | Required | Description |
|---|---|---|---|
| `feedback_id` | UUID | generated | Primary key |
| `tenant_id` | UUID | yes | From the authenticated principal (AIQG token row or JWT claim) — never from the body |
| `response_event_id` | string ≤200 | one-of | Exact event reference (`[[response-event]]`) |
| `client_request_id` | string ≤200 | one-of | The app's stable id sent on the original LLM call (e.g. `X-Request-ID`); recorded on the `[[request-event]]` payload |
| `signal_type` | enum | yes | `thumb`, `accept_reject`, `rating`, `task_success`, `reward`, `edit_distance`, `custom` |
| `value` | float | yes | Normalized numeric value (§4) |
| `source_app` | string | no | Defaults to the AIQG token's `source_app` on the token-auth path |
| `occurred_at` | timestamptz | no | When the outcome happened; defaults to ingest time |
| `experiment_id` / `variant` | string | no | Copied from the resolved event line when promoted (experiments runner, Phase 2) |
| `resolved` | bool | — | Whether the referenced event was found in Loki at ingest |
| `metadata` | JSONB ≤8KiB | no | Caller payload; may carry PII — keep minimal |

---

## 4. Validation & Normalization Rules

| signal_type | Accepted JSON `value` | Stored as |
|---|---|---|
| `thumb` | `1`, `-1`, `true`, `false` | `+1` / `-1` |
| `accept_reject` | `1`, `-1`, `true`, `false` | `+1` (accepted) / `-1` |
| `rating` | number `1..5` | as-is |
| `task_success` | `true`, `false`, `0`, `1` | `1` / `0` |
| `reward` | any number | as-is |
| `edit_distance` | number `≥ 0` | as-is |
| `custom` | any number or boolean | as-is (bool → 1/0) |

Correlation ids: ≤200 printable chars, no `"` or `\` (they are embedded in LogQL matchers). At least one id required; `response_event_id` wins when both are present.

---

## 5. Correlation Resolution (ingest-time)

```
response_event_id mode (exact, 1 Loki query):
  {namespace="tas-llm-router"} |= "aiqg response event" |= "<id>"
    | json | tenant_id="<tenant>" | response_event_id="<id>"

client_request_id mode (2 queries — the response event does NOT carry it):
  hop 1: request-event line filter |= "<crid>", confirm
         payload.data.client_request_id == crid → request_event_id
  hop 2: response event by promoted request_event_id
```

Window: 30 days back (Loki retention), 5 s budget. On miss / error / timeout the row is stored `resolved=false` — never a rejection. A future backfill job may re-resolve unresolved rows; until then they are still aggregable by `client_request_id`.

## 6. API

```
POST /api/v1/feedback        → 201 (row) | 400 | 401/403 | 429 | 500
GET  /api/v1/feedback        → 200 {tenant_id, window_days, count, feedback[]}
     ?days=7&limit=100&response_event_id=...
```

Auth: `DataPlaneAuth` — `Authorization: Bearer tas_qg_live_*` (resolved against `aiqg.token`; suspended → 403) **or** a Keycloak JWT with `tenant_id` claim. Token-auth keeps working when Keycloak is down.

Example:

```json
POST /api/v1/feedback
{ "client_request_id": "req-2026-06-12-001",
  "signal_type": "task_success",
  "value": true,
  "source_app": "support-triage",
  "metadata": { "ticket": "SUP-4412" } }
```

## 7. Cross-Service Integration

- **Experiments runner (Phase 2b)** — results queries join per-variant feedback via the denormalized `experiment_id`/`variant`; an experiment with a quality objective is blocked until feedback (or the judge) exists for its workflow type
- **CLEAR Efficacy** — feedback upgrades the structural-only Efficacy heuristic toward true task-success measurement
- **Gateway (deferred)** — an optional `TAS-Response-Event-Id` response header on `tas-llm-router` would let apps capture the exact id at call time; the `client_request_id` mode works with zero gateway changes

## 8. Related Documentation

- `[[response-event]]`, `[[request-event]]` — the correlated events
- `[[token]]` (account.md §tokens) — the data-plane auth credential
- `tas-llm-router/docs/AIQG-EXPERIMENTS-RUNNER.md` §6.5 — design source
