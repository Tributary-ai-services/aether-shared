# AIQG Classification & Attribution Drift

---

**Metadata**

```yaml
service: tas-llm-router (Axis-1 per-event stamping) + Spark aggregator (rollup) + aiqg-dashboard-be (Axis-2 baseline worker + /metrics/classification-drift) + aiqg-ui (surface)
model: ClassificationDrift / AttributionDrift (derived signals, not a source table)
database: per-event fields on [[response-event]] (TimescaleDB JSONB + promoted columns); Axis-2 baseline in aiqg.event_metrics rollup
schema_location: tas-llm-router/pkg/aiqg/events/event.go (fields); tas-spark-jobs/jobs/aiqg_aggregator (rollup)
version: 0.1.0
last_updated: 2026-08-03
status: planned (Plan #8 Phase 2 = Axis-1; Phase 3 = Axis-2)
spec_refs: source-spec-v0.2.md §3.9; AIQG-AGENT-FLOW-ATTRIBUTION.md
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #8
```

---

## 1. Overview

The gateway runs two inference systems (workflow `Classify()` and the agent-attribution ladder). This model reconciles what a client **declares** (OTel `gen_ai.*` per [[otel-genai-ingestion]], or a `TAS-*` assertion) against what the gateway **infers**, on two axes.

### ⚠ Naming-collision guard (load-bearing)

There is already a `/metrics/drift` endpoint + `DriftCard` in aiqg-dashboard-be/aiqg-ui — that is **headline-metric drift** (CLEAR / latency / cost period-over-period). This model is a **different concept** and MUST be named distinctly end-to-end: **classification drift** and **attribution drift**, exposed at **`/metrics/classification-drift`** (NOT `/metrics/drift`) and surfaced via a new `ClassificationDriftCard` (NOT the existing `DriftCard`). Do not repurpose the existing endpoint, card, or field names.

### The two axes

- **Axis 1 — cross-source disagreement (per-event).** For a request that carries a declared signal, stamp both declared and inferred + a drift flag on the event, even when one side "wins" the precedence ladder. _"OTel said agent A, we inferred agent B."_ / _"OTel said `agentic`, heuristic said `rag`."_
- **Axis 2 — temporal / behavioral drift (vs baseline).** For a *stable declared identity*, detect when the **inferred** view moves over time — fingerprint version lineage, flow/step-topology distribution, and `workflow_type` mix drifting off an established baseline. Surfaces classifier calibration drift AND agent-version / impersonation signals.

---

## 2. Design principles

1. **Always compute both, never discard the loser.** Precedence decides the *effective* value; drift needs *both* declared and inferred, so `buildAgentContext` / the workflow selector compute and record both regardless of who wins.
2. **Drift is only meaningful when a declared signal exists.** With no declared source, `*_declared` is empty and `*_drift` is unset (not `false`) — absence ≠ agreement.
3. **Additive, `omitempty`, snake_case.** Same discipline as [[token-accounting]] — new fields only, no rename/retype of existing event fields; a round-trip test pins the keys.
4. **Loki: fields, not labels.** Drift values are promoted as unwrappable fields, never stream labels (cardinality).

---

## 3. Axis-1 event fields (Contract, additive on [[response-event]])

Stamped on the ResponseEvent when a declared signal is present. All `omitempty`.

### Workflow drift

| Field (snake_case) | Type | Description |
|---|---|---|
| `workflow_declared` | string | Declared workflow_type from the mapped OTel op-name (or a `TAS-Workflow` assertion). Empty when the op-name fell through/excluded. |
| `workflow_declared_op` | string | Raw `gen_ai.operation.name` as received (kept even on fallthrough, for auditability). |
| `workflow_inferred` | string | Heuristic `Classify()` result (always computed). |
| `workflow_drift` | *bool | `true` iff both declared and inferred are present and differ. Pointer → tri-state (unset = no declared signal to compare). |

### Agent/attribution drift

| Field (snake_case) | Type | Description |
|---|---|---|
| `agent_declared` | string | Declared agent id (OTel `gen_ai.agent.id` or `TAS-Agent-Id`). |
| `agent_inferred` | string | The fingerprinted `agent_surrogate_id` (always computed when derivable). |
| `agent_drift` | *bool | `true` iff both present and the declared identity does not match the inferred surrogate's expected lineage. Pointer → tri-state. |
| `drift_source` | enum string | Which declared channel drove the comparison: `otel` \| `tas_asserted` \| `none`. Distinguishes an OTel disagreement from a `TAS-*` one. |

