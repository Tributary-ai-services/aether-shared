# Cross-Service ID Mapping Chain

**Purpose**: Document how identifiers flow and transform between services to identify inconsistencies and gaps.

---

## User Identity Chain

### Flow: Keycloak → Aether-BE → AudiModal → DeepLake

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. User Registration in Keycloak                                    │
├─────────────────────────────────────────────────────────────────────┤
│ Keycloak generates:                                                  │
│   - User UUID: 570d9941-f4be-46d6-9662-15a2ed0a3cb1                 │
│   - Email: john@scharber.com                                         │
│   - Realm: "aether"                                                  │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 2. First Login → Aether-BE /users/me                                │
├─────────────────────────────────────────────────────────────────────┤
│ Aether-BE receives JWT with:                                        │
│   - sub: 570d9941-f4be-46d6-9662-15a2ed0a3cb1 (Keycloak UUID)      │
│                                                                      │
│ Aether-BE creates:                                                   │
│   - User.id: 570d9941-f4be-46d6-9662-15a2ed0a3cb1 (same as KC)     │
│   - User.tenant_id: tenant_1767395606 (NEW - timestamp-based)       │
│   - User.personal_space_id: space_1767395606 (derived)              │
│   - User.email: john@scharber.com (synced from Keycloak)            │
│                                                                      │
│ Neo4j Node Created: (:User {id, tenant_id, personal_space_id})      │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 3. Aether-BE → AudiModal Tenant Creation                            │
├─────────────────────────────────────────────────────────────────────┤
│ Aether-BE calls CreateTenant():                                      │
│   - Generates: tenant_1767395606 (unique per user)                   │
│   - Returns: audimodal_tenant_id = 9855e094-... (SHARED UUID)       │
│                                                                      │
│ User model updated:                                                  │
│   - User.personal_tenant_id: tenant_1767395606                       │
│   - User.personal_api_key: <api_key_from_audimodal>                 │
│   - INTERNAL MAPPING: audimodal_tenant_id stored separately          │
│                                                                      │
│ ⚠️ INCONSISTENCY RISK: All users share same audimodal_tenant_id     │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 4. Document Upload Flow                                             │
├─────────────────────────────────────────────────────────────────────┤
│ Client uploads to Aether-BE:                                        │
│   Headers: X-Space-ID: space_1767395606                             │
│                                                                      │
│ Aether-BE creates Document:                                         │
│   - Document.id: <uuid>                                              │
│   - Document.tenant_id: tenant_1767395606                            │
│   - Document.space_id: space_1767395606                              │
│   - Document.storage_path: tenant_1767395606/files/<filename>        │
│                                                                      │
│ Aether-BE → AudiModal API:                                          │
│   POST /api/v1/tenants/9855e094-.../files                           │
│   (Uses shared audimodal_tenant_id for all users)                   │
│                                                                      │
│ AudiModal creates:                                                   │
│   - File.id: <uuid>                                                  │
│   - File.tenant_id: 9855e094-... (SHARED)                           │
│   - File.storage_key: <minio_path>                                  │
│                                                                      │
│ ⚠️ DATA ISOLATION ISSUE: Files from different users in same tenant  │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 5. Vector Embedding Flow → DeepLake                                 │
├─────────────────────────────────────────────────────────────────────┤
│ After processing, Aether-BE → DeepLake:                             │
│   - Dataset ID: derived from space_id or tenant_id?                 │
│   - Vector metadata: includes document_id, user_id                  │
│                                                                      │
│ ⚠️ MAPPING UNCLEAR: How does DeepLake namespace vectors per user?   │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Notebook & Document Hierarchy Chain

