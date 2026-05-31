# Notebook-Space Relationship Implementation Plan

**Created**: 2026-01-11
**Status**: Implemented (reconciled with as-built code 2026-05-30)
**Author**: Claude Code Analysis

> **Reconciliation note (2026-05-30):** This document was originally written as a forward-looking proposal. It has been updated to describe the **as-built** implementation in `aether-be`. Where the shipped code intentionally diverged from the original proposal, the divergence is called out inline with a **⚠ Divergence** marker. Code is the source of truth; this doc tracks it.

## Executive Summary

This document describes how Notebooks and Spaces are modeled in the TAS platform's Neo4j database. Notebooks carry embedded `space_id` / `tenant_id` / `space_type` fields **and** an explicit `(:Notebook)-[:BELONGS_TO]->(:Space)` relationship (hybrid model — embedded fields for fast/secure filtering, relationships for traversal and RBAC).

### Key Principle: Space = Tenant

A **Space** is the top-level isolation boundary in TAS. Each Space maps 1:1 to a `tenant_id` that flows across all services (Aether, AudiModal, DeepLake, Agent Builder). The creator of a personal Space is its owner (via `OWNS`); organization Spaces are owned by an Organization and accessed via org membership. Other users access via RBAC (`MEMBER_OF`).

---

## Current State Analysis

### What Exists Today

**Embedded Fields on Notebook Node:**
```go
type Notebook struct {
    ID        string `json:"id"`
    SpaceID   string `json:"space_id"`    // "space_1767395606"
    TenantID  string `json:"tenant_id"`   // "tenant_1767395606"
    SpaceType string `json:"space_type"`  // "personal" | "organization"
    OwnerID   string `json:"owner_id"`
    // ...
}
```

**Relationships (all implemented):**
```cypher
(Notebook)-[:OWNED_BY]->(User)         // Notebook ownership
(Notebook)-[:CONTAINS]->(Notebook)     // Parent-child hierarchy
(Notebook)-[:BELONGS_TO]->(Space)      // Notebook → Space containment
(User)-[:OWNS]->(Space)                // Personal space ownership
(User)-[:MEMBER_OF {role,...}]->(Space)         // Direct RBAC membership
(User)-[:MEMBER_OF {role,...}]->(Organization)  // Org membership
(Organization)-[:HAS_SPACE]->(Space)            // Org → its space(s)
(Conversation)-[:BELONGS_TO]->(Notebook)        // (related, in conversation.go)
```

Implemented in: `internal/services/space.go`, `internal/services/notebook.go`, `internal/services/user.go`. A personal Space + `OWNS` relationship is created automatically at user creation (`user.go`).

### ID Format Convention

```
tenant_id: "tenant_<unix_timestamp>"  → Cross-service identifier
space_id:  "space_<unix_timestamp>"   → Internal Aether identifier

Example:
  tenant_id: "tenant_1767395606"
  space_id:  "space_1767395606"

Rule: space_id and tenant_id share the SAME timestamp, different prefix.
Both are minted together in models.NewSpace().
```

> **⚠ Divergence — `audimodal_tenant_id` is NOT derived from the timestamp.**
> The original proposal assumed `audimodal_tenant_id == tenant_id`. In the shipped
> code, `AudimodalTenantID` holds the **UUID returned by AudiModal** when the tenant
> is created there — it is a distinct value, not the `tenant_<timestamp>` string.
> `deeplake_namespace` does equal `tenant_id`. See the validation section below.

---

## Proposed Architecture

### 1. Space Node Schema

Space is a first-class Neo4j node. As-built struct (`internal/models/space.go`):

