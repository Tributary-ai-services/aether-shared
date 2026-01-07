# TAS Data Model Migration Guide

**Last Updated**: 2026-01-06
**Target Audience**: Platform developers and DevOps engineers

---

## Overview

This guide assists developers in migrating between different versions of TAS data models, handling schema changes, and maintaining data consistency across services during platform upgrades.

## General Migration Principles

### 1. Always Back Up First
```bash
# Neo4j backup
cypher-shell "CALL apoc.export.graphml.all('/backup/neo4j-backup.graphml', {})"

# PostgreSQL backup
pg_dump -h localhost -U tasuser -d tas_shared > /backup/postgres-backup.sql

# MinIO backup (document storage)
mc mirror myminio/aether-storage /backup/minio-storage
```

### 2. Follow the Migration Order
1. **Shared Infrastructure** - Upgrade Redis, PostgreSQL, Kafka, Keycloak first
2. **Data Layer** - Migrate Neo4j graph database, then PostgreSQL entities
3. **Storage Layer** - Update MinIO bucket structures if needed
4. **Application Services** - Backend services (aether-be, audimodal, deeplake-api)
5. **Frontend** - Update aether frontend last (backward compatible APIs)

### 3. Use Feature Flags
```go
// Example: Gradual rollout of new schema fields
if featureFlags.IsEnabled("new_space_isolation") {
    user.PersonalSpaceID = generateSpaceID(user.PersonalTenantID)
}
```

## Common Migration Scenarios

### Scenario 1: Adding tenant_id and space_id to Existing Nodes

**Context**: Migrating from non-isolated to space-based multi-tenancy.

**Steps**:

1. **Add fields without constraints** (allows gradual migration):
```cypher
// Add fields to existing User nodes
MATCH (u:User)
WHERE u.personal_tenant_id IS NULL
SET u.personal_tenant_id = 'tenant_' + toString(timestamp()),
    u.personal_space_id = 'space_' + toString(timestamp());
```

2. **Verify uniqueness**:
```cypher
MATCH (u:User)
WITH u.personal_tenant_id AS tenant_id, count(u) AS user_count
WHERE user_count > 1
RETURN tenant_id, user_count;
```

3. **Add constraints after migration**:
```cypher
CREATE CONSTRAINT user_tenant_unique IF NOT EXISTS
FOR (u:User) REQUIRE u.personal_tenant_id IS UNIQUE;
```

### Scenario 2: Migrating Document Processing Status

**Context**: Adding new processing states or changing status values.

**Steps**:

1. **Map old status to new status**:
```cypher
// Update documents from old "processed" to new "completed" status
MATCH (d:Document)
WHERE d.status = 'processed'
SET d.status = 'completed',
    d.migrated_at = datetime(),
    d.migration_version = '2.0';
```

2. **Add processing metadata**:
```cypher
MATCH (d:Document)
WHERE d.processing_time IS NULL
SET d.processing_time = 0,
    d.confidence_score = 0.0;
```

### Scenario 3: Embedding Model Migration

**Context**: Switching from text-embedding-ada-002 (1536 dims) to text-embedding-3-large (3072 dims).

**Steps**:

1. **Mark documents for re-embedding**:
```python
# In DeepLake API
for dataset in get_all_datasets(tenant_id):
    dataset.add_field("requires_reembedding", dtype="bool", default=False)
    dataset.filter(lambda x: x["model"] == "text-embedding-ada-002").update({
        "requires_reembedding": True
    })
```

2. **Gradual re-embedding**:
```python
# Background job to re-embed documents
async def reembed_documents():
    while True:
        docs_needing_reembedding = dataset.filter(
            lambda x: x["requires_reembedding"] == True
        ).fetch(limit=100)

        for doc in docs_needing_reembedding:
            new_embedding = await generate_embedding(
                doc["content"],
                model="text-embedding-3-large"
            )
            dataset.update(doc["id"], {
                "embedding": new_embedding,
                "model": "text-embedding-3-large",
                "requires_reembedding": False
            })

        if len(docs_needing_reembedding) == 0:
            break
```

