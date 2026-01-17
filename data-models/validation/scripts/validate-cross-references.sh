#!/bin/bash

# Cross-Service ID Reference Validation Script
# Verifies data consistency across TAS platform services

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0
WARNING_CHECKS=0

echo "=========================================="
echo "TAS Platform Data Consistency Validation"
echo "=========================================="
echo ""

# Configuration
NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}"
NEO4J_USER="${NEO4J_USERNAME:-neo4j}"
NEO4J_PASS="${NEO4J_PASSWORD:-password}"
NEO4J_DB="${NEO4J_DATABASE:-neo4j}"

POSTGRES_HOST="${DB_HOST:-localhost}"
POSTGRES_PORT="${DB_PORT:-5432}"
POSTGRES_USER="${DB_USER:-tasuser}"
POSTGRES_DB="${DB_NAME:-tas_shared}"

AETHER_API="${AETHER_API_URL:-https://aether-api.tas.scharber.com}"
KEYCLOAK_URL="${KEYCLOAK_URL:-https://keycloak.tas.scharber.com}"

# Helper functions
pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED_CHECKS++))
    ((TOTAL_CHECKS++))
}

fail() {
    echo -e "${RED}✗${NC} $1"
    if [ $# -gt 1 ]; then
        echo -e "  ${RED}Details:${NC} $2"
    fi
    ((FAILED_CHECKS++))
    ((TOTAL_CHECKS++))
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    if [ $# -gt 1 ]; then
        echo -e "  ${YELLOW}Details:${NC} $2"
    fi
    ((WARNING_CHECKS++))
    ((TOTAL_CHECKS++))
}

info() {
    echo -e "ℹ $1"
}

section() {
    echo ""
    echo "-------------------------------------------"
    echo "$1"
    echo "-------------------------------------------"
}

# Check if required tools are available
check_dependencies() {
    section "Checking Dependencies"

    if command -v cypher-shell &> /dev/null; then
        pass "cypher-shell available"
    else
        fail "cypher-shell not found" "Install Neo4j or use kubectl exec for k8s"
    fi

    if command -v psql &> /dev/null; then
        pass "psql available"
    else
        warn "psql not found" "PostgreSQL checks will be skipped"
    fi

    if command -v curl &> /dev/null; then
        pass "curl available"
    else
        fail "curl not found" "Required for API checks"
    fi

    if command -v jq &> /dev/null; then
        pass "jq available"
    else
        fail "jq not found" "Required for JSON parsing"
    fi
}

# Check 1: Verify all users have unique tenant_id
check_unique_tenant_ids() {
    section "Check 1: User Tenant ID Uniqueness"

    info "Querying Neo4j for user tenant IDs..."

    QUERY="MATCH (u:User) WHERE u.personal_tenant_id IS NOT NULL RETURN u.id AS user_id, u.personal_tenant_id AS tenant_id ORDER BY u.created_at"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        # Count total users
        TOTAL_USERS=$(echo "$RESULT" | tail -n +2 | wc -l)

        # Count unique tenant IDs
        UNIQUE_TENANTS=$(echo "$RESULT" | tail -n +2 | awk '{print $2}' | sort -u | wc -l)

        if [ "$TOTAL_USERS" -eq 0 ]; then
            warn "No users found in Neo4j"
        elif [ "$TOTAL_USERS" -eq "$UNIQUE_TENANTS" ]; then
            pass "All $TOTAL_USERS users have unique tenant_id values"
        else
            fail "Duplicate tenant_id detected" "$TOTAL_USERS users but only $UNIQUE_TENANTS unique tenant IDs"
            info "Checking for duplicates..."
            echo "$RESULT" | tail -n +2 | awk '{print $2}' | sort | uniq -d
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 2: Verify tenant_id format compliance
check_tenant_id_format() {
    section "Check 2: Tenant ID Format Validation"

    info "Checking for tenant_<timestamp> format..."

    QUERY="MATCH (u:User) WHERE u.personal_tenant_id IS NOT NULL RETURN u.personal_tenant_id AS tenant_id"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        INVALID_FORMAT=0

        while IFS= read -r tenant_id; do
            # Skip header
            if [[ "$tenant_id" == "tenant_id" ]]; then
                continue
            fi

            # Check format: tenant_<digits>
            if ! [[ "$tenant_id" =~ ^tenant_[0-9]+$ ]]; then
                ((INVALID_FORMAT++))
                info "Invalid format: $tenant_id"
            fi
        done <<< "$RESULT"

        if [ "$INVALID_FORMAT" -eq 0 ]; then
            pass "All tenant IDs follow tenant_<timestamp> format"
        else
            fail "$INVALID_FORMAT tenant IDs have invalid format"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 3: Verify space_id derivation
check_space_id_derivation() {
    section "Check 3: Space ID Derivation Consistency"

    info "Verifying space_id = space_<id> derived from tenant_<id>..."

    QUERY="MATCH (u:User) WHERE u.personal_tenant_id IS NOT NULL AND u.personal_space_id IS NOT NULL RETURN u.personal_tenant_id AS tenant_id, u.personal_space_id AS space_id"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        MISMATCH_COUNT=0

        while IFS=$'\t' read -r tenant_id space_id; do
            # Skip header
            if [[ "$tenant_id" == "tenant_id" ]]; then
                continue
            fi

            # Extract numeric portion from tenant_id
            EXPECTED_SPACE_ID=$(echo "$tenant_id" | sed 's/tenant_/space_/')

            if [ "$space_id" != "$EXPECTED_SPACE_ID" ]; then
                ((MISMATCH_COUNT++))
                info "Mismatch: tenant=$tenant_id -> space=$space_id (expected: $EXPECTED_SPACE_ID)"
            fi
        done <<< "$RESULT"

        if [ "$MISMATCH_COUNT" -eq 0 ]; then
            pass "All space IDs correctly derived from tenant IDs"
        else
            fail "$MISMATCH_COUNT space IDs incorrectly derived"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 4: Verify notebooks have tenant_id and space_id
check_notebook_isolation() {
    section "Check 4: Notebook Tenant/Space Isolation"

    info "Checking notebooks for tenant_id and space_id..."

    QUERY="MATCH (n:Notebook) RETURN count(n) AS total, count(n.tenant_id) AS with_tenant, count(n.space_id) AS with_space"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        TOTAL=$(echo "$RESULT" | tail -n 1 | awk '{print $1}')
        WITH_TENANT=$(echo "$RESULT" | tail -n 1 | awk '{print $2}')
        WITH_SPACE=$(echo "$RESULT" | tail -n 1 | awk '{print $3}')

        if [ "$TOTAL" -eq 0 ]; then
            warn "No notebooks found in Neo4j"
        elif [ "$TOTAL" -eq "$WITH_TENANT" ] && [ "$TOTAL" -eq "$WITH_SPACE" ]; then
            pass "All $TOTAL notebooks have tenant_id and space_id"
        else
            fail "Notebooks missing isolation fields" "Total: $TOTAL, With tenant_id: $WITH_TENANT, With space_id: $WITH_SPACE"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 5: Verify documents have tenant_id and space_id
check_document_isolation() {
    section "Check 5: Document Tenant/Space Isolation"

    info "Checking documents for tenant_id and space_id..."

    QUERY="MATCH (d:Document) RETURN count(d) AS total, count(d.tenant_id) AS with_tenant, count(d.space_id) AS with_space"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        TOTAL=$(echo "$RESULT" | tail -n 1 | awk '{print $1}')
        WITH_TENANT=$(echo "$RESULT" | tail -n 1 | awk '{print $2}')
        WITH_SPACE=$(echo "$RESULT" | tail -n 1 | awk '{print $3}')

        if [ "$TOTAL" -eq 0 ]; then
            warn "No documents found in Neo4j"
        elif [ "$TOTAL" -eq "$WITH_TENANT" ] && [ "$TOTAL" -eq "$WITH_SPACE" ]; then
            pass "All $TOTAL documents have tenant_id and space_id"
        else
            fail "Documents missing isolation fields" "Total: $TOTAL, With tenant_id: $WITH_TENANT, With space_id: $WITH_SPACE"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 6: Verify no shared tenant IDs across users
check_no_shared_tenants() {
    section "Check 6: No Shared Tenant IDs Across Users"

    info "Looking for tenant IDs shared by multiple users..."

    QUERY="MATCH (u:User) WHERE u.personal_tenant_id IS NOT NULL WITH u.personal_tenant_id AS tenant_id, count(u) AS user_count WHERE user_count > 1 RETURN tenant_id, user_count"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        SHARED_COUNT=$(echo "$RESULT" | tail -n +2 | wc -l)

        if [ "$SHARED_COUNT" -eq 0 ]; then
            pass "No tenant IDs shared across multiple users"
        else
            fail "$SHARED_COUNT tenant IDs are shared by multiple users"
            echo "$RESULT"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 7: Cross-check Space nodes exist for users
check_space_nodes() {
    section "Check 7: Space Node Existence"

    info "Verifying Space nodes exist for each user's personal_space_id..."

    QUERY="MATCH (u:User) WHERE u.personal_space_id IS NOT NULL OPTIONAL MATCH (s:Space {id: u.personal_space_id}) RETURN count(u) AS users_with_space_id, count(s) AS matching_space_nodes"

    if RESULT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1); then
        USERS=$(echo "$RESULT" | tail -n 1 | awk '{print $1}')
        SPACES=$(echo "$RESULT" | tail -n 1 | awk '{print $2}')

        if [ "$USERS" -eq 0 ]; then
            warn "No users with personal_space_id found"
        elif [ "$USERS" -eq "$SPACES" ]; then
            pass "All $USERS users have matching Space nodes"
        else
            warn "Space node mismatch" "Users with space_id: $USERS, Matching Space nodes: $SPACES"
        fi
    else
        fail "Failed to query Neo4j" "$RESULT"
    fi
}

# Check 8: Verify Keycloak user sync (if accessible)
check_keycloak_sync() {
    section "Check 8: Keycloak User Synchronization"

    if [ -z "${KEYCLOAK_ADMIN_TOKEN:-}" ]; then
        warn "Skipping Keycloak check - KEYCLOAK_ADMIN_TOKEN not set"
        return
    fi

    info "Comparing user counts between Keycloak and Neo4j..."

    # Get Keycloak user count
    KC_RESPONSE=$(curl -k -s -H "Authorization: Bearer $KEYCLOAK_ADMIN_TOKEN" \
        "$KEYCLOAK_URL/admin/realms/aether/users/count" || echo "0")

    # Get Neo4j user count
    QUERY="MATCH (u:User) RETURN count(u) AS count"
    NEO4J_COUNT=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DB" "$QUERY" --format plain 2>&1 | tail -n 1)

    if [ "$KC_RESPONSE" -eq "$NEO4J_COUNT" ]; then
        pass "User counts match: Keycloak=$KC_RESPONSE, Neo4j=$NEO4J_COUNT"
    else
        warn "User count mismatch" "Keycloak: $KC_RESPONSE, Neo4j: $NEO4J_COUNT"
    fi
}

# Check 9: PostgreSQL Agent Builder schema (if accessible)
check_agent_builder_schema() {
    section "Check 9: Agent Builder Space ID Column"

    if ! command -v psql &> /dev/null; then
        warn "Skipping PostgreSQL check - psql not available"
        return
    fi

    info "Checking if agents table has space_id column..."

    if RESULT=$(PGPASSWORD="$POSTGRES_PASS" psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -c "SELECT column_name FROM information_schema.columns WHERE table_name='agents' AND column_name='space_id';" 2>&1); then
        if echo "$RESULT" | grep -q "space_id"; then
            pass "agents table has space_id column"
        else
            warn "agents table missing space_id column" "Space isolation may not be enforced"
        fi
    else
        warn "Could not query PostgreSQL" "$RESULT"
    fi
}

# Summary
print_summary() {
    echo ""
    echo "=========================================="
    echo "Validation Summary"
    echo "=========================================="
    echo -e "Total Checks:   $TOTAL_CHECKS"
    echo -e "${GREEN}Passed:${NC}        $PASSED_CHECKS"
    echo -e "${RED}Failed:${NC}        $FAILED_CHECKS"
    echo -e "${YELLOW}Warnings:${NC}      $WARNING_CHECKS"
    echo ""

    if [ "$FAILED_CHECKS" -eq 0 ]; then
        echo -e "${GREEN}✓ All critical checks passed!${NC}"
        exit 0
    else
        echo -e "${RED}✗ $FAILED_CHECKS critical issues found${NC}"
        echo "Review the failures above and fix data inconsistencies"
        exit 1
    fi
}

# Main execution
main() {
    check_dependencies
    check_unique_tenant_ids
    check_tenant_id_format
    check_space_id_derivation
    check_notebook_isolation
    check_document_isolation
    check_no_shared_tenants
    check_space_nodes
    check_keycloak_sync
    check_agent_builder_schema
    print_summary
}

# Run if executed directly
if [ "${BASH_SOURCE[0]}" == "${0}" ]; then
    main "$@"
fi