```go
type Space struct {
    // Identity
    ID       string `json:"id" validate:"required"`        // "space_<timestamp>"
    TenantID string `json:"tenant_id" validate:"required"` // "tenant_<timestamp>" - cross-service

    // Cross-Service Mapping
    AudimodalTenantID string `json:"audimodal_tenant_id,omitempty"` // UUID returned by AudiModal (NOT == TenantID)
    DeeplakeNamespace string `json:"deeplake_namespace,omitempty"`  // == TenantID
    DeeplakeAPIKey    string `json:"-"`                              // API key, never serialized

    // Display
    Name        string `json:"name" validate:"required,min=1,max=100"`
    Description string `json:"description,omitempty" validate:"max=500"`

    // Type & Ownership
    Type       SpaceType      `json:"type" validate:"required,oneof=personal organization"`
    Visibility string         `json:"visibility" validate:"required,oneof=private team organization public"`
    OwnerID    string         `json:"owner_id" validate:"required"`
    OwnerType  SpaceOwnerType `json:"owner_type" validate:"required,oneof=user organization"`

    // Status (soft-delete lifecycle)
    Status    SpaceStatus `json:"status" validate:"required,oneof=active suspended deleted"`
    DeletedAt *time.Time  `json:"deleted_at,omitempty"`
    DeletedBy string      `json:"deleted_by,omitempty"`

    // Settings
    Quotas   *SpaceQuotas           `json:"quotas,omitempty"`
    Settings map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`

    // Timestamps
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type SpaceQuotas struct {
    MaxNotebooks     int   `json:"max_notebooks"`
    MaxDocuments     int   `json:"max_documents"`
    MaxStorageBytes  int64 `json:"max_storage_bytes"`
    MaxMembersCount  int   `json:"max_members_count"`  // org spaces
    MaxTeamsCount    int   `json:"max_teams_count"`    // org spaces
    UsedNotebooks    int   `json:"used_notebooks"`     // live usage counters
    UsedDocuments    int   `json:"used_documents"`
    UsedStorageBytes int64 `json:"used_storage_bytes"`
}
```

Constructors: `NewSpace(...)`, `NewPersonalSpace(userID, userName)`, `NewOrganizationSpace(orgID, orgName)`. Defaults via `DefaultSpaceQuotas(type)` (personal: 100 notebooks / 1000 docs / 5 GB / 1 member; organization: larger). Lifecycle helpers on the struct: `SoftDelete`, `Restore`, `Suspend`, `IsActive`, `CanUserModify/Delete/Invite`.

> **⚠ Divergence from proposal:** added `Visibility`, `Status`/`DeletedAt`/`DeletedBy` (soft delete),
> `Settings`, `DeeplakeAPIKey`, and usage-counter quota fields. `Type`/`OwnerType` are typed
> (`SpaceType`, `SpaceOwnerType`) rather than raw strings.

> **⚠ Property-naming caveat (verify against live DB):** Go field `Type` serializes to JSON
> `type`, but several Cypher queries read **`space.space_type`** (e.g. `GetUserSpaces`), and
> notebooks store **`space_type`**. Migration `002` creates an index on `s.type`. Whether Space
> nodes persist the type under `type`, `space_type`, or both is exactly what the Neo4j
> verification step below must confirm.

### 2. Relationship Structure

```
                       OWNS
   ┌─────────┐──────────────────────────>┌─────────┐
   │  User   │                            │  Space  │
   └─────────┘──MEMBER_OF{role,perms}────>└─────────┘
     │   │                                   ^   ^
     │   │ MEMBER_OF{role}                   │   │ HAS_SPACE
     │   v                                   │   │
     │ ┌──────────────┐                      │  ┌──────────────┐
     │ │ Organization │──────────────────────┘  │ Organization │
     │ └──────────────┘  (org spaces)           └──────────────┘
     │                                  BELONGS_TO │
     │            OWNED_BY                         │
     └───────────────────────────>┌──────────┐────┘
                                   │ Notebook │
                                   └──────────┘
```

**Two access paths to a Space:**
- **Personal:** `(User)-[:OWNS]->(Space {space_type:"personal"})`, plus optional `(User)-[:MEMBER_OF]->(Space)` for invited collaborators.
- **Organization:** `(User)-[:MEMBER_OF]->(Organization)-[:HAS_SPACE]->(Space {space_type:"organization"})`. The user's role comes from the `MEMBER_OF`→Organization edge.

#### 2.1 OWNS Relationship (Creator → Space)

```cypher
// Personal space ownership
(u:User)-[:OWNS]->(s:Space {type: "personal"})