### Flow: Aether Frontend → Aether-BE → Neo4j

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. Create Notebook (Frontend)                                       │
├─────────────────────────────────────────────────────────────────────┤
│ Frontend State:                                                      │
│   - currentSpaceId: space_1767395606 (from localStorage/Redux)      │
│   - notebookName: "My Research"                                      │
│                                                                      │
│ API Call:                                                            │
│   POST /api/v1/notebooks                                             │
│   Headers: X-Space-ID: space_1767395606                              │
│   Body: {name: "My Research", parent_id: null}                       │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 2. Aether-BE Notebook Creation                                      │
├─────────────────────────────────────────────────────────────────────┤
│ Space Context Middleware resolves:                                  │
│   - space_id: space_1767395606                                       │
│   - tenant_id: tenant_1767395606 (derived from space_id)             │
│   - user_id: 570d9941-f4be-46d6-9662-15a2ed0a3cb1 (from JWT)        │
│                                                                      │
│ NotebookService.Create():                                            │
│   - Generates notebook_id: <uuid>                                    │
│   - Sets tenant_id: tenant_1767395606                                │
│   - Sets space_id: space_1767395606                                  │
│   - Sets space_type: "personal"                                      │
│   - Sets owner_id: <user_id>                                         │
│                                                                      │
│ Neo4j Query:                                                         │
│   CREATE (n:Notebook {                                               │
│     id: $id,                                                         │
│     tenant_id: $tenant_id,    ← CRITICAL for isolation               │
│     space_id: $space_id,      ← CRITICAL for isolation               │
│     space_type: $space_type,                                         │
│     name: $name,                                                     │
│     owner_id: $owner_id                                              │
│   })                                                                 │
│   CREATE (u:User {id: $owner_id})-[:OWNS]->(n)                       │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 3. Query Notebooks (List)                                           │
├─────────────────────────────────────────────────────────────────────┤
│ API Call:                                                            │
│   GET /api/v1/notebooks                                              │
│   Headers: X-Space-ID: space_1767395606                              │
│                                                                      │
│ Neo4j Query:                                                         │
│   MATCH (u:User {id: $userId})-[:OWNS]->(n:Notebook)                │
│   WHERE n.tenant_id = $tenantId                                      │
│     AND n.space_id = $spaceId         ← Double filtering             │
│     AND n.deleted_at IS NULL                                         │
│   RETURN n                                                           │
│                                                                      │
│ ✅ CONSISTENT: Both tenant_id and space_id validated                │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Agent Execution Chain

### Flow: Aether-BE → TAS Agent Builder → TAS LLM Router

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. Create Agent Request                                             │
├─────────────────────────────────────────────────────────────────────┤
│ Aether-BE receives:                                                  │
│   POST /api/v1/agents                                                │
│   Headers:                                                           │
│     Authorization: Bearer <jwt>                                      │
│     X-Space-ID: space_1767395606                                     │
│   Body: {name: "Research Assistant", capabilities: [...]}           │
│                                                                      │
│ Aether-BE forwards to TAS Agent Builder:                            │
│   POST http://tas-agent-builder:8087/api/v1/agents                  │
│   Headers:                                                           │
│     Authorization: Bearer <jwt> (forwarded)                          │
│     X-Space-ID: space_1767395606 (forwarded)                         │
│                                                                      │
│ ⚠️ MAPPING QUESTION: Does Agent Builder use space_id?               │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 2. Agent Builder Creates Agent                                      │
├─────────────────────────────────────────────────────────────────────┤
│ TAS Agent Builder (PostgreSQL):                                     │
│   INSERT INTO agents (                                               │
│     id,                                                              │
│     space_id,          ← Does it extract from header?                │
│     user_id,           ← Extracted from JWT                          │
│     name,                                                            │
│     config              ← JSONB with capabilities                    │
│   )                                                                  │
│                                                                      │
│ Returns:                                                             │
│   {agent_id: <uuid>, space_id: space_1767395606}                     │
│                                                                      │
│ ⚠️ NEEDS VERIFICATION: Does space_id get stored in PostgreSQL?      │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ 3. Execute Agent → LLM Router                                       │
├─────────────────────────────────────────────────────────────────────┤
│ Agent Builder → TAS LLM Router:                                     │
│   POST http://tas-llm-router:8085/api/v1/chat/completions           │
│   Headers:                                                           │
│     Authorization: Bearer <jwt>                                      │
│     X-Request-ID: <uuid>                                             │
│     X-User-ID: 570d9941-... (from JWT)                               │
│                                                                      │
│ LLM Router (stateless):                                             │
│   - Validates JWT                                                    │
│   - Extracts user_id from JWT                                        │
│   - Routes to appropriate LLM backend                                │
│   - Logs request with user_id for audit                              │
│                                                                      │
│ ❌ NO SPACE CONTEXT: LLM Router doesn't use space_id                │
│    Only user_id from JWT for authorization/logging                   │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Identified Inconsistencies

### 🔴 Critical Issues

#### 1. **AudiModal Shared Tenant ID**
- **Problem**: All users share `audimodal_tenant_id = 9855e094-36a6-4d3a-a4f5-d77da4614439`
- **Impact**: Data isolation compromised at AudiModal level
- **Location**: `aether-be/internal/services/audimodal.go:66-88`
- **Status**: FIXED in latest code (generates unique `tenant_<timestamp>`)
- **Verification Needed**: Confirm production deployment uses fixed version

#### 2. **Space ID Not Propagated to TAS Agent Builder**
- **Problem**: Unclear if `space_id` is stored in Agent Builder PostgreSQL
- **Impact**: Agents may not be properly isolated per space
- **Location**: `tas-agent-builder` database schema unknown
- **Action Required**: Verify PostgreSQL schema includes `space_id` column

