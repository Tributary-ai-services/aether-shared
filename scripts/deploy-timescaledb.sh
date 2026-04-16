#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INFRA_DIR="$SCRIPT_DIR/../k8s-shared-infrastructure"
MIGRATIONS_DIR="$INFRA_DIR/timescaledb-migrations"
NAMESPACE="tas-shared"

echo "=== Deploying TimescaleDB for TAS Events Pipeline ==="

# 1. Apply StatefulSet + Service + Secret
echo "Applying TimescaleDB StatefulSet..."
kubectl apply -f "$INFRA_DIR/timescaledb.yaml"

# 2. Wait for TimescaleDB to be ready
echo "Waiting for TimescaleDB pod to be ready..."
kubectl wait --for=condition=ready pod -l app=timescaledb-shared -n "$NAMESPACE" --timeout=120s

# 3. Create ConfigMap from SQL migration files
echo "Creating migrations ConfigMap..."
kubectl create configmap timescaledb-migrations \
  -n "$NAMESPACE" \
  --from-file="$MIGRATIONS_DIR/001_events.sql" \
  --from-file="$MIGRATIONS_DIR/002_aggregates.sql" \
  --from-file="$MIGRATIONS_DIR/003_retention.sql" \
  --dry-run=client -o yaml | kubectl apply -f -

# 4. Delete previous migration job if it exists (jobs are immutable)
kubectl delete job timescaledb-migrate -n "$NAMESPACE" --ignore-not-found

# 5. Run migration job
echo "Running migrations..."
kubectl apply -f "$INFRA_DIR/timescaledb-migrations-job.yaml"

# 6. Wait for migration to complete
echo "Waiting for migrations to finish..."
kubectl wait --for=condition=complete job/timescaledb-migrate -n "$NAMESPACE" --timeout=120s

echo "=== TimescaleDB deployment complete ==="
echo ""
echo "Verify with:"
echo "  kubectl exec -n $NAMESPACE statefulset/timescaledb-shared -- psql -U tas_events_rw -d tas_events -c '\\d+ events'"
echo "  kubectl exec -n $NAMESPACE statefulset/timescaledb-shared -- psql -U tas_events_rw -d tas_events -c 'SELECT * FROM timescaledb_information.continuous_aggregates'"
