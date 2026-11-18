# AIQG Organizational Structure & Actor Attribution

---

**Metadata**

```yaml
service: aiqg-dashboard-be (structure model, membership, proposer, rollups, API) + tas-llm-router (token owner + baggage user on events) + aiqg-import (uploader on imported sessions) + aiqg-ui (Organization settings, unit filters, By-unit report)
models: OrgUnit, OrgMember, WorkflowFunction, OrgProposal
databases:
  - aiqg.org_unit (Postgres, aiqg db) — NEW
  - aiqg.org_member (Postgres, aiqg db) — NEW, append-only, effective-dated
  - aiqg.workflow_function (Postgres, aiqg db) — NEW, append-only
  - aiqg.org_proposal (Postgres, aiqg db) — NEW
  - aiqg.token (Postgres) — EXTENDED: owner_sub, owner_email
  - aiqg.imported_trace, aiqg.sdlc_turn (Postgres) — EXTENDED: actor columns
  - aiqg.event_metrics (Timescale) — EXTENDED: token_id, owner_sub; NEW rollup event_metrics_actor_1h
extends: tas-llm-router/docs/AIQG-AGENT-FLOW-ATTRIBUTION.md (adds the human actor; keeps its header contract and ladder)
vocabulary_from: tas-session-corpus/docs/DESIGN-continuous-labeling.md §8.3-8.8, tas-session-corpus/docs/LABELS.md
version: 0.1.0
last_updated: 2026-11-18 (rev 2: F1/F2 added as P2a)
status: proposed (D1-D3 decided with John 2026-11-18; D4 open)
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #22
```

---

## 1. Overview

### Purpose

Every hierarchy TAS has today exists for **data isolation and access**: tenant, Space, AIQG account, realm roles. None of it describes **structure**: which business function, team or person an AI request belongs to. AIQG is flat below the tenant, so cost, activity mix, workflows and CLEAR cannot be rolled up by function, team or person.

This document adds two things:

1. **A structure model** inside a tenant: nestable org units (for example *function → team*) whose members are people, agents, tokens, apps and repos. It is separate from isolation and never grants or restricts access.
2. **Actor attribution**: every gateway event, imported session and SDLC turn records *who did it*, so it can be mapped to a unit.

### Decisions

| # | Decision | Decided |
|---|---|---|
| D1 | **AIQG owns units and membership.** New `aiqg.org_*` tables in the aiqg database. Keycloak groups and Aether Org/Team/`department` are at most *seed sources* for proposals (§7), never the source of truth. | John, 2026-11-18 |
| D2 | **The human actor comes from the token, refined by baggage.** The token's owner (the Keycloak user who issued it) is the baseline identity on every event. W3C `baggage` `user.id`, sent by callers that share one token across many people, refines it. Both are stored; neither overwrites the other. | John, 2026-11-18 |
| D3 | **Levels come from the session-corpus design, unchanged.** request < turn < task < workflow, inside a session (§2). This document adds no level. The attribution doc's `flow` is not a level, and its `agent` becomes an actor. | John, 2026-11-18 |
| D4 | **Membership is single-primary and effective-dated.** Each member has one primary unit per tenant at any instant (rollups sum to the tenant total); changes are append-only with `valid_from`; an admin write that cannot write its audit row is refused. | **Open** — recommended, not yet confirmed |

### Not this

- **Not access control.** A unit never decides what anyone can see. Isolation stays tenant / Space / realm role.
- **Not billing.** Per-unit cost is an internal chargeback view. The patent freedom-to-operate pass in the attribution doc still gates any per-agent *billing*.
- **Not a new level.** "Workflow" remains Plan #21's unit. "Flow" remains a caller-set run or experiment id.
- **Not Aether's org model.** Aether Organization / Team / membership is untouched.

---

## 2. Vocabulary (D3)

### 2.1 Units of work

Taken from `tas-session-corpus/docs/DESIGN-continuous-labeling.md` §8.5 and `LABELS.md`, and as defined in `aiqg-dashboard-be/internal/store/migrations/041_sdlc_workflows.sql:3-11`.

