#!/bin/bash

################################################################################
# Neo4j Initialization Script for TAS Aether Backend
#
# This script automatically initializes Neo4j for the Aether platform:
# - Runs all database migrations in correct order
# - Creates constraints and indexes
# - Verifies schema is ready
#
# Usage:
#   ./init-neo4j.sh [--dry-run] [--neo4j-uri URI]
#
# Environment Variables:
#   NEO4J_URI      - Neo4j connection URI (default: bolt://neo4j.aether-be:7687)
#   NEO4J_USERNAME - Neo4j username (default: neo4j)
#   NEO4J_PASSWORD - Neo4j password (default: password)
#   NEO4J_DATABASE - Neo4j database name (default: neo4j)
################################################################################

set -e  # Exit on error
set -u  # Exit on undefined variable

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
# bolt+ssc:// = bolt with self-signed certificate (required for Neo4j 5.x with TLS)
NEO4J_URI="${NEO4J_URI:-bolt+ssc://localhost:7687}"
NEO4J_USERNAME="${NEO4J_USERNAME:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-password}"
NEO4J_DATABASE="${NEO4J_DATABASE:-neo4j}"
DRY_RUN=false
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/home/jscharber/eng/TAS/aether-be/migrations}"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --neo4j-uri)
            NEO4J_URI="$2"
            shift 2
            ;;
        --migrations-dir)
            MIGRATIONS_DIR="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--dry-run] [--neo4j-uri URI] [--migrations-dir DIR]"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

################################################################################
# Helper Functions
################################################################################

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if cypher-shell is installed
check_cypher_shell() {
    if command -v cypher-shell &> /dev/null; then
        log_success "cypher-shell found: $(cypher-shell --version 2>&1 | head -1)"
        return 0
    else
        log_error "cypher-shell not found"
        log_info "Please install Neo4j client tools or run this script inside Neo4j container"
        log_info "Alternative: kubectl exec -it neo4j-0 -n aether-be -- /bin/bash"
        return 1
    fi
}

# Wait for Neo4j to be ready
wait_for_neo4j() {
    log_info "Waiting for Neo4j to be ready at $NEO4J_URI..."
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
            "RETURN 1" > /dev/null 2>&1; then
            log_success "Neo4j is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        echo -n "."
        sleep 2
    done

    log_error "Neo4j did not become ready after $max_attempts attempts"
    return 1
}

# Execute Cypher query
execute_cypher() {
    local query="$1"
    local description="${2:-Executing query}"

    if $DRY_RUN; then
        log_warning "DRY RUN: Would execute: $description"
        return 0
    fi

    log_info "$description..."

    local output
    output=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
        "$query" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to execute: $description"
        log_error "Error: $output"
        return 1
    fi

    log_success "$description completed"
}

# Execute Cypher file
execute_cypher_file() {
    local file_path="$1"
    local description="${2:-Executing $(basename $file_path)}"

    if [ ! -f "$file_path" ]; then
        log_error "Migration file not found: $file_path"
        return 1
    fi

    if $DRY_RUN; then
        log_warning "DRY RUN: Would execute file: $file_path"
        return 0
    fi

    log_info "$description..."

    local output
    output=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
        --file "$file_path" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to execute: $description"
        log_error "Error: $output"
        return 1
    fi

    log_success "$description completed"
}

