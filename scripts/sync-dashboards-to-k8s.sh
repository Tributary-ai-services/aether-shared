#!/bin/bash

# sync-dashboards-to-k8s.sh
# Syncs Grafana dashboards from local files to Kubernetes ConfigMaps
# Usage: ./sync-dashboards-to-k8s.sh [--dry-run] [--category CATEGORY]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
DASHBOARDS_DIR="$REPO_ROOT/shared-monitoring/grafana/dashboards"
PROVISIONING_DIR="$REPO_ROOT/shared-monitoring/grafana/provisioning/dashboards"
K8S_NAMESPACE="tas-shared"
TEMP_DIR="/tmp/grafana-dashboards-sync"

# Parse arguments
DRY_RUN=false
CATEGORY=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    --category)
      CATEGORY="$2"
      shift 2
      ;;
    --help)
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --dry-run         Show what would be done without making changes"
      echo "  --category NAME   Only sync dashboards from specified category"
      echo "  --help            Show this help message"
      echo ""
      echo "Categories: llm-router, audimodal, loki, deeplake, aether, infrastructure"
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      exit 1
      ;;
  esac
done

# Functions
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

check_prerequisites() {
  log_info "Checking prerequisites..."

  if ! command -v kubectl &> /dev/null; then
    log_error "kubectl not found. Please install kubectl."
    exit 1
  fi

  if ! kubectl cluster-info &> /dev/null; then
    log_error "Cannot connect to Kubernetes cluster."
    exit 1
  fi

  if ! kubectl get namespace "$K8S_NAMESPACE" &> /dev/null; then
    log_error "Namespace $K8S_NAMESPACE does not exist."
    exit 1
  fi

  if [ ! -d "$DASHBOARDS_DIR" ]; then
    log_error "Dashboards directory not found: $DASHBOARDS_DIR"
    exit 1
  fi

  log_success "Prerequisites check passed"
}

count_dashboards() {
  local category=$1
  local count=0

  if [ -d "$DASHBOARDS_DIR/$category" ]; then
    count=$(find "$DASHBOARDS_DIR/$category" -name "*.json" -type f | wc -l)
  fi

  echo "$count"
}

generate_configmap() {
  local category=$1
  local configmap_name="grafana-dashboards-$category"
  local category_dir="$DASHBOARDS_DIR/$category"

  if [ ! -d "$category_dir" ]; then
    log_warning "Category directory not found: $category_dir"
    return 1
  fi

  local dashboard_count=$(count_dashboards "$category")
  if [ "$dashboard_count" -eq 0 ]; then
    log_warning "No dashboards found in category: $category"
    return 1
  fi

  log_info "Generating ConfigMap for category: $category ($dashboard_count dashboards)"

  # Create ConfigMap YAML
  local configmap_file="$TEMP_DIR/${configmap_name}.yaml"

  cat > "$configmap_file" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: $configmap_name
  namespace: $K8S_NAMESPACE
  labels:
    app: grafana-shared
    component: dashboards
    category: $category
data:
EOF

  # Add each dashboard JSON file as a data entry
  local file_count=0
  while IFS= read -r dashboard_file; do
    local filename=$(basename "$dashboard_file")
    echo "  $filename: |" >> "$configmap_file"
    sed 's/^/    /' "$dashboard_file" >> "$configmap_file"
    ((file_count++))
    log_info "  Added: $filename"
  done < <(find "$category_dir" -name "*.json" -type f | sort)

  if [ "$file_count" -eq 0 ]; then
    log_error "No JSON files processed for category: $category"
    rm "$configmap_file"
    return 1
  fi

  log_success "Generated ConfigMap: $configmap_file"
  return 0
}

generate_provisioning_configmap() {
  log_info "Generating provisioning ConfigMap..."

  local configmap_name="grafana-provisioning-dashboards"
  local configmap_file="$TEMP_DIR/${configmap_name}.yaml"

  cat > "$configmap_file" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: $configmap_name
  namespace: $K8S_NAMESPACE
  labels:
    app: grafana-shared
    component: provisioning
data:
EOF

  # Add each provisioning YAML file
  while IFS= read -r prov_file; do
    local filename=$(basename "$prov_file")
    log_info "  Adding provisioning config: $filename"
    echo "  $filename: |" >> "$configmap_file"
    sed 's/^/    /' "$prov_file" >> "$configmap_file"
  done < <(find "$PROVISIONING_DIR" -name "*.yml" -o -name "*.yaml" | sort)

  log_success "Generated provisioning ConfigMap: $configmap_file"
}

