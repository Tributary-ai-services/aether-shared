# AIQG Organizational Structure & Actor Attribution

---

**Metadata**

```yaml
service: aiqg-dashboard-be (structure model, membership, roles, targets, follow-ups, proposer, rollups, API) + tas-llm-router (token owner + baggage user on events) + aiqg-import (uploader on imported sessions) + aiqg-ui (new Home, Follow-ups, Ask, Settings ▸ Organization)
models: Team (Aether, extended), TeamHistory, TeamMemberHistory, TokenPlacement, PersonDefaultTeam, WorkflowOwner, OrgTarget, OrgProposal, FollowUp, FollowUpEvent, HomePin, McpServerConfig, McpConnection
databases:
  - Aether Team (Neo4j, aether-be) — EXTENDED: nesting, slug, archive, joining, change events (source of truth for teams)
  - aiqg.team_history, aiqg.team_member_history (Postgres, aiqg db) — NEW, append-only copy projected from Aether's change events
  - aiqg.token_placement (Postgres, aiqg db) — NEW, append-only: which team a token reports under (the org is the token's tenant)
  - aiqg.account (Postgres) — EXTENDED: kind, aether_org_id, aether_tenant_id, aether_user_id (the link to Aether)
  - aiqg.person_default_team (Postgres, aiqg db) — NEW, append-only
  - aiqg.workflow_owner (Postgres, aiqg db) — NEW, append-only (was workflow_function in rev 2)
  - aiqg.org_target (Postgres, aiqg db) — NEW, append-only
  - aiqg.org_proposal (Postgres, aiqg db) — NEW
  - aiqg.followup, aiqg.followup_event (Postgres, aiqg db) — NEW
  - aiqg.home_layout (Postgres, aiqg db) — NEW (per-user and org-default Home layouts)
  - aiqg.home_pin (Postgres, aiqg db) — NEW (saved Ask question; placed on Home as a widget)
  - aiqg.mcp_server_config, aiqg.mcp_connection (Postgres, aiqg db) — NEW: the MCP servers and encrypted connections an org configures (§9.6)
  - aiqg.token (Postgres) — EXTENDED: owner_sub, owner_email
  - aiqg.imported_trace, aiqg.sdlc_turn (Postgres) — EXTENDED: actor columns
  - aiqg.event_metrics (Timescale) — EXTENDED: token_id, owner_sub; NEW rollup event_metrics_actor_1h
reuses: aiqg.report_snapshot (frozen snapshot on Forward), aiqg.digest_subscription + digest worker (weekly email), Plan #20 data MCP (Ask), aiqg.route_rule (recommendations, later)
extends: tas-llm-router/docs/AIQG-AGENT-FLOW-ATTRIBUTION.md (adds the human actor; keeps its header contract and ladder)
vocabulary_from: tas-session-corpus/docs/DESIGN-continuous-labeling.md §8.3-8.8, tas-session-corpus/docs/LABELS.md
wireframe: https://claude.ai/artifact/RC6zR1NmaYeNwKFVQxyCWb
version: 0.8.0
last_updated: 2026-11-19 (rev 9: MCP servers are configured and managed per tenant in AIQG, through the proxy (D18, §9.6). rev 5: teams are Aether's Team, extended; AIQG keeps a time-stamped copy. rev 4: an org IS a tenant, as in Aether; AIQG orgs link to Aether Organizations; multi-org viewer. rev 3: GitHub-style many-to-many structure, owners + targets, executive Home, Forward + Follow-ups, Ask)
status: proposed (D1-D14 decided with John 2026-11-18; D15-D18 2026-11-19)
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #22
```

---

## 1. Overview

### Purpose

Every hierarchy TAS has today exists for **data isolation and access**: tenant, Space, AIQG account, realm roles. None of it describes **structure**: which org, team or person an AI request belongs to. AIQG is flat below the tenant, so cost, activity mix, workflows and CLEAR cannot be rolled up by org, team or person, and there is nobody to hand a problem to.

This document adds four things:

1. **A structure model** inside a tenant, shaped like GitHub's: orgs, teams owned by an org (nestable), and people who can belong to any number of orgs and teams (§5).
2. **Actor attribution**: every gateway event, imported session and SDLC turn records *who did it* (§4).
3. **Ownership and targets**: every org and team has an owner, and owners set the targets that decide what counts as a problem (§5.3, §8).
4. **An executive Home** that lands on what you own, puts problems first, lets you hand a problem to its owner, and answers questions in plain language (§10).

The primary reader of Home is a CEO or CIO. They will rarely open Traffic Explorer. They look for what is wrong, drill a level or two, and forward the problem to the manager who owns it.

### Decisions

| # | Decision | Decided |
|---|---|---|
| D1 | **AIQG owns units and membership.** *(Amended by D14: Aether owns orgs and teams; AIQG owns what hangs off them.)* New `aiqg.org_*` tables in the aiqg database. Keycloak groups and Aether Org/Team/`department` are at most *seed sources* for proposals (§7), never the source of truth. | John, 2026-11-18 |
| D2 | **The human actor comes from the token, refined by baggage.** The token's owner (the Keycloak user who issued it) is the baseline identity on every event. W3C `baggage` `user.id`, sent by callers that share one token across many people, refines it. Both are stored; neither overwrites the other. | John, 2026-11-18 |
| D3 | **Levels come from the session-corpus design, unchanged.** request < turn < task < workflow, inside a session (§2). This document adds no level. The attribution doc's `flow` is not a level, and its `agent` becomes an actor. | John, 2026-11-18 |
| D4 | **Membership is many-to-many, like GitHub.** A person can be in any number of orgs and any number of teams. A team belongs to exactly one org and can nest under one parent team. Membership is append-only and effective-dated. *(Revised: rev 2 proposed single-primary.)* | John, 2026-11-18 |
| D5 | **The org pays, and an org is a tenant.** As in Aether, a tenant is a data-isolation boundary, and each org has exactly one; each person also has a personal tenant (a GitHub personal account). A token already belongs to one tenant (`aiqg.token.tenant_id`), so the token's tenant *is* the org that pays; a token in a personal tenant bills to that person. A token optionally reports under one team inside its org. *(Revised in rev 4: rev 3 put several orgs inside one tenant.)* | John, 2026-11-18 |
| D6 | **Every org and team has an owner.** New per-unit roles: org `owner` / `manager` / `member`, team `manager` / `member` (GitHub's org owner and team maintainer). Forwarding and the landing view depend on it. | John, 2026-11-18 |
| D7 | **You land on what you own.** Home opens on the orgs where you are owner, admin or billing and the teams you manage; a CEO who owns several orgs lands on all of them side by side (§5.4). When the scope holds one org, Home opens one level down on its teams. | John, 2026-11-18 |
| D8 | **Problems first, judged against the owner's own targets.** "Red" means the unit missed a target its owner set, not that it ranks below a peer. No target means no status colour, only the number. | John, 2026-11-18 |
| D9 | **Forward instead of investigate.** A problem is handed to the owning manager with a frozen snapshot and a reply-by date, and tracked in Follow-ups (open → acknowledged → resolved). | John, 2026-11-18 |
| D10 | **✦ Ask is scoped to the viewer and cites its sources.** Built on the Plan #20 data MCP server; model calls go through our own gateway; every answer shows how it was computed. | John, 2026-11-18 |
| D11 | **Person-level ranking is for owners and managers only** by default (tenant setting). | John, 2026-11-18 |
| D13 | **AIQG orgs link to Aether Organizations; the models must not drift.** Each AIQG account (tenant) records the Aether Organization or personal user it belongs to (§5.0). One org, one id, in both products. | John, 2026-11-18 |
| D14 | **Aether is the source of truth for orgs, teams and membership.** Aether's existing `Team` is extended (nesting, joining, archive, change events) rather than duplicated, so agent-builder, notebooks and AIQG share one team model. AIQG keeps an append-only, time-stamped copy for attribution and owns only what is AIQG's: token placement, targets, follow-ups, workflow owners, baggage-key mappings. Amends D1. | John, 2026-11-18 |
| D12 | **Plain language on Home.** Spend, Speed, Quality, Data safety, Uptime, Overall; the CLEAR letter is shown on hover, not as the label. **No left-hand navigation on Home**: it keeps today's clean layout, with Ask and Follow-ups in the header. | John, 2026-11-18 |
| D15 | **An agent belongs to its owner, inside a tenant; "public" is a visibility choice, not a change of owner.** agent-builder already scopes every agent by `tenant_id` + `owner_id` + `space_id`, and the owner can make it public in the UI. AIQG follows that: an agent's org is its tenant and its team is its owner's default team in that org (§5.2), unless someone places it on a team explicitly. Making an agent public changes who can *use* it, never who it is attributed to or who pays: a run is billed to the tenant of the token that made it (D5). Answers Q10. | John, 2026-11-19 |
| D16 | **Tool calls get the same visibility as LLM vendor calls.** Failed, slow and never-called tools, and sensitive data in tool results, are measured per server and per tool, credited to the calling org and team, and owned by the team that runs the server. Two sources (the MCP proxy and the LLM gateway) emit one event type, so tools that bypass the proxy are still seen (§9.5). | John, 2026-11-19 |
| D17 | **Home is a dashboard of widgets with a default per role, and each person can customize it.** The executive default puts Needs your attention, the org cards, the CLEAR scorecard, Spend, Data safety (prompts and tool results) and a CEO/CIO-level Tools (MCP) overview first. Every widget drills to its report, including Analysis ▸ Tools. An org owner may set the default for their org. Ask pins are widgets. | John, 2026-11-19 |
| D18 | **MCP servers are configured and managed in AIQG, per tenant, through the proxy.** An org adds its own servers and connections (credentials) in Settings, as Aether users manage connections today, and the proxy injects the caller's connection on each call. Our shared servers stay operator-managed. Aether moves its tool calls onto the proxy, so both products share one registry and one connection store (§9.6). | John, 2026-11-19 |

### Not this

- **Not access control.** Roles decide where you land, who may edit structure and targets, and who sees person-level rankings (D11). They do not hide tenant data: isolation stays tenant / Space / realm role, and any tenant user can still open Traffic Explorer as today.
- **Not billing to the customer.** "The org pays" is internal attribution of the tenant's invoice. The patent freedom-to-operate pass in the attribution doc still gates any per-agent *billing*.
- **Not a new level.** "Workflow" remains Plan #21's unit. "Flow" remains a caller-set run or experiment id.
- **Not a second org model.** AIQG orgs are Aether Organizations (D13); this plan adds what Aether lacks (nestable teams, token placement, targets, follow-ups) rather than a parallel org list. The Spend Governor's `Tenant ─ Org ─ Team (nestable) ─ User` hierarchy (`SPEND_GOVERNOR_DESIGN.md` §2.1) is the same shape, so its budgets can attach to these units later.

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

### 2.3 Structure terms

**"Tenant" means a data-isolation boundary, everywhere** — the Aether meaning. Every tenant is either one org or one person's personal scope. There is no level above the org: a person who runs several orgs is a member of several tenants, as on GitHub.com. (GitHub's *Enterprise Server* is a self-hosted install, the equivalent of a self-hosted AIQG deployment, not a level in the data model. Enterprise Cloud does offer an "enterprise account" that groups orgs; we do not model one.)