# Create basic constraints
create_constraints() {
    log_info "Creating database constraints..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create constraints"
        return 0
    fi

    # User constraints
    execute_cypher \
        "CREATE CONSTRAINT user_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.id IS UNIQUE" \
        "Creating User.id unique constraint"

    execute_cypher \
        "CREATE CONSTRAINT user_keycloak_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.keycloak_id IS UNIQUE" \
        "Creating User.keycloak_id unique constraint"

    execute_cypher \
        "CREATE CONSTRAINT user_email_unique IF NOT EXISTS FOR (u:User) REQUIRE u.email IS UNIQUE" \
        "Creating User.email unique constraint"

    # Space constraints
    execute_cypher \
        "CREATE CONSTRAINT space_id_unique IF NOT EXISTS FOR (s:Space) REQUIRE s.id IS UNIQUE" \
        "Creating Space.id unique constraint"

    # Organization constraints
    execute_cypher \
        "CREATE CONSTRAINT organization_id_unique IF NOT EXISTS FOR (o:Organization) REQUIRE o.id IS UNIQUE" \
        "Creating Organization.id unique constraint"

    # Notebook constraints
    execute_cypher \
        "CREATE CONSTRAINT notebook_id_unique IF NOT EXISTS FOR (n:Notebook) REQUIRE n.id IS UNIQUE" \
        "Creating Notebook.id unique constraint"

    # Document constraints
    execute_cypher \
        "CREATE CONSTRAINT document_id_unique IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE" \
        "Creating Document.id unique constraint"

    # Agent constraints
    execute_cypher \
        "CREATE CONSTRAINT agent_id_unique IF NOT EXISTS FOR (a:Agent) REQUIRE a.id IS UNIQUE" \
        "Creating Agent.id unique constraint"

    log_success "All constraints created"
}

# Create indexes for performance
create_indexes() {
    log_info "Creating database indexes..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create indexes"
        return 0
    fi

    # User indexes
    execute_cypher \
        "CREATE INDEX user_tenant_id_index IF NOT EXISTS FOR (u:User) ON (u.tenant_id)" \
        "Creating User.tenant_id index"

    execute_cypher \
        "CREATE INDEX user_space_id_index IF NOT EXISTS FOR (u:User) ON (u.personal_space_id)" \
        "Creating User.personal_space_id index"

    # Notebook indexes
    execute_cypher \
        "CREATE INDEX notebook_space_id_index IF NOT EXISTS FOR (n:Notebook) ON (n.space_id)" \
        "Creating Notebook.space_id index"

    execute_cypher \
        "CREATE INDEX notebook_tenant_id_index IF NOT EXISTS FOR (n:Notebook) ON (n.tenant_id)" \
        "Creating Notebook.tenant_id index"

    # Document indexes
    execute_cypher \
        "CREATE INDEX document_notebook_id_index IF NOT EXISTS FOR (d:Document) ON (d.notebook_id)" \
        "Creating Document.notebook_id index"

    execute_cypher \
        "CREATE INDEX document_space_id_index IF NOT EXISTS FOR (d:Document) ON (d.space_id)" \
        "Creating Document.space_id index"

    execute_cypher \
        "CREATE INDEX document_tenant_id_index IF NOT EXISTS FOR (d:Document) ON (d.tenant_id)" \
        "Creating Document.tenant_id index"

    execute_cypher \
        "CREATE INDEX document_status_index IF NOT EXISTS FOR (d:Document) ON (d.status)" \
        "Creating Document.status index"

    # Agent indexes
    execute_cypher \
        "CREATE INDEX agent_space_id_index IF NOT EXISTS FOR (a:Agent) ON (a.space_id)" \
        "Creating Agent.space_id index"

    execute_cypher \
        "CREATE INDEX agent_tenant_id_index IF NOT EXISTS FOR (a:Agent) ON (a.tenant_id)" \
        "Creating Agent.tenant_id index"

    log_success "All indexes created"
}

# Run Cypher migrations
run_cypher_migrations() {
    log_info "Running Cypher migration files..."

    if [ ! -d "$MIGRATIONS_DIR" ]; then
        log_warning "Migrations directory not found: $MIGRATIONS_DIR"
        log_warning "Skipping migration files (constraints and indexes already created)"
        return 0
    fi

    # Define migration files in order
    local migrations=(
        "add_tenant_fields_to_organization.cypher"
        "add_tenant_fields_to_users.cypher"
        "add_space_fields_to_notebooks.cypher"
        "add_space_fields_to_documents.cypher"
        "add_chunk_schema.cypher"
        "003_add_agent_support.cypher"
    )

    for migration in "${migrations[@]}"; do
        local migration_path="$MIGRATIONS_DIR/$migration"
        if [ -f "$migration_path" ]; then
            execute_cypher_file "$migration_path" "Running migration: $migration"
        else
            log_warning "Migration file not found: $migration (skipping)"
        fi
    done

    log_success "Cypher migrations completed"
}