// Organization space ownership
(o:Organization)-[:OWNS]->(s:Space {type: "organization"})
```

**Properties:** None (ownership is implicit in relationship existence)

**Cardinality:**
- User can own multiple spaces (1 personal + N organization)
- Personal space: exactly 1 per user
- Organization space: 1 per organization

#### 2.2 MEMBER_OF Relationship (RBAC Access)

```cypher
(u:User)-[:MEMBER_OF {
    role: "admin" | "member" | "viewer",
    permissions: ["read", "write", "admin"],
    joined_at: datetime(),
    invited_by: "user-uuid",
    expires_at: datetime()  // Optional, for temporary access
}]->(s:Space)
```

**Role Hierarchy:**
| Role | Capabilities |
|------|-------------|
| owner | Full control (implicit via OWNS, not stored in MEMBER_OF) |
| admin | Manage members, settings, quotas, all content |
| member | Create/edit notebooks and documents |
| viewer | Read-only access |

**Note:** The owner is identified by the `OWNS` relationship, not by `MEMBER_OF`. This avoids duplication.

#### 2.3 BELONGS_TO Relationship (Notebook → Space)

```cypher
(n:Notebook)-[:BELONGS_TO]->(s:Space)
```

**Properties:** None (containment is simple membership)

**Cardinality:**
- Each Notebook belongs to exactly ONE Space
- A Space can contain many Notebooks

**Critical Rule:** Notebooks cannot be moved across space boundaries.

### 3. Notebook Model (Updated)

Keep embedded fields for query performance (hybrid approach):

```go
type Notebook struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`

    // Space reference - BOTH fields required
    SpaceID     string `json:"space_id"`    // "space_1767395606"
    TenantID    string `json:"tenant_id"`   // "tenant_1767395606"
    SpaceType   string `json:"space_type"`  // "personal" | "organization"

    // Ownership
    OwnerID     string `json:"owner_id"`

    // ... other existing fields
}
```

**Query Pattern (Required):**
```cypher
// ALL notebook queries MUST include BOTH filters for security
MATCH (n:Notebook)
WHERE n.tenant_id = $tenant_id
  AND n.space_id = $space_id
  AND n.status = "active"
RETURN n
```

---

## Query Patterns

### Get All Notebooks in a Space (Property-based, fast)

```cypher
MATCH (n:Notebook {space_id: $space_id, status: "active"})
OPTIONAL MATCH (n)-[:OWNED_BY]->(owner:User)
RETURN n, owner
ORDER BY n.updated_at DESC
```

### Get All Spaces a User Can Access (as-built, `GetUserSpaces`)

```cypher
MATCH (u:User {keycloak_id: $keycloak_id})

// Personal spaces (direct ownership)
OPTIONAL MATCH (u)-[:OWNS]->(personalSpace:Space)
WHERE personalSpace.space_type = 'personal'

// Organization spaces (via org membership)
OPTIONAL MATCH (u)-[orgMembership:MEMBER_OF]->(org:Organization)-[:HAS_SPACE]->(orgSpace:Space)

WITH u,
     COLLECT(DISTINCT {space: personalSpace, role: 'owner', org_id: null, org_name: null}) AS personal_spaces,
     COLLECT(DISTINCT {space: orgSpace, role: orgMembership.role, org_id: org.id, org_name: org.name}) AS org_spaces
UNWIND (personal_spaces + org_spaces) AS si
WITH si WHERE si.space IS NOT NULL
RETURN DISTINCT si.space.id AS id, si.space.name AS name, si.space.space_type AS type,
       si.space.tenant_id AS tenant_id, si.role AS role,
       si.org_id AS organization_id, si.org_name AS organization_name
ORDER BY si.space.created_at DESC
```

> Note: user is matched by **`keycloak_id`** (JWT subject), and space type is read from
> **`space_type`**, not `type`.

### Get All Notebooks User Can Access (Relationship-based)

```cypher
MATCH (u:User {id: $user_id})-[:OWNS|MEMBER_OF]->(s:Space)<-[:BELONGS_TO]-(n:Notebook)
WHERE n.status = "active"
RETURN n, s
ORDER BY n.updated_at DESC
```

### Check User Access to Specific Notebook

```cypher
MATCH (u:User {id: $user_id})-[r:OWNS|MEMBER_OF]->(s:Space)<-[:BELONGS_TO]-(n:Notebook {id: $notebook_id})
RETURN n,
       type(r) as access_type,
       CASE WHEN type(r) = 'OWNS' THEN 'owner' ELSE r.role END as role
```

### Check If User Can Write to Notebook

