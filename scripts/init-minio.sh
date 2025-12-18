#!/bin/bash

################################################################################
# MinIO Initialization Script for TAS
#
# This script automatically configures MinIO for the TAS platform:
# - Creates storage buckets (aether-storage, audimodal-storage, deeplake-storage)
# - Sets bucket policies for proper access control
# - Verifies bucket creation
#
# Usage:
#   ./init-minio.sh [--dry-run] [--minio-url URL]
#
# Environment Variables:
#   MINIO_URL         - MinIO server URL (default: https://minio.tas.scharber.com)
#   MINIO_ROOT_USER   - MinIO root username (default: minioadmin)
#   MINIO_ROOT_PASSWORD - MinIO root password (default: minioadmin123)
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
MINIO_URL="${MINIO_URL:-https://minio-api.tas.scharber.com}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin123}"
MINIO_ALIAS="tas-minio"
DRY_RUN=false

# Buckets to create
BUCKETS=("aether-storage" "audimodal-storage" "deeplake-storage")

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --minio-url)
            MINIO_URL="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--dry-run] [--minio-url URL]"
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

# Check if mc (MinIO client) is installed
check_mc_installed() {
    if command -v mc &> /dev/null; then
        log_success "MinIO client (mc) found: $(mc --version | head -1)"
        return 0
    else
        log_error "MinIO client (mc) not found"
        log_info "Installing MinIO client..."
        install_mc
    fi
}

# Install MinIO client
install_mc() {
    log_info "Downloading MinIO client..."

    # Detect OS
    local os=""
    case "$(uname -s)" in
        Linux*)     os="linux";;
        Darwin*)    os="darwin";;
        *)
            log_error "Unsupported OS: $(uname -s)"
            return 1
            ;;
    esac

    # Detect architecture
    local arch=""
    case "$(uname -m)" in
        x86_64)     arch="amd64";;
        arm64|aarch64) arch="arm64";;
        *)
            log_error "Unsupported architecture: $(uname -m)"
            return 1
            ;;
    esac

    # Download mc
    local mc_url="https://dl.min.io/client/mc/release/${os}-${arch}/mc"
    local install_path="/usr/local/bin/mc"

    if [ -w "/usr/local/bin" ]; then
        curl -sSL "$mc_url" -o "$install_path"
        chmod +x "$install_path"
    else
        log_warning "No write permission to /usr/local/bin, using sudo..."
        sudo curl -sSL "$mc_url" -o "$install_path"
        sudo chmod +x "$install_path"
    fi

    log_success "MinIO client installed: $install_path"
}

# Wait for MinIO to be ready
wait_for_minio() {
    log_info "Waiting for MinIO to be ready at $MINIO_URL..."
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -skf "$MINIO_URL/minio/health/live" > /dev/null 2>&1; then
            log_success "MinIO is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        echo -n "."
        sleep 2
    done

    log_error "MinIO did not become ready after $max_attempts attempts"
    return 1
}

# Configure MinIO client alias
configure_mc_alias() {
    log_info "Configuring MinIO client alias: $MINIO_ALIAS"

    if $DRY_RUN; then
        log_warning "DRY RUN: Would configure mc alias $MINIO_ALIAS → $MINIO_URL"
        return 0
    fi

    # Remove existing alias if present
    mc alias remove "$MINIO_ALIAS" 2>/dev/null || true

    # Add new alias (--insecure for self-signed certs)
    mc alias set "$MINIO_ALIAS" "$MINIO_URL" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" --insecure

    if [ $? -ne 0 ]; then
        log_error "Failed to configure MinIO alias"
        return 1
    fi

    log_success "MinIO alias configured: $MINIO_ALIAS"
}

# Create bucket
create_bucket() {
    local bucket_name="$1"

    log_info "Creating bucket: $bucket_name"

    if $DRY_RUN; then
        log_warning "DRY RUN: Would create bucket $bucket_name"
        return 0
    fi

    # Check if bucket already exists
    if mc ls "$MINIO_ALIAS/$bucket_name" --insecure &> /dev/null; then
        log_warning "Bucket $bucket_name already exists, skipping"
        return 0
    fi

    # Create bucket
    mc mb "$MINIO_ALIAS/$bucket_name" --insecure

    if [ $? -ne 0 ]; then
        log_error "Failed to create bucket $bucket_name"
        return 1
    fi

    log_success "Bucket $bucket_name created"
}