| Unit | Definition | Key | Storage today |
|---|---|---|---|
| **request** | one gateway call | `request_event_id` | `aiqg.event_metrics` |
| **task** | a run of agent work serving one purpose, inside a turn | `task_id` | `aiqg.sdlc_task` |
| **turn** | one human prompt plus everything the agent did before the next prompt | `turn_id` | `aiqg.sdlc_turn` |
| **session** (container) | one sitting; for Claude Code, `x-claude-code-session-id` | `session_id` | derived from `sdlc_turn.session_id` (no table) |
| **workflow** | one goal pursued to an end state, possibly across sessions, days and people | `workflow_id` (`T-<ticket>`, `W-<turn>`) | `aiqg.sdlc_workflow` (linker in `internal/sdlclabel/workflows.go`) |

### 2.2 How the attribution doc's terms map

The attribution doc (`tas-llm-router/docs/AIQG-AGENT-FLOW-ATTRIBUTION.md`, "Locked identity model") uses agent → conversation → flow → step. Mapped onto §2.1:

| Attribution doc | Becomes | Notes |
|---|---|---|
| `step` | **request** | identical: one request/response event |
| `conversation` | **session** | design §8.6 G1 reads `x-claude-code-session-id` into it |
| `flow` | an **attribute**, not a level | design §8.3, §8.8: a caller-set run or experiment id; on `claude-tas` it names the experiment arm. Shown in the UI as "run". Never a workflow key. |
| `agent` | an **actor** (§2.3) | the software that did the work, alongside the human |
| — | **turn**, **task** | no gateway field yet; turn boundary is design §8.6 G3 |
| — | **workflow** | AIQG-built after the fact; hangs off the *what-it-served* axis |

The stored router column `workflow` stays and is shown as **"workload type"** (already the UI convention, `aiqg-ui` `TrafficExplorerPage.tsx` `HierarchyPanel`). It is not the workflow above.

### 2.3 Two axes, kept apart

| Axis | Question | Attached to | Resolves to |
|---|---|---|---|
| **Who did it** (actor) | which person, and which agent, produced this work | request, turn, session | an **org unit** via membership |
| **What it served** | which business function the work was for | workflow | a **business function** unit via `workflow_function` |

Kept separate because they disagree routinely: an engineer on the platform team (who) can spend a week on a sales-demo workflow (what). Collapsing them makes one of the two views wrong.

**Actor kinds:** `person` (a Keycloak user, by `sub`), `user_key` (a pseudonymous baggage `user.id`), `agent` (an `agent_id` or an agent surrogate cluster), `token` (`token_id`), `source_app`, `repo`. Only `person` and `user_key` are humans. The others are members so that work with no known human still lands in a unit.

---

## 3. Current state (verified on `origin/main` 2026-11-18)

| Area | Today | Cite |
|---|---|---|
| Token | `aiqg.token`: tenant, account, `source_app`, `label`, `scopes`, `expires_at`. **No owner.** The issuer's `sub`/email exist only in `audit_log` (`token.issued`, `resource_id` = `token_id`) | aiqg-dashboard-be `migrations/001_init.sql:15-27`, `036`; `internal/handlers/tokens.go:118-132` |
| Gateway validate | `/internal/auth/validate` returns `token_id, tenant_id, aiqg_account_id, source_app, suspended`. **No `label`, no owner** | `internal/handlers/internal_auth.go:39-46`; router `pkg/aiqg/tokens/dashboard_resolver.go:81-87` |
| Gateway user | `AgentContext.user_id` comes **only** from baggage `user.id`; self-asserted | router `pkg/aiqg/events/event.go:132-176`, `builder.go:321-444` |
| Token id on events | `tas_auth_token_id` on the **request** event only; not on the response event, not promoted to Loki | router `event.go:77-123`, `emitter.go:368-400` |
| Timescale | `event_metrics` has `user_id, agent_id, principal_id, flow_id, conversation_id, source_app, identity_source`; **no token_id**. Rollups group by `(bucket, tenant, workflow, vendor)` only. Raw rows kept **90 days** | aether-shared `timescaledb-migrations/004:47-63,130,176`, `006:26` |
| Per-identity rollups | computed from raw rows at query time (`by = agent \| user \| client_ip`) | aiqg-dashboard-be `internal/timescale/client.go:1232-1300` |
| Imported sessions | `imported_trace` has **no uploader or token**; the code adapter sets no `Subject` | `migrations/015`, `039`; aiqg-import `adapter/code/code.go:181-182` |
| SDLC | `sdlc_turn` has `repo`, `session_id`, **no actor**. Ticket prefixes hard-coded `(OPS\|SEC\|DOC\|AIQG\|AB\|AM\|TEST\|P\d\|PLAN\|AUTH\|ARCH)-\d{1,3}` | `migrations/038:25-41`; `internal/sdlclabel/workflows.go:48` |
| Override pattern | `sdlc_workflow_name`: append-only INSERT, newest wins via LATERAL; audit write is **best-effort** | `migrations/041:86-94`; `internal/store/sdlc_flows.go:450-453,719-725`; `internal/handlers/sdlc_flows.go:405-440` |
| Structure | none: no team, department, project or unit table; Keycloak groups never read; `cohort` is an experiment selector | `migrations/011:15` |
| Aether | `department` is free text on the org `MEMBER_OF` edge only; teams do not nest and reference the org by property; aether-be group helpers have no caller | aether-be `models/organization.go:99`, `services/team.go:46-50`, `services/user.go:756` |
| Claude Code | sends `x-claude-code-session-id` and `metadata.user_id` (account uuid, device id, session id); no baggage | corpus design §8.4 |

