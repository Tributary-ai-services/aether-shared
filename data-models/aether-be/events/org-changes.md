# Org and team changes: `aether.org.changes.v1`

---

**Metadata**

```yaml
service: aether-be
topic: aether.org.changes.v1
wire_form: CloudEvents 1.0, structured mode (go-events/kafkabind)
cloudevent_type: com.tas.aether.org.<type>
key: Aether Organization.id
producer: aether-be internal/services/org_outbox.go (OrgChangeRelay)
consumers: aiqg-dashboard-be team history (Plan #22 2.2)
retention: 30 days
version: 1.1.0
last_updated: 2026-11-21
status: implemented (aether-be #69); publishing gated by AETHER_ORG_EVENTS_ENABLED (default off)
```

---

## 1. Overview

aether-be owns organizations, teams and membership. Every change to them is published here, so other services (first, AIQG's time-stamped team history, design `aiqg/org-structure-and-attribution.md` §5.1) can keep a copy without polling.

**Guarantees.**
- **No lost or invented events.** Each mutation writes an `(:OrgChangeOutbox)` node in the *same* Neo4j transaction as the change. A change that rolls back writes no event; a change that commits always has one.
- **At-least-once.** The relay publishes pending outbox nodes, then marks them. A relay that dies in between re-publishes on restart. **Consumers must dedupe on `event_id`** (also the CloudEvent `id`).
- **Per-org order.** Messages are keyed by org id, so one org's events land on one partition in commit order. Events from one transaction are ordered by `ord`.
- **Nothing lost while off.** The outbox is written even when publishing is disabled; enabling `AETHER_ORG_EVENTS_ENABLED` publishes the backlog. Published outbox nodes are pruned after 7 days.

## 2. Event types

| type | when | `team` | `member` / `owned` |
|---|---|---|---|
| `team.created` | a team is created | yes | — |
| `team.renamed` | name or slug changed | yes | — |
| `team.moved` | parent changed (`CHILD_OF`), including to top level | yes | — |
| `team.archived` | archived (directly, or because its org was deleted) | yes | — |
| `team.member.added` | joined, approved, invited, or created the team (owner) | yes | yes |
| `team.member.removed` | removed from the team, or from the org | yes | yes |
| `team.member.role_changed` | role changed | yes | yes |
| `team.member.joined_at_changed` | `joined_at` back-dated or corrected | yes | yes |
| `org.member.added` | added to the org, including the implied edge from joining a team | — | yes |
| `org.member.removed` | removed from the org | — | yes |
| `org.member.role_changed` | org role changed | — | yes |
| `team.repo.added` | a repo was added to the team (one event per repo) — AE-36 | yes | `owned` |
| `team.repo.removed` | a repo was removed (one event per repo) — AE-36 | yes | `owned` |
| `team.source_app.added` | a source app was added (one per app) — AE-36 | yes | `owned` |
| `team.source_app.removed` | a source app was removed (one per app) — AE-36 | yes | `owned` |
| `org.deleted` | the org was deleted (its teams were archived first, one `team.archived` each) | — | — |

## 3. Data (`OrgChangeEvent`)

```json
{
  "event_id": "8b0c…",                    // = CloudEvent id; dedupe on this
  "type": "team.member.added",
  "org_id": "4c4d7143-…",                 // Aether Organization.id; also the Kafka key
  "occurred_at": "2026-11-21T17:02:11Z",
  "actor_sub": "8014617b-…",              // Keycloak sub of who made the change; absent for system changes
  "team": {                               // the team's state AFTER the change
    "id": "…", "parent_team_id": "…", "name": "LLM Router", "slug": "llm-router",
    "visibility": "organization", "archived": false,
    "repos": ["tributary-ai-services/tas-llm-router"],   // AE-36: always present; [] = owns none
    "source_apps": ["claude-code"]                       // AE-36: always present; [] = owns none
  },
  "member": {
    "sub": "9088f68b-…",                  // Keycloak sub (= AIQG owner_sub). Never the Aether user id.
    "role": "member",                     // owner | admin | member | viewer (team); owner | admin | member | billing (org)
    "valid_from": "2026-10-01T00:00:00Z"  // joined_at for adds (may be back-dated); the change time otherwise
  }
}
```

A repo or source-app event carries `owned` instead of `member` (AE-36):

```json
{
  "event_id": "…", "type": "team.repo.added", "org_id": "…", "occurred_at": "…", "actor_sub": "…",
  "team": { "id": "…", "name": "LLM Router", "slug": "llm-router", "archived": false,
            "repos": ["tributary-ai-services/tas-llm-router"], "source_apps": [] },
  "owned": {
    "kind": "repo",                                // repo | source_app (= AIQG team_member_history.member_kind)
    "key": "tributary-ai-services/tas-llm-router"  // repo: lowercase "owner/name"; source_app: as stored, case preserved
  }
}
```

**Additive change (AE-36).** `team.repos`, `team.source_apps` and `owned` are new fields and the four `team.repo.*` / `team.source_app.*` types are new types; nothing existing changed. On every event that has a `team`, the two lists are the team's state after the change. An event written before AE-36 has neither key, which is different from `[]` (owns none). One write that changes several items commits all its events in one transaction, removals first, ordered by `ord`. The effective time of a repo/source-app event is `occurred_at` (there is no back-dating).

## 4. Consuming

- Treat events as **facts with a `valid_from`**, not commands: a back-dated `team.member.added` means "member since 1 October", and history before the event's arrival should be re-attributed.
- An `org.member.removed` is preceded by a `team.member.removed` for each of that person's teams in the org.
- Unknown types and unknown fields must be ignored (both are additive). aiqg-dashboard-be's `orghistory.Rows` does both today, so until it maps the AE-36 types it records nothing for repos/source apps.

## 5. Related

- `aether-be/nodes/team.md` · `aether-be/relationships/member-of.md`
- `aiqg/org-structure-and-attribution.md` §5.1 (the AIQG copy)
- Plan: `aiqg/org-structure-implementation-plan.md` Wave 2, A.4