### Scenario 4: Relationship Schema Changes

**Context**: Adding properties to existing relationships.

**Steps**:

1. **Add properties to relationships**:
```cypher
// Add timestamps to OWNED_BY relationships
MATCH (n:Notebook)-[r:OWNED_BY]->(u:User)
WHERE r.created_at IS NULL
SET r.created_at = n.created_at,
    r.ownership_type = 'personal';
```

2. **Migrate relationship types**:
```cypher
// Replace old relationship with new one
MATCH (d:Document)-[old:BELONGS_TO_NOTEBOOK]->(n:Notebook)
CREATE (d)-[new:BELONGS_TO {
    created_at: old.created_at,
    order_index: old.order_index
}]->(n)
DELETE old;
```

## Platform-Specific Migrations

### Neo4j Graph Database Migrations

**Tool**: Use APOC procedures for bulk operations.

```cypher
// Install APOC if not available
CALL dbms.listConfig() YIELD name, value
WHERE name = 'dbms.security.procedures.unrestricted'
RETURN value;
```

**Safe Migration Pattern**:
```cypher
// 1. Create new nodes/relationships without deleting old ones
MATCH (old:OldNodeType)
CREATE (new:NewNodeType)
SET new = properties(old),
    new.migrated_from = old.id,
    new.migration_timestamp = datetime();

// 2. Verify migration
MATCH (old:OldNodeType), (new:NewNodeType)
WHERE new.migrated_from = old.id
RETURN count(*) AS migrated_count;

// 3. Delete old nodes only after verification
MATCH (old:OldNodeType)
WHERE EXISTS((new:NewNodeType {migrated_from: old.id}))
DETACH DELETE old;
```

### PostgreSQL Entity Migrations

**Tool**: Use Alembic or manual SQL migrations.

```sql
-- Add column with default value (non-breaking)
ALTER TABLE audimodal_files
ADD COLUMN IF NOT EXISTS space_id VARCHAR(255);

-- Populate new column
UPDATE audimodal_files af
SET space_id = (
    SELECT u.personal_space_id
    FROM users u
    WHERE u.personal_tenant_id = af.tenant_id
    LIMIT 1
);

-- Add constraint after population
ALTER TABLE audimodal_files
ADD CONSTRAINT fk_space_id FOREIGN KEY (space_id)
REFERENCES spaces(id) ON DELETE CASCADE;
```

### DeepLake Vector Database Migrations

**Tool**: DeepLake 4.0 dataset versioning.

```python
import deeplake

# Create new dataset version
ds = deeplake.open("hub://tenant_123/documents")
ds_v2 = ds.copy("hub://tenant_123/documents_v2")

# Add new field to schema
ds_v2.add_field("space_id", dtype="str")

# Migrate data
for i, vector in enumerate(ds):
    ds_v2[i] = {
        **vector,
        "space_id": derive_space_id(vector["tenant_id"])
    }

# Swap datasets after verification
ds.rename("hub://tenant_123/documents_old")
ds_v2.rename("hub://tenant_123/documents")
```

## Rollback Procedures

### Quick Rollback (< 1 hour window)

```bash
# 1. Stop services
kubectl scale deployment/aether-backend --replicas=0 -n aether-be

# 2. Restore from backup
psql -U tasuser -d tas_shared < /backup/postgres-backup.sql

# 3. Revert Docker images
kubectl set image deployment/aether-backend \
    aether-backend=aether-backend:previous-version -n aether-be

# 4. Restart services
kubectl scale deployment/aether-backend --replicas=3 -n aether-be
```

### Extended Rollback (> 1 hour window)

Use point-in-time recovery for PostgreSQL and Neo4j transaction logs for graph database.

## Validation After Migration

Run the platform validation script:

