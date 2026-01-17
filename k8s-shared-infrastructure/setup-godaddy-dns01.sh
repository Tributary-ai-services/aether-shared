#!/bin/bash
set -e

echo "========================================="
echo "Setting up GoDaddy DNS-01 ACME Solver"
echo "========================================="
echo ""

# Check if API credentials are provided
if [ -z "$GODADDY_API_KEY" ] || [ -z "$GODADDY_API_SECRET" ]; then
    echo "ERROR: GoDaddy API credentials not set!"
    echo ""
    echo "Please set the following environment variables:"
    echo "  export GODADDY_API_KEY='your-api-key'"
    echo "  export GODADDY_API_SECRET='your-api-secret'"
    echo ""
    echo "Get your API keys from: https://developer.godaddy.com/keys"
    echo "Make sure to use 'Production' environment keys"
    exit 1
fi

echo "✓ API credentials found"
echo ""

# Step 1: Add the cert-manager-webhook-godaddy Helm repository
echo "Step 1/5: Adding Helm repository..."
helm repo add cert-manager-webhook-godaddy https://fred78290.github.io/cert-manager-webhook-godaddy
helm repo update

echo "✓ Helm repository added"
echo ""

# Step 2: Install the webhook
echo "Step 2/5: Installing GoDaddy webhook..."
helm upgrade --install cert-manager-webhook-godaddy \
  cert-manager-webhook-godaddy/cert-manager-webhook-godaddy \
  --namespace cert-manager \
  --set groupName=acme.scharber.com \
  --wait

echo "✓ Webhook installed"
echo ""

# Step 3: Create the API credentials secret
echo "Step 3/5: Creating GoDaddy API credentials secret..."
kubectl create secret generic godaddy-api-key \
  --from-literal=api-key="$GODADDY_API_KEY" \
  --from-literal=api-secret="$GODADDY_API_SECRET" \
  --namespace cert-manager \
  --dry-run=client -o yaml | kubectl apply -f -

echo "✓ Secret created"
echo ""

# Step 4: Update the ClusterIssuer
echo "Step 4/5: Updating ClusterIssuer to use DNS-01..."
kubectl apply -f cert-manager.yaml

echo "✓ ClusterIssuer updated"
echo ""

# Step 5: Delete existing failed certificates to force retry
echo "Step 5/5: Cleaning up failed certificate requests..."
kubectl delete certificaterequest -n tas-shared --all
kubectl delete challenge -n tas-shared --all 2>/dev/null || true
kubectl delete order -n tas-shared --all 2>/dev/null || true

echo "✓ Cleanup complete"
echo ""

echo "========================================="
echo "Setup Complete!"
echo "========================================="
echo ""
echo "Certificates will now use DNS-01 challenges via GoDaddy."
echo ""
echo "Monitor certificate status with:"
echo "  kubectl get certificate -n tas-shared"
echo "  kubectl describe certificate <name> -n tas-shared"
echo ""
echo "Check webhook logs with:"
echo "  kubectl logs -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-godaddy"
echo ""
