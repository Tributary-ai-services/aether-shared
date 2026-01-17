#!/bin/bash

################################################################################
# Keycloak Initialization Script for TAS Aether
#
# This script automatically configures Keycloak for the Aether platform:
# - Creates the "aether" realm
# - Creates aether-backend client (confidential OIDC)
# - Creates aether-frontend client (public OIDC)
# - Configures default roles (user, admin, viewer)
# - Enables user registration
# - Extracts and saves client secrets
#
# Usage:
#   ./init-keycloak.sh [--dry-run] [--keycloak-url URL]
#
# Environment Variables:
#   KEYCLOAK_URL      - Keycloak server URL (default: http://keycloak-shared.tas-shared:8080)
#   KEYCLOAK_ADMIN    - Admin username (default: admin)
#   KEYCLOAK_PASSWORD - Admin password (default: admin123)
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
KEYCLOAK_URL="${KEYCLOAK_URL:-http://keycloak-shared.tas-shared:8080}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-admin123}"
REALM_NAME="aether"
DRY_RUN=false
OUTPUT_FILE="keycloak-secrets.env"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --keycloak-url)
            KEYCLOAK_URL="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--dry-run] [--keycloak-url URL]"
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

# Wait for Keycloak to be ready
wait_for_keycloak() {
    log_info "Waiting for Keycloak to be ready at $KEYCLOAK_URL..."
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -skf "$KEYCLOAK_URL/health/ready" > /dev/null 2>&1 || curl -skf "$KEYCLOAK_URL/" > /dev/null 2>&1; then
            log_success "Keycloak is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        echo -n "."
        sleep 2
    done

    log_error "Keycloak did not become ready after $max_attempts attempts"
    return 1
}

# Get admin access token
get_admin_token() {
    log_info "Obtaining admin access token..."

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "username=$KEYCLOAK_ADMIN" \
        -d "password=$KEYCLOAK_PASSWORD" \
        -d "grant_type=password" \
        -d "client_id=admin-cli" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to obtain admin token"
        log_error "Response: $response"
        return 1
    fi

    ADMIN_TOKEN=$(echo "$response" | grep -o '"access_token":"[^"]*' | sed 's/"access_token":"//')

    if [ -z "$ADMIN_TOKEN" ]; then
        log_error "Failed to extract access token from response"
        return 1
    fi

    log_success "Admin token obtained"
}

# Create realm
create_realm() {
    log_info "Creating realm: $REALM_NAME..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create realm $REALM_NAME"
        return 0
    fi

    # Check if realm already exists
    local realm_check
    realm_check=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    if [ $? -eq 0 ]; then
        log_warning "Realm $REALM_NAME already exists, skipping creation"
        return 0
    fi

    # Create realm
    local realm_config
    realm_config=$(cat <<EOF
{
  "realm": "$REALM_NAME",
  "enabled": true,
  "displayName": "Aether AI Platform",
  "registrationAllowed": true,
  "registrationEmailAsUsername": false,
  "editUsernameAllowed": true,
  "resetPasswordAllowed": true,
  "rememberMe": true,
  "verifyEmail": false,
  "loginWithEmailAllowed": true,
  "duplicateEmailsAllowed": false,
  "sslRequired": "external",
  "accessTokenLifespan": 300,
  "accessTokenLifespanForImplicitFlow": 900,
  "ssoSessionIdleTimeout": 1800,
  "ssoSessionMaxLifespan": 36000,
  "offlineSessionIdleTimeout": 2592000,
  "accessCodeLifespan": 60,
  "accessCodeLifespanUserAction": 300,
  "accessCodeLifespanLogin": 1800,
  "actionTokenGeneratedByAdminLifespan": 43200,
  "actionTokenGeneratedByUserLifespan": 300,
  "defaultSignatureAlgorithm": "RS256"
}
EOF
    )

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$realm_config" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to create realm"
        log_error "Response: $response"
        return 1
    fi

    log_success "Realm $REALM_NAME created"
}