| Term | Meaning | Aether | GitHub |
|---|---|---|---|
| **org** | a unit that **pays** for its tokens' traffic, has owners, and is **one tenant** | `Organization` + `org.TenantID` (`services/space_context.go:176`) | organization |
| **personal scope** | one person's own tenant; holds their personal tokens | `User.PersonalTenantID` (`space_context.go:262`) | personal account |
| **tenant** | the isolation boundary of an org or a personal scope; the key on every AIQG row | `SpaceContext.TenantID` | (implicit) |
| **team** | a unit inside one org; may nest under one parent team | `Team` (flat, unused today) | team / child team |
| **owner / admin / manager** | who answers for an org (owner, admin) or a team (manager) | org roles `owner / admin / member / billing` | org owner / team maintainer |
| **deployment** | one running AIQG (ours, or a customer's self-hosted) | — | GitHub Enterprise Server instance |

Org roles follow Aether's names: `owner`, `admin` (rev 3 called it "manager"), `member`, and **`billing`** — may see spend and set Spend targets without editing structure, which matters now that the org pays.

### 2.4 Three ways to count, kept apart

Many-to-many membership means a person's work cannot simply be summed into every team they belong to. Each view answers a different question and says which it is:

| View | Question | Decided by | Sums to the invoice? |
|---|---|---|---|
| **Billed** | which org pays | the token's tenant: an org, or a person's personal scope (§5.2) | **yes** |
| **Reported under** | which team inside that org it shows under | the token's team; else the person's default team in that org; else "(no team)" | **yes**, within the org |
| **Involving** | which teams' people did it | every team the resolved person belongs to | no — overlaps, labelled "involving" |
| **Served** | which org or team the work was *for* | the workflow's owner (§6) | no — only workflow-linked work |

"Who did it" and "what it served" disagree routinely: an engineer on the Platform team can spend a week on a Sales demo workflow. Home shows Billed and Reported-under by default; Involving and Served are tabs, never blended into one chart.

**Actor kinds:** `person` (a Keycloak user, by `sub`), `user_key` (a pseudonymous baggage `user.id`), `agent` (an `agent_id` or an agent surrogate cluster), `token` (`token_id`), `source_app`, `repo`. Only `person` and `user_key` are humans.

---

## 3. Current state (verified on `origin/main` 2026-11-18)

| Area | Today | Cite |
|---|---|---|
| Token | `aiqg.token`: tenant, account, `source_app`, `label`, `scopes`, `expires_at`. **No owner.** The issuer's `sub`/email exist only in `audit_log` (`token.issued`, `resource_id` = `token_id`) | aiqg-dashboard-be `migrations/001_init.sql:15-27`, `036`; `internal/handlers/tokens.go:118-132` |
| Gateway validate | `/internal/auth/validate` returns `token_id, tenant_id, aiqg_account_id, source_app, suspended`. **No `label`, no owner** | `internal/handlers/internal_auth.go:39-46`; router `pkg/aiqg/tokens/dashboard_resolver.go:81-87` |
| Gateway user | `AgentContext.user_id` comes **only** from baggage `user.id`; self-asserted | router `pkg/aiqg/events/event.go:132-176`, `builder.go:321-444` |
| Token id on events | `tas_auth_token_id` on the **request** event only; not on the response event, not promoted to Loki | router `event.go:77-123`, `emitter.go:368-400` |
| Timescale | `event_metrics` has `user_id, agent_id, principal_id, flow_id, conversation_id, source_app, identity_source`, `tags`; **no token_id**. Rollups group by `(bucket, tenant, workflow, vendor)` only. Raw rows kept **90 days** | aether-shared `timescaledb-migrations/004:47-63,130,176`, `006:26` |
| Per-identity rollups | computed from raw rows at query time (`by = agent \| user \| client_ip`) | aiqg-dashboard-be `internal/timescale/client.go:1232-1300` |
| Data exposure | tenant-wide counts **by pattern** from `event_metrics.tags` (`QueryTagCounts`); **no per-app split** | `internal/handlers/metrics.go:395-440` |
| Frozen reports | `aiqg.report_snapshot`: immutable JSONB body by id, append-only | `migrations/004_report_snapshot.sql` |
| Data MCP (Plan #20) | Phase 0 live (tenant-filtered `aiqg_mcp` views, scoped tokens); Phase 1 draft #178 has two tools, `usage_timeseries` and `cost_breakdown` | BACKLOG Plan #20 |
| Aether teams | `Team` model, service and routes exist (`models/team.go`, `services/team.go`, `handlers/routes.go:513-526`); `Notebook.team_id`, `Agent.team_id`, prompt `shared_with_teams` exist. **Live: 0 Team nodes, and `team_id` is empty on all 41 notebooks and 23 agents.** Two code paths delete teams wholesale: org delete (`services/organization.go:520-529`) and the demo-data reset (`scripts/setup-demo-data.go:104-109`, which also deletes every org and every `MEMBER_OF`). Which one removed the earlier teams is not known; the nightly backups go back about 65 days | aether-be; Neo4j 2026-11-18 |
| Aether orgs | `Organization` with roles `owner / admin / member / billing` on `MEMBER_OF`; a user may be in many orgs; creating an org creates its own tenant in AudiModal (`services/organization.go:83-118`). Live: one org, **TAS** (`4c4d7143…`, tenant `tenant_87f5d6f6-116a-4460-926e-f0b281966d3d`), one member (owner), **0 teams**, 2 spaces | aether-be `models/organization.go`; Neo4j 2026-11-18 |
| Aether tenants | the space context gives exactly one tenant per scope: org → `org.TenantID`, personal → `user.PersonalTenantID`. A second path gives each `Space` node its own `tenant_<unix-seconds>` id (the TAS org's two spaces have two more ids), and older personal tenants use the same non-UUID form | `services/space_context.go:176,232,262`; `models/space.go:154` |
| AIQG tenants | `tenant_id` is a Keycloak user attribute copied into the JWT; one `aiqg.account` row per tenant. Live: `65ea3247…` (our traffic, 631 tokens) and `a689c0b2…` (demo); 3 more users carry ids with no account row. **Neither id appears anywhere in Neo4j**: no AIQG tenant is linked to an Aether org or person | `internal/auth/keycloak.go:40-49`; `migrations/002_account.sql`; Keycloak + Postgres 2026-11-18 |
| AIQG ↔ Space binding | `account.md` specifies a 1:1 `(:Space)-[:HAS_AIQG_ACCOUNT]->(:AIQGAccount)` edge via `Space.tenant_id`. **Never built**: 0 edges, 0 nodes; accounts live in Postgres. The doc is stale | `aiqg/account.md` §2, §5 |
| MCP proxy | federation + Gatekeeper boundary scan (G2) built and enabled, log-only. **Since 2026-11-03 it reaches 1 of 12 servers:** its NetworkPolicy allows egress only on 53/80/443/8080, so Redis (6379) and servers on 8000/8087/8091/8092 are refused, about 33,000 errors a day, while every CR still says `Registered` (MCP-14). Findings are log lines only; no metrics, no events, no caller identity | tas-mcp `internal/scanning/install.go:61-82`, `k8s/base/networkpolicy.yaml`; Loki 2026-11-19 |
| MCP configuration | **Nobody can configure an MCP server except with `kubectl`.** AIQG has no MCP screen or route. The proxy's admin API (register, get, delete, invoke, health; no update) authenticates no caller. A server record has **no tenant**, so the federated set is one global list, and credentials are one `AuthConfig` per server or a CR `secretRef`, never per customer. Servers come only from `FederatedMCPServer` CRs (12 live). Aether *does* let users manage connections (database CRUD, test, schema; MCP server list, tools, test) and injects the Space's connection on each call, but aether-be calls every server at its own URL, **bypassing the proxy** | tas-mcp `internal/federation/http_handlers.go:30-47`, `types.go` `MCPServer`/`AuthConfig`; operator `api/v1/federatedmcpserver_types.go:80`; aether-be `handlers/routes.go:685-705,814-821`, `handlers/mcp_handler.go` `injectConnectionParams`, `config/config.go:456-479`; cluster 2026-11-19 |
| Tool calls at the gateway | `tools_declared` boolean; flows linked by echoed `tool_call_id`; **no tool names recorded** | router `pkg/aiqg/events/event.go:142-160,299`, `builder.go:291-346` |
| Home page | four link cards (Live Monitor, Traffic Explorer, Governance, Reports) under the spend-cap and exposure banners; no left-hand nav | aiqg-ui `src/pages/HomePage.tsx` |
| Imported sessions | `imported_trace` has **no uploader or token**; the code adapter sets no `Subject` | `migrations/015`, `039`; aiqg-import `adapter/code/code.go:181-182` |
| SDLC | `sdlc_turn` has `repo`, `session_id`, **no actor**. Ticket prefixes hard-coded `(OPS\|SEC\|DOC\|AIQG\|AB\|AM\|TEST\|P\d\|PLAN\|AUTH\|ARCH)-\d{1,3}` | `migrations/038:25-41`; `internal/sdlclabel/workflows.go:48` |
| Override pattern | `sdlc_workflow_name`: append-only INSERT, newest wins via LATERAL; audit write is **best-effort** | `migrations/041:86-94`; `internal/store/sdlc_flows.go:450-453,719-725`; `internal/handlers/sdlc_flows.go:405-440` |
| Structure | none: no org, team, department or project table; Keycloak groups never read; `cohort` is an experiment selector | `migrations/011:15` |
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

Both values are stored on every event; the **resolved person** is computed at read time, so a membership fix re-attributes history without rewriting events.

```
resolved_person =
  member(user_key = user_id)      if user_id present and mapped to a person
  else owner_sub                  if the token has an owner
  else unattributed
```

`identity_source` gains no new value; a new `actor_source ∈ {baggage_mapped, token_owner, jwt, unattributed}` is returned by the read API, not stored. When a mapped baggage user differs from the token owner, both are shown; that is the normal case for a shared service token, not an anomaly.

The resolved person only drives the **Involving** view and the default-team fallback (§2.4). **Billing never depends on it**: it follows the token, which is authenticated.

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

## 5. Structure model (D1, D4, D5, D6, D13)

### 5.0 The link to Aether (D13)

```sql
ALTER TABLE aiqg.account
  ADD COLUMN kind             TEXT CHECK (kind IN ('org','personal')),
  ADD COLUMN aether_org_id    UUID,   -- kind='org': Aether Organization.id
  ADD COLUMN aether_tenant_id TEXT,   -- Aether's own tenant id for that org/person, as stored there ('tenant_…')
  ADD COLUMN aether_user_id   UUID,   -- kind='personal': Aether User.id
  ADD CONSTRAINT account_aether_org_unique  UNIQUE (aether_org_id),
  ADD CONSTRAINT account_aether_user_unique UNIQUE (aether_user_id);
```

- **One org, one AIQG tenant.** An AIQG account is bound to exactly one Aether Organization (or one person, for a personal scope), and vice versa. This replaces the never-built Space binding in `account.md`: the right Aether anchor is the space *context* (org or person), not a `Space` node, because one org owns several Space nodes with different ids.
- **The AIQG tenant id stays a UUID and is not rekeyed.** Aether's ids are strings (`tenant_<uuid>` for orgs, `tenant_<unix-seconds>` for older personal tenants) and cannot go into AIQG's UUID columns; rewriting every token, event and rollup to a new key buys nothing the link column does not. New orgs created after the link ships *may* use the UUID part of Aether's id as the AIQG tenant id, so the two read the same.
- **Creating an org** in either product creates the other side: an Aether org created with AIQG enabled gets an `aiqg.account`; an AIQG org created first creates the Aether org through aether-be's API. A nightly check reports accounts with no Aether link and Aether orgs with an AIQG flag but no account (drift is a reported error, not a silent state).
- **Our own data:** link `65ea3247…` (our traffic) to the Aether org **TAS** (`4c4d7143…`), and `a689c0b2…` (demo) to a new Aether org "AIQG Demo". The three users whose Keycloak `tenant_id` has no account row are reviewed and either linked to their personal scope or cleared.
- **The Keycloak `tenant_id` attribute** stays the default tenant for a sign-in (§5.4) until the multi-org viewer ships; it is no longer the only tenant a person can see.

### 5.1 Teams and membership: Aether's Team, extended (D14)

**Aether owns orgs, teams and membership; AIQG keeps a time-stamped copy.** Teams already exist in Aether — `Team` with `organization_id`, `(:User)-[:MEMBER_OF {role, joined_at, invited_by}]->(:Team)` with roles `owner / admin / member / viewer`, a full CRUD + member API (`internal/handlers/routes.go:513-526`), a team page in the Aether UI, and `team_id` on `Notebook` and `Agent` and `shared_with_teams` on prompts. Extending that one model, rather than building a second, means an AIQG team *is* the team that owns an agent-builder agent or a notebook, and any service can reuse it.

**Extensions to Aether's Team** (aether-be):

| Change | Why |
|---|---|
| `(:Team)-[:CHILD_OF]->(:Team)`, at most one parent, same org, no cycles (checked on write) | nesting (D4) |
| `(:Team)-[:BELONGS_TO]->(:Organization)` alongside the existing `organization_id` property | traversals, and the org-delete cascade can be replaced (below) |
| `slug` (unique per org), `archived_at` | stable keys for links and history; a team is **archived, never deleted** |
| **Joining:** a member of the org may join a team whose `visibility` is `organization` (self-join) or request to join a `private` one (approved by the team's owner/admin); `public` keeps today's meaning | "teams can be joined", as on GitHub |
| `joined_at` settable on add (back-dating, "since 1 Oct"), by the team's owner/admin | time-correct attribution |
| **Non-human members by property, not edge:** `Agent.team_id` (exists), plus team-owned `repos` and `source_apps` lists | the proposer (§7) and the Involving view |
| Every change publishes an event to a new Kafka topic `aether.org.changes.v1` (`team.created / renamed / moved / archived`, `team.member.added / removed / role_changed`, `org.member.*`), keyed by org | AIQG's history copy; aether-be publishes nothing for org or team changes today |
| **Remove the two paths that delete every team:** `deleteOrganizationInternal` `DETACH DELETE`s an org's teams (`services/organization.go:520-529`), and `scripts/setup-demo-data.go:104-109` runs `MATCH (t:Team) DELETE t` — and also deletes **every** `MEMBER_OF` edge and **every** `Organization`, not only demo ones | teams are referenced by history and billing reports; deleting them orphans both |

**Role mapping.** Aether team `owner` and `admin` are what this document calls a team **manager**; `member` and `viewer` are members (`viewer` is excluded from forwarding). Org roles are Aether's `owner / admin / member / billing` (§2.3).

**AIQG's copy** (aiqg-dashboard-be), written only by the event consumer, never by an AIQG API:

```sql
-- Team state over time, projected from aether.org.changes.v1. Append-only.
CREATE TABLE aiqg.team_history (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       UUID NOT NULL,      -- the org's AIQG tenant (via §5.0 link)
  aether_team_id  UUID NOT NULL,
  parent_team_id  UUID,               -- Aether id; NULL = top-level team
  name            TEXT NOT NULL,
  slug            TEXT NOT NULL,
  archived        BOOLEAN NOT NULL DEFAULT false,
  valid_from      TIMESTAMPTZ NOT NULL,
  event_id        TEXT NOT NULL UNIQUE  -- idempotent replay
);
-- Membership over time. Many-to-many. Append-only.
CREATE TABLE aiqg.team_member_history (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       UUID NOT NULL,
  aether_team_id  UUID,               -- NULL = org-level membership row
  member_kind     TEXT NOT NULL CHECK (member_kind IN ('person','agent','user_key','source_app','repo','mcp_server')),
  member_key      TEXT NOT NULL,      -- Keycloak sub | agent id | baggage user.id | source_app | repo | MCP server id
  role            TEXT,               -- Aether role name, persons only
  op              TEXT NOT NULL CHECK (op IN ('add','remove')),
  valid_from      TIMESTAMPTZ NOT NULL,
  event_id        TEXT NOT NULL UNIQUE
);
CREATE INDEX ON aiqg.team_member_history (tenant_id, member_kind, member_key, valid_from DESC);
CREATE INDEX ON aiqg.team_member_history (tenant_id, aether_team_id, valid_from DESC);
```

- **Everywhere else in this document, a "unit" or `team_id` is an Aether team id**, and an org is its AIQG tenant. The tables in §5.2, §6, §8 and §9.3 hold Aether team ids with no foreign key.
- **Baggage `user_key` mapping** is the one membership AIQG originates (Aether has no such concept): an owner maps a pseudonymous key to a person, and AIQG writes that row directly, with `event_id = 'aiqg:<uuid>'`.
- **People are Aether users by Keycloak `sub`.** Aether's `User.keycloak_id` joins AIQG's `owner_sub`; the consumer writes the `sub`, never the Aether user id, so events and membership use one key.
- **Effective membership at time t** for a `(member, team)` pair = its newest row with `valid_from ≤ t`; the member is in the team if that row's `op` is `add`. A person can be in many orgs and many teams at once.
- **First sync** backfills the current Aether state with `valid_from = joined_at` (or the team's `created_at`). A nightly reconcile compares the copy with Aether's API and reports any difference as drift (D13); it never silently overwrites.
- **Org membership is implied by team membership**, as on GitHub: Aether adds an org `member` edge when someone joins a team in an org they are not in.
- **Child teams**: a child team's members are not direct members of the parent, but the parent's rollup includes the child's (as GitHub's child teams inherit access but not membership).
- **Archived teams** roll up to their parent (or the org) in reports.
- **Who may do what** (D6):

| Role | On | May |
|---|---|---|
| `owner` | org | everything in the org: teams, members, roles, targets, token placement; receives forwarded org-level issues |
| `admin` | org | same as owner except changing owners |
| `billing` | org | sees all spend in the org and sets Spend targets; no structure edits |
| `owner` / `admin` ("manager") | team | team membership and join requests, the team's targets, placing tokens in the team; receives forwarded team issues |
| `member` | org or team | appears in rollups; sees the unit's Home view; may join `organization`-visible teams |
| `viewer` | team | sees the team's Home view; not a forwarding target |
| `aiqg-admin` (realm role, existing) | deployment | operator of the AIQG deployment; **not** an org role, and after the multi-org viewer ships it no longer implies seeing every org (§5.4) |

- **An org must have at least one owner** (Aether enforces it). A team without an owner/admin forwards to its org's owners and is flagged "no manager" in Settings.
- **Audit:** structure changes are audited where they are made (aether-be); AIQG's own writes (token placement, targets, workflow owners, `user_key` mappings, follow-ups) append to `aiqg.audit_log` (`org.token.placed`, `org.target.set`, `org.workflow_owner.set`, `org.user_key.mapped`) **in the same transaction**, fail-closed. This is stricter than the current best-effort `sdlc.workflow.renamed`.

### 5.2 Token placement: the org pays (D5)

The org that pays is the token's tenant, which every token already has (`aiqg.token.tenant_id`, `migrations/001_init.sql`). Billing therefore needs **no new data**; only the team a token reports under does.

```sql
-- Which team inside its org a token reports under, from when. Append-only.
CREATE TABLE aiqg.token_placement (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   UUID NOT NULL,                       -- = the token's tenant
  token_id    UUID NOT NULL REFERENCES aiqg.token(id),
  team_id     UUID,                            -- Aether team id of that tenant; NULL = "(no team)" / default-team fallback
  valid_from  TIMESTAMPTZ NOT NULL,
  set_by      TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON aiqg.token_placement (tenant_id, token_id, valid_from DESC);

-- A person's default team per org: where their traffic on an org token with no team is reported.
CREATE TABLE aiqg.person_default_team (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   UUID NOT NULL,
  person_sub  TEXT NOT NULL,
  org_tenant_id UUID NOT NULL,                     -- the org (tenant) this default applies in
  team_id     UUID,                            -- Aether team id; NULL = cleared
  valid_from  TIMESTAMPTZ NOT NULL,
  set_by      TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- **A token is in exactly one tenant for life**, optionally one team at a time. This is what makes Billed totals add up despite many-to-many people: each org's total is simply its tenant's total, as today.
- **Moving a token to another org is not supported**: issue a new token in the other org. Moving tenants would move billed history across an isolation boundary.
- **Issuing a token** asks which of the issuer's orgs (or Personal) it belongs to, and optionally a team. The issuer must be a member of that org; placing into a team requires being its manager or an org owner/admin.
- **Existing tokens** stay where they are: all 631 of ours are in `65ea3247…`, which §5.0 links to the TAS org. Owners then place them in teams; a placement back-dated to the token's creation re-attributes its whole history.
- **Report-under resolution** for one event, at the event's time:

```
org    = event.tenant_id                         (an org tenant, or a personal tenant)
team   = token_placement.team_id
         else person_default_team(resolved_person, org)
         else '(no team)'
```

- **Agents, apps and repos** (`member_kind` agent / source_app / repo) are team members so that work with no known human, and proposals (§7), still land somewhere in the Involving view. They never decide billing.

### 5.3 Example: TAS's own tenant (seed for dogfooding)

The first org to use this is our own: the existing Aether org **TAS** (`4c4d7143…`), linked to our AIQG tenant `65ea3247…` (§5.0). One engineering team, and a child team per top-level product, with each product's repos as members (which also feeds the `repo_majority` proposer in §7):

```
TAS                                   org     owner: John   (Aether Organization 4c4d7143…, AIQG tenant 65ea3247…)
└── Engineering                       team    manager: John
    ├── LLM Router                    team    repos: tas-llm-router, llm-invocation, Gatekeeper
    ├── AIQG                          team    repos: aiqg-dashboard-be, aiqg-ui, aiqg-import, tas-spark-jobs, tas-aiqg, aiqg-analysis
    ├── MCP                           team    repos: tas-mcp, tas-mcp-servers
    ├── Aether                        team    repos: aether, aether-be
    ├── Document processing           team    repos: audimodal, deeplake-api
    ├── Agents & workflows            team    repos: tas-agent-builder, tas-workflow-builder
    └── Platform                      team    repos: aether-shared, shared-monitoring, deploy, tas-claude
```

Because John's scope holds one org, Home (D7) opens one level down on the product teams. Tokens are placed per product team (for example the Claude Code dogfood token from Plan #18 under Engineering, the demo-traffic tokens under AIQG). The product list is a starting point for John to edit in Settings, not a fixed seed in code.

### 5.4 Seeing several orgs: the multi-org viewer

Because each org is its own tenant, the executive view in D7 ("each org they manage") spans **several tenants**. Today a sign-in carries exactly one tenant (the JWT `tenant_id` claim) and the `tenant_isolation` middleware scopes every query to it (`internal/middleware/auth.go:112`). So:

- **The viewer's orgs** come from org membership (Aether `MEMBER_OF`, per D14): every org where they are `owner`, `admin` or `billing`, plus orgs where they manage a team.
- **The active tenant** is chosen per request from that list (an org switcher in the header, as on GitHub), validated server-side against membership on every call. The JWT `tenant_id` remains the default when nothing is chosen. A request naming a tenant the caller is not a member of is refused with 403 and audited.
- **The all-orgs Home** is the one cross-tenant read: the server fans out one scoped query per org the viewer may see and returns the cards side by side. No query ever spans two tenants at once, and nothing is summed across orgs except on that page, for that viewer.
- **This is the highest-risk change in the plan** (it is a new way across an isolation boundary). It ships behind its own phase (P2b), with a two-org test: a member of A only sees nothing of B through any endpoint, including `/ask` and `/home`.

---

## 6. What it served: workflow → owner

```sql
CREATE TABLE aiqg.workflow_owner (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    UUID NOT NULL,
  workflow_id  TEXT NOT NULL,          -- sdlc_workflow.workflow_id
  unit_id      UUID,                            -- Aether team id, or the org's tenant id; NULL = cleared
  source       TEXT NOT NULL CHECK (source IN ('admin','owner','proposal_accepted')),
  set_by       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Same shape as `sdlc_workflow_name`: append-only, newest wins, keyed on `workflow_id` so it survives a relink (the linker rebuilds `sdlc_workflow` whole). A workflow whose id changes on relink (`W-` → `T-` once a ticket appears) loses its assignment; the proposer (§7) re-proposes from the old row's evidence.

Gateway requests not linked to any workflow have no *served* value; reports show them as "no workflow", not as the actor's unit.

---

## 7. Infer, then confirm

The proposer suggests; only an owner, manager or admin decides. The pattern follows AIQG-71 (inferred session names) and the two are built on the same table.

```sql
CREATE TABLE aiqg.org_proposal (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    UUID NOT NULL,
  target_kind  TEXT NOT NULL,          -- a member_kind, 'token' (placement), or 'workflow'
  target_key   TEXT NOT NULL,
  unit_id      UUID,                            -- Aether team id
  rule         TEXT NOT NULL,          -- which rule proposed it
  confidence   TEXT NOT NULL CHECK (confidence IN ('high','medium','low')),
  evidence     JSONB NOT NULL,         -- ids and counts only, no text
  proposer_version TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','accepted','rejected','superseded')),
  decided_by   TEXT, decided_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Rules.** With many-to-many membership a target can receive several proposals; each is accepted or rejected on its own.

| Rule | Proposes | Confidence |
|---|---|---|
| `repo_team` | a person joins each team owning ≥30% of the repos they touched in 30 days | medium |
| `ticket_prefix` | a workflow's owner from its ticket prefix, via a **per-tenant prefix → unit map** (replaces the hard-coded regex at `workflows.go:48`) | high |
| `token_label` | a token's placement from `label`/`source_app` matching a unit name or slug | medium |
| `token_owner_team` | a token's team = the owner's team, when the owner is in exactly one team of the token's org | medium |
| `co_actor` | an agent joins the team of the person who owns ≥80% of its requests | medium |
| `seed_aether_department` | a person's team from Aether `MEMBER_OF.department` (optional, one-shot import) | low |
| `seed_keycloak_group` | a person's team from Keycloak group membership (optional, one-shot import) | low |

Accepting a proposal writes the corresponding change: a team membership through aether-be's team API (which then flows back through the history copy), or an AIQG row (`token_placement`, `workflow_owner`, a `user_key` mapping) with `source='proposal_accepted'` and the proposal id in the audit metadata. Rejected proposals are not re-raised for the same evidence. Proposals run nightly and on demand, and are shown to the owner or manager of the proposed unit, not only to admins.

---

## 8. Targets, status and "needs your attention" (D8)

### 8.1 Targets

```sql
-- What an owner says "good" looks like for their unit. Append-only; newest per (unit, metric) wins.
CREATE TABLE aiqg.org_target (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   UUID NOT NULL,
  unit_id     UUID NOT NULL,                   -- Aether team id (or the org's tenant id)
  metric      TEXT NOT NULL CHECK (metric IN
              ('spend','speed','quality','data_safety','uptime','overall')),
  comparator  TEXT NOT NULL CHECK (comparator IN ('max','min')),
  threshold   NUMERIC,               -- NULL = target cleared
  period      TEXT NOT NULL CHECK (period IN ('day','week','month')),
  set_by      TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

| Home name | CLEAR | Measured as | Typical target |
|---|---|---|---|
| Spend | C (`clear_cost`) | USD in the period | max $ per month (a budget) |
| Speed | L (`clear_latency`) | p95 latency, and the CLEAR score | max p95 seconds |
| Quality | E (`clear_efficacy`) | CLEAR efficacy (judged floor) | min score |
| Data safety | A (`clear_assurance`) | policy hits / sensitive-data findings | max 0 leaks |
| Uptime | R (`clear_reliability`) | success rate | min % |
| Overall | composite (`clear_composite`) | composite score | min score |

- **Status:** red = the unit missed its target in the current period (or is projected to, for Spend, at the current run rate); amber = within 10% of missing; green = meeting it; **no target = no colour**, the number alone. A unit inherits its parent's target only if the owner turns that on; the default is no inheritance, so a team is never red against a number nobody gave it.
- A Spend target can later back a Spend Governor budget on the same unit (`SPEND_GOVERNOR_DESIGN.md` §2.1); in this plan it only reports.
- **Quality caveat:** CLEAR Efficacy does not detect incorrect answers well (see `project_aiqg_efficacy_saturation`); a Quality status is only as good as the judged floor behind it, and Home says so on hover.

### 8.2 Needs your attention

The top of Home is a short list (at most five) of units in the viewer's scope that are **red**, ranked by impact:

| Metric | Impact |
|---|---|
| Spend | dollars over budget (or projected over) |
| Quality, Speed, Uptime | shortfall × requests affected |
| Data safety | count of leaks × severity of the data type |

Each item names the unit, says what is wrong in one sentence, names the owner, and offers **Drill in** and **Forward** (§9.3). An item already forwarded shows its follow-up state instead of a second Forward button.

---

## 9. Rollups, Forward, Ask

### 9.1 Rollups

Raw events age out at 90 days, so per-unit history must be materialized before that.

- **New continuous aggregate** `aiqg.event_metrics_actor_1h`, grouped by `(bucket, tenant_id, token_id, owner_sub, user_id, agent_id, source_app, workflow, vendor, model)` with request count, tokens, cost, latency sums and CLEAR sums. Kept indefinitely, like the other rollups. **Placement and membership are not baked in.**
- **Resolution at query time:** join the aggregate to effective `token_placement` (Billed, Reported-under), `team_member_history` (Involving) or `workflow_owner` (Served) rows by bucket time, then roll up the tree. Moving a token or a person therefore re-attributes all history, including history older than 90 days.
- **Activity mix** comes from `sdlc_label` via `sdlc_turn.actor_sub`; **workflows** from `sdlc_workflow`.
- **Cardinality check before building:** `user_id` is caller-set and unbounded. The aggregate folds `user_id` values that occur in fewer than N requests per hour into `'(other)'` if the row count measured in staging exceeds budget. Measure first.
- **Data safety by app and data type** needs a new query: exposure today is tenant-wide counts by pattern (`QueryTagCounts`); the per-unit view groups `event_metrics.tags` by `source_app` and token placement. Whether `tags` carries enough to name the data type per event must be checked before P4 (§14).

### 9.2 Home summary

One endpoint serves Home so the page makes one call: `GET /api/v1/home?scope=<unit_id|tenant>&period=&widgets=` returns one section per widget in the viewer's layout (§11.1). The original sections are the needs-attention list, one card per child unit (owner, spend + trend, the five scores, status per target), and the six top-5 tables. The tables rank **orgs, teams, apps or workflows** (switch), ordered by needs-attention (worst against target first) or best. Person rows appear only for viewers allowed by D11.

### 9.3 Forward and Follow-ups (D9)

```sql
CREATE TABLE aiqg.followup (
  id            UUID PRIMARY KEY,
  tenant_id     UUID NOT NULL,
  unit_id       UUID NOT NULL,                   -- Aether team id (or the org's tenant id)
  metric        TEXT NOT NULL,               -- as org_target.metric, or 'other'
  title         TEXT NOT NULL,               -- the one-sentence issue
  snapshot_id   UUID NOT NULL REFERENCES aiqg.report_snapshot(id),  -- frozen numbers
  to_sub        TEXT NOT NULL,               -- the unit's manager or owner
  cc_subs       TEXT[] NOT NULL DEFAULT '{}',
  note          TEXT NOT NULL DEFAULT '',
  reply_by      DATE,
  created_by    TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- State changes and replies. Append-only; current state = newest row.
CREATE TABLE aiqg.followup_event (
  id           BIGSERIAL PRIMARY KEY,
  followup_id  UUID NOT NULL REFERENCES aiqg.followup(id),
  state        TEXT CHECK (state IN ('open','acknowledged','resolved','closed')),
  comment      TEXT,
  by_sub       TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- **Forward** defaults To = the unit's manager (else owner), Cc = the parent org's owner, and freezes the drilled view into `report_snapshot` so the manager sees the same numbers the executive saw. The snapshot body gains a `kind: "home_drill"` and its scope; existing report snapshots are unaffected.
- The recipient gets an email through the existing mail worker, with a link to the follow-up, and sees it in **Follow-ups** (header link, with a count of open items). States: open → acknowledged → resolved (by the recipient) → closed (by the sender). Overdue items (past `reply_by`) are flagged on the sender's Home.
- A follow-up also links to the **live** view, labelled as live, so the manager can see whether the numbers have moved since.

### 9.4 ✦ Ask (D10)

- **Backend:** the Plan #20 data MCP server (`cmd/data-mcp`) is the tool layer. The dashboard backend runs the model loop, calling the model **through our own gateway** with a service token in each org the question touches (so Ask's own cost shows up, honestly, on that org's Home). The MCP tools are called with a token scoped to that org's tenant.
- **Viewer scoping:** the dashboard passes the viewer's scope (the orgs and teams D7 gives them, and D11's person rule) into every tool call as a filter; a question about several orgs runs one tool call per org tenant (§5.4); a viewer cannot ask their way into a unit or person view their Home would not show. Tenant isolation is still enforced by the `aiqg_mcp` views, independently.
- **Provenance:** every answer lists the tools called, their arguments (time window, filters) and row counts under "How I got this". No answer without at least one tool call; if the tools cannot answer, Ask says which data is missing rather than guessing.
- **Actions on an answer:** Pin to Home (`aiqg.home_pin`: viewer, question, tool calls, chart spec), Show as view (opens the same query as a table/chart), Forward (§9.3, snapshotting the answer).
- **Tools needed beyond Plan #20 Phase 1** (`usage_timeseries`, `cost_breakdown`): quality/CLEAR by unit, data exposure by app and data type, follow-up list, and unit tree. These are Plan #20 Phase 1 work, built here as the first consumer.

---

### 9.5 The tool layer: MCP and tool calls (D16)

**Why.** LLM vendor calls get full treatment: speed, failures, cost, quality, data safety, by vendor and model. Tool calls get none, yet an agent's answer is only as good as the tools it calls. Today a failed or slow tool call is invisible, and so are compliance findings in tool output and tools that are never called. §3 shows how badly this can go unnoticed: the production MCP proxy has reached 1 of its 12 servers since 2026-11-03 (MCP-14). It logged about 33,000 errors a day, and the operator still reports every server as `Registered`.

**Two sources, one event.** Two components see tool calls, and they cover different ground:

| Source | Sees | Misses |
|---|---|---|
| **MCP proxy** (`tas-mcp` federation) | Every call routed through it: server, tool, duration, outcome, result size, reduction, and boundary-scan findings on the result (G2). It also sees each server's tool catalogue (`tools/list`). | Tools the client runs itself: Claude Code's local MCP servers, plain function calls, any agent that talks to a server directly. agent-builder calls `napkin-mcp` directly today (`MCP_SERVER_URL`). |
| **LLM gateway** (`tas-llm-router`) | Every tool the model is offered (the request's `tools`) and every call the model makes (`tool_calls` in the response), proxy or not. It also sees when a result comes back: the next request echoes the `tool_call_id`, which the flow linker already matches (`builder.go:291-346`). | Why a call failed, how long the tool itself took (round trip includes the client), and anything about the server. |

Both emit one CloudEvent type, `com.tas.aiqg.tool_call.v1`, on the existing `tas.aiqg.events.v1` topic, so the Spark job lands them next to LLM events. A call seen by both is joined on `tool_call_id`, with the proxy's fields winning. Fields:

```
tenant_id, token_id, owner_sub, user_id, agent_id, source_app, flow_id, step_id   -- same identity as LLM events (§4)
tool_call_id, tool_name, server_id (NULL = not via the proxy), observed_by ('proxy'|'gateway'|'both')
outcome  'ok' | 'tool_error' | 'timeout' | 'unreachable' | 'blocked' | 'no_result'
duration_ms (proxy), round_trip_ms (gateway), result_bytes, result_tokens_est, reduced_bytes
findings  { "<pattern category>": count }   -- counts only; never the matched value
```

- **`no_result`** is a call the model made whose result never came back within the flow. The gateway is the only source that can see it.
- **Never called.** Not an event. Each night, compare the catalogue (proxy `tools/list` plus the gateway's offered tool names) with the calls over the window. Report tools offered but not called in 30 days per server, and per app. Offering an unused tool costs input tokens on every request: the gateway can measure that cost from the size of the `tools` schema.
- **Server health.** The proxy probes each registered server on a schedule and publishes reachability. A server is healthy only when its probe passes, not when its CR says `Registered`. MCP-14 is the case this rule exists for.

**Caller identity at the proxy.** The proxy knows nothing about its caller today (§3): no tenant, user or token. Callers send the same `TAS-Auth` token the gateway accepts, plus W3C `baggage`. The proxy resolves the token through `/internal/auth/validate`, as the gateway does, with the same short cache. A call without a token is still served and recorded, under `(unattributed)` in an operator-only view. Turning that into a refusal is a per-tenant setting, off by default (compare open question 1).

**Attribution and ownership.**
- **Who called** follows the token, like any LLM event: org = token tenant, team = placement or default team (§5.2). A tool call has no vendor cost of its own. Its cost is the tokens its result adds to the next prompt, `result_tokens_est`, priced at that request's model rate.
- **Who owns the server.** Each MCP server is a member of an owning team, with new member kind `mcp_server` in `team_member_history` (answers open question 2). In the §5.3 seed the TAS servers belong to **Engineering ▸ MCP**.
- **Who a problem goes to** depends on what is wrong:

| Problem | Forward to | Cc |
|---|---|---|
| Server unreachable, failing or slow | server's owning team | calling team, if one team carries most of the calls |
| Sensitive data in tool results | calling team (they chose to fetch it) | server's owning team (they can filter at source) |
| Tool never called | server's owning team (retire it?) | apps that offer it (stop offering it) |
| `no_result` calls | calling team (their client dropped the result) | — |

**Plain-language metrics**, the same five as LLM calls, plus one:

| Metric | Tool layer meaning |
|---|---|
| Uptime | share of calls with `outcome = ok`; a server whose probe fails counts as down |
| Speed | p95 `duration_ms` (proxy) or `round_trip_ms` (gateway only) |
| Data safety | findings per 1,000 results, weighted by data type, as for prompts (§8.2) |
| Spend | tokens added to prompts by tool results; savings from reduction shown next to it |
| Quality | `no_result` and `tool_error` rate (a call that failed and was then retried counts once) |
| **Unused** | tools offered but not called in 30 days, and the input tokens they cost |

Targets (§8.1) apply per server and per tool, with defaults: Uptime ≥ 99%, p95 under 5 s, zero high-severity findings. A server that misses one appears in **Needs your attention**, ranked by calls affected, like any other unit.

**Rollup.** New continuous aggregate `aiqg.tool_call_1h`, grouped by `(bucket, tenant_id, token_id, owner_sub, source_app, agent_id, server_id, tool_name, observed_by, outcome)`, with count, duration sum and p95 sketch, result bytes and tokens, and finding counts. The §9.1 placement joins apply unchanged. `tool_name` cardinality is bounded by the catalogue. Names not in the catalogue fold into `(other)` per tenant past 200.

**Scan once: the attestation.** Every turn re-sends the whole conversation, so the gateway re-scans every earlier message and every earlier tool result on every turn. A tool result is also scanned at the proxy and then scanned again at the gateway. That costs scan time on the hot path, and it inflates Data safety: one email in one tool result is counted once per turn for the rest of the conversation. Gatekeeper already has the mechanism to stop this, but nothing that scans uses it.

| Piece | State on `origin/main` | Cite |
|---|---|---|
| Attestation record | Built. A signed record of one scan: `content_hash` (SHA-256 of the scanned content), scan profile, rule sets, clean, violation count, max severity, redacted, TTL. HMAC signer and verifier; `CanSkip` checks hash, tenant, trust tier and profile. Header `X-TAS-Scan-Attestation`. Default TTL 5 minutes. | Gatekeeper `pkg/types/types.go:10-20`, `pkg/attest/attestor.go:37-96`, `pkg/pipeline/default_processor.go:101-111` |
| Attestation cache | In-memory map per process only | Gatekeeper `pkg/attest/cache_impl.go:21-70` |
| LLM gateway | **Honors but never receives or mints one.** `HonorAttestations: true`, but `EnableAttestation = false // Simplified for now`, and neither `ProcessRequest` sets `Attestation`. The redaction path scans each message a second time, with a comment saying "attestation removes the double-scan later". | router `internal/gatekeeper/gatekeeper.go:118,164-168,338-349,401,486-497` |
| MCP proxy | **Mints none.** The interface comment says "content-hash attested", but the only hash is the reduce-once LRU (reduction only, per pod). The scan has no dedupe. | tas-mcp `internal/federation/result_processor.go:21`, `reducing_processor.go:62-81,211-225` |

Design:
- **Look up by content, not by header.** A header works for one service calling the next. It cannot follow a tool result, which leaves the proxy, enters the agent, and comes back days later inside an LLM request body. So the scanner that sees content first writes the attestation to a **shared store** (Redis, the same one the federation registry uses). Each later scanner hashes what it is about to scan and looks it up. The key is `(tenant_id, sha256(normalised content), scan_profile, ruleset_version)`. A header is still sent where one exists, as a fast path.
- **Unit of scanning is the message, not the request.** The gateway hashes each message (and each tool result inside one), skips the ones with a valid attestation, and scans only what is new. On a long conversation that is most of the scan cost.
- **TTL follows the conversation**, not 5 minutes: 24 hours by default, refreshed on each hit, because the history is re-sent for as long as the conversation runs.
- **A rule change invalidates.** `ruleset_version` is in the key, so new patterns re-scan old content once.
- **Redaction.** When the proxy redacts, it attests the *output* hash too, so the redacted text the gateway later sees is recognised.
- **Count each finding once.** A finding is counted against `(tenant_id, content_hash, pattern)` at the first scan, by the source that made it: the proxy for tool results, the gateway for prompts. Later sightings record `scan_skipped = valid_attestation` and the `attestation_id`, and add no findings. This is the rule that makes the Data-safety numbers in §8 and on the Tools page mean "how many times sensitive data entered", not "how many turns it stayed in context".
- **Measure it.** Events carry `scan_skipped` and `scan_ms`. The Tools page and Live Monitor show skip rate and scan time saved.
- **Tamper evidence.** The signer is the same HMAC signer the audit-log gap notes as unwired. Wiring it here makes it the first live use. It does not make the audit log tamper-evident.

Tracked as GK-5. Lands in P4-Tools (proxy writes, gateway reads), but the gateway half helps prompts on its own and can ship first.

**What exists now.** The scan (G2), reduction and federation are built and enabled (`tas-mcp` PR #27; `SCANNING_ENABLED=true`, log-only). Findings are a zap log line with `server_id` and pattern counts (`internal/scanning/install.go:61-82`). There are no Prometheus metrics, no events, and no caller identity. The gateway records `tools_declared` as a boolean and links flows by `tool_call_id`, but records no tool names. The gateway's prompt scan **does** cover `role: tool` messages and Anthropic `tool_result` blocks (verified live 2026-11-20, §14.2 Q11), so findings in tool results are already caught but credited to the prompt rather than the tool.

### 9.6 Configuring MCP servers (D18)

**Why.** Aether lets users configure and manage tool connections. AIQG cannot. In Aether (Settings ▸ Developer Tools ▸ Data Sources) a user adds a database connection to their Space with its credentials, edits, tests and deletes it, and browses its schema. They can also list each MCP server's tools and test the server. On every tool call, aether-be injects the Space's connection into the request (`injectConnectionParams`). But aether-be calls each server at its own URL (`DBHUB_BASE_URL`, `NAPKIN_MCP_BASE_URL`, `POSTGRES_MCP_BASE_URL` and others, `config/config.go:456-479`). None of that traffic passes the proxy, so it gets no reduction, no boundary scan and no tool events under §9.5.

AIQG has the proxy and nothing to manage it with:

| Piece | State on `origin/main` (2026-11-19) | Cite |
|---|---|---|
| UI and API | None. aiqg-ui has no MCP screen, and aiqg-dashboard-be has no MCP route; only the demo-flow runner calls `/invoke` | aiqg-dashboard-be `internal/demoflows/runner.go:162` |
| Proxy admin API | register, get, delete, invoke, health. **No update.** No caller authentication | tas-mcp `internal/federation/http_handlers.go:30-47` |
| Server record | **No tenant**: one global set shared by every caller | `internal/federation/types.go` `MCPServer` |
| Credentials | one `AuthConfig{type, config}` per server, or a CR `secretRef`. No per-customer credential, no per-call injection | `types.go` `AuthConfig`; operator `api/v1/federatedmcpserver_types.go:80` |
| How a server is added | `kubectl apply` of a `FederatedMCPServer`. The operator registers it and re-registers it on drift every 2 minutes. It never removes a server it did not create | `pkg/controllers/federatedmcpserver_controller.go:31-114` |
| Registry | Redis in production (`FEDERATION_REGISTRY=redis`), so replicas agree | `cmd/server/main.go:115`; live 2026-11-19 |
| Live set | 12 CRs, all in `tas-mcp-servers`; `reduce` is true on 5, false on 1 and unset on 6 | cluster 2026-11-19 |

So a customer cannot connect their own Salesforce, Qualtrics or database, and nothing an org owner sees on the Tools page can be changed from it.

**Design.**
- **Two kinds of server, one registry.** A **platform** server is one we run and share (today's 12). It is still declared as a CR and is read-only to tenants. A **tenant** server belongs to one tenant, which is one org (D5). It is created in AIQG, and only that tenant's tokens can see or call it. The proxy record gains `tenant_id` (NULL = platform), `owner_team_id` and `managed_by` (`operator` or `aiqg`). The operator touches only `managed_by = operator` rows, so its drift-healing never overwrites a server made in the UI, and AIQG cannot edit a CR-managed one.
- **Connections, as in Aether.** A connection is one tenant's credentials and parameters for one server: a database DSN, an OAuth client for a CRM, an API key. A platform server can run with a tenant's own connection (for example `dbhub` with the customer's database). A tenant server has its own connection.
  - Stored encrypted like BYOK provider keys (Plan #14: AES-GCM envelope, KEK in a Kubernetes Secret).
  - Shown only as a label and the last four characters. Never returned to the browser, logged, or put on an event.
  - The proxy resolves the caller's tenant from `TAS-Auth` (§9.5), loads that tenant's connection and injects it on invoke. This is the server-side form of Aether's `injectConnectionParams`.
- **Who may change what.**
  - Org owners and admins add, edit and delete tenant servers and connections. The owning team's manager edits their server's settings.
  - Every change is audited fail-closed: `mcp_server.created|updated|deleted` and `mcp_connection.created|rotated|deleted`, with no secret in the metadata.
  - Platform servers change only through their CRs, by `aiqg-platform-operator` (W0.2).
- **The proxy's admin API requires authentication.** Register, update and delete require a service token that only aiqg-dashboard-be holds; the browser never calls the proxy. aiqg-dashboard-be checks the user's role and writes the audit row, then calls the proxy.
- **Invoke follows §9.5's caller-identity rule, with one exception.** A call to a **tenant** server without a token that resolves to that tenant is refused, because serving it would hand one tenant's credentials to an unknown caller. The observe-only default still applies to platform servers.
- **Settings managed in the UI, per server:**
  - endpoint, protocol and auth type;
  - `reduce`;
  - **allowed tools**: an allowlist over the server's `tools/list`, so write tools can be switched off, as the EV pilot requires for CRM servers;
  - owning team, health-check interval, and enabled or disabled.

  **Test connection** runs `initialize` and `tools/list` with the tenant's connection and shows the tools found.
- **Aether moves onto the proxy.** aether-be calls the proxy's invoke with the org's token instead of each server's URL. Its database connections become proxy connections, so both products share one store. Until then, Aether traffic stays invisible to §9.5, and the Tools page's coverage line says so.

**Data.** The registry stays in the proxy, in Redis. AIQG keeps the system of record for what a user configured, so a flushed proxy registry can be rebuilt from it:

```sql
CREATE TABLE aiqg.mcp_server_config (
  id             UUID PRIMARY KEY,
  tenant_id      UUID NOT NULL,                 -- the org that owns it
  server_id      TEXT NOT NULL,                 -- the proxy's id
  owner_team_id  UUID,                          -- Aether team id
  spec           JSONB NOT NULL,                -- endpoint, protocol, auth type, reduce, allowed_tools, health check
  enabled        BOOLEAN NOT NULL DEFAULT true,
  version        INT NOT NULL,
  updated_by     TEXT NOT NULL,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, server_id)
);

CREATE TABLE aiqg.mcp_connection (
  id             UUID PRIMARY KEY,
  tenant_id      UUID NOT NULL,
  server_id      TEXT NOT NULL,                 -- a platform or tenant server
  label          TEXT NOT NULL,
  ciphertext     BYTEA NOT NULL,                -- envelope-encrypted, as provider_credential
  key_last4      TEXT,
  created_by     TEXT NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  rotated_at     TIMESTAMPTZ,
  deleted_at     TIMESTAMPTZ
);
```

- A reconcile job pushes `mcp_server_config` to the proxy and reports drift, like the §5.1 history reconcile.
- The proxy fetches a connection through an internal endpoint, `/internal/mcp/connections/resolve`, which accepts only the service token. It caches the connection in memory for 60 seconds and never persists it.

---

## 10. API (aiqg-dashboard-be)

| Method | Path | Gate |
|---|---|---|
| — | team and member CRUD, join requests | **aether-be** `/api/v1/teams…` (existing routes, extended per §5.1); aiqg-ui calls it through aiqg-dashboard-be with the user's own token, so Aether enforces the roles |
| GET | `/api/v1/org/teams` (tree, from the history copy, with current members) | org member |
| PUT | `/api/v1/org/teams/:id/targets/:metric` | owner/admin of the team or org |
| PUT | `/api/v1/org/user-keys/:key` `{person_sub, valid_from}` | org owner/admin |
| PUT | `/api/v1/tokens/:id/placement` `{team_id, valid_from}` | org owner/manager; team manager into their team |
| PUT | `/api/v1/org/people/:sub/default-team` `{org_id, team_id}` | the person, or an org owner/manager |
| PUT | `/api/v1/sdlc/workflows/:id/owner` | `aiqg-admin`, or owner/manager of the target unit |
| GET | `/api/v1/org/proposals?status=open` | owner/manager of the proposed unit; `aiqg-admin` |
| POST | `/api/v1/org/proposals/:id/accept` \| `reject` | same |
| GET | `/api/v1/org/actors/unassigned` | `aiqg-admin`, org owners — people, tokens, agents seen with no membership/placement |
| GET | `/api/v1/me/scope` | signed-in user — the units the caller owns or manages (drives D7) |
| GET | `/api/v1/home?scope=&period=` | org member (D11 applied) |
| GET | `/api/v1/metrics/units?view=billed\|reported\|involving\|served` | org member (D11 applied) |
| GET / POST | `/api/v1/followups` | org member (lists those to, cc or by the caller; admins all) |
| POST | `/api/v1/followups/:id/events` | recipient, cc or sender |
| POST | `/api/v1/ask` (streamed) | org member, viewer-scoped |
| GET / POST / DELETE | `/api/v1/home/pins` | the viewer |
| GET / PUT | `/api/v1/home/layout` (resolved layout; PUT writes the viewer's own) | the viewer |
| PUT / DELETE | `/api/v1/org/home-default` | org owner (fail-closed audit) |
| GET | `/api/v1/mcp/servers` (platform servers plus the tenant's own, with health, tools and owning team) | org member |
| POST / PUT / DELETE | `/api/v1/mcp/servers[/:id]` | org owner/admin; the owning team's manager for PUT (§9.6) |
| POST | `/api/v1/mcp/servers/:id/test` | same as PUT |
| GET / POST / DELETE | `/api/v1/mcp/connections[/:id]` (POST to an existing id rotates; secrets are write-only) | org owner/admin |

Every write is tenant-scoped from the JWT and audited fail-closed (§5.1). The `/admin/users` fix (F1) is a precondition for every member picker.

---

## 11. UI (aiqg-ui)

The wireframe is the reference: <https://claude.ai/artifact/RC6zR1NmaYeNwKFVQxyCWb>.

### 11.1 Home is a dashboard of widgets (D17)

Home replaces the current landing page with a **grid of widgets**. Everyone starts from a default layout for their role, and anyone can change their own.

- **No left-hand navigation** (D12). The header carries the scope ("All my orgs · 3" with an org switcher, as on GitHub), a period switch, **Customize**, **Follow-ups (n)** and **✦ Ask**. Analysis, Governance, Reports and Experiments stay in the app header as today.
- **Page scope and period apply to every widget** unless the widget is pinned to a fixed scope (for example, "Engineering only" on a CEO's page). Drilling into a card, row or issue re-scopes the whole page (breadcrumb: Acme › Sales › Demo). The back button works.
- **Every widget drills somewhere.** Its title and rows link to the full report it summarises (table below). Executives drill to report pages, not Traffic Explorer. Owners and managers also get "Open in Traffic Explorer" at the bottom level.
- **Plain names** with the CLEAR letter on hover (D12). Every red or amber says which target it missed.
- **Phone:** a single column in layout order. Tables become top-3 lists with "more", and Ask is full screen.

**Widget catalogue.** Sizes are on a 12-column grid: S = 3, M = 6, L = 12.

| Widget | Shows | Drills to | Default for | Size |
|---|---|---|---|---|
| **Needs your attention** | the top five problems against targets, LLM and tool alike (§8.2, §9.5), with Forward | the unit, then the report for that metric | everyone | L |
| **Your orgs / teams** | one card per child unit: owner, spend and trend, the scores, Tools uptime, status pills | re-scopes Home to that unit | exec, owner | L |
| **CLEAR scorecard** | the six top-5 tables. Rank switch: orgs / teams / apps / workflows / **tools**. Order switch: needs attention / best | Reports ▸ CLEAR Scorecard; the Tools view drills to Analysis ▸ Tools | exec, owner, manager | L |
| **Data safety** | sensitive data that came in, counted once (§9.5 attestation), **split by where it came from: prompts vs tool results**. Shows data types, trend, top apps and top tools, and the share of hits under a redact policy vs log-only | prompts: Reports ▸ What's leaking. Tool results: Analysis ▸ Tools | exec, owner, manager | M |
| **Tools (MCP) overview** | the executive view of the tool layer, in one card: calls that worked against target, servers down or slow, sensitive data in tool results, what tool output cost in prompt tokens and what reduction saved, tools never called and what offering them costs, and coverage (proxy vs gateway only). Ends with the worst tool problem and its owner, with Forward | Analysis ▸ Tools | exec | M |
| **Tool health** | the compact version: calls that worked, p95, servers down, unused tools | Analysis ▸ Tools | owner, manager | S |
| **Spend and forecast** | month to date against budget, projected month end, by child unit; spend-cap state | Reports ▸ Cost | exec, owner, billing | M |
| **Savings** | what caching, payload reduction and routing saved this period, in dollars, next to spend | Reports ▸ Caching, Reports ▸ Model economics | exec, owner | S |
| **Adoption** | active people, apps and agents, their trend, and the share of the org's people using AI this period | Analysis ▸ Activity (scoped version) | exec, owner | S |
| **Vendor mix and risk** | spend share by vendor and model, single-vendor dependency, vendor uptime | Reports ▸ Provider Quality, Reports ▸ Reliability | exec (CIO) | S |
| **Compliance posture** | policy bundles on (HIPAA, PCI...), enforced vs log-only, NIST AI RMF coverage, open high-severity findings | Governance ▸ Policies, Reports ▸ NIST AI RMF | exec (CIO), owner | S |
| **Visibility** | the share of known AI use AIQG can see: gateway traffic vs imported traces vs known-but-unrouted apps | Reports ▸ Imported Traces | exec (CIO) | S |
| **Follow-ups** | open items you forwarded or received, overdue first | Follow-ups | everyone | S |
| **Recommendations** (P8) | cheaper or more capable model, raise limits, caching, tighten policy, each with estimated savings or risk (§14.1) | the recommendation detail | exec, owner | M |
| **My usage** | the viewer's own spend and requests (Plan #11) | Settings ▸ My usage | member | S |
| **Pinned answer** | any ✦ Ask answer pinned to Home; re-runs on the page period | the answer as a view | — (added by pinning) | S or M |

**Default layouts** (the system defaults; an org owner may save their own for the org, below):

| Role | Layout, in order |
|---|---|
| **Executive**: owner, admin or billing on several orgs (the CEO/CIO view) | Needs your attention · Your orgs · CLEAR scorecard · Spend and forecast + Data safety · **Tools (MCP) overview** + Savings + Adoption · Follow-ups + Compliance posture + Vendor mix + Visibility |
| **Org owner** | Needs your attention · Your teams · Spend and forecast + Data safety · CLEAR scorecard · Tool health + Savings · Follow-ups |
| **Team manager** | Needs your attention · Data safety + Tool health · CLEAR scorecard (apps) · Follow-ups |
| **Member** | My usage · their team's card · Follow-ups |

**Customize.** **Customize** turns on edit mode:
- drag to reorder, or move up and down with the keyboard;
- set the size (S, M or L) or remove a widget;
- **+ Add widget** opens the catalogue, grouped Problems / Money / Safety / Quality / Tools / Adoption / Pinned;
- **Reset to default**.

An org owner also sees **Save as default for <org>**. Their members then start from that layout instead of the system one, and can still change their own. A widget never shows data outside the viewer's scope. Widgets that need person rows follow D11. A widget the viewer may not use is not offered.

**Storage.** One table, newest wins, following the `sdlc_workflow_name` override pattern:

```sql
CREATE TABLE aiqg.home_layout (
  id BIGSERIAL PRIMARY KEY,
  tenant_id UUID NOT NULL,
  owner_kind TEXT NOT NULL CHECK (owner_kind IN ('user','org_default')),
  owner_key  TEXT NOT NULL,            -- Keycloak sub, or the org's tenant id
  layout     JSONB NOT NULL,           -- [{"widget":"data_safety","w":6,"scope":null,"settings":{}}...]
  layout_version INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by TEXT NOT NULL
);
CREATE INDEX ON aiqg.home_layout (tenant_id, owner_kind, owner_key, created_at DESC);
```

- **Resolution:** the user's newest row, else the org default, else the system default for the viewer's role.
- **System defaults live in code**, versioned, so a new widget can be added to defaults without a migration.
- **`aiqg.home_pin` is absorbed:** a pin is a widget entry `{"widget":"ask_pin","settings":{"pin_id":...}}`, and the pin table keeps only the saved question.
- **Unknown widget types** in a stored layout are skipped, not errors, so removing a widget never breaks a page.
- **UI state:** layout and edit mode live in a Redux slice `homeLayout`, per the repo rule.

**Data.** `GET /api/v1/home?scope=&period=&widgets=a,b,c` returns one section per requested widget, so the page still makes one call. Each widget's section is computed by the same query as its drill target, so the two numbers always agree.

### 11.2 Other surfaces

- **Tools** page, under **Analysis** at `/tools`, after Traffic Explorer (`src/config/nav.ts:64-74`), the tool-layer twin of Reliability and Provider Quality: a coverage line (calls via proxy vs seen by gateway only), the six metrics, a server table that drills to its tools, Never called, Data safety in tool results, and the server probe history. Scoped like Home: an org owner sees their org's calls across all servers; the MCP team also sees every caller of the servers it owns.
- **Home** gains tool problems in Needs your attention, a **Tools** row on each org card, and **Tools** in the CLEAR table's Orgs / Teams / Apps / Workflows switch.
- **Settings ▸ Tools (MCP)** (§9.6): platform and org servers with health and owning team; add or edit a server (endpoint, protocol, auth type, `reduce`, allowed tools picked from `tools/list`, owning team, enabled); **Test connection**; Connections with label, last four and **Rotate**. The Tools page links each server to its settings. Org owners and admins edit; members read.
- **Follow-ups** page: To me / From me / Cc; state, reply-by, snapshot vs live.
- **✦ Ask** panel: question box, suggested questions for the current scope, answer as text + table/chart, "How I got this", Pin / Show as view / Forward.
- **Settings ▸ Organization**: org and team tree editor (nesting by drag), members with roles and a "since" date, token placement, default team, targets per unit, the **Unassigned** list (ordered by cost), and the proposal queue. Org owners see their orgs; admins see all.
- **Token issue dialog** gains Org (required, or Personal) and Team (optional).
- **Unit filter** in Traffic Explorer and Engineering (Sessions, Workflows), and an owner column on workflows.
- Labels: "session" for `conversation_id`, "run" for `flow_id`, "workload type" for the router `workflow` column.

### 11.3 Weekly digest (proposed, not yet decided)

The same "Needs your attention" list and org cards, emailed weekly to owners through the existing `digest_subscription` and digest worker, with Forward links. Executives often live in email; the data and the rendering are the Home endpoint's, so it costs one template.

---

## 12. Privacy & security

- `owner_sub` and `owner_email` are personal data. Email is stored on the token row only (for display); events carry `owner_sub` only.
- Baggage `user.id` stays pseudonymous (attribution doc rule); mapping it to a person is an owner/admin act recorded in `team_member_history`.
- **Person-level rankings and costs** (D11) are visible to owners and managers of a unit containing the person, and to admins, by default. Tenant setting `org_person_visibility: managers | everyone`, default `managers`. A person always sees their own.
- **Ask** never returns what the viewer's Home would not show (§9.4); questions and answers are logged with the viewer's `sub` for audit, and answers store ids and aggregates, not prompt text.
- **Follow-up notes** are free text written by the sender; they are not sent to the model and not indexed.
- No text is stored by the proposer; evidence is ids and counts.
- Two admin-surface gaps found during this survey (F1, F2) are in scope as phase **P2a** (§13), because §10 and §11 build on the same admin surface:

| # | Finding | Cite (aiqg-dashboard-be, `origin/main` `c687e1c`) | Why it blocks this plan | Fix |
|---|---|---|---|---|
| F1 | `GET /admin/users` lists **every user in the realm**, with no tenant filter. It is admin-gated, but `aiqg-admin` is a realm role, so an admin of one tenant sees the users of every tenant. Harmless while John is the only tenant admin; a cross-tenant exposure once a second tenant has one. | `internal/handlers/admin.go:109-166` (`listUsers`); `internal/keycloak/admin.go` `ListUsers` | The member picker, Forward's To/Cc and the Unassigned list need a user list; reusing this one would show other tenants' people in a tenant's org tree. | Filter on the Keycloak user attribute `tenant_id` (already read for auditors, `internal/keycloak/admin.go:62-70`) to the caller's JWT tenant; a platform-wide list only for a separate platform-operator role. Test: two tenants, an admin of A sees none of B. |
| F2 | Realm-role **grant and revoke are written to zap logs only**, not to `aiqg.audit_log`. The SDLC auditor grant/revoke *is* audited (`sdlc_auditors.go:256,282`), so the two admin paths disagree. | `internal/handlers/admin.go:191,244` | §5 requires every org write to be audited fail-closed; admin *role* changes decide who may make those writes, so leaving them unaudited makes the org audit trail incomplete at its root. Logs also age out (Loki 30 days). | Append `admin.role.granted` / `admin.role.revoked` (actor, target sub, role, tenant) to `audit_log` in the same request, fail-closed like §5; document both actions in `audit-log-entry.md`. |
| F3 | **`is_public` is platform-wide, not org-wide.** The read rule is `(owner_id = ? AND space_id = ?) OR is_internal = true OR is_public = true`, with no tenant condition, so an agent a user marks public is readable, runnable and copyable by every signed-in user of every tenant. Today only the 16 system agents (`tenant_id = 'system'`) are public, so nothing has leaked; the first customer who clicks "make public" shares their system prompt and notebook bindings with every other org. Writes are already owner-only (AB-5). | tas-agent-builder `services/impl/agent_service_impl.go:38-43` (read), `:425-427` (duplicate), `:473-474` (list by space); live `agent_builder.agents`: 16 public+internal system agents, 2 private, 2026-11-19 | D4 makes orgs real tenants side by side; a one-bit public flag cannot say "my org" versus "everyone", and organisation-wide sharing is what a team lead will mean. | Replace the boolean with the visibility Aether's `Team` already uses: `private \| organization \| public`. `organization` adds `tenant_id = <caller's tenant>` to the read rule; `public` stays reserved for system and template agents, gated by an admin role. Migrate existing `is_public = true` rows to `public` (all are system) and default new shares to `organization`. Mirror the field on aether-be's `Agent` node. Separate change in tas-agent-builder; tracked as AB-13. |

---

## 13. Phasing

| Phase | Scope | Repos | Ships independently? |
|---|---|---|---|
| **P0** | this document; backlog corrections from §3 | aether-shared | yes |
| **P1 Record who** | §4.3: token owner + backfill, validate fields, response-event token id + owner, Timescale columns, import uploader, `sdlc_turn.actor_sub` | be, router, aether-shared, aiqg-import | yes — "who sent this" works on the existing events API |
| **P2a Admin-surface fixes** | §12 F1 (tenant-filter `/admin/users`) and F2 (audit role grant/revoke, fail-closed) | be, aether-shared (`audit-log-entry.md`) | yes — security fixes; must land before any member picker ships |
| **P2-Aether Team extension** | §5.1 nesting, slug, archive, joining, back-dated `joined_at`, `aether.org.changes.v1` events; remove the two delete-all-teams paths; Aether UI team page shows nesting and join | aether-be, aether, aether-shared (Neo4j model docs) | yes — useful to Aether on its own |
| **P2 Structure** | §5.0 link to Aether (link our two tenants; drift check), §5.1 history consumer + first sync + nightly reconcile, §5.2 token placement, §6, §10, fail-closed audit; seed the TAS org (§5.3) in Aether; correct `account.md` | be, aether-shared | after P2-Aether |
| **P2b Multi-org viewer** | §5.4 org switcher, per-request tenant validated against membership, all-orgs fan-out; two-org isolation test | be, ui | after P2a, P2; before P5's all-orgs Home |
| **P3 Infer/confirm** | §7 proposer + per-tenant ticket-prefix map; AIQG-71 on the same machinery | be | after P2 |
| **P4 Rollups + targets** | §9.1 aggregate (cardinality measured first), §8 targets and status, `/home`, `/metrics/units`, data-safety-by-app query | aether-shared, be | after P1, P2 |
| **P4-Tools** | §9.5: fix MCP-14 first; scan-once attestation in a shared store (gateway half can ship first, GK-5); proxy accepts `TAS-Auth` + baggage and resolves it; `tool_call.v1` events from proxy and gateway (tool names, `tool_call_id`, outcome); server probes; catalogue snapshot; `tool_call_1h`; `mcp_server` member kind; tool targets | tas-mcp, router, tas-spark-jobs, aether-shared, be | MCP-14 alone, yes; the rest after P1, P2 |
| **P4-Config** | §9.6: tenant-scoped registry, `managed_by`, update route and admin authentication on the proxy; the operator leaves non-CR servers alone; `mcp_server_config` + reconcile; encrypted `mcp_connection` + resolve endpoint + injection on invoke; Settings ▸ Tools (MCP); then aether-be calls through the proxy | tas-mcp, tas-mcp-servers, be, ui, aether-be, aether | after P2 and P4-Tools' caller identity (T.1); the Aether move last |
| **P5 Home + Settings** | §11.1 new Home as widgets (replaces `HomePage.tsx`): widget framework, the catalogue minus tool and P8 widgets, system default layouts; ship with defaults first, then Customize, org defaults and `home_layout`; §11.2 Settings ▸ Organization, token dialog, unit filters | ui | after P2-P4 |
| **P5-Tools** | §11.2 **Tools** page; Tools (MCP) overview and Tool health widgets; tool-results half of the Data safety widget; Tools in the CLEAR rank switch | ui | after P4-Tools, P5 |
| **P6 Forward + Follow-ups** | §9.3 tables, email, Follow-ups page; §11.3 weekly digest if agreed | be, ui | after P5 |
| **P7 ✦ Ask** | §9.4: the extra Plan #20 tools, the model loop through our gateway, viewer scoping, provenance, pins | be (data-mcp + dashboard), ui | after P4; depends on Plan #20 Phase 1 |
| **P8 Recommended actions** (future) | §14.1 | be, ui, router | after P4, P6 |
| **P9 Close-out** | model docs (`token.md`, `response-event.md`, `request-event.md`, `audit-log-entry.md`), backlog + artifact | aether-shared | — |

**Plan #11 Phase 2** (Keycloak `sub` → gateway `user_id`) is satisfied by P1: the token owner *is* the Keycloak `sub` on the event, with no Neo4j lookup. `/me/usage` filters on `owner_sub = jwt.sub OR mapped user_key`.

**Verification for P1:** issue a token, send one request with and one without `baggage: user.id=…`, and confirm in Timescale and Loki that both events carry `token_id` and `owner_sub`, and only the second carries `user_id`. Confirm the backfill count equals the number of `token.issued` rows whose token still exists.

**Verification for P2/P4:** on the TAS tenant, Billed totals for the period equal the tenant total to the cent; a person in two teams appears in both under Involving and once under Reported-under; moving a token to another team back-dated to its creation moves its whole history in one refresh.

---

## 14. Future work and open questions

### 14.1 Recommended actions at the owner level (future, P8)

Home will eventually say not only *what* is wrong but *what to try*, addressed to the unit's owner and tied to the target it would fix. Each recommendation carries its evidence, the expected effect on the target, and a way to apply it that starts in **observe** mode (an `aiqg.route_rule` or a per-unit config change that is measured before it is enforced), approved by the owner.

| Recommendation | When | Builds on |
|---|---|---|
| **Evaluate a cheaper model** | Spend is red and the workload has a cheaper model proven to hold quality for it | Workload Model Fit (merged 2026-09-12), expected-cost routing |
| **Use a more capable model** | Quality is red and the judge attributes the shortfall to the model, not the prompt | judged quality floor; routing quality gate |
| **Raise output/window limits** | answers are being cut off: a high share of responses end at `max_tokens` or the context window (`finish_reason = length`), losing output the caller paid for; the earlier routing measurements found 47% truncation on some traffic | per-route/per-unit dynamic configuration of `max_tokens` and context settings |
| **Turn on caching or reduction** | repeated prompts or large static context on a unit's traffic | response cache, prompt-cache control, payload reduction |
| **Tighten a policy** | Data safety is red under a log-only bundle | policy bundles (redact once the gateway enforces) |

Dynamic configuration (options such as window sizes, `max_tokens`, cache and reduction settings, set per unit or per workload and changeable without a deploy) is the mechanism most of these need; it is its own design and is out of scope here beyond reserving the "apply" action.

### 14.2 Open questions

1. Should a tenant be able to require **per-person tokens** (refuse a shared token without baggage `user.id`)? Not in v1.
2. ~~Does an MCP server need its own member kind?~~ Yes: `mcp_server` (§9.5).
3. `user_id` cardinality budget for §9.1 — measure on current traffic before choosing N.
4. Does `event_metrics.tags` carry the data type per event well enough for "what type of data is leaking, by app"? Check before P4; if not, the router must emit the matched pattern's category.
5. Should targets inherit from parent units by default? Proposed: no (§8.1).
6. Weekly digest (§11.3): agree before P6.
7. Who may create orgs: any signed-in person (as on GitHub and in Aether today), or only `aiqg-admin`?
8. ~~Source of truth for org membership and teams.~~ Decided as D14: Aether owns orgs, teams and membership.
9. Should `viewer` team members see person-level rows on the team's Home, or only totals?
10. ~~Do agent-builder agents inherit their team from the creating user's default team?~~ Decided as D15: yes. An agent is scoped by tenant and owner; its team is the owner's default team unless placed. Sharing is visibility only (F3).
11. ~~Does the gateway's prompt scan cover `role: tool` messages?~~ **Answered 2026-11-20 (Plan #22 W0.5): yes, in both API shapes, so P4-Tools *re-credits* tool-result findings to the tool rather than adding them.** Code: `ScanMessages` (`tas-llm-router internal/gatekeeper/gatekeeper.go:316`) scans every role except `assistant` unless a message carries `trust: pre_scanned`. The live ConfigMap sets no `scan_policy`, so the defaults apply (always `user`, never `assistant`, everything else scanned). `/v1/messages` fans each Anthropic `tool_result` block out into a `role: tool` message (`internal/server/anthropic_messages.go:334`) *before* the shared scan at `server.go:1294`. Live test on `llm-router-aiqg` (5.118.0-rc.1, tenant a689c0b2), one Luhn-valid test card per request:

    | message carrying the card                      | inbound findings          |
    |------------------------------------------------|---------------------------|
    | `role: tool` (OpenAI `/v1/chat/completions`)   | 1 × `pii_credit_card`     |
    | `tool_result` block (Anthropic `/v1/messages`) | 1 × `pii_credit_card`     |
    | `role: user` (positive control)                | 1 × `pii_credit_card`     |
    | `role: assistant` (negative control)           | 0                         |

    Two consequences. First, a tool-result finding is today credited to the prompt (`assurance_inbound_count`, `tag_pii_credit_card`) with nothing saying it came from a tool; P4-Tools adds the attribution, not the detection. Second, because the whole history is re-scanned on every turn, that one card is counted again on every later turn of the conversation. That is the double count GK-5a (W0.4) removes. With the MCP proxy's own scan (G2) the same content is also scanned a third time at the proxy, which is the case §9.5's shared attestation store covers.
12. Should a call to the MCP proxy without a token be refused? Proposed: served and recorded as `(unattributed)` by default, refusal a per-tenant setting.
13. May a tenant add an MCP server hosted outside our cluster (a vendor's remote endpoint, or one the customer runs)? Proposed: yes, HTTPS only, through a per-tenant egress allowlist on the proxy (§9.6).
14. Which connection kinds ship first? Proposed: database DSN (as Aether has today, for `dbhub`) and OAuth client credentials (the CRM servers the EV pilot needs).

---

## 15. Related documentation

- `tas-llm-router/docs/AIQG-AGENT-FLOW-ATTRIBUTION.md` — identity ladder, header contract, `AgentContext`
- `tas-session-corpus/docs/DESIGN-continuous-labeling.md` §8.3-8.8 — units, `flow_id` is not a workflow key, Claude Code headers
- `tas-session-corpus/docs/LABELS.md` — turn and task definitions
- [activity-reporting-and-onboarding.md](./activity-reporting-and-onboarding.md) — Plan #15, dashboard user vs gateway user
- [token.md](./token.md), [request-event.md](./request-event.md), [response-event.md](./response-event.md), [audit-log-entry.md](./audit-log-entry.md)
- [provider-credentials.md](./provider-credentials.md) — the BYOK envelope encryption that MCP connections reuse (§9.6)
- `SPEND_GOVERNOR_DESIGN.md` §2.1 — the Tenant → Org → Team → User hierarchy; budgets can attach to these units
- GitHub Docs, "About organizations", "About teams" (nested teams) — the structure model D4 follows
- Wireframe: <https://claude.ai/artifact/RC6zR1NmaYeNwKFVQxyCWb>