```cypher
MATCH (u:User {id: $user_id})-[r:OWNS|MEMBER_OF]->(s:Space)<-[:BELONGS_TO]-(n:Notebook {id: $notebook_id})
WHERE type(r) = 'OWNS'
   OR r.role IN ['admin', 'member']
   OR 'write' IN r.permissions
RETURN count(*) > 0 as can_write
```

### Get All Spaces User Belongs To

```cypher
MATCH (u:User {id: $user_id})-[r:OWNS|MEMBER_OF]->(s:Space)
RETURN s,
       type(r) as relationship,
       CASE WHEN type(r) = 'OWNS' THEN 'owner' ELSE r.role END as role
ORDER BY s.created_at
```

### Invite User to Space

```cypher
MATCH (s:Space {id: $space_id}), (u:User {id: $invitee_id})
CREATE (u)-[:MEMBER_OF {
    role: $role,
    permissions: $permissions,
    joined_at: datetime(),
    invited_by: $inviter_id
}]->(s)
RETURN u, s
```

---

## Migration Plan

> **As-built:** migrations shipped in `aether-be/migrations/`. Run order:
> 1. `001_migrate_to_space_model.go` — adds `space_id`/`tenant_id` to existing notebooks & documents (Go runner; has `//go:build ignore`, run via `go run`).
> 2. `002_add_space_constraints.cypher` — Space uniqueness constraints (`space_id_unique`, `space_tenant_id_unique`) + indexes.
> 3. `003_backfill_space_relationships.cypher` — creates `OWNS` (User→personal Space, keyed on `user.personal_space_id`) and `BELONGS_TO` (Notebook→Space, keyed on `notebook.space_id`). All backfilled edges tagged `migrated: true` for rollback.
> 4. `004_link_orphan_spaces_to_organizations.cypher` (+ `run_004_link_orphan_spaces.sh`) — links org spaces via `HAS_SPACE`.
> - Rollback: `rollback_space_model.go`. Helper cypher: `add_space_fields_to_notebooks.cypher`, `add_space_fields_to_documents.cypher`.
>
> The phases below are the original design intent, retained for context. The actual
> backfill keys off `user.personal_space_id` / `notebook.space_id` (not the proposal's
> `u.personal_tenant_id`).

### Phase 1: Create Space Nodes

```cypher
// Create Space nodes from existing tenant_id values on notebooks
MATCH (n:Notebook)
WHERE n.tenant_id IS NOT NULL
WITH DISTINCT n.tenant_id AS tenant_id,
              n.space_id AS space_id,
              n.space_type AS space_type,
              n.owner_id AS owner_id
WHERE NOT EXISTS { MATCH (s:Space {id: space_id}) }
CREATE (s:Space {
    id: space_id,
    tenant_id: tenant_id,
    name: "Migrated Space",
    type: COALESCE(space_type, "personal"),
    owner_id: owner_id,
    owner_type: "user",
    audimodal_tenant_id: tenant_id,
    deeplake_namespace: tenant_id,
    created_at: datetime(),
    updated_at: datetime()
})
RETURN count(s) as spaces_created
```

### Phase 2: Create OWNS Relationships

```cypher
// Link users to their personal spaces via OWNS
MATCH (u:User)
WHERE u.personal_tenant_id IS NOT NULL
MATCH (s:Space {tenant_id: u.personal_tenant_id})
WHERE NOT EXISTS { (u)-[:OWNS]->(s) }
MERGE (u)-[:OWNS]->(s)
SET s.name = COALESCE(u.full_name, u.username) + "'s Space",
    s.owner_id = u.id
RETURN count(*) as ownership_relationships_created
```

### Phase 3: Create BELONGS_TO Relationships

```cypher
// Link notebooks to their spaces
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL
MATCH (s:Space {id: n.space_id})
WHERE NOT EXISTS { (n)-[:BELONGS_TO]->(s) }
CREATE (n)-[:BELONGS_TO]->(s)
RETURN count(*) as notebook_relationships_created
```

### Phase 4: Verify Migration

```cypher
// Verify all notebooks have BELONGS_TO relationship
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL
  AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) }
RETURN count(n) as orphaned_notebooks

// Verify all personal spaces have OWNS relationship
MATCH (s:Space {type: "personal"})
WHERE NOT EXISTS { (:User)-[:OWNS]->(s) }
RETURN count(s) as orphaned_spaces

// Verify consistency between embedded field and relationship
MATCH (n:Notebook)-[:BELONGS_TO]->(s:Space)
WHERE n.space_id <> s.id
RETURN n.id, n.space_id, s.id as relationship_space_id
```

