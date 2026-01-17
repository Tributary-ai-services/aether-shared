# TAS Data Models - Developer Quick Reference

**Last Updated**: 2026-01-05
**Version**: 1.0

---

## 🚀 Quick Start (5 Minutes)

### 1. Browse Documentation

**Start here**: [`README.md`](./README.md) - Complete navigation hub

**Need something specific?**
- Find a model → Use [`INDEX.md`](./INDEX.md)
- Common patterns → Read [`overview/QUICK-START.md`](./overview/QUICK-START.md)
- Check progress → See [`PHASE-STATUS.md`](./PHASE-STATUS.md)
- Understand issues → Review [`overview/INCONSISTENCIES-FOUND.md`](./overview/INCONSISTENCIES-FOUND.md)

### 2. Find Your Service

```
data-models/
├── keycloak/           ← Authentication & identity
├── aether-be/          ← Neo4j backend (User, Notebook, Document)
├── aether/             ← React frontend TypeScript types
├── audimodal/          ← Document processing
├── tas-agent-builder/  ← Agent configuration
├── deeplake-api/       ← Vector database
├── tas-llm-router/     ← LLM routing
└── tas-mcp/            ← MCP protocol
```

### 3. Run Validation

```bash
cd /home/jscharber/eng/TAS/aether-shared/data-models
./validation/scripts/validate-cross-references.sh
```

---

## 📚 Completed Documentation

### ✅ Foundation (Phase 1 - 100%)

| Document | Purpose | Lines |
|----------|---------|-------|
| [README.md](./README.md) | Main navigation | 3,500+ |
| [INDEX.md](./INDEX.md) | Complete index | 300+ |
| [QUICK-START.md](./overview/QUICK-START.md) | Developer guide | 300+ |
| [TEMPLATE.md](./overview/TEMPLATE.md) | Doc template | 380+ |
| [PROGRESS-SUMMARY.md](./overview/PROGRESS-SUMMARY.md) | Status tracking | 370+ |
| [INCONSISTENCIES-FOUND.md](./overview/INCONSISTENCIES-FOUND.md) | Issues report | 320+ |
| [ID-MAPPING-CHAIN.md](./cross-service/mappings/id-mapping-chain.md) | Data flows | 800+ |
| [PHASE-STATUS.md](./PHASE-STATUS.md) | Phase tracking | 400+ |
| [validate-cross-references.sh](./validation/scripts/validate-cross-references.sh) | Validation | 200+ |

### ✅ Core Models (Phase 2 - 18%)

| Model | File | Lines | Status |
|-------|------|-------|--------|
| User Node | [aether-be/nodes/user.md](./aether-be/nodes/user.md) | 500+ | ✅ Complete |
| Notebook Node | [aether-be/nodes/notebook.md](./aether-be/nodes/notebook.md) | 550+ | ✅ Complete |
| Document Node | aether-be/nodes/document.md | - | ⏳ Pending |
| Space Node | aether-be/nodes/space.md | - | ⏳ Pending |

---

## 🔑 Essential Patterns

### ID Formats

```plaintext
Keycloak User:     570d9941-f4be-46d6-9662-15a2ed0a3cb1  (UUID)
Aether User:       570d9941-f4be-46d6-9662-15a2ed0a3cb1  (same UUID)
Tenant ID:         tenant_1767395606                     (timestamp-based)
Space ID:          space_1767395606                      (derived from tenant)
```

**Critical Rules**:
- ✅ User IDs are synchronized 1:1 between Keycloak and Aether
- ✅ Tenant IDs follow `tenant_<unix_timestamp>` format
- ✅ Space IDs are derived: `space_<timestamp>` (remove "tenant_" prefix)
- ❌ NEVER use UUIDs for tenant/space IDs

### Multi-Tenancy Queries

**Always filter by BOTH tenant_id AND space_id**:

```cypher
// ✅ CORRECT - Proper isolation
MATCH (u:User {keycloak_id: $keycloak_id})-[:OWNS]->(n:Notebook)
WHERE n.tenant_id = $tenant_id
  AND n.space_id = $space_id
  AND n.deleted_at IS NULL
RETURN n

// ❌ WRONG - Missing space_id filter
MATCH (u:User)-[:OWNS]->(n:Notebook)
WHERE n.tenant_id = $tenant_id  // NOT ENOUGH!
RETURN n
```

