# Notebook-Space Relationship Implementation Plan

**Created**: 2026-01-11
**Status**: Proposed
**Author**: Claude Code Analysis

## Executive Summary

This document outlines the plan to properly model the relationship between Notebooks and Spaces in the TAS platform's Neo4j database. The current implementation uses embedded fields (`space_id`, `tenant_id`) on Notebook nodes without explicit graph relationships. This plan aligns with the documented data model architecture while addressing identified gaps.

### Key Principle: Space = Tenant

A **Space** is the top-level isolation boundary in TAS. Each Space maps 1:1 to a `tenant_id` that flows across all services (Aether, AudiModal, DeepLake, Agent Builder). The creator of a Space is automatically the owner/admin, and other users access via RBAC permissions.

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

**Existing Relationships:**
```cypher
(Notebook)-[:OWNED_BY]->(User)      // Implemented
(Notebook)-[:CONTAINS]->(Notebook)  // Parent-child hierarchy, implemented
```

**What's Missing:**
- No dedicated Space node in Neo4j
- No explicit `BELONGS_TO` relationship between Notebook and Space
- No `OWNS` relationship between User and Space
- No `MEMBER_OF` relationship for RBAC

### ID Format Convention

```
tenant_id: "tenant_<unix_timestamp>"  → Cross-service identifier
space_id:  "space_<unix_timestamp>"   → Internal Aether identifier

Example:
  tenant_id: "tenant_1767395606"
  space_id:  "space_1767395606"

Rule: Both share the SAME timestamp, different prefix
```

---

## Proposed Architecture

### 1. Space Node Schema

Create Space as a first-class Neo4j node:

```go
type Space struct {
    // Identity
    ID          string    `json:"id"`          // "space_1767395606" (primary)
    TenantID    string    `json:"tenant_id"`   // "tenant_1767395606" (cross-service)

    // Display
    Name        string    `json:"name"`
    Description string    `json:"description"`

    // Type & Ownership
    Type        string    `json:"type"`        // "personal" | "organization"
    OwnerID     string    `json:"owner_id"`    // User ID or Organization ID
    OwnerType   string    `json:"owner_type"`  // "user" | "organization"

    // Cross-Service Mapping
    AudimodalTenantID  string `json:"audimodal_tenant_id"`  // Must equal TenantID
    DeeplakeNamespace  string `json:"deeplake_namespace"`   // Must equal TenantID

    // Quotas
    Quotas      SpaceQuotas `json:"quotas"`

    // Metadata
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

type SpaceQuotas struct {
    MaxStorageBytes       int64 `json:"max_storage_bytes"`
    MaxDocuments          int   `json:"max_documents"`
    MaxNotebooks          int   `json:"max_notebooks"`
    MonthlyProcessingMins int   `json:"monthly_processing_mins"`
}
```

**Neo4j Node Creation:**
```cypher
CREATE (s:Space {
    id: $id,
    tenant_id: $tenant_id,
    name: $name,
    description: $description,
    type: $type,
    owner_id: $owner_id,
    owner_type: $owner_type,
    audimodal_tenant_id: $tenant_id,
    deeplake_namespace: $tenant_id,
    quotas: $quotas,
    created_at: datetime(),
    updated_at: datetime()
})
RETURN s
```

### 2. Relationship Structure

```
┌─────────┐     OWNS      ┌─────────┐
│  User   │──────────────>│  Space  │
└─────────┘               └─────────┘
     │                         ^
     │                         │
     │    MEMBER_OF            │ BELONGS_TO
     │  {role, permissions}    │
     │                         │
     └────────────────>   ┌──────────┐
                          │ Notebook │
                          └──────────┘
```

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

- [ ] Create `internal/models/space.go` with Space struct
- [ ] Create `internal/repositories/space_repository.go`
- [ ] Create `internal/services/space_service.go`
- [ ] Update `internal/handlers/space.go` with CRUD endpoints
- [ ] Update `internal/services/notebook.go` to create BELONGS_TO relationship
- [ ] Update `internal/services/user.go` to create Space + OWNS on user creation
- [ ] Add RBAC permission checks using relationship queries
- [ ] Create migration file in `migrations/` directory

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/spaces` | Create new space |
| GET | `/api/v1/spaces` | List user's spaces (owned + member) |
| GET | `/api/v1/spaces/:id` | Get space details |
| PATCH | `/api/v1/spaces/:id` | Update space settings |
| DELETE | `/api/v1/spaces/:id` | Delete space (owner only) |
| POST | `/api/v1/spaces/:id/members` | Invite user to space |
| GET | `/api/v1/spaces/:id/members` | List space members |
| PATCH | `/api/v1/spaces/:id/members/:userId` | Update member role |
| DELETE | `/api/v1/spaces/:id/members/:userId` | Remove member |

### Frontend Changes (aether)

- [ ] Update Redux space slice to handle Space as entity
- [ ] Create space management UI (settings, members)
- [ ] Update notebook list to show space context
- [ ] Add space switcher component
- [ ] Implement member invitation flow

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

### Validation Rule

```go
func (s *Space) Validate() error {
    // Ensure tenant_id and space_id share same timestamp
    spaceTimestamp := strings.TrimPrefix(s.ID, "space_")
    tenantTimestamp := strings.TrimPrefix(s.TenantID, "tenant_")

    if spaceTimestamp != tenantTimestamp {
        return fmt.Errorf("space_id and tenant_id must share same timestamp: got %s vs %s",
            spaceTimestamp, tenantTimestamp)
    }

    // Ensure cross-service IDs match
    if s.AudimodalTenantID != s.TenantID {
        return fmt.Errorf("audimodal_tenant_id must equal tenant_id")
    }
    if s.DeeplakeNamespace != s.TenantID {
        return fmt.Errorf("deeplake_namespace must equal tenant_id")
    }

    return nil
}
```

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

## Open Questions

1. **Organization Spaces**: How are organization spaces created? Via separate Organization entity or directly?

2. **Space Transfer**: Can ownership of a space be transferred to another user?

3. **Space Deletion**: What happens to notebooks when a space is deleted? Cascade delete or prevent deletion if non-empty?

4. **Cross-Space Sharing**: Should notebooks ever be visible in multiple spaces (via separate relationship), or strictly one space only?

5. **Guest Access**: Should there be a "guest" role with even more limited access than viewer?

---

## References

- `aether-shared/data-models/aether-be/nodes/notebook.md` - Notebook model documentation
- `aether-shared/data-models/aether-be/nodes/space.md` - Space architecture documentation
- `aether-shared/data-models/cross-service/` - Cross-service integration patterns
- `SPACE_BASED_IMPLEMENTATION_PLAN.md` - Original space-based multi-tenancy plan