### Phase 5: Create Indexes

```cypher
// Space indexes
CREATE INDEX space_id_idx IF NOT EXISTS FOR (s:Space) ON (s.id);
CREATE INDEX space_tenant_idx IF NOT EXISTS FOR (s:Space) ON (s.tenant_id);
CREATE INDEX space_owner_idx IF NOT EXISTS FOR (s:Space) ON (s.owner_id);
CREATE INDEX space_type_idx IF NOT EXISTS FOR (s:Space) ON (s.type);

// Ensure uniqueness
CREATE CONSTRAINT space_id_unique IF NOT EXISTS FOR (s:Space) REQUIRE s.id IS UNIQUE;
CREATE CONSTRAINT space_tenant_unique IF NOT EXISTS FOR (s:Space) REQUIRE s.tenant_id IS UNIQUE;
```

---

## Implementation Checklist

### Backend Changes (aether-be)

- [x] `internal/models/space.go` with Space struct (+ `space_context.go`)
- [~] ~~`internal/repositories/space_repository.go`~~ — **divergence:** no separate repository; Neo4j queries live inline in `internal/services/space.go`
- [x] `internal/services/space.go` (space service, ~1085 lines)
- [x] `internal/handlers/space.go` with CRUD + member endpoints
- [x] `internal/services/notebook.go` creates `BELONGS_TO` relationship
- [x] `internal/services/user.go` creates personal Space + `OWNS` on user creation
- [x] RBAC permission checks via relationship queries (`GetUserRoleInSpace`, `CanUser*`)
- [x] Migration files in `migrations/` (`001`–`004` + rollback)
- [ ] **Pending:** confirm migrations have been run against the live Neo4j (see Verification)
- [ ] **Pending:** resolve `type` vs `space_type` property-naming caveat

### API Endpoints (as-built — `internal/handlers/routes.go:603`)

| Method | Endpoint | Handler | Description |
|--------|----------|---------|-------------|
| POST | `/api/v1/spaces` | `CreateSpace` | Create new space |
| GET | `/api/v1/spaces` | `GetSpaces` | List user's spaces (owned + member) |
| GET | `/api/v1/spaces/:id` | `GetSpace` | Get space details |
| **PUT** | `/api/v1/spaces/:id` | `UpdateSpace` | Update space settings (⚠ PUT, not PATCH) |
| DELETE | `/api/v1/spaces/:id` | `DeleteSpace` | Soft-delete space (owner only) |
| GET | `/api/v1/spaces/:id/members` | `ListSpaceMembers` | List space members |
| POST | `/api/v1/spaces/:id/members` | `AddSpaceMember` | Invite user to space |
| PATCH | `/api/v1/spaces/:id/members/:userId` | `UpdateSpaceMember` | Update member role |
| DELETE | `/api/v1/spaces/:id/members/:userId` | `RemoveSpaceMember` | Remove member |
| GET | `/api/v1/users/me/spaces` | `GetUserSpaces` | Current user's spaces (convenience) |

### Frontend Changes (aether)

- [x] Redux space slice — `store/slices/spacesSlice.js`
- [x] Space management UI — `CreateSpaceModal`, `ManageSpacesModal`, `components/spaces/`
- [x] Notebook list space context — `contexts/SpaceContext.jsx`, `hooks/useSpaces.js`
- [x] Space switcher — `components/ui/SpaceSelector.jsx`
- [x] Member invitation flow — `components/spaces/SpaceMembersPanel.jsx`, `hooks/useSpaceRole.js`

---

## Cross-Service Considerations

### Tenant ID Propagation

The `tenant_id` MUST be identical across all services:

```
Aether Space.tenant_id
    ↓
AudiModal Tenant.id (same value)
    ↓
DeepLake namespace (same value)
    ↓
Agent Builder space_id (same value)
```

### Validation (as-built)

> **⚠ Divergence:** there is **no** `Space.Validate()` method. Field-level validation is
> enforced via `validate:` struct tags (see the struct above) using the project's validator.
> `space_id`/`tenant_id` are minted together in `NewSpace()` so they share a timestamp by
> construction rather than by a runtime check.