### Authentication Flow

```
1. Frontend sends JWT to Aether Backend
2. Backend extracts keycloak_id from JWT sub claim
3. Backend queries: MATCH (u:User {keycloak_id: $sub})
4. If not found → Auto-create user + trigger onboarding
5. Return user data with tenant_id and space_id
```

---

## 🎯 Common Use Cases

### Case 1: Find User's Notebooks

```cypher
MATCH (u:User {keycloak_id: $keycloak_id})-[:OWNS]->(n:Notebook)
WHERE n.tenant_id = $tenant_id
  AND n.space_id = $space_id
  AND n.status = "active"
  AND n.parent_id IS NULL  // Top-level only
RETURN n
ORDER BY n.updated_at DESC
```

### Case 2: Get Notebook with Documents

```cypher
MATCH (n:Notebook {id: $notebook_id})
WHERE n.tenant_id = $tenant_id
  AND n.space_id = $space_id
OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
WHERE d.status = "processed"
  AND d.deleted_at IS NULL
RETURN n, collect(d) as documents
```

### Case 3: Search Documents by Content

```cypher
CALL db.index.fulltext.queryNodes('documentSearchIndex', $query)
YIELD node, score
MATCH (node:Document)
WHERE node.tenant_id = $tenant_id
  AND node.space_id = $space_id
  AND node.status = "processed"
RETURN node
ORDER BY score DESC
LIMIT 20
```

### Case 4: Create New Notebook

```cypher
CREATE (n:Notebook {
  id: $id,
  name: $name,
  description: $description,
  visibility: "private",
  status: "active",
  owner_id: $owner_id,
  space_type: $space_type,
  space_id: $space_id,
  tenant_id: $tenant_id,
  document_count: 0,
  total_size_bytes: 0,
  tags: $tags,
  search_text: $search_text,
  created_at: datetime(),
  updated_at: datetime()
})
// Create ownership relationship
WITH n
MATCH (u:User {id: $owner_id})
CREATE (u)-[:OWNS]->(n)
RETURN n
```

---

## 🛡️ Security Checklist

Before deploying any query:

- [ ] Filters by `tenant_id`
- [ ] Filters by `space_id` (if space-aware)
- [ ] Excludes soft-deleted records (`deleted_at IS NULL`)
- [ ] Validates user has access to the space
- [ ] Never exposes cross-tenant data
- [ ] Uses parameterized queries (prevent injection)
- [ ] Includes proper error handling

---

## 🚨 Critical Issues to Know

### Issue #1: AudiModal Shared Tenant (FIXED)
- **Status**: ✅ Fixed in code, ⚠️ needs production verification
- **What happened**: All users were sharing same `audimodal_tenant_id`
- **Fix**: Now generates unique `tenant_<timestamp>` per user
- **Action**: Run validation script to confirm

### Issue #2: Agent Builder Space Isolation (UNKNOWN)
- **Status**: ⚠️ Needs investigation
- **What**: Unknown if PostgreSQL `agents` table has `space_id` column
- **Action**: Check schema and add if missing

### Issue #3: LLM Router Space Tracking (MISSING)
- **Status**: ⚠️ Enhancement needed
- **What**: LLM Router doesn't track `space_id` in logs
- **Impact**: Cannot audit usage per space
- **Action**: Add `X-Space-ID` header support

See [`INCONSISTENCIES-FOUND.md`](./overview/INCONSISTENCIES-FOUND.md) for complete details.

---

## 📖 Documentation Standards

### When documenting a new model:

1. **Copy the template**:
   ```bash
   cp overview/TEMPLATE.md {service}/{category}/{model-name}.md
   ```

2. **Fill required sections** (all 14):
   - Metadata header
   - Overview
   - Schema definition
   - Relationships
   - Validation rules
   - Lifecycle & state transitions
   - Examples (CRUD queries)
   - Cross-service references
   - Tenant & space isolation
   - Performance considerations
   - Security & compliance
   - Migration history
   - Known issues
   - Related documentation

3. **Update main README**:
   Add link to your model in `data-models/README.md`

4. **Run validation**:
   Ensure your examples work and follow patterns

---

## 🔧 Useful Commands

### Validation

