#!/bin/bash

# cert-manager Testing and Validation Script
# This script tests cert-manager functionality comprehensively

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="tas-shared"
CERT_MANAGER_NAMESPACE="cert-manager"
TEST_DOMAIN="test.tas.yourdomain.com"  # CHANGE THIS

echo -e "${BLUE}🧪 Testing cert-manager Installation and Configuration${NC}"

# Function to run test and show result
run_test() {
    local test_name="$1"
    local test_command="$2"
    local expected_pattern="$3"
    
    echo -e "${YELLOW}Testing: $test_name${NC}"
    
    if output=$(eval "$test_command" 2>&1); then
        if [[ -z "$expected_pattern" ]] || echo "$output" | grep -q "$expected_pattern"; then
            echo -e "${GREEN}✅ PASS: $test_name${NC}"
            return 0
        else
            echo -e "${RED}❌ FAIL: $test_name - Pattern '$expected_pattern' not found${NC}"
            echo -e "${YELLOW}Output: $output${NC}"
            return 1
        fi
    else
        echo -e "${RED}❌ FAIL: $test_name - Command failed${NC}"
        echo -e "${YELLOW}Output: $output${NC}"
        return 1
    fi
}

# Function to wait for condition with timeout
wait_for_condition() {
    local description="$1"
    local command="$2"
    local expected_pattern="$3"
    local timeout="${4:-60}"
    local interval="${5:-5}"
    
    echo -e "${YELLOW}⏳ Waiting for: $description${NC}"
    
    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        if output=$(eval "$command" 2>/dev/null); then
            if [[ -z "$expected_pattern" ]] || echo "$output" | grep -q "$expected_pattern"; then
                echo -e "${GREEN}✅ Ready: $description${NC}"
                return 0
            fi
        fi
        
        echo -e "   Waiting... (${elapsed}s/${timeout}s)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done
    
    echo -e "${RED}❌ Timeout: $description${NC}"
    return 1
}

# Test 1: Check cert-manager pods are running
echo -e "\n${BLUE}📋 Test 1: cert-manager Pods Status${NC}"
run_test "cert-manager controller pod" "kubectl get pod -l app=cert-manager -n $CERT_MANAGER_NAMESPACE" "Running"
run_test "cert-manager cainjector pod" "kubectl get pod -l app=cainjector -n $CERT_MANAGER_NAMESPACE" "Running"
run_test "cert-manager webhook pod" "kubectl get pod -l app=webhook -n $CERT_MANAGER_NAMESPACE" "Running"

# Test 2: Check CRDs are installed
echo -e "\n${BLUE}📋 Test 2: Custom Resource Definitions${NC}"
run_test "Certificate CRD" "kubectl get crd certificates.cert-manager.io" "certificates.cert-manager.io"
run_test "ClusterIssuer CRD" "kubectl get crd clusterissuers.cert-manager.io" "clusterissuers.cert-manager.io"
run_test "Issuer CRD" "kubectl get crd issuers.cert-manager.io" "issuers.cert-manager.io"
run_test "CertificateRequest CRD" "kubectl get crd certificaterequests.cert-manager.io" "certificaterequests.cert-manager.io"

# Test 3: Check ClusterIssuers are created
echo -e "\n${BLUE}📋 Test 3: ClusterIssuers Status${NC}"
run_test "Let's Encrypt Staging ClusterIssuer" "kubectl get clusterissuer letsencrypt-staging" "letsencrypt-staging"
run_test "Let's Encrypt Production ClusterIssuer" "kubectl get clusterissuer letsencrypt-prod" "letsencrypt-prod"

# Test 4: Check webhook is responsive
echo -e "\n${BLUE}📋 Test 4: Webhook Validation${NC}"
run_test "Webhook configuration" "kubectl get validatingadmissionwebhook cert-manager-webhook" "cert-manager-webhook"
run_test "Mutating webhook configuration" "kubectl get mutatingadmissionwebhook cert-manager-webhook" "cert-manager-webhook"

# Test 5: Create test certificate
echo -e "\n${BLUE}📋 Test 5: Certificate Creation Test${NC}"

# Ensure tas-shared namespace exists
kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# Create a test certificate
echo -e "${YELLOW}Creating test certificate...${NC}"
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert-manager
  namespace: $NAMESPACE
  labels:
    test: cert-manager-validation
spec:
  secretName: test-cert-manager-tls
  issuerRef:
    name: letsencrypt-staging
    kind: ClusterIssuer
  dnsNames:
  - $TEST_DOMAIN
  duration: 2160h
  renewBefore: 360h
EOF

# Wait for certificate to be processed
wait_for_condition "Certificate to be created" "kubectl get certificate test-cert-manager -n $NAMESPACE" "test-cert-manager" 30 5

# Check certificate status
run_test "Certificate exists" "kubectl get certificate test-cert-manager -n $NAMESPACE" "test-cert-manager"

# Test 6: Check certificate request is created
echo -e "\n${BLUE}📋 Test 6: Certificate Request Processing${NC}"
wait_for_condition "CertificateRequest to be created" "kubectl get certificaterequest -n $NAMESPACE" "test-cert-manager" 60 5

if kubectl get certificaterequest -n $NAMESPACE -l cert-manager.io/certificate-name=test-cert-manager &>/dev/null; then
    CERT_REQUEST=$(kubectl get certificaterequest -n $NAMESPACE -l cert-manager.io/certificate-name=test-cert-manager -o jsonpath='{.items[0].metadata.name}')
    run_test "CertificateRequest created" "kubectl get certificaterequest $CERT_REQUEST -n $NAMESPACE" "$CERT_REQUEST"
    
    # Check certificate request status
    echo -e "${YELLOW}Certificate Request Status:${NC}"
    kubectl describe certificaterequest $CERT_REQUEST -n $NAMESPACE | grep -A 10 "Status:"