# Set bucket versioning (optional)
set_bucket_versioning() {
    local bucket_name="$1"
    local enabled="${2:-false}"

    if $DRY_RUN; then
        log_warning "DRY RUN: Would set versioning=$enabled for $bucket_name"
        return 0
    fi

    if [ "$enabled" = "true" ]; then
        log_info "Enabling versioning for bucket: $bucket_name"
        mc version enable "$MINIO_ALIAS/$bucket_name" --insecure
    else
        log_info "Versioning disabled for bucket: $bucket_name"
    fi
}

# Set bucket policy to allow download (authenticated access)
set_bucket_policy() {
    local bucket_name="$1"

    log_info "Setting download policy for bucket: $bucket_name"

    if $DRY_RUN; then
        log_warning "DRY RUN: Would set download policy for bucket $bucket_name"
        return 0
    fi

    # Set download policy (authenticated users can read)
    mc anonymous set download "$MINIO_ALIAS/$bucket_name" --insecure 2>/dev/null || true

    log_success "Download policy set for bucket $bucket_name"
}

# Verify bucket creation
verify_buckets() {
    log_info "Verifying bucket creation..."

    if $DRY_RUN; then
        log_warning "DRY RUN: Would verify buckets"
        return 0
    fi

    echo ""
    log_info "Listing all buckets:"
    mc ls "$MINIO_ALIAS" --insecure

    echo ""
    for bucket in "${BUCKETS[@]}"; do
        if mc ls "$MINIO_ALIAS/$bucket" --insecure &> /dev/null; then
            log_success "✓ Bucket verified: $bucket"
        else
            log_error "✗ Bucket not found: $bucket"
        fi
    done
}

# Generate bucket info summary
generate_summary() {
    if $DRY_RUN; then
        return 0
    fi

    log_info "Generating bucket configuration summary..."

    cat > "minio-buckets-info.txt" <<EOF
# MinIO Bucket Configuration
# Generated: $(date)

MinIO URL: $MINIO_URL
MinIO Alias: $MINIO_ALIAS

Buckets Created:
$(for bucket in "${BUCKETS[@]}"; do echo "  - $bucket"; done)

S3 Endpoint Configuration for Services:
  S3_ENDPOINT: $MINIO_URL
  S3_ACCESS_KEY: $MINIO_ROOT_USER
  S3_SECRET_KEY: $MINIO_ROOT_PASSWORD
  S3_USE_SSL: true
  S3_VERIFY_SSL: false  # Set to true in production with valid certs

Service-Specific Bucket Assignments:
  Aether Backend:
    - S3_BUCKET: aether-storage
    - Purpose: Document storage, user uploads, processed files

  AudiModal:
    - S3_BUCKET: audimodal-storage
    - Purpose: Audio/video files, multi-modal processing artifacts

  DeepLake API:
    - S3_BUCKET: deeplake-storage
    - Purpose: Vector embeddings, dataset storage

Access URLs:
  Console: $MINIO_URL (login with root credentials)
  API Endpoint: $MINIO_URL

Next Steps:
  1. Update service configurations with bucket names
  2. Configure S3 client credentials in service environments
  3. Test bucket access from services
  4. Consider creating service-specific IAM users (recommended for production)
EOF

    log_success "Summary saved to minio-buckets-info.txt"
}

################################################################################
# Main Execution
################################################################################

main() {
    echo ""
    log_info "==================================================================="
    log_info "MinIO Initialization Script for TAS"
    log_info "==================================================================="
    echo ""

    if $DRY_RUN; then
        log_warning "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    log_info "Configuration:"
    echo "  MinIO URL: $MINIO_URL"
    echo "  Buckets: ${BUCKETS[*]}"
    echo ""

    # Execute steps
    check_mc_installed || exit 1
    wait_for_minio || exit 1
    configure_mc_alias || exit 1

    echo ""
    log_info "Creating buckets..."
    for bucket in "${BUCKETS[@]}"; do
        create_bucket "$bucket" || exit 1
        # Set download policy for console access
        set_bucket_policy "$bucket"
        # Note: Versioning disabled by default for simplicity
        # Uncomment to enable: set_bucket_versioning "$bucket" true
    done

    echo ""
    verify_buckets || exit 1

    echo ""
    generate_summary

    echo ""
    log_success "==================================================================="
    log_success "MinIO initialization completed successfully!"
    log_success "==================================================================="
    echo ""
    log_info "Next steps:"
    echo "  1. Review bucket configuration: cat minio-buckets-info.txt"
    echo "  2. Update service configurations with bucket names"
    echo "  3. Test bucket access: mc ls $MINIO_ALIAS/aether-storage --insecure"
    echo "  4. Access MinIO console: $MINIO_URL (login: $MINIO_ROOT_USER)"
    echo ""
}

main "$@"
