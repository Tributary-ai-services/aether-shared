# Team Node

---

**Metadata**

```yaml
service: aether-be
model: Team
database: Neo4j
label: Team
version: 2.1.0
last_updated: 2026-11-21
status: implemented (Plan #22 A.1–A.4: aether-be #66, #67, #68, #69); repos / source_apps: AE-36 (aether-be #74)
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
| `repos` | list of string, or absent | GitHub repos the team owns, `owner/name`, stored **lowercase**, sorted, unique. At most 200. Absent = none (an empty list is stored as no property). AE-36. |
| `source_apps` | list of string, or absent | AIQG `source_app` values the team owns, sorted, unique, **case preserved** (AIQG matches `source_app` exactly). 1–100 characters of letters, digits and `. _ - : @ +`, starting with a letter or digit; no spaces or `/`. At most 100. AE-36. |
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
| `PUT` | `/api/v1/teams/:id/repos` | `{repos: ["owner/name", …]}` replaces the list (`[]` clears; the key is required); managers |
| `POST` | `/api/v1/teams/:id/repos` | `{repo: "owner/name"}` adds one (already owned = no-op); managers |
| `DELETE` | `/api/v1/teams/:id/repos/:owner/:name` | removes one; 404 `repo_not_owned` if the team does not own it; managers |
| `PUT` | `/api/v1/teams/:id/source-apps` | `{source_apps: [...]}` replaces (`[]` clears; key required); managers |
| `POST` | `/api/v1/teams/:id/source-apps` | `{source_app: "..."}` adds one; managers |
| `DELETE` | `/api/v1/teams/:id/source-apps/:source_app` | removes one; 404 `source_app_not_owned`; managers |

"Managers" = a team owner/admin or an org owner/admin. The repo and source-app writes return the team (`repos`, `sourceApps` in the response, always arrays); a malformed value is `400 invalid_repo` / `invalid_source_app`, too many is `400 too_many_repos` / `too_many_source_apps`, an archived team is `409 team_archived`. `PUT /teams/:id` does **not** take `repos` / `source_apps`.

### 5.1 Repos and source apps

They are the team's non-human members (design `aiqg/org-structure-and-attribution.md` §5.1): AIQG's proposer (§7, `repo_majority` / `repo_team`) attributes traffic to the team that owns its repo or source app. Nothing stops two teams in an org owning the same repo; the proposer has to treat that as ambiguous. They are also returned by the internal snapshot (`GET /internal/v1/orgs/:id/snapshot`, `teams[].repos` / `teams[].source_apps`, always arrays).

## 6. Events

Every change publishes to `aether.org.changes.v1`; see `aether-be/events/org-changes.md`.
