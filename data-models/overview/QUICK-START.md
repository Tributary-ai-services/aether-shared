# Data Models Quick Start Guide

**For developers who need to quickly understand TAS data models**

---

## 🚀 Quick Navigation

### I Need To...

**Understand how IDs flow between services**
→ Read [ID Mapping Chain](../cross-service/mappings/id-mapping-chain.md)

**Check if my data is consistent**
→ Run `./validation/scripts/validate-cross-references.sh`

**Document a new model**
→ Use [TEMPLATE.md](./TEMPLATE.md)

**See critical issues**
→ Read [INCONSISTENCIES-FOUND.md](./INCONSISTENCIES-FOUND.md)

**Browse all models**
→ Start at [Main README](../README.md)

**Check project status**
→ Read [PROGRESS-SUMMARY.md](./PROGRESS-SUMMARY.md)

---

## 📍 Where Are My Models?

```
aether-shared/data-models/
├── keycloak/              ← Authentication & identity
├── aether-be/             ← Neo4j graph database models
├── aether/                ← React frontend TypeScript types
├── audimodal/             ← Document processing models
├── tas-agent-builder/     ← Agent configuration models
├── deeplake-api/          ← Vector database models
├── tas-llm-router/        ← LLM request/response formats
├── tas-mcp/               ← MCP protocol definitions
└── cross-service/         ← How services connect
```

---

## 🔑 Essential ID Patterns

### User Identity
```
Keycloak User ID:    570d9941-f4be-46d6-9662-15a2ed0a3cb1 (UUID)
Aether User ID:      570d9941-f4be-46d6-9662-15a2ed0a3cb1 (same)
Tenant ID:           tenant_1767395606 (timestamp-based)
Space ID:            space_1767395606 (derived from tenant)
```

### Pattern Rules
- **Keycloak → Aether**: User IDs are synced 1:1
- **Tenant ID**: Always `tenant_<unix_timestamp>`
- **Space ID**: Always `space_<same_timestamp>` (remove "tenant_" prefix)
- **Never**: Use UUIDs for tenant/space IDs

---

## ⚡ Common Queries

### Neo4j: Get User's Notebooks in a Space
```cypher
MATCH (u:User {id: $userId})-[:OWNS]->(n:Notebook)
WHERE n.tenant_id = $tenantId
  AND n.space_id = $spaceId
  AND n.deleted_at IS NULL
RETURN n
ORDER BY n.created_at DESC
```

### Neo4j: Get Documents with Space Isolation
```cypher
MATCH (n:Notebook {id: $notebookId})-[:CONTAINS]->(d:Document)
WHERE d.tenant_id = $tenantId
  AND d.space_id = $spaceId
  AND d.deleted_at IS NULL
RETURN d
ORDER BY d.created_at DESC
```

### PostgreSQL: Query AudiModal Files
```sql
SELECT * FROM files
WHERE tenant_id = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 20;
```

---

## 🛡️ Data Isolation Checklist

Before writing any query:

- [ ] Filter by `tenant_id`
- [ ] Filter by `space_id` (if space-aware)
- [ ] Exclude soft-deleted (`deleted_at IS NULL`)
- [ ] Validate user has access to the space
- [ ] Never expose cross-tenant data

---

## 🚨 Critical Issues (As of 2026-01-03)

### 🔴 Issue #1: AudiModal Shared Tenant
**Status**: Fixed in code, needs production verification
**Action**: Run validation script to confirm

### 🔴 Issue #2: Agent Builder Space Isolation
**Status**: Unknown if `space_id` column exists
**Action**: Check PostgreSQL schema

### 🔴 Issue #3: LLM Router No Space Tracking
**Status**: Enhancement needed
**Action**: Add `X-Space-ID` header support

→ Full details in [INCONSISTENCIES-FOUND.md](./INCONSISTENCIES-FOUND.md)

---

## ✅ Validation

### Run Automated Checks
```bash
cd /home/jscharber/eng/TAS/aether-shared/data-models
./validation/scripts/validate-cross-references.sh
```