#### 3. **LLM Router Missing Space Context**
- **Problem**: LLM Router only tracks `user_id`, not `space_id`
- **Impact**: Cannot audit/limit LLM usage per space
- **Location**: `tas-llm-router` request handling
- **Severity**: Medium (impacts analytics, not security)
- **Action Required**: Add `X-Space-ID` header to LLM Router requests

### 🟡 Medium Issues

#### 4. **DeepLake Dataset Namespacing Unclear**
- **Problem**: How are vectors partitioned per user/space?
- **Impact**: May cause cross-user data leakage in search results
- **Location**: `deeplake-api` dataset creation logic
- **Action Required**: Document dataset naming convention

#### 5. **Frontend Space Selection Persistence**
- **Problem**: Space context stored in localStorage and Redux
- **Impact**: Stale space_id if localStorage cleared but Redux persists
- **Location**: `aether/src/services/aetherApi.js`
- **Severity**: Low (UX issue, not security)
- **Action Required**: Ensure single source of truth for space context

### 🟢 Minor Issues

#### 6. **Inconsistent ID Format Documentation**
- **Problem**: Some docs show `space_<user_id>`, actual is `space_<timestamp>`
- **Impact**: Developer confusion
- **Location**: Multiple README files
- **Action Required**: Update all docs to reflect `space_<timestamp>` pattern

---

## Consistency Verification Checklist

### User Identity
- [x] Keycloak UUID → Aether User ID (1:1 mapping)
- [x] Aether User ID → tenant_id generation (timestamp-based)
- [x] tenant_id → space_id derivation (remove "tenant_" prefix)
- [ ] **TODO**: Verify all users have unique tenant_id in production
- [ ] **TODO**: Confirm AudiModal tenant isolation fix is deployed

### Data Isolation
- [x] Neo4j queries filter by tenant_id AND space_id
- [x] Document service validates space ownership
- [x] Notebook service validates space ownership
- [ ] **TODO**: Verify Agent Builder filters by space_id
- [ ] **TODO**: Verify DeepLake datasets are namespaced per tenant/space

### Cross-Service Headers
- [x] Aether-BE requires X-Space-ID header
- [x] Frontend sends X-Space-ID on all API calls
- [ ] **TODO**: Agent Builder should validate X-Space-ID
- [ ] **TODO**: LLM Router should accept and log X-Space-ID
- [ ] **TODO**: DeepLake API should namespace by space_id

---

## Recommended ID Format Standards

### Keycloak
```
User UUID:   570d9941-f4be-46d6-9662-15a2ed0a3cb1
Realm:       aether
Client ID:   aether-frontend, aether-backend
```

### Aether-BE (Neo4j)
```
User ID:           570d9941-f4be-46d6-9662-15a2ed0a3cb1 (synced from Keycloak)
Tenant ID:         tenant_1767395606 (generated on first login)
Space ID:          space_1767395606 (derived from tenant_id)
Notebook ID:       <uuid>
Document ID:       <uuid>
Organization ID:   <uuid> (for future org spaces)
```

### AudiModal (PostgreSQL)
```
Tenant ID:         tenant_1767395606 (passed from Aether-BE)
File ID:           <uuid>
Processing Job ID: <uuid>
Storage Path:      tenant_1767395606/files/<filename>
```

### TAS Agent Builder (PostgreSQL)
```
Agent ID:          <uuid>
Execution ID:      <uuid>
Space ID:          space_1767395606 (SHOULD BE STORED)
User ID:           570d9941-f4be-46d6-9662-15a2ed0a3cb1 (from JWT)
```

### DeepLake
```
Dataset ID:        tenant_1767395606 or space_1767395606? (NEEDS CLARIFICATION)
Vector ID:         <uuid>
Metadata:          {user_id, document_id, chunk_id, space_id}
```

### TAS LLM Router
```
Request ID:        <uuid>
User ID:           570d9941-f4be-46d6-9662-15a2ed0a3cb1 (from JWT)
Space ID:          (NOT CURRENTLY TRACKED - SHOULD BE)
Model ID:          claude-3-opus, gpt-4, etc.
```

---

## Next Steps

1. **Audit Production Data** - Run queries to verify all users have unique tenant_id values
2. **Schema Verification** - Document actual PostgreSQL schemas for Agent Builder and AudiModal
3. **DeepLake Investigation** - Understand vector dataset namespacing strategy
4. **Header Propagation** - Ensure X-Space-ID flows through all service chains
5. **Documentation Sync** - Update all READMEs with accurate ID patterns

---

**Last Updated**: 2026-01-03
**Audited By**: Data Model Documentation Initiative
**Status**: 🟡 In Progress - Critical issues identified and documented