Corrections this implies for the backlog: the attribution doc is **partially shipped** (`aiqg-v5.24`), not design-only; Plan #15 P1 is **built** (migrations 019-021, `session_capture`); and "no user is recorded on gateway events" should read "no **authenticated** user".

---

## 4. Actor attribution (D2)

### 4.1 Sources

| Source | Field | Trust | Applies to |
|---|---|---|---|
| **Token owner** | `owner_sub` (Keycloak `sub` of the issuer) | authenticated: the bearer proves the token, issuance proves the owner | every AIQG request; imported sessions uploaded with a token |
| **Baggage `user.id`** | `user_id` (pseudonymous key) | asserted by the caller | requests from apps that share one token across people |
| Dashboard JWT | `sub` | authenticated | imports done through the web UI |
| Agent | `agent_id` / `agent_surrogate_id` | asserted / inferred (existing ladder) | requests |

**Not used: `metadata.user_id`.** Corpus design §8.6 G1: it holds a device id and an account uuid and neither is stored raw. Claude Code attribution comes from the token owner (one token per developer) and the session header.

### 4.2 Resolution

Both values are stored on every event; the **resolved human** is computed at read time, so a membership fix re-attributes history without rewriting events.

```
resolved_person =
  member(user_key = user_id)      if user_id present and mapped to a person
  else owner_sub                  if the token has an owner
  else unattributed
```

`identity_source` gains no new value; a new `actor_source ∈ {baggage_mapped, token_owner, jwt, unattributed}` is returned by the read API, not stored. When a mapped baggage user differs from the token owner, both are shown; that is the normal case for a shared service token, not an anomaly.

### 4.3 Changes

**aiqg-dashboard-be**
- Migration: `aiqg.token` + `owner_sub TEXT`, `owner_email TEXT`. Issue sets them from the JWT. Backfill from `audit_log` where `action='token.issued' AND resource_id=token_id`; tokens with no issue row (pre-audit, K8s `MapResolver` secrets) stay NULL and show as "unowned".
- `/internal/auth/validate` additionally returns `label`, `owner_sub`. Additive JSON; old gateways ignore it.
- Imports: `imported_trace` + `uploaded_by_sub`, `uploaded_by_token_id`. Web import sets the JWT `sub`; `import:code` sets the token id (owner joins through it). `sdlc_turn` + `actor_sub` copied from the trace at extraction.

**tas-llm-router**
- Decode `label`, `owner_sub` into `tokens.Token`.
- Stamp `tas_auth_token_id` and `token_owner_sub` on the **response** event (today request only), and promote both to the Loki line.
- Baggage `user.id` handling unchanged (already parsed and stripped before the vendor).

**Timescale**
- `event_metrics` + `token_id UUID`, `owner_sub TEXT` (additive; ingest maps them from the response event).

**aiqg-import**
- Code adapter: set `Subject.OSUser` and `Subject.Machine` as hashes only, for proposals (§7), never as identity.

---

## 5. Structure model (D1, D4)