The original proposal's cross-service equality check is **obsolete and must not be
implemented as written**, because `AudimodalTenantID` is the UUID AudiModal returns — it is
not equal to `tenant_id`:

```go
// OBSOLETE — do NOT enforce. Kept only to document the rejected design.
// if s.AudimodalTenantID != s.TenantID { ... }   // WRONG: audimodal id is a UUID

// Still true:  s.DeeplakeNamespace == s.TenantID  (set in NewSpace)
```

If a validation hook is added later, the correct invariants are:
- `space_id` and `tenant_id` share the same `<timestamp>` suffix.
- `deeplake_namespace == tenant_id`.
- `audimodal_tenant_id` is a non-empty UUID **once the AudiModal tenant has been provisioned** (may be empty before provisioning).

---

## Security Considerations

### Query Security Rules

1. **ALL notebook queries MUST filter by tenant_id AND space_id**
2. **Never trust client-provided space_id** - validate against user's accessible spaces
3. **Check OWNS or MEMBER_OF relationship** before any space operation
4. **Audit log all MEMBER_OF changes** (invites, role changes, removals)

### Access Control Matrix

| Operation | Owner | Admin | Member | Viewer |
|-----------|-------|-------|--------|--------|
| View space | ✅ | ✅ | ✅ | ✅ |
| Edit space settings | ✅ | ✅ | ❌ | ❌ |
| Delete space | ✅ | ❌ | ❌ | ❌ |
| Invite members | ✅ | ✅ | ❌ | ❌ |
| Remove members | ✅ | ✅ | ❌ | ❌ |
| Create notebook | ✅ | ✅ | ✅ | ❌ |
| Edit notebook | ✅ | ✅ | ✅ | ❌ |
| Delete notebook | ✅ | ✅ | ❌ | ❌ |
| View notebook | ✅ | ✅ | ✅ | ✅ |

---

## Open Questions — Resolutions (as-built)

1. **Organization Spaces** — ✅ Resolved. Created against a separate `Organization` entity and linked via `(Organization)-[:HAS_SPACE]->(Space)`; users reach them through `(User)-[:MEMBER_OF]->(Organization)`. Constructor: `NewOrganizationSpace(orgID, orgName)`.

2. **Space Transfer** — ⛔ Not implemented. No ownership-transfer endpoint exists. Still open.

3. **Space Deletion** — ✅ Resolved as **soft delete**. `DeleteSpace` sets `Status="deleted"` + `DeletedAt`/`DeletedBy` (`SoftDelete()`); notebooks are not cascade-hard-deleted. `Restore()` exists in the model but is not exposed via a route.

4. **Cross-Space Sharing** — ✅ Resolved: **one space only**. A Notebook has exactly one `BELONGS_TO` and carries embedded `space_id`/`tenant_id`. No multi-space relationship.

5. **Guest Access** — ⛔ Not implemented. Roles are `admin` / `member` / `viewer` (owner via `OWNS`); no `guest`. `AddMemberRequest.Role` is `oneof=admin member viewer`. Still open.

### Still Open / Follow-ups
- Ownership transfer (Q2) and guest role (Q5).
- `type` vs `space_type` property naming (see caveat in §1) — needs live-DB confirmation.
- Expose `Restore()` via an API route if undelete is a product requirement.

---

## Live Neo4j Verification (2026-05-30)

Verified against production Neo4j (`aether-be/neo4j-0`, via port-forward). **10 Space nodes, 45 Notebooks.**

### ✅ Healthy
- All 45 notebooks have an embedded `space_id`.
- `BELONGS_TO` relationship/embedded `space_id` agreement: **0 mismatches**.
- All 6 personal spaces have exactly one `OWNS` edge from a User.
- No duplicate `tenant_id` values.
- `MEMBER_OF` unused so far (0 members) — expected; invitation flow just hasn't been exercised.

### ❌ Issues found

1. **Property naming split — `type` vs `space_type` (confirms §1 caveat).**
   8 spaces store `space_type`; **2 older spaces store `type`** (`space_1766596584`, `space_1767207502`) and have **no `space_type`**.
   **Impact:** `GetUserSpaces` filters `WHERE personalSpace.space_type = 'personal'`, so these 2 spaces are **invisible** to their owners' space list — and `space_1766596584` holds **34 notebooks**. High-priority bug.