# Create roles
create_roles() {
    log_info "Creating default roles..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create roles (user, admin, viewer)"
        return 0
    fi

    local roles=("user" "admin" "viewer")

    for role in "${roles[@]}"; do
        # Check if role exists
        local role_check
        role_check=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles/$role" \
            -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

        if [ $? -eq 0 ]; then
            log_warning "Role $role already exists, skipping"
            continue
        fi

        # Create role
        local role_config
        role_config=$(cat <<EOF
{
  "name": "$role",
  "description": "Default $role role for Aether platform",
  "composite": false,
  "clientRole": false
}
EOF
        )

        local response
        response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles" \
            -H "Authorization: Bearer $ADMIN_TOKEN" \
            -H "Content-Type: application/json" \
            -d "$role_config" 2>&1)

        if [ $? -ne 0 ]; then
            log_error "Failed to create role $role"
            log_error "Response: $response"
            return 1
        fi

        log_success "Role $role created"
    done

    # Set 'user' as default role
    log_info "Setting 'user' as default role..."

    # Get role ID
    local role_id
    role_id=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles/user" \
        -H "Authorization: Bearer $ADMIN_TOKEN" | grep -o '"id":"[^"]*' | sed 's/"id":"//')

    if [ -z "$role_id" ]; then
        log_error "Failed to get user role ID"
        return 1
    fi

    # Add to default roles
    local response
    response=$(curl -skf -X PUT "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles/user" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"user\",\"id\":\"$role_id\",\"composite\":false,\"clientRole\":false}" 2>&1)

    log_success "Default roles configured"
}

# Create aether-backend client
create_backend_client() {
    log_info "Creating aether-backend client..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create aether-backend client"
        return 0
    fi

    # Check if client exists
    local client_check
    client_check=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients?clientId=aether-backend" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    if echo "$client_check" | grep -q "aether-backend"; then
        log_warning "Client aether-backend already exists, skipping creation"
        # Get existing client secret
        get_backend_client_secret
        return 0
    fi

    # Create client
    local client_config
    client_config=$(cat <<EOF
{
  "clientId": "aether-backend",
  "name": "Aether Backend API",
  "description": "Backend API service for Aether platform",
  "enabled": true,
  "protocol": "openid-connect",
  "publicClient": false,
  "bearerOnly": false,
  "standardFlowEnabled": true,
  "implicitFlowEnabled": false,
  "directAccessGrantsEnabled": true,
  "serviceAccountsEnabled": true,
  "authorizationServicesEnabled": false,
  "redirectUris": [
    "https://aether-api.tas.scharber.com/*",
    "https://aether.tas.scharber.com/*",
    "http://localhost:*",
    "http://localhost:8080/*"
  ],
  "webOrigins": ["+"],
  "attributes": {
    "access.token.lifespan": "300",
    "client.secret.creation.time": "$(date +%s)"
  }
}
EOF
    )

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$client_config" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to create aether-backend client"
        log_error "Response: $response"
        return 1
    fi

    log_success "Client aether-backend created"

    # Get client secret
    get_backend_client_secret
}

# Get backend client secret
get_backend_client_secret() {
    log_info "Retrieving aether-backend client secret..."

    # Get client ID (not clientId)
    local client_uuid
    client_uuid=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients?clientId=aether-backend" \
        -H "Authorization: Bearer $ADMIN_TOKEN" | grep -o '"id":"[^"]*' | head -1 | sed 's/"id":"//')

    if [ -z "$client_uuid" ]; then
        log_error "Failed to get aether-backend client UUID"
        return 1
    fi

    # Get client secret
    local secret_response
    secret_response=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients/$client_uuid/client-secret" \
        -H "Authorization: Bearer $ADMIN_TOKEN")

    BACKEND_CLIENT_SECRET=$(echo "$secret_response" | grep -o '"value":"[^"]*' | sed 's/"value":"//')

    if [ -z "$BACKEND_CLIENT_SECRET" ]; then
        log_error "Failed to retrieve client secret"
        return 1
    fi

    log_success "Client secret retrieved: ${BACKEND_CLIENT_SECRET:0:8}..."
}

# Create aether-frontend client
create_frontend_client() {
    log_info "Creating aether-frontend client..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create aether-frontend client"
        return 0
    fi

    # Check if client exists
    local client_check
    client_check=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients?clientId=aether-frontend" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    if echo "$client_check" | grep -q "aether-frontend"; then
        log_warning "Client aether-frontend already exists, skipping creation"
        return 0
    fi

    # Create client
    local client_config
    client_config=$(cat <<EOF
{
  "clientId": "aether-frontend",
  "name": "Aether Frontend",
  "description": "Frontend web application for Aether platform",
  "enabled": true,
  "protocol": "openid-connect",
  "publicClient": true,
  "bearerOnly": false,
  "standardFlowEnabled": true,
  "implicitFlowEnabled": false,
  "directAccessGrantsEnabled": true,
  "serviceAccountsEnabled": false,
  "authorizationServicesEnabled": false,
  "redirectUris": [
    "https://aether.tas.scharber.com/*",
    "http://localhost:3000/*",
    "http://localhost:3001/*"
  ],
  "webOrigins": ["+"],
  "attributes": {
    "pkce.code.challenge.method": "S256"
  }
}
EOF
    )

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$client_config" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to create aether-frontend client"
        log_error "Response: $response"
        return 1
    fi

    log_success "Client aether-frontend created"
}

# Save secrets to file
save_secrets() {
    if $DRY_RUN; then
        log_warning "DRY RUN: Would save secrets to $OUTPUT_FILE"
        return 0
    fi

    log_info "Saving secrets to $OUTPUT_FILE..."

    cat > "$OUTPUT_FILE" <<EOF
# Keycloak Configuration for Aether
# Generated: $(date)

KEYCLOAK_URL=$KEYCLOAK_URL
KEYCLOAK_REALM=$REALM_NAME
KEYCLOAK_BACKEND_CLIENT_ID=aether-backend
KEYCLOAK_BACKEND_CLIENT_SECRET=$BACKEND_CLIENT_SECRET
KEYCLOAK_FRONTEND_CLIENT_ID=aether-frontend

# Backend allowed issuers (internal and external)
KEYCLOAK_ALLOWED_ISSUERS=http://keycloak-shared.tas-shared:8080/realms/$REALM_NAME,https://keycloak.tas.scharber.com/realms/$REALM_NAME

# Usage:
# 1. Source this file: source $OUTPUT_FILE
# 2. Or use in docker-compose.yml as env_file
# 3. Or create Kubernetes secret:
#    kubectl create secret generic aether-backend-secret --from-env-file=$OUTPUT_FILE -n aether-be
EOF

    chmod 600 "$OUTPUT_FILE"
    log_success "Secrets saved to $OUTPUT_FILE"

    echo ""
    log_info "To use these secrets:"
    echo "  Kubernetes: kubectl create secret generic aether-backend-secret --from-env-file=$OUTPUT_FILE -n aether-be"
    echo "  Docker Compose: Add 'env_file: $OUTPUT_FILE' to aether-backend service"
    echo ""
}

################################################################################
# Main Execution
################################################################################

main() {
    echo ""
    log_info "==================================================================="
    log_info "Keycloak Initialization Script for TAS Aether"
    log_info "==================================================================="
    echo ""

    if $DRY_RUN; then
        log_warning "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    log_info "Configuration:"
    echo "  Keycloak URL: $KEYCLOAK_URL"
    echo "  Realm: $REALM_NAME"
    echo "  Output file: $OUTPUT_FILE"
    echo ""

    # Execute steps
    wait_for_keycloak || exit 1
    get_admin_token || exit 1
    create_realm || exit 1
    create_roles || exit 1
    create_backend_client || exit 1
    create_frontend_client || exit 1
    save_secrets || exit 1

    echo ""
    log_success "==================================================================="
    log_success "Keycloak initialization completed successfully!"
    log_success "==================================================================="
    echo ""
    log_info "Next steps:"
    echo "  1. Review the secrets file: cat $OUTPUT_FILE"
    echo "  2. Create Kubernetes secret for aether-backend"
    echo "  3. Deploy aether-backend and aether-frontend services"
    echo "  4. Test user registration at: $KEYCLOAK_URL/realms/$REALM_NAME/account"
    echo ""
}

main "$@"
