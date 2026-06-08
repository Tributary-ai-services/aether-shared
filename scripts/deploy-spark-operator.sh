#!/bin/bash
set -e

NAMESPACE="tas-shared"

echo "=== Deploying Spark Operator for TAS Events Pipeline ==="

# 1. Add Spark Operator Helm repo
echo "Adding Spark Operator Helm repo..."
helm repo add spark-operator https://kubeflow.github.io/spark-operator 2>/dev/null || true
helm repo update

# 2. Install Spark Operator
echo "Installing Spark Operator..."
helm upgrade --install spark-operator spark-operator/spark-operator \
  --namespace "$NAMESPACE" \
  --set webhook.enable=true \
  --set sparkJobNamespace="$NAMESPACE" \
  --wait --timeout 120s

# 3. Verify CRD
echo "Verifying SparkApplication CRD..."
kubectl get crd sparkapplications.sparkoperator.k8s.io

# 4. Create MinIO bucket for checkpoints
echo "Creating spark-checkpoints MinIO bucket..."
kubectl run --rm -i minio-mc --image=minio/mc:latest --restart=Never \
  --command -- sh -c "
    mc alias set local http://minio-shared.${NAMESPACE}.svc.cluster.local:9000 minioadmin minioadmin123 && \
    mc mb local/spark-checkpoints --ignore-existing && \
    echo 'Bucket spark-checkpoints ready'
  " 2>/dev/null || echo "MinIO bucket creation skipped (may already exist)"

echo ""
echo "=== Spark Operator ready ==="
echo ""
echo "Deploy the events aggregator with:"
echo "  kubectl apply -f k8s-shared-infrastructure/spark/events-aggregator-sparkapp.yaml"
echo ""
echo "Check status with:"
echo "  kubectl get sparkapplication -n $NAMESPACE"