# Verify schema
verify_schema() {
    log_info "Verifying database schema..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would verify schema"
        return 0
    fi

    # Check constraints
    log_info "Checking constraints..."
    local constraints
    constraints=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
        "SHOW CONSTRAINTS" 2>&1 | grep -c "UNIQUE" || true)

    if [ "$constraints" -gt 0 ]; then
        log_success "Found $constraints unique constraints"
    else
        log_warning "No constraints found"
    fi

    # Check indexes
    log_info "Checking indexes..."
    local indexes
    indexes=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
        "SHOW INDEXES" 2>&1 | grep -c "ONLINE" || true)

    if [ "$indexes" -gt 0 ]; then
        log_success "Found $indexes indexes"
    else
        log_warning "No indexes found"
    fi

    # Check node count
    log_info "Checking database content..."
    local node_count
    node_count=$(cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -d "$NEO4J_DATABASE" \
        "MATCH (n) RETURN count(n) AS count" 2>&1 | tail -1 | tr -d '[:space:]' || echo "0")

    log_info "Database contains $node_count nodes"

    log_success "Schema verification completed"
}

# Generate summary
generate_summary() {
    if $DRY_RUN; then
        return 0
    fi

    log_info "Generating Neo4j configuration summary..."

    cat > "neo4j-init-summary.txt" <<EOF
# Neo4j Initialization Summary
# Generated: $(date)

Neo4j Connection:
  URI: $NEO4J_URI
  Database: $NEO4J_DATABASE
  Username: $NEO4J_USERNAME

Schema Components:
  ✓ Constraints for unique IDs (User, Space, Organization, Notebook, Document, Agent)
  ✓ Indexes for performance (tenant_id, space_id, status fields)
  ✓ Migration files applied (space-based multi-tenancy model)

Node Types:
  - User: User accounts with personal spaces
  - Space: Workspace isolation boundaries
  - Organization: Multi-user organizations
  - Notebook: Document collections
  - Document: Files and content
  - Agent: AI agents with notebook access
  - Chunk: Document chunks for vector search

Key Relationships:
  - User -[:HAS_SPACE]-> Space
  - User -[:OWNS]-> Notebook
  - User -[:OWNS]-> Agent
  - Notebook -[:CONTAINS]-> Document
  - Document -[:HAS_CHUNK]-> Chunk
  - Agent -[:CAN_SEARCH]-> Notebook

Multi-Tenancy Fields:
  - tenant_id: Logical tenant identifier
  - space_id: Workspace isolation boundary
  - personal_space_id: User's private workspace

Next Steps:
  1. Deploy aether-backend service
  2. Test user creation and onboarding
  3. Verify space-based isolation works correctly
  4. Monitor Neo4j performance metrics

Verification Commands:
  # Show all constraints
  cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" "SHOW CONSTRAINTS"

  # Show all indexes
  cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" "SHOW INDEXES"

  # Count nodes by type
  cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" "MATCH (n) RETURN labels(n), count(n)"
EOF

    log_success "Summary saved to neo4j-init-summary.txt"
}

################################################################################
# Main Execution
################################################################################

main() {
    echo ""
    log_info "==================================================================="
    log_info "Neo4j Initialization Script for TAS Aether Backend"
    log_info "==================================================================="
    echo ""

    if $DRY_RUN; then
        log_warning "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    log_info "Configuration:"
    echo "  Neo4j URI: $NEO4J_URI"
    echo "  Database: $NEO4J_DATABASE"
    echo "  Migrations: $MIGRATIONS_DIR"
    echo ""

    # Execute steps
    check_cypher_shell || exit 1
    wait_for_neo4j || exit 1

    echo ""
    create_constraints || exit 1

    echo ""
    create_indexes || exit 1

    echo ""
    run_cypher_migrations || exit 1

    echo ""
    verify_schema || exit 1

    echo ""
    generate_summary

    echo ""
    log_success "==================================================================="
    log_success "Neo4j initialization completed successfully!"
    log_success "==================================================================="
    echo ""
    log_info "Next steps:"
    echo "  1. Review summary: cat neo4j-init-summary.txt"
    echo "  2. Deploy aether-backend service"
    echo "  3. Test database connection from backend"
    echo "  4. Monitor Neo4j at: http://localhost:7474 (or via port-forward)"
    echo ""
}

main "$@"
