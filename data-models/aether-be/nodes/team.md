# Team Node

---

**Metadata**

```yaml
service: aether-be
model: Team
database: Neo4j
label: Team
version: 2.0.0
last_updated: 2026-11-21
status: implemented (Plan #22 A.1–A.4: aether-be #66, #67, #68, #69)
go_struct: aether-be/internal/models/team.go
```

---

## 1. Overview

A team is a group of people inside one Aether organization. Teams nest (one parent, same org), can be joined according to their visibility, and are **archived, never deleted**: other systems (AIQG team history, billing reports) refer to them by id.

## 2. Properties

| Property | Type | Notes |
|---|---|---|
| `id` | string (UUID) | Unique (`team_id_unique`). |
| `organization_id` | string | The owning org. Also the `BELONGS_TO` edge. |
| `name`, `description`, `icon` | string | |
| `slug` | string | Unique per org (`team_org_slug_unique` on `(organization_id, slug)`). Derived from the name (`-2`, `-3` on collision) unless given. |
| `visibility` | `private` \| `organization` \| `public` | Who sees it and how one joins (§4). |
| `parent_team_id` | string or null | Mirrors the `CHILD_OF` edge; written with it, nothing else writes it. |
| `settings` | string (JSON) | Stored as JSON text: Neo4j properties cannot be maps. (Before A.2 it was stored as a map, which made every create fail.) |
| `archived_at`, `archived_reason` | datetime, string | Set instead of deleting. `archived_reason` = `archived` or `organization_deleted`. |
| `created_by`, `created_at`, `updated_at` | | |

## 3. Relationships

| Edge | Notes |
|---|---|
| `(:Team)-[:BELONGS_TO]->(:Organization)` | Written at create. |
| `(:Team)-[:CHILD_OF]->(:Team)` | At most one; same org; the parent is not archived; no cycles (checked in the write transaction). |
| `(:User)-[:MEMBER_OF {role, joined_at, invited_by}]->(:Team)` | Roles `owner` / `admin` / `member` / `viewer`. `joined_at` may be back-dated by a manager, never in the future. |
| `(:User)-[:JOIN_REQUEST {id, status, requested_at, decided_by, decided_at}]->(:Team)` | Requests to join a private team. `status`: `pending` → `approved` / `denied`; kept after the decision. |

Joining a team in an org the person is not in also creates `(:User)-[:MEMBER_OF {role: 'member', implied_by_team}]->(:Organization)`.

## 4. Visibility, joining and reading

| visibility | Listed to | Readable by id by | Joining |
|---|---|---|---|
| `public` | everyone | everyone | anyone signed in joins |
| `organization` | org members | org members | org members self-join |
| `private` | the team's members | org members (so a shared link can lead to a request) | a request, approved by a team owner/admin or an org owner/admin |

Anyone else gets 404. Archived teams are listed to no one, stay readable by id, and refuse writes (`409 team_archived`).

## 5. API (aether-be)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/v1/teams` | `parent_team_id?`, `slug?` |
| `PUT` | `/api/v1/teams/:id` | also `slug?`, `parent_team_id?` |
| `DELETE` | `/api/v1/teams/:id` | **archives** |
| `POST` | `/api/v1/teams/:id/move` | `{parent_team_id}` (`""` = top level); a manager of the team and of the new parent, or an org owner/admin |
| `POST` | `/api/v1/teams/:id/join` | 200 `{status: joined}` or 202 `{status: requested}` |
| `GET` | `/api/v1/teams/:id/join-requests` | managers |
| `POST` | `/api/v1/teams/:id/join-requests/:request_id` | `{approve, joined_at?}`; managers |
| `PUT` | `/api/v1/teams/:id/members/:user_id/joined-at` | `{joined_at}`; managers |

## 6. Events

Every change publishes to `aether.org.changes.v1`; see `aether-be/events/org-changes.md`.
