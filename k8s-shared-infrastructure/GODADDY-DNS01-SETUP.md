# GoDaddy DNS-01 ACME Challenge Setup

This guide explains how to configure cert-manager to use DNS-01 challenges with GoDaddy for TLS certificate issuance with private IP addresses.

## Why DNS-01?

DNS-01 challenges allow Let's Encrypt to issue certificates without needing to reach your server over HTTP. This is essential when:
- Your k3s cluster uses private IP addresses (192.168.x.x)
- You don't want to expose your cluster to the public internet
- You can't configure port forwarding on your router

## Prerequisites

1. A GoDaddy account with DNS management access for `tas.scharber.com`
2. kubectl access to your k3s cluster
3. Helm 3 installed

## Step 1: Get GoDaddy API Credentials

1. Go to https://developer.godaddy.com/keys
2. Click "Create New API Key"
3. Name it something like "cert-manager-k3s"
4. **IMPORTANT**: Select **"Production"** environment (not "Test")
5. Copy both the **API Key** and **API Secret** immediately (you can't view the secret again)

## Step 2: Set Environment Variables

```bash
export GODADDY_API_KEY='your-api-key-here'
export GODADDY_API_SECRET='your-api-secret-here'
```

## Step 3: Run the Setup Script

```bash
cd /home/jscharber/eng/TAS/aether-shared/k8s-shared-infrastructure
./setup-godaddy-dns01.sh
```

This script will:
1. Add the cert-manager-webhook-godaddy Helm repository
2. Install the GoDaddy webhook in the cert-manager namespace
3. Create a Kubernetes secret with your API credentials
4. Update the letsencrypt-prod ClusterIssuer to use DNS-01
5. Clean up any failed certificate requests

## Step 4: Verify Installation

Check that the webhook is running:
```bash
kubectl get pods -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-godaddy
```

You should see a pod in "Running" state.

## Step 5: Monitor Certificate Issuance

Watch the certificates get issued:
```bash
watch kubectl get certificate -n tas-shared
```

The READY column should change from `False` to `True` within a few minutes.

## How DNS-01 Works

1. cert-manager requests a certificate from Let's Encrypt
2. Let's Encrypt responds with a DNS challenge token
3. The GoDaddy webhook creates a TXT record at `_acme-challenge.<domain>.tas.scharber.com`
4. Let's Encrypt queries DNS to verify the TXT record exists
5. Once verified, Let's Encrypt issues the certificate
6. The webhook automatically deletes the TXT record

## Troubleshooting

### Check webhook logs:
```bash
kubectl logs -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-godaddy
```

### Check certificate status:
```bash
kubectl describe certificate <certificate-name> -n tas-shared
```

### Check certificate request details:
```bash
kubectl describe certificaterequest -n tas-shared
```

### Check challenge details:
```bash
kubectl get challenge -n tas-shared
kubectl describe challenge <challenge-name> -n tas-shared
```

### Common Issues

**"API key not found" error:**
- Make sure you used "Production" keys, not "Test" keys
- Verify the secret was created correctly: `kubectl get secret godaddy-api-key -n cert-manager -o yaml`

**"Timeout waiting for DNS propagation":**
- GoDaddy DNS can take 1-10 minutes to propagate
- The webhook will automatically retry
- Check GoDaddy's DNS management to see if TXT records are being created/deleted

**"Invalid groupName":**
- The groupName in cert-manager.yaml must match the Helm installation
- Both should be set to `acme.scharber.com`

## Security Notes

- The GoDaddy API credentials are stored in a Kubernetes secret
- These credentials have full DNS management access to your domain
- Consider using a separate GoDaddy sub-account with DNS-only permissions
- Rotate API keys periodically for security

## Reverting to HTTP-01

If you need to revert to HTTP-01 challenges (requires public IP + port forwarding):

1. Edit `cert-manager.yaml` and replace the `dns01` solver with:
```yaml
    solvers:
    - http01:
        ingress:
          class: nginx
```

2. Apply the changes:
```bash
kubectl apply -f cert-manager.yaml
```

3. Uninstall the webhook (optional):
```bash
helm uninstall cert-manager-webhook-godaddy -n cert-manager
```

## References

- [GoDaddy Webhook GitHub](https://github.com/Fred78290/cert-manager-webhook-godaddy)
- [cert-manager DNS-01 Documentation](https://cert-manager.io/docs/configuration/acme/dns01/)
- [Let's Encrypt Challenge Types](https://letsencrypt.org/docs/challenge-types/)
