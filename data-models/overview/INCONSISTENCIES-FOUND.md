# Data Model Inconsistencies - Critical Findings

**Status**: 🔴 Action Required
**Date**: 2026-01-03
**Severity**: High - Impacts data isolation and multi-tenancy

---

## Executive Summary

During comprehensive data model mapping across the TAS platform, **6 critical inconsistencies** were identified that affect data isolation, cross-service integration, and audit capabilities. These issues require immediate attention to ensure proper space-based multi-tenancy.

---

## Critical Issues (🔴 Immediate Action Required)

### 1. AudiModal Shared Tenant ID

**Severity**: 🔴 CRITICAL - Data Isolation Risk
**Status**: ✅ Fixed in code, ⚠️ Needs production verification

#### Problem
All users were sharing the same `audimodal_tenant_id = 9855e094-36a6-4d3a-a4f5-d77da4614439`, breaking tenant isolation at the file storage layer.

#### Impact
- Files from different users stored in the same AudiModal tenant
- Potential cross-user data leakage
- Violates space-based isolation model

#### Root Cause
`internal/services/audimodal.go:CreateTenant()` was returning a hardcoded shared UUID instead of generating unique tenant IDs per user.

#### Fix Applied
```go
// BEFORE (BROKEN)
return &CreateTenantResponse{
    TenantID: "9855e094-36a6-4d3a-a4f5-d77da4614439", // Shared!
    APIKey:   s.apiKey,
}

// AFTER (FIXED)
tenantID := fmt.Sprintf("tenant_%d", time.Now().Unix())
return &CreateTenantResponse{
    TenantID: tenantID,  // Unique per user
    APIKey:   s.apiKey,
}
```

#### Verification Required
- [ ] Run validation script to confirm all users have unique `personal_tenant_id`
- [ ] Verify no documents/files share the old UUID tenant ID
- [ ] Confirm AudiModal API calls use per-user tenant IDs

#### Location
- File: `/home/jscharber/eng/TAS/aether-be/internal/services/audimodal.go:66-88`
- Deployment: Check if production is running latest version

---

### 2. TAS Agent Builder - Missing Space Context

**Severity**: 🔴 CRITICAL - Space Isolation Unknown
**Status**: ⚠️ Needs Investigation

#### Problem
Unclear if TAS Agent Builder properly stores and validates `space_id` for agent isolation.

#### Impact
- Agents may not be properly isolated per space
- Users could potentially access agents from other spaces
- Violates space-based multi-tenancy model

#### Unknown Details
1. Does PostgreSQL `agents` table have `space_id` column?
2. Are agent queries filtered by `space_id`?
3. Is `X-Space-ID` header validated on agent creation/execution?

#### Investigation Required
```bash
# Check PostgreSQL schema
PGPASSWORD=taspassword psql -h localhost -U tasuser -d tas_shared \
  -c "\d agents"

# Look for space_id column
PGPASSWORD=taspassword psql -h localhost -U tasuser -d tas_shared \
  -c "SELECT column_name FROM information_schema.columns
      WHERE table_name='agents' AND column_name='space_id';"
```

#### Action Items
- [ ] Document actual PostgreSQL schema for `agents` table
- [ ] Verify `space_id` is stored on agent creation
- [ ] Check if agent queries filter by `space_id`
- [ ] Add schema validation to automated tests

#### Location
- Service: `tas-agent-builder` (PostgreSQL database)
- Files: Unknown schema definition location

---

### 3. TAS LLM Router - No Space Context Tracking

**Severity**: 🟡 MEDIUM - Audit/Analytics Gap
**Status**: ⚠️ Enhancement Needed

#### Problem
TAS LLM Router only tracks `user_id` from JWT, not `space_id`, limiting usage analytics and audit capabilities.

#### Impact
- Cannot track LLM usage per space
- Cannot implement space-level rate limiting
- Audit logs lack space context
- Analytics cannot segment by space

#### Current State
```go
// LLM Router only extracts user_id from JWT
userID := claims["sub"].(string)  // Keycloak UUID
// No space_id extraction or logging
```

#### Recommended Fix
1. Accept `X-Space-ID` header in LLM Router requests
2. Validate space_id against user's authorized spaces
3. Include space_id in audit logs and metrics
4. Add space_id to Prometheus metrics for usage tracking

#### Example Implementation
```go
// Extract from header
spaceID := c.GetHeader("X-Space-ID")

// Log with space context
logger.Info("LLM request",
    zap.String("user_id", userID),
    zap.String("space_id", spaceID),  // NEW
    zap.String("model", model),
)

// Metrics with space label
llmRequestsTotal.WithLabelValues(userID, spaceID, model).Inc()
```

#### Action Items
- [ ] Update LLM Router to accept `X-Space-ID` header
- [ ] Add space_id to request logs
- [ ] Add space_id to Prometheus metrics
- [ ] Update Agent Builder to forward `X-Space-ID` header