apply_configmap() {
  local configmap_file=$1
  local configmap_name=$(basename "$configmap_file" .yaml)

  if [ "$DRY_RUN" = true ]; then
    log_info "[DRY-RUN] Would apply: $configmap_file"
    return 0
  fi

  log_info "Applying ConfigMap: $configmap_name"

  if kubectl apply -f "$configmap_file"; then
    log_success "Applied: $configmap_name"
    return 0
  else
    log_error "Failed to apply: $configmap_name"
    return 1
  fi
}

restart_grafana() {
  if [ "$DRY_RUN" = true ]; then
    log_info "[DRY-RUN] Would restart Grafana deployment"
    return 0
  fi

  log_info "Restarting Grafana to load new dashboards..."

  if kubectl rollout restart deployment grafana-shared -n "$K8S_NAMESPACE"; then
    log_success "Grafana restart initiated"

    log_info "Waiting for Grafana to be ready..."
    if kubectl rollout status deployment grafana-shared -n "$K8S_NAMESPACE" --timeout=120s; then
      log_success "Grafana is ready"
      return 0
    else
      log_error "Grafana restart timed out"
      return 1
    fi
  else
    log_error "Failed to restart Grafana"
    return 1
  fi
}

verify_deployment() {
  log_info "Verifying deployment..."

  # Check if Grafana pod is running
  local pod_status=$(kubectl get pods -n "$K8S_NAMESPACE" -l app=grafana-shared -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "NotFound")

  if [ "$pod_status" = "Running" ]; then
    log_success "Grafana pod is running"
  else
    log_error "Grafana pod status: $pod_status"
    return 1
  fi

  # List all dashboard ConfigMaps
  log_info "Dashboard ConfigMaps:"
  kubectl get configmaps -n "$K8S_NAMESPACE" -l component=dashboards -o custom-columns=NAME:.metadata.name,CATEGORY:.metadata.labels.category,SIZE:.data | head -20

  return 0
}

print_summary() {
  echo ""
  echo "========================================="
  echo "         Dashboard Sync Summary          "
  echo "========================================="
  echo ""

  local categories=("llm-router" "audimodal" "loki" "deeplake" "aether" "infrastructure")
  local total=0

  for cat in "${categories[@]}"; do
    local count=$(count_dashboards "$cat")
    printf "  %-20s %3d dashboards\n" "$cat:" "$count"
    total=$((total + count))
  done

  echo ""
  printf "  %-20s %3d dashboards\n" "TOTAL:" "$total"
  echo ""
  echo "========================================="
  echo ""
}

# Main execution
main() {
  log_info "Starting Grafana dashboard sync to Kubernetes"
  echo ""

  # Check prerequisites
  check_prerequisites

  # Create temp directory
  mkdir -p "$TEMP_DIR"

  # Print summary
  print_summary

  # Determine which categories to process
  local categories_to_process=()

  if [ -n "$CATEGORY" ]; then
    categories_to_process=("$CATEGORY")
    log_info "Processing single category: $CATEGORY"
  else
    categories_to_process=("llm-router" "audimodal" "loki" "deeplake" "aether" "infrastructure")
    log_info "Processing all categories"
  fi

  echo ""

  # Generate ConfigMaps for each category
  local generated_count=0
  for category in "${categories_to_process[@]}"; do
    if generate_configmap "$category"; then
      ((generated_count++))
    fi
    echo ""
  done

  if [ "$generated_count" -eq 0 ]; then
    log_error "No ConfigMaps generated. Exiting."
    exit 1
  fi

  # Generate provisioning ConfigMap
  generate_provisioning_configmap
  echo ""

  # Apply ConfigMaps
  log_info "Applying ConfigMaps to Kubernetes..."
  local applied_count=0
  local failed_count=0

  for configmap_file in "$TEMP_DIR"/*.yaml; do
    if apply_configmap "$configmap_file"; then
      ((applied_count++))
    else
      ((failed_count++))
    fi
  done

  echo ""
  log_info "Applied: $applied_count, Failed: $failed_count"
  echo ""

  if [ "$failed_count" -gt 0 ]; then
    log_error "Some ConfigMaps failed to apply"
    exit 1
  fi

  # Restart Grafana
  if restart_grafana; then
    log_success "Dashboard sync completed successfully!"
  else
    log_error "Dashboard sync completed but Grafana restart failed"
    exit 1
  fi

  echo ""

  # Verify deployment
  verify_deployment

  echo ""
  log_success "All done! Dashboards are now available in Grafana."
  log_info "Access Grafana at: https://grafana.tas.scharber.com"

  # Cleanup
  if [ "$DRY_RUN" = false ]; then
    rm -rf "$TEMP_DIR"
  else
    log_info "ConfigMap files saved in: $TEMP_DIR"
  fi
}

# Run main function
main