### What Gets Checked
1. ✓ Unique tenant IDs per user
2. ✓ Correct `tenant_<timestamp>` format
3. ✓ Proper space ID derivation
4. ✓ Notebooks have tenant/space isolation
5. ✓ Documents have tenant/space isolation
6. ✓ No shared tenant IDs across users
7. ✓ Space nodes exist for users
8. ⚠ Keycloak user sync (optional)
9. ⚠ Agent Builder schema (optional)

---

## 📝 Adding a New Model

### 1. Copy the Template
```bash
cp overview/TEMPLATE.md {service}/{category}/{model-name}.md
```

### 2. Fill In Required Sections
- Metadata (service, model, database)
- Schema definition with all fields
- Relationships to other models
- Example queries (create, read, update, delete)
- Cross-service references

### 3. Required Sections
1. Overview
2. Schema Definition
3. Relationships
4. Validation Rules
5. Examples
6. Cross-Service References
7. Tenant & Space Isolation (if applicable)

### 4. Update Main README
Add link to your new model in `data-models/README.md`

---

## 🔄 Data Flow Examples

### User Onboarding
```
1. Keycloak creates user (UUID)
   ↓
2. Aether-BE syncs user on first login
   ↓
3. Generate tenant_<timestamp>
   ↓
4. Derive space_<timestamp>
   ↓
5. Create Space node in Neo4j
   ↓
6. Create "Getting Started" notebook
   ↓
7. Initialize DeepLake dataset (if needed)
```

### Document Upload
```
1. Frontend → Aether-BE (with X-Space-ID header)
   ↓
2. Validate space ownership
   ↓
3. Create Document node with tenant_id + space_id
   ↓
4. Upload file to MinIO (tenant_<id>/files/...)
   ↓
5. Send to AudiModal for processing
   ↓
6. Extract text and metadata
   ↓
7. Generate embeddings → DeepLake
   ↓
8. Update Document.status = "processed"
```

---

## 🎯 Best Practices

### When Writing Code

**Always**:
- Include `tenant_id` and `space_id` in all data models
- Filter queries by both tenant and space
- Validate space ownership before operations
- Use soft deletes (`deleted_at`)
- Include audit fields (`created_at`, `updated_at`)

**Never**:
- Expose data across tenant boundaries
- Use UUIDs for tenant/space IDs
- Skip space validation
- Hard delete data
- Trust user input for tenant/space

### When Documenting

**Include**:
- Complete field definitions with types
- Relationships to other models
- Example queries with proper isolation
- Cross-service ID mappings
- Security and compliance notes

**Keep Updated**:
- Version history
- Last reviewed date
- Migration notes when schema changes

---

## 📚 Resources

### Documentation
- [Main README](../README.md) - Complete navigation
- [Template](./TEMPLATE.md) - Model documentation template
- [Progress Summary](./PROGRESS-SUMMARY.md) - Project status

### Technical Details
- [ID Mapping Chain](../cross-service/mappings/id-mapping-chain.md) - Data flows
- [Validation Scripts](../validation/scripts/) - Automated testing
- [Inconsistencies Report](./INCONSISTENCIES-FOUND.md) - Known issues

### Platform Docs
- [Space Tenant Model](../../SPACE_TENANT_MODEL_SUMMARY.md) - Architecture
- [Aether-Shared](../../aether-shared/README.md) - Infrastructure
- [Services & Ports](../../aether-shared/services-and-ports.md) - Port mappings

---

## 🆘 Common Issues

### "Query returns data from wrong tenant"
→ Add `WHERE tenant_id = $tenantId` to your query

### "User can't see their notebooks"
→ Check `space_id` matches user's `personal_space_id`

### "Validation script fails"
→ Check Neo4j connection and credentials in env vars

### "Can't find model documentation"
→ Check service directory in `data-models/{service}/`

### "Don't know which ID to use"
→ See [ID Mapping Chain](../cross-service/mappings/id-mapping-chain.md)

---

## 💡 Tips

1. **Start with the main README** - It has complete navigation
2. **Use the template** - Don't start from scratch
3. **Run validation often** - Catch issues early
4. **Document as you code** - Don't postpone documentation
5. **Link between docs** - Use relative paths for cross-references

---

**Last Updated**: 2026-01-03
**Next Review**: 2026-01-10

For questions, see individual service CLAUDE.md files or the root CLAUDE.md.