```sql
-- One node in a tenant's org tree.
CREATE TABLE aiqg.org_unit (
  id          UUID PRIMARY KEY,
  tenant_id   UUID NOT NULL,
  parent_id   UUID REFERENCES aiqg.org_unit(id),   -- NULL = top level
  kind        TEXT NOT NULL CHECK (kind IN ('function','team','group')),
  name        TEXT NOT NULL,
  archived_at TIMESTAMPTZ,
  created_by  TEXT NOT NULL,                       -- Keycloak sub
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, parent_id, name)
);

-- Who/what belongs to a unit, from when. Append-only: a move is a new row.
CREATE TABLE aiqg.org_member (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    UUID NOT NULL,
  member_kind  TEXT NOT NULL CHECK (member_kind IN
               ('person','user_key','agent','token','source_app','repo')),
  member_key   TEXT NOT NULL,          -- sub | baggage user.id | agent id | token_id | source_app | repo short name
  unit_id      UUID REFERENCES aiqg.org_unit(id),  -- NULL = removed from all units
  valid_from   TIMESTAMPTZ NOT NULL,
  source       TEXT NOT NULL CHECK (source IN ('admin','proposal_accepted','seed')),
  set_by       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON aiqg.org_member (tenant_id, member_kind, member_key, valid_from DESC);
```

- **Effective membership at time t** = the newest row with `valid_from ≤ t` for that `(member_kind, member_key)`. Back-dating is allowed (an admin can say "since 1 Oct"); it is itself a new row, so the history of what was believed when is kept.
- **Single primary (D4):** one unit per member at any instant, so per-unit sums equal the tenant total. Secondary memberships are out of scope for v1.
- **Precedence when several members match one event** (a person on team A using a token assigned to team B): `person` > `user_key` > `agent` > `token` > `source_app` > `repo`. The UI shows which member decided.
- **Audit:** every write appends to `aiqg.audit_log` (`org.unit.created|renamed|archived`, `org.member.set`, `org.function.set`) **in the same transaction**; if the audit insert fails the write fails. This is stricter than the current best-effort `sdlc.workflow.renamed`.
- **Archiving** a unit never deletes rows; members of an archived unit roll up to its parent in reports.

---

## 6. What it served: workflow → business function