```bash
# Run all consistency checks
./validation/scripts/validate-cross-references.sh

# Check specific issue
grep -A 20 "AudiModal Shared Tenant" overview/INCONSISTENCIES-FOUND.md
```

### Search Documentation

```bash
# Find all references to a model
grep -r "User node" .

# Find ID patterns
grep -r "tenant_" . | grep -v ".git"

# Find query examples
grep -r "MATCH (u:User" .
```

### Neo4j Direct Queries

```bash
# Connect to Neo4j
cypher-shell -u neo4j -p password

# List all node labels
CALL db.labels();

# Count users
MATCH (u:User) RETURN count(u);

# Check indexes
CALL db.indexes();
```

---

## 💡 Pro Tips

### For Developers:

1. **Start with the main README** - It has everything
2. **Use the QUICK-START** - Common patterns are there
3. **Check INCONSISTENCIES** - Avoid known pitfalls
4. **Run validation often** - Catch issues early
5. **Follow the template** - Consistency matters

### For Architects:

1. **Review ID-MAPPING-CHAIN** - Complete data flows
2. **Check PHASE-STATUS** - Track progress
3. **Read INCONSISTENCIES** - Critical issues listed
4. **Validate production** - Run the script regularly

### For Operators:

1. **Run validation script** - Automated health check
2. **Monitor critical issues** - Track resolution status
3. **Check audit logs** - Verify data isolation
4. **Review performance** - Optimize slow queries

---

## 🆘 Common Problems

### "Query returns data from wrong tenant"
→ Add `WHERE tenant_id = $tenantId` to your query

### "User can't see their notebooks"
→ Check `space_id` matches user's `personal_space_id`

### "Validation script fails"
→ Check Neo4j connection and credentials in env vars

### "Can't find model documentation"
→ Check service directory in `data-models/{service}/`

### "Don't know which ID to use"
→ See [ID Mapping Chain](./cross-service/mappings/id-mapping-chain.md)

### "Need to understand data flow"
→ Read [ID Mapping Chain](./cross-service/mappings/id-mapping-chain.md) section for your use case

---

## 📞 Getting Help

### Documentation Resources:
- **Main Navigation**: [README.md](./README.md)
- **Quick Patterns**: [QUICK-START.md](./overview/QUICK-START.md)
- **Complete Index**: [INDEX.md](./INDEX.md)
- **Template**: [TEMPLATE.md](./overview/TEMPLATE.md)

### Technical Resources:
- **ID Mappings**: [ID-MAPPING-CHAIN.md](./cross-service/mappings/id-mapping-chain.md)
- **Known Issues**: [INCONSISTENCIES-FOUND.md](./overview/INCONSISTENCIES-FOUND.md)
- **Validation**: [validate-cross-references.sh](./validation/scripts/validate-cross-references.sh)

### Platform Documentation:
- **Architecture**: `../../SPACE_TENANT_MODEL_SUMMARY.md`
- **Infrastructure**: `../../aether-shared/README.md`
- **Service Ports**: `../../aether-shared/services-and-ports.md`

---

## 🎯 Quick Links

| What | Where |
|------|-------|
| Browse all models | [README.md](./README.md) |
| Find specific model | [INDEX.md](./INDEX.md) |
| Common queries | [QUICK-START.md](./overview/QUICK-START.md) |
| Document new model | [TEMPLATE.md](./overview/TEMPLATE.md) |
| Check progress | [PHASE-STATUS.md](./PHASE-STATUS.md) |
| Known issues | [INCONSISTENCIES-FOUND.md](./overview/INCONSISTENCIES-FOUND.md) |
| Data flows | [ID-MAPPING-CHAIN.md](./cross-service/mappings/id-mapping-chain.md) |
| Run validation | `./validation/scripts/validate-cross-references.sh` |

---

## 📊 Current Status

**Overall Completion**: 20% (13 of 54 files)

**Phase Breakdown**:
- ✅ Phase 1 Foundation: 100% complete
- 🟡 Phase 2 Core Models: 18% complete
- ⏳ Phase 3 Extended: 0% complete
- ⏳ Phase 4 Vector/LLM: 0% complete
- ⏳ Phase 5 Integration: 0% complete

**Last Updated**: 2026-01-05
**Next Milestone**: Complete Phase 2 core models

---

**Maintained by**: TAS Platform Team
**For Questions**: See individual service CLAUDE.md files or root CLAUDE.md
