#!/bin/bash

# validate-dashboards.sh
# Validates Grafana dashboard JSON files for syntax and required fields
# Usage: ./validate-dashboards.sh [DASHBOARD_FILE or DIRECTORY]

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
DASHBOARDS_DIR="$REPO_ROOT/shared-monitoring/grafana/dashboards"

# Counters
TOTAL_FILES=0
VALID_FILES=0
INVALID_FILES=0
WARNING_COUNT=0

# Functions
log_info() {
  echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
  echo -e "${GREEN}[✓]${NC} $1"
}

log_warning() {
  echo -e "${YELLOW}[⚠]${NC} $1"
  ((WARNING_COUNT++))
}

log_error() {
  echo -e "${RED}[✗]${NC} $1"
}

validate_json_syntax() {
  local file=$1

  if ! jq empty "$file" 2>/dev/null; then
    log_error "Invalid JSON syntax: $file"
    return 1
  fi

  return 0
}

validate_required_fields() {
  local file=$1
  local has_error=false

  # Check for required top-level fields
  local required_fields=("title" "panels")

  for field in "${required_fields[@]}"; do
    if ! jq -e ".$field" "$file" &>/dev/null; then
      log_error "  Missing required field '$field' in: $(basename "$file")"
      has_error=true
    fi
  done

  # Check title is not empty
  local title=$(jq -r '.title // empty' "$file")
  if [ -z "$title" ]; then
    log_error "  Empty title in: $(basename "$file")"
    has_error=true
  fi

  # Check if panels array exists and is not empty
  local panel_count=$(jq '.panels | length' "$file" 2>/dev/null || echo "0")
  if [ "$panel_count" -eq 0 ]; then
    log_warning "  No panels defined in: $(basename "$file")"
  fi

  if [ "$has_error" = true ]; then
    return 1
  fi

  return 0
}

validate_uid() {
  local file=$1

  local uid=$(jq -r '.uid // empty' "$file")
  if [ -z "$uid" ]; then
    log_warning "  No UID defined in: $(basename "$file") (Grafana will auto-generate)"
  else
    # Check if UID contains only valid characters
    if ! [[ "$uid" =~ ^[a-zA-Z0-9_-]+$ ]]; then
      log_error "  Invalid UID format in: $(basename "$file") (must be alphanumeric with - or _)"
      return 1
    fi
  fi

  return 0
}

validate_panels() {
  local file=$1
  local has_error=false

  # Check each panel has required fields
  local panel_count=$(jq '.panels | length' "$file" 2>/dev/null || echo "0")

  for ((i=0; i<panel_count; i++)); do
    local panel_id=$(jq -r ".panels[$i].id // empty" "$file")
    local panel_title=$(jq -r ".panels[$i].title // empty" "$file")
    local panel_type=$(jq -r ".panels[$i].type // empty" "$file")

    if [ -z "$panel_id" ]; then
      log_warning "  Panel $i missing ID in: $(basename "$file")"
    fi

    if [ -z "$panel_title" ]; then
      log_warning "  Panel $i missing title in: $(basename "$file")"
    fi

    if [ -z "$panel_type" ]; then
      log_error "  Panel $i missing type in: $(basename "$file")"
      has_error=true
    fi

    # Check if panel has targets (queries)
    local target_count=$(jq ".panels[$i].targets | length" "$file" 2>/dev/null || echo "0")
    if [ "$target_count" -eq 0 ] && [ "$panel_type" != "row" ]; then
      log_warning "  Panel $i '$panel_title' has no data queries in: $(basename "$file")"
    fi
  done

  if [ "$has_error" = true ]; then
    return 1
  fi

  return 0
}

validate_datasources() {
  local file=$1

  # Check if dashboard references known datasources
  local datasources=$(jq -r '.. | .datasource? // empty | select(type == "string")' "$file" 2>/dev/null | sort -u)

  if [ -n "$datasources" ]; then
    while IFS= read -r ds; do
      if [ "$ds" != "Prometheus" ] && [ "$ds" != "Loki" ] && [ "$ds" != "-- Grafana --" ]; then
        log_warning "  References unknown datasource '$ds' in: $(basename "$file")"
      fi
    done <<< "$datasources"
  fi

  return 0
}

validate_file() {
  local file=$1
  local filename=$(basename "$file")
  local is_valid=true

  echo ""
  log_info "Validating: $filename"

  ((TOTAL_FILES++))

  # JSON syntax check
  if ! validate_json_syntax "$file"; then
    ((INVALID_FILES++))
    return 1
  fi

  # Required fields check
  if ! validate_required_fields "$file"; then
    is_valid=false
  fi

  # UID validation
  if ! validate_uid "$file"; then
    is_valid=false
  fi

  # Panel validation
  if ! validate_panels "$file"; then
    is_valid=false
  fi

  # Datasource validation
  validate_datasources "$file"

  # Final verdict
  if [ "$is_valid" = true ]; then
    ((VALID_FILES++))
    log_success "$filename is valid"
  else
    ((INVALID_FILES++))
    log_error "$filename has errors"
  fi

  return $([ "$is_valid" = true ] && echo 0 || echo 1)
}

print_summary() {
  echo ""
  echo "========================================="
  echo "       Validation Summary"
  echo "========================================="
  echo ""
  printf "  Total files:    %3d\n" "$TOTAL_FILES"
  printf "  Valid:          %3d\n" "$VALID_FILES"
  printf "  Invalid:        %3d\n" "$INVALID_FILES"
  printf "  Warnings:       %3d\n" "$WARNING_COUNT"
  echo ""

  if [ "$INVALID_FILES" -eq 0 ]; then
    log_success "All dashboards passed validation!"
    echo "========================================="
    return 0
  else
    log_error "$INVALID_FILES dashboard(s) failed validation"
    echo "========================================="
    return 1
  fi
}

# Main
main() {
  log_info "Grafana Dashboard Validator"
  echo ""

  # Check for jq
  if ! command -v jq &> /dev/null; then
    log_error "jq is required but not installed. Please install jq."
    exit 1
  fi

  # Determine what to validate
  if [ $# -eq 0 ]; then
    # No arguments, validate all dashboards
    log_info "No arguments provided. Validating all dashboards in: $DASHBOARDS_DIR"
    TARGET="$DASHBOARDS_DIR"
  else
    TARGET="$1"
  fi

  if [ ! -e "$TARGET" ]; then
    log_error "Path does not exist: $TARGET"
    exit 1
  fi

  # Process files
  if [ -f "$TARGET" ]; then
    # Single file
    validate_file "$TARGET"
  elif [ -d "$TARGET" ]; then
    # Directory - find all JSON files
    while IFS= read -r file; do
      validate_file "$file"
    done < <(find "$TARGET" -name "*.json" -type f | sort)
  else
    log_error "Invalid target: $TARGET"
    exit 1
  fi

  # Print summary
  print_summary
}

# Run main
main "$@"