**Note on `agent_drift` semantics (v0.1):** a strict id≠surrogate comparison is noisy (declared ids and gateway surrogates are different namespaces). v0.1 computes `agent_drift` as *"a declared agent is present AND the gateway would have attributed a different agent via a stronger-than-fingerprint tier"* — i.e. genuine cross-source conflict — leaving surrogate-lineage matching (MinHash) to Axis-2. Documented so consumers don't over-read it.

### `otel_map_version`

`otel_map_version` (string, `omitempty`) records the §3 mapping-table version from [[otel-genai-ingestion]] so historical drift stays interpretable across map changes.

---

## 4. Axis-1 rollup (Spark → aiqg.event_metrics)

Additive nullable columns on the externally-owned `aiqg.event_metrics` hypertable (mirror the prior agent/flow `ALTER` pattern; migration applied out-of-band):

```sql
workflow_declared            TEXT,
workflow_inferred            TEXT,
workflow_drift               BOOLEAN,
agent_declared               TEXT,
agent_drift                  BOOLEAN,
drift_source                 TEXT,
```

Rollup counters (per scope/window) the dashboard reads: `classification_drift_count`, `classification_compared_count` (denominator = events with a declared signal), and a declared×inferred confusion tally for the matrix. **Until the columns land, dashboard-be uses a Loki fallback** (unwrap the promoted fields) — same TS-first/Loki-fallback shape as the caching + cost work.

---

## 5. Axis-2 baseline & detection (Phase 3)

For each **stable declared identity** (declared agent id, or a pinned principal), maintain a rolling baseline of the **inferred** distributions:

- **fingerprint version lineage** — MinHash similarity of the request-shape fingerprint vs the identity's established fingerprint; a threshold break = a new agent version (or impersonation).
- **flow/step-topology histogram** — distribution of `flow_step_seq` / step counts.
- **`workflow_type` mix** — the inferred-type histogram.

A dashboard-be worker (mirroring the shipped `autostop.go` / reduction auto-rollback worker) computes rolling drift vs baseline and emits drift events; a new **`classification_drift`** alert kind lands in Governance ▸ Alerts. **REC (open):** store the baseline in the TS rollup rather than a new table, to match the shipped agent/flow rollup path — confirm when Phase 3 starts.

**FTO note:** the deterministic MinHash version-lineage is FTO-clear; do NOT pull in CDC content-propagation lineage without sign-off (see `project_aiqg_agent_flow.md`).

---

## 6. Alert semantics

- **Axis-1**: `classification_drift_rate` = `classification_drift_count / classification_compared_count` over a window; alert when it exceeds a threshold for a scope (indicates the map or the heuristic is miscalibrated for that client).
- **Axis-2**: per-identity baseline break (fingerprint-lineage threshold or topology/mix divergence) → `classification_drift` alert; a sudden lineage break on a stable declared id is the impersonation/version signal.

Both are distinct from the headline-metric drift alerting.

---

## 7. UI surface (Phase 4)

New **Classification Drift** card/report (`ClassificationDriftCard`, distinct from `DriftCard`): declared-vs-inferred confusion matrix, drift-rate trend, per-agent fingerprint-lineage timeline, impersonation-flag surface. Wire the `classification_drift` alert kind into Governance ▸ Alerts. New Grafana dashboard `grafana/dashboards/llm-router/aiqg-classification-drift.json`.

---

## 8. Testing

- Round-trip JSON pins the snake_case keys in §3 (fills the current gap — only a CloudEvents-shape test exists).
- Builder: declared present + differs → `workflow_drift=true`; declared absent → `workflow_drift` unset (nil), not false; both present + equal → `false`.
- `drift_source` set to `otel` vs `tas_asserted` vs `none` correctly.
- Emitter: drift fields promoted as fields (not labels); absent when no declared signal.
- dashboard-be: `/metrics/classification-drift` TS path + Loki fallback; naming does not collide with `/metrics/drift`.

---

## 9. Related Documentation

- [[otel-genai-ingestion]] — produces the declared signal this compares
- [[workflow-classification]] — the inferred workflow taxonomy + source enum
- [[response-event]] — host of the additive drift fields + `agent_context`
- [[aggregated-metrics]] — rollup home for the drift counters
- [[token-accounting]] — the additive-field discipline this mirrors