fi

# Test 7: Check ACME challenge creation (if domain is real and reachable)
echo -e "\n${BLUE}📋 Test 7: ACME Challenge Processing${NC}"

# Wait a bit for ACME processing to start
sleep 10

if kubectl get challenges -n $NAMESPACE &>/dev/null; then
    run_test "ACME Challenge created" "kubectl get challenges -n $NAMESPACE" ""
    
    echo -e "${YELLOW}Challenge Status:${NC}"
    kubectl get challenges -n $NAMESPACE -o wide 2>/dev/null || echo "No challenges found"
fi

if kubectl get orders -n $NAMESPACE &>/dev/null; then
    run_test "ACME Order created" "kubectl get orders -n $NAMESPACE" ""
    
    echo -e "${YELLOW}Order Status:${NC}"
    kubectl get orders -n $NAMESPACE -o wide 2>/dev/null || echo "No orders found"
fi

# Test 8: Check logs for errors
echo -e "\n${BLUE}📋 Test 8: Component Logs Check${NC}"

echo -e "${YELLOW}Checking cert-manager controller logs for errors...${NC}"
if kubectl logs -n $CERT_MANAGER_NAMESPACE deployment/cert-manager --tail=20 | grep -i error; then
    echo -e "${YELLOW}⚠️  Found errors in cert-manager controller logs${NC}"
else
    echo -e "${GREEN}✅ No errors in cert-manager controller logs${NC}"
fi

echo -e "${YELLOW}Checking cert-manager webhook logs for errors...${NC}"
if kubectl logs -n $CERT_MANAGER_NAMESPACE deployment/cert-manager-webhook --tail=20 | grep -i error; then
    echo -e "${YELLOW}⚠️  Found errors in cert-manager webhook logs${NC}"
else
    echo -e "${GREEN}✅ No errors in cert-manager webhook logs${NC}"
fi

# Test 9: Test self-signed issuer functionality
echo -e "\n${BLUE}📋 Test 9: Self-Signed Certificate Test${NC}"

# Create self-signed issuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: test-selfsigned
  namespace: $NAMESPACE
spec:
  selfSigned: {}
EOF

# Create self-signed certificate
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-selfsigned-cert
  namespace: $NAMESPACE
spec:
  secretName: test-selfsigned-cert-tls
  issuerRef:
    name: test-selfsigned
    kind: Issuer
  dnsNames:
  - test-selfsigned.local
  duration: 24h
EOF

# Wait for self-signed certificate
wait_for_condition "Self-signed certificate to be ready" "kubectl get certificate test-selfsigned-cert -n $NAMESPACE -o jsonpath='{.status.conditions[?(@.type==\"Ready\")].status}'" "True" 30 5

if kubectl get secret test-selfsigned-cert-tls -n $NAMESPACE &>/dev/null; then
    echo -e "${GREEN}✅ Self-signed certificate created successfully${NC}"
    
    # Verify certificate content
    if kubectl get secret test-selfsigned-cert-tls -n $NAMESPACE -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout | grep -q "test-selfsigned.local"; then
        echo -e "${GREEN}✅ Certificate contains correct DNS name${NC}"
    else
        echo -e "${YELLOW}⚠️  Certificate may not contain expected DNS name${NC}"
    fi
else
    echo -e "${RED}❌ Self-signed certificate secret not created${NC}"
fi

# Test 10: Resource cleanup test
echo -e "\n${BLUE}📋 Test 10: Resource Cleanup${NC}"

echo -e "${YELLOW}Cleaning up test resources...${NC}"
kubectl delete certificate test-cert-manager test-selfsigned-cert -n $NAMESPACE --ignore-not-found=true
kubectl delete issuer test-selfsigned -n $NAMESPACE --ignore-not-found=true
kubectl delete secret test-cert-manager-tls test-selfsigned-cert-tls -n $NAMESPACE --ignore-not-found=true

# Clean up any leftover certificate requests
kubectl delete certificaterequests -n $NAMESPACE -l cert-manager.io/certificate-name=test-cert-manager --ignore-not-found=true
kubectl delete certificaterequests -n $NAMESPACE -l cert-manager.io/certificate-name=test-selfsigned-cert --ignore-not-found=true

echo -e "${GREEN}✅ Test resources cleaned up${NC}"

# Summary
echo -e "\n${BLUE}📊 Test Summary${NC}"
echo -e "cert-manager testing completed!"
echo ""
echo -e "${GREEN}🎉 If all tests passed, cert-manager is ready for use!${NC}"
echo ""
echo -e "${YELLOW}📝 Next Steps:${NC}"
echo -e "   1. Update domain names in ingress.yaml to match your actual domains"
echo -e "   2. Update email addresses in ClusterIssuer configurations"
echo -e "   3. Configure DNS records to point to your cluster LoadBalancer"
echo -e "   4. Test with real domains using staging issuer first"
echo -e "   5. Switch to production issuer after successful testing"
echo ""
echo -e "${BLUE}🔗 Useful Commands:${NC}"
echo -e "   Watch certificates: kubectl get certificates -n $NAMESPACE -w"
echo -e "   Describe certificate: kubectl describe certificate <cert-name> -n $NAMESPACE"
echo -e "   Check certificate requests: kubectl get certificaterequests -n $NAMESPACE"
echo -e "   Check challenges: kubectl get challenges -n $NAMESPACE"
echo -e "   Check orders: kubectl get orders -n $NAMESPACE"