2. **4 orphaned notebooks → non-existent Spaces.**
   Notebooks `77d9f775…`, `476acec8…`, `b2456841…`, `51ddd18f…` carry `space_id`s (`space_1766443645`, `space_1766444009`, `space_1767375053`, `space_1767375380`) for which **no Space node exists at all**. They have no `BELONGS_TO`. The Space nodes were never created (migration `001` set the field but the node was never minted).

3. **Migrations 002–004 never applied.**
   Only the `space_id_unique` constraint exists. Missing: `space_tenant_id_unique` constraint and all migration-002 indexes (`space_owner_id_idx`, `space_type_idx`, `space_status_idx`, `space_owner_type_idx`).
   The `OWNS` / `BELONGS_TO` / `HAS_SPACE` edges that *do* exist were created by the **application at runtime** (`user.go`, `notebook.go`), not by the backfill migration — which is why pre-existing data (the 4 orphans, the 2 `type`-only spaces) was never reconciled.

4. **`status` only on organization spaces (4 of 10).**
   The 6 personal spaces have **no `status`** property, so `Space.IsActive()` (`Status == "active"`) returns false for them. Any query that filters `status = 'active'` will silently drop all personal spaces. Currently `GetUserSpaces` does not filter on status, so it's latent — but fragile.

5. **Organization-space wiring inconsistent.**
   - `space_1768924118`, `space_1768953334`: org-type but linked by a stray `User-[:OWNS]->Space` and **no** `HAS_SPACE` (one has `owner_id: ""`).
   - `space_1769019019`, `space_1772236887`: correctly `Organization-[:HAS_SPACE]->Space` but **no** `OWNS`.
   Two different wiring conventions exist for org spaces.

6. **ID format wider than documented.**
   3 spaces use `space_<UUID>` instead of `space_<unix_timestamp>`. The "space_id and tenant_id share the same timestamp" invariant does **not** hold for these. The ID convention has effectively expanded to allow UUIDs.

### Remediation applied (2026-05-30)

Fixed on branch `fix/space-data-model-reconciliation` and against production Neo4j:

- ✅ **#1 `space_type` split** — `GetUserSpaces` now reads `COALESCE(space_type, type)` (filter + return), and the 2 legacy spaces were backfilled (`space_type` from `type`). Restores access to the 34-notebook space. (`internal/services/space.go`)
- ✅ **#2 4 orphans** — confirmed owners no longer exist and nodes had 0 relationships / 0 content; **hard-deleted** (`DETACH DELETE`). Notebook count 45 → 41, orphaned 0.
- ✅ **#3 migrations 002–04** — applied missing `space_tenant_id_unique` constraint + indexes (`owner_id`, `space_type`, `status`, `owner_type`). Migration `002` file corrected: `s.type` → `s.space_type`.
- ✅ **#4 `status`** — backfilled `status:"active"` onto the 6 personal spaces.
- 📄 Captured as idempotent migration `aether-be/migrations/005_reconcile_space_data.cypher`.

**Post-fix integrity sweep:** 41 notebooks / 0 orphaned; 10 spaces / 0 missing `space_type` / 0 missing `status` / 0 personal spaces without owner.

### Still open (deferred follow-ups)
- ⛔ **#5 Org-space wiring inconsistency** — 2 org spaces use a stray `User-[:OWNS]->Space` with no `HAS_SPACE`; 2 use `HAS_SPACE` with no `OWNS`. Left as-is (touching org access is higher-risk); needs a decision on the canonical convention before normalizing.
- ⛔ **#6 UUID-form space IDs** — 3 spaces use `space_<UUID>`; the timestamp invariant doesn't hold. Documented, not "fixed" (it's a convention expansion, not corruption).
- ⛔ Ownership transfer (Q2) and guest role (Q5) — still unimplemented features.

---

## References

- `aether-shared/data-models/aether-be/nodes/notebook.md` - Notebook model documentation
- `aether-shared/data-models/aether-be/nodes/space.md` - Space architecture documentation
- `aether-shared/data-models/cross-service/` - Cross-service integration patterns
- `SPACE_BASED_IMPLEMENTATION_PLAN.md` - Original space-based multi-tenancy plan