#### Location
- Service: `tas-llm-router`
- Files: Request handler and logging middleware

---

## Medium Priority Issues (🟡 Should Address Soon)

### 4. DeepLake Dataset Namespacing Unclear

**Severity**: 🟡 MEDIUM - Potential Data Leakage
**Status**: ⚠️ Needs Documentation

#### Problem
It's unclear how DeepLake datasets are namespaced per user/space for vector search isolation.

#### Impact
- Vector search results might cross space boundaries
- Embeddings from one user's documents might appear in another's search

#### Questions to Answer
1. Are datasets created per `tenant_id` or per `space_id`?
2. How are vector queries scoped to prevent cross-user leakage?
3. What's the dataset naming convention?

#### Investigation Required
```python
# Check DeepLake dataset creation code
# Look for tenant_id or space_id in dataset names
```

#### Action Items
- [ ] Document DeepLake dataset naming convention
- [ ] Verify datasets are isolated per tenant/space
- [ ] Add dataset isolation tests
- [ ] Document in data models directory

#### Location
- Service: `deeplake-api`
- Files: Dataset creation and query logic

---

### 5. Frontend Space Selection Persistence

**Severity**: 🟢 LOW - UX Issue
**Status**: ⚠️ Minor Improvement

#### Problem
Space context stored in both localStorage and Redux, creating potential staleness issues.

#### Impact
- If localStorage cleared but Redux persists, stale space_id could be used
- User experience inconsistency

#### Recommended Fix
- Use Redux as single source of truth
- Sync to localStorage only for persistence between sessions
- Clear localStorage on logout

#### Action Items
- [ ] Refactor to use Redux as primary source
- [ ] Add localStorage sync on space change
- [ ] Clear space context on logout

#### Location
- Service: `aether` frontend
- Files: `src/services/aetherApi.js`, Redux store

---

### 6. Inconsistent ID Format Documentation

**Severity**: 🟢 LOW - Documentation Issue
**Status**: ⚠️ Needs Update

#### Problem
Some documentation shows `space_<user_id>` when actual format is `space_<timestamp>`.

#### Impact
- Developer confusion
- Incorrect assumptions in new code

#### Action Items
- [ ] Update all README files to show `space_<timestamp>`
- [ ] Update SPACE_TENANT_MODEL_SUMMARY.md
- [ ] Add ID format examples to data model docs

#### Location
- Multiple README files across repositories

---

## Validation Checklist

Run the automated validation script to verify data consistency:

```bash
# Copy script to Neo4j pod (Kubernetes)
kubectl cp /home/jscharber/eng/TAS/aether-shared/data-models/validation/scripts/validate-cross-references.sh \
  aether-be/neo4j-0:/tmp/

# Execute validation
kubectl exec -n aether-be neo4j-0 -- bash /tmp/validate-cross-references.sh
```

### Expected Results
- ✅ All users have unique `tenant_<timestamp>` IDs
- ✅ All `space_id` correctly derived from `tenant_id`
- ✅ All notebooks have `tenant_id` and `space_id`
- ✅ All documents have `tenant_id` and `space_id`
- ✅ No shared tenant IDs across users
- ✅ Space nodes exist for all user personal spaces

---

## Remediation Timeline

### Week 1 (Immediate)
- [x] Fix AudiModal shared tenant ID (DONE - verify production)
- [ ] Validate production data with automated script
- [ ] Investigate Agent Builder schema

### Week 2
- [ ] Add space_id to LLM Router logs
- [ ] Document DeepLake dataset namespacing
- [ ] Fix Agent Builder if space_id missing

### Week 3
- [ ] Add space_id to LLM Router metrics
- [ ] Frontend space context refactoring
- [ ] Update all documentation

### Week 4
- [ ] Full integration tests for space isolation
- [ ] Performance testing with multi-tenant load
- [ ] Security audit of space boundaries

---

## Monitoring & Prevention

### Automated Checks
Add to CI/CD pipeline:
```bash
# Run on every deployment
./aether-shared/data-models/validation/scripts/validate-cross-references.sh
```

### Metrics to Track
- Unique tenant_id count vs user count (should be 1:1)
- Documents without tenant_id or space_id (should be 0)
- Notebooks without tenant_id or space_id (should be 0)

### Alerts
- Alert if duplicate tenant_id detected
- Alert if space_id derivation mismatch found
- Alert if cross-space data access attempted

---

## References

- [ID Mapping Chain](../cross-service/mappings/id-mapping-chain.md) - Full data flow documentation
- [Validation Script](../validation/scripts/validate-cross-references.sh) - Automated consistency checks
- [Space Tenant Model](../../SPACE_TENANT_MODEL_SUMMARY.md) - Original design document

---

**Last Updated**: 2026-01-03
**Next Review**: 2026-01-10
**Owner**: Platform Team