```bash
cd /home/jscharber/eng/TAS/aether-shared/data-models/validation/scripts
./validate-cross-references.sh
```

Expected output:
```
✓ All users have unique tenant_id values
✓ All tenant IDs follow tenant_<timestamp> format
✓ All space IDs correctly derived from tenant IDs
✓ All notebooks have tenant_id and space_id
✓ All documents have tenant_id and space_id
```

## Migration Checklist

- [ ] **Pre-Migration**
  - [ ] Full backup of all databases
  - [ ] Document current schema version
  - [ ] Test migration on staging environment
  - [ ] Prepare rollback scripts
  - [ ] Schedule maintenance window
  - [ ] Notify users of downtime

- [ ] **During Migration**
  - [ ] Enable maintenance mode
  - [ ] Stop application services (keep infrastructure running)
  - [ ] Run database migrations
  - [ ] Verify data consistency
  - [ ] Update application code
  - [ ] Run integration tests

- [ ] **Post-Migration**
  - [ ] Run validation scripts
  - [ ] Verify all services healthy
  - [ ] Check logs for errors
  - [ ] Test critical user workflows
  - [ ] Monitor performance metrics
  - [ ] Disable maintenance mode
  - [ ] Notify users of completion

## Known Issues & Solutions

### Issue 1: Duplicate tenant_id Values

**Symptom**: Multiple users sharing the same personal_tenant_id.

**Solution**:
```cypher
// Find duplicates
MATCH (u:User)
WITH u.personal_tenant_id AS tenant_id, collect(u.id) AS user_ids
WHERE size(user_ids) > 1
RETURN tenant_id, user_ids;

// Fix by regenerating for all but first user
MATCH (u:User)
WHERE u.personal_tenant_id = 'tenant_12345'
WITH u ORDER BY u.created_at
WITH collect(u) AS users
UNWIND range(1, size(users)-1) AS idx
WITH users[idx] AS user_to_fix
SET user_to_fix.personal_tenant_id = 'tenant_' + toString(timestamp()),
    user_to_fix.personal_space_id = 'space_' + toString(timestamp());
```

### Issue 2: Missing Space Nodes

**Symptom**: Users have personal_space_id but no corresponding Space node.

**Solution**:
```cypher
// Create missing Space nodes
MATCH (u:User)
WHERE u.personal_space_id IS NOT NULL
AND NOT EXISTS((:Space {id: u.personal_space_id}))
CREATE (:Space {
    id: u.personal_space_id,
    name: u.username + "'s Personal Space",
    type: 'personal',
    tenant_id: u.personal_tenant_id,
    created_at: u.created_at,
    is_default: true
});
```

### Issue 3: Embedding Dimension Mismatch

**Symptom**: Documents embedded with different model dimensions in same dataset.

**Solution**:
```python
# Split dataset by embedding model
ds = deeplake.open("hub://tenant_123/documents")

# Create separate datasets for each model
ada_002_docs = ds.filter(lambda x: x["model"] == "text-embedding-ada-002")
embedding_3_docs = ds.filter(lambda x: x["model"] == "text-embedding-3-large")

ada_002_docs.save("hub://tenant_123/documents_ada002")
embedding_3_docs.save("hub://tenant_123/documents_v3")
```

## Emergency Contacts

- **Platform Team**: #tas-platform (Slack)
- **On-Call Engineer**: See PagerDuty rotation
- **Database Admin**: See runbook for escalation

## Related Documentation

- [Platform ERD](../cross-service/diagrams/platform-erd.md)
- [User Onboarding Flow](../cross-service/flows/user-onboarding.md)
- [Document Upload Flow](../cross-service/flows/document-upload.md)
- [Validation Script](../validation/scripts/validate-cross-references.sh)
- [PHASE-STATUS.md](../PHASE-STATUS.md)

---

**Version**: 1.0.0
**Last Reviewed**: 2026-01-06
**Next Review**: 2026-02-06
