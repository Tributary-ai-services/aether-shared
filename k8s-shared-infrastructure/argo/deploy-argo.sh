#!/bin/bash
set -euo pipefail

# Deploy Argo Workflows + Argo Events to K3s cluster
# Prerequisites: kubectl configured, K3s running, MinIO available

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARGO_WF_VERSION="${ARGO_WF_VERSION:-v3.6.4}"
ARGO_EVENTS_VERSION="${ARGO_EVENTS_VERSION:-v1.9.3}"

echo "=== TAS Argo Infrastructure Deployment ==="
echo "Argo Workflows version: ${ARGO_WF_VERSION}"
echo "Argo Events version: ${ARGO_EVENTS_VERSION}"
echo ""

# -----------------------------------------------
# Step 1: Create namespaces
# -----------------------------------------------
echo "[1/8] Creating namespaces..."
kubectl apply -f "${SCRIPT_DIR}/argo-workflows/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/argo-events/namespace.yaml"

# -----------------------------------------------
# Step 2: Install Argo Workflows controller + server
# -----------------------------------------------
echo "[2/8] Installing Argo Workflows ${ARGO_WF_VERSION}..."
kubectl apply -n argo -f "https://github.com/argoproj/argo-workflows/releases/download/${ARGO_WF_VERSION}/install.yaml"

echo "  Waiting for Argo Workflows controller..."
kubectl rollout status deployment/workflow-controller -n argo --timeout=120s || true
kubectl rollout status deployment/argo-server -n argo --timeout=120s || true

# -----------------------------------------------
# Step 3: Install Argo Events controller
# -----------------------------------------------
echo "[3/8] Installing Argo Events ${ARGO_EVENTS_VERSION}..."
kubectl apply -n argo-events -f "https://github.com/argoproj/argo-events/releases/download/${ARGO_EVENTS_VERSION}/install.yaml"

echo "  Waiting for Argo Events controller..."
kubectl rollout status deployment/controller-manager -n argo-events --timeout=120s || true

# -----------------------------------------------
# Step 4: Apply RBAC
# -----------------------------------------------
echo "[4/8] Applying RBAC resources..."
kubectl apply -f "${SCRIPT_DIR}/rbac.yaml"

# -----------------------------------------------
# Step 5: Configure artifact repository (MinIO)
# -----------------------------------------------
echo "[5/8] Configuring artifact repository..."
kubectl apply -f "${SCRIPT_DIR}/argo-workflows/minio-credentials.yaml"
kubectl apply -f "${SCRIPT_DIR}/argo-workflows/artifact-repositories.yaml"
kubectl apply -f "${SCRIPT_DIR}/semaphore-configmap.yaml"

# Create argo-artifacts bucket in MinIO if it doesn't exist
echo "  Creating argo-artifacts bucket..."
kubectl run minio-setup --rm -i --restart=Never \
  --image=minio/mc:latest \
  --namespace=argo \
  -- sh -c '
    mc alias set minio http://minio-shared.tas-shared.svc.cluster.local:9000 minioadmin minioadmin123
    mc mb --ignore-existing minio/argo-artifacts
    echo "Bucket argo-artifacts ready"
  ' 2>/dev/null || echo "  (bucket may already exist)"

# -----------------------------------------------
# Step 6: Deploy EventBus
# -----------------------------------------------
echo "[6/8] Deploying EventBus..."
kubectl apply -f "${SCRIPT_DIR}/argo-events/minio-credentials.yaml"
kubectl apply -f "${SCRIPT_DIR}/argo-events/eventbus.yaml"

echo "  Waiting for EventBus to be ready..."
sleep 10
kubectl get eventbus -n argo-events || true

# -----------------------------------------------
# Step 7: Deploy EventSources
# -----------------------------------------------
echo "[7/8] Deploying EventSources..."
kubectl apply -f "${SCRIPT_DIR}/argo-events/event-sources/"

echo "  Waiting for EventSources..."
sleep 5
kubectl get eventsources -n argo-events || true

# -----------------------------------------------
# Step 8: Deploy Sensors and Ingress
# -----------------------------------------------
echo "[8/8] Deploying Sensors and Ingress..."
kubectl apply -f "${SCRIPT_DIR}/argo-events/sensors/"
kubectl apply -f "${SCRIPT_DIR}/argo-workflows/ingress.yaml"

echo ""
echo "=== Deployment Summary ==="
echo ""
echo "Argo Workflows:"
kubectl get pods -n argo --no-headers 2>/dev/null || echo "  (pending)"
echo ""
echo "Argo Events:"
kubectl get pods -n argo-events --no-headers 2>/dev/null || echo "  (pending)"
echo ""
echo "EventBus:"
kubectl get eventbus -n argo-events --no-headers 2>/dev/null || echo "  (pending)"
echo ""
echo "EventSources:"
kubectl get eventsources -n argo-events --no-headers 2>/dev/null || echo "  (pending)"
echo ""
echo "Sensors:"
kubectl get sensors -n argo-events --no-headers 2>/dev/null || echo "  (pending)"
echo ""
echo "Access Argo UI: https://argo.tas.scharber.com"
echo ""
echo "=== Done ==="