```sql
CREATE TABLE aiqg.workflow_function (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    UUID NOT NULL,
  workflow_id  TEXT NOT NULL,          -- sdlc_workflow.workflow_id
  unit_id      UUID REFERENCES aiqg.org_unit(id),   -- kind='function'; NULL = cleared
  source       TEXT NOT NULL CHECK (source IN ('admin','proposal_accepted')),
  set_by       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Same shape as `sdlc_workflow_name`: append-only, newest wins, keyed on `workflow_id` so it survives a relink (the linker rebuilds `sdlc_workflow` whole). A workflow whose id changes on relink (`W-` → `T-` once a ticket appears) loses its assignment; the proposer (§7) re-proposes from the old row's evidence.

Gateway requests not linked to any workflow have no *what-it-served* value; reports show them as "no workflow", not as the actor's unit.

---

## 7. Infer, then confirm

The proposer suggests; only an admin decides. The pattern follows AIQG-71 (inferred session names) and the two are built on the same table.

```sql
CREATE TABLE aiqg.org_proposal (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    UUID NOT NULL,
  target_kind  TEXT NOT NULL,          -- a member_kind, or 'workflow'
  target_key   TEXT NOT NULL,
  unit_id      UUID REFERENCES aiqg.org_unit(id),
  rule         TEXT NOT NULL,          -- which rule proposed it
  confidence   TEXT NOT NULL CHECK (confidence IN ('high','medium','low')),
  evidence     JSONB NOT NULL,         -- ids and counts only, no text
  proposer_version TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','accepted','rejected','superseded')),
  decided_by   TEXT, decided_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Rules, first match wins per target:**

| Rule | Proposes | Confidence |
|---|---|---|
| `repo_majority` | a person's unit = the unit owning ≥70% of the repos they touched in 30 days | medium |
| `ticket_prefix` | a workflow's function from its ticket prefix, via a **per-tenant prefix → function map** (replaces the hard-coded regex at `workflows.go:48`) | high |
| `token_label` | a token's unit from `label`/`source_app` matching a unit name | medium |
| `co_actor` | an agent's unit = the unit of the person who owns ≥80% of its requests | medium |
| `seed_aether_department` | a person's unit from Aether `MEMBER_OF.department` (optional, one-shot import) | low |
| `seed_keycloak_group` | a person's unit from Keycloak group membership (optional, one-shot import) | low |

Accepting a proposal writes an `org_member` / `workflow_function` row with `source='proposal_accepted'` and the proposal id in the audit metadata. Rejected proposals are not re-raised for the same evidence. Proposals run nightly and on demand.

---

## 8. Rollups

Raw events age out at 90 days, so per-unit history must be materialized before that.

- **New continuous aggregate** `aiqg.event_metrics_actor_1h`, grouped by `(bucket, tenant_id, owner_sub, user_id, agent_id, token_id, source_app, workflow, vendor, model)` with request count, tokens, cost and CLEAR sums. Kept indefinitely, like the other rollups. Membership is **not** baked in.
- **Unit resolution at query time:** join the aggregate to effective `org_member` rows by bucket time, apply §5 precedence, then roll up the unit tree. A membership fix therefore re-attributes all history, including history older than 90 days.
- **Activity mix** comes from `sdlc_label` via `sdlc_turn.actor_sub`; **workflows** from `sdlc_workflow` (actor axis: workflows a unit's people touched; served axis: workflows assigned to a function).
- **Cardinality check before building:** `user_id` is caller-set and unbounded. The aggregate drops `user_id` values that occur in fewer than N requests per hour into `'(other)'` if the row count measured in staging exceeds budget. Measure first.

`GET /api/v1/metrics/units?axis=actor|served&from&to&unit_id` returns per-unit cost, requests, tokens, CLEAR, activity mix and workflow counts, with an `unattributed` row so totals reconcile to the tenant.

---

## 9. API (aiqg-dashboard-be)

| Method | Path | Gate |
|---|---|---|
| GET | `/api/v1/org/units` | tenant user |
| POST / PATCH | `/api/v1/org/units[/:id]` | `aiqg-admin` |
| GET | `/api/v1/org/members?kind=&unit_id=` | tenant user |
| PUT | `/api/v1/org/members/:kind/:key` `{unit_id, valid_from}` | `aiqg-admin` |
| PUT | `/api/v1/sdlc/workflows/:id/function` | `aiqg-admin` |
| GET | `/api/v1/org/proposals?status=open` | `aiqg-admin` |
| POST | `/api/v1/org/proposals/:id/accept` \| `reject` | `aiqg-admin` |
| GET | `/api/v1/metrics/units` | tenant user |
| GET | `/api/v1/org/actors/unassigned` | `aiqg-admin` — actors seen in the window with no membership |

Every write is tenant-scoped from the JWT and audited fail-closed (§5).

---

## 10. UI (aiqg-ui)

- **Settings ▸ Organization**: unit tree editor; member assignment with a "since" date; an **Unassigned** list (people, tokens, agents seen recently with no unit, ordered by cost); the proposal queue with accept/reject.
- **Unit filter** in Traffic Explorer and Engineering (Sessions, Workflows), and a **function** column on workflows.
- **Reports ▸ By unit**: actor axis and served axis as two tabs, never one blended chart; an "unattributed" row always shown.
- Labels: "session" for `conversation_id`, "run" for `flow_id`, "workload type" for the router `workflow` column.

---

## 11. Privacy & security

- `owner_sub` and `owner_email` are personal data. Email is stored on the token row only (for display); events carry `owner_sub` only.
- Baggage `user.id` stays pseudonymous (attribution doc rule); mapping it to a person is an admin act recorded in `org_member`.
- Per-person rollups are visible to tenant users by default. Whether a non-admin may see *other* people's cost is a per-tenant setting (`org_person_visibility: admins | everyone`, default `admins`).
- No text is stored by the proposer; evidence is ids and counts.
- Two admin-surface gaps found during this survey are in scope as phase **P2a** (§12), because §9 and §10 build on the same admin surface:

| # | Finding | Cite (aiqg-dashboard-be, `origin/main` `c687e1c`) | Why it blocks this plan | Fix |
|---|---|---|---|---|
| F1 | `GET /admin/users` lists **every user in the realm**, with no tenant filter. It is admin-gated, but `aiqg-admin` is a realm role, so an admin of one tenant sees the users of every tenant. Harmless while John is the only tenant admin; a cross-tenant exposure once a second tenant has one. | `internal/handlers/admin.go:109-166` (`listUsers`); `internal/keycloak/admin.go` `ListUsers` | The §10 member picker and the Unassigned list need a user list; reusing this one would show other tenants' people in a tenant's org tree. | Filter on the Keycloak user attribute `tenant_id` (already read for auditors, `internal/keycloak/admin.go:62-70`) to the caller's JWT tenant; a platform-wide list only for a separate platform-operator role. Test: two tenants, an admin of A sees none of B. |
| F2 | Realm-role **grant and revoke are written to zap logs only**, not to `aiqg.audit_log`. The SDLC auditor grant/revoke *is* audited (`sdlc_auditors.go:256,282`), so the two admin paths disagree. | `internal/handlers/admin.go:191,244` | §5 requires every org write to be audited fail-closed; admin *role* changes decide who may make those writes, so leaving them unaudited makes the org audit trail incomplete at its root. Logs also age out (Loki 30 days). | Append `admin.role.granted` / `admin.role.revoked` (actor, target sub, role, tenant) to `audit_log` in the same request, fail-closed like §5; document both actions in `audit-log-entry.md`. |

---

## 12. Phasing

| Phase | Scope | Repos | Ships independently? |
|---|---|---|---|
| **P0** | this document; backlog corrections from §3 | aether-shared | yes |
| **P1 Record who** | §4.3: token owner + backfill, validate fields, response-event token id + owner, Timescale columns, import uploader, `sdlc_turn.actor_sub` | be, router, aether-shared, aiqg-import | yes — "who sent this" works on the existing events API |
| **P2a Admin-surface fixes** | §11 F1 (tenant-filter `/admin/users`) and F2 (audit role grant/revoke, fail-closed) | be, aether-shared (`audit-log-entry.md`) | yes — security fixes, independent of the rest; must land before P5 exposes the member picker |
| **P2 Structure** | §5, §6 tables + §9 CRUD + fail-closed audit | be | yes (admin API only) |
| **P3 Infer/confirm** | §7 proposer + per-tenant ticket-prefix map; AIQG-71 on the same machinery | be | after P2 |
| **P4 Rollups** | §8 aggregate (cardinality measured first) + `/metrics/units` | aether-shared, be | after P1, P2 |
| **P5 UI** | §10 | ui | after P2-P4 |
| **P6 Close-out** | model docs (`token.md`, `response-event.md`, `request-event.md`), backlog + artifact | aether-shared | — |

**Plan #11 Phase 2** (Keycloak `sub` → gateway `user_id`) is satisfied by P1: the token owner *is* the Keycloak `sub` on the event, with no Neo4j lookup. `/me/usage` filters on `owner_sub = jwt.sub OR mapped user_key`.

**Verification for P1:** issue a token, send one request with and one without `baggage: user.id=…`, and confirm in Timescale and Loki that both events carry `token_id` and `owner_sub`, and only the second carries `user_id`. Confirm the backfill count equals the number of `token.issued` rows whose token still exists.

---

## 13. Open questions

1. **D4** — confirm single-primary, effective-dated membership.
2. Should a tenant be able to require **per-person tokens** (refuse a shared token without baggage `user.id`)? Not in v1.
3. `kind` values: is `function | team | group` enough, or should kinds be tenant-defined?
4. Do the six fixed member kinds cover agent-builder agents and MCP servers, or does an MCP server need its own kind?
5. `user_id` cardinality budget for §8 — measure on current traffic before choosing N.

---

## 14. Related documentation

- `tas-llm-router/docs/AIQG-AGENT-FLOW-ATTRIBUTION.md` — identity ladder, header contract, `AgentContext`
- `tas-session-corpus/docs/DESIGN-continuous-labeling.md` §8.3-8.8 — units, `flow_id` is not a workflow key, Claude Code headers
- `tas-session-corpus/docs/LABELS.md` — turn and task definitions
- [activity-reporting-and-onboarding.md](./activity-reporting-and-onboarding.md) — Plan #15, dashboard user vs gateway user
- [token.md](./token.md), [request-event.md](./request-event.md), [response-event.md](./response-event.md), [audit-log-entry.md](./audit-log-entry.md)
- `SPEND_GOVERNOR_DESIGN.md` §2.1 — the Tenant → Org → Team → User budget hierarchy this model can feed
