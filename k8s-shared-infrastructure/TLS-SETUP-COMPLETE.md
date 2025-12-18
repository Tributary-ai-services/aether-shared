# TLS Certificate Setup Complete ✅

## Summary

All TAS services now have valid TLS certificates issued by a self-signed Certificate Authority (CA). This provides:

- ✅ **Encrypted HTTPS connections** for all services
- ✅ **Works with private IP addresses** (no public exposure needed)
- ✅ **No external dependencies** (no Let's Encrypt, no DNS provider API)
- ✅ **10-year validity** for the root CA
- ✅ **Automatic renewal** handled by cert-manager

## Certificate Status

All 9 certificates are **READY**:

```
NAME                READY   SECRET
alertmanager-tls    True    alertmanager-tls
dashboard-tls       True    dashboard-tls
grafana-tls         True    grafana-tls
keycloak-tls        True    keycloak-tls
loki-tls            True    loki-tls
minio-api-tls       True    minio-api-tls
minio-console-tls   True    minio-console-tls
pgadmin-tls         True    pgadmin-tls
prometheus-tls      True    prometheus-tls
```

## Services with HTTPS

All services are now accessible via HTTPS:

- https://keycloak.tas.scharber.com - Identity Management
- https://grafana.tas.scharber.com - Dashboards & Visualization
- https://prometheus.tas.scharber.com - Metrics Collection
- https://loki.tas.scharber.com - Log Aggregation
- https://dashboard.tas.scharber.com - Services Dashboard
- https://pgadmin.tas.scharber.com - PostgreSQL Management
- https://minio.tas.scharber.com - Object Storage Console
- https://minio-api.tas.scharber.com - MinIO S3 API
- https://alerts.tas.scharber.com - AlertManager

## Trust the Root CA Certificate

To avoid browser security warnings, you need to add the TAS Root CA to your system's trusted certificates.

### Root CA Certificate Location

The root CA certificate has been exported to:
```
/home/jscharber/eng/TAS/aether-shared/k8s-shared-infrastructure/tas-root-ca.crt
```

### Installation Instructions

#### Windows
1. Double-click `tas-root-ca.crt`
2. Click "Install Certificate"
3. Select "Local Machine" → Next
4. Select "Place all certificates in the following store" → Browse
5. Select "Trusted Root Certification Authorities" → OK
6. Click Next → Finish
7. Restart your browser

#### macOS
1. Double-click `tas-root-ca.crt`
2. Keychain Access will open
3. Select "System" keychain
4. Double-click the "TAS Root CA" certificate
5. Expand "Trust" section
6. Set "When using this certificate" to "Always Trust"
7. Close and enter your password
8. Restart your browser

#### Linux (Ubuntu/Debian)
```bash
sudo cp tas-root-ca.crt /usr/local/share/ca-certificates/tas-root-ca.crt
sudo update-ca-certificates
```

#### Linux (RHEL/CentOS/Fedora)
```bash
sudo cp tas-root-ca.crt /etc/pki/ca-trust/source/anchors/
sudo update-ca-trust
```

#### Firefox (All Platforms)
Firefox uses its own certificate store:
1. Settings → Privacy & Security
2. Scroll to "Certificates" → View Certificates
3. Authorities tab → Import
4. Select `tas-root-ca.crt`
5. Check "Trust this CA to identify websites"
6. Click OK

### Copy to Windows Host (from WSL)

If you're using WSL, copy the certificate to Windows:
```bash
cp /home/jscharber/eng/TAS/aether-shared/k8s-shared-infrastructure/tas-root-ca.crt /mnt/c/Users/YourUsername/Downloads/
```

Then install it on Windows following the instructions above.

## Technical Details

### Certificate Hierarchy

```
TAS Root CA (self-signed, 10 years)
  └─> tas-ca-issuer (ClusterIssuer)
      ├─> alertmanager-tls
      ├─> dashboard-tls
      ├─> grafana-tls
      ├─> keycloak-tls
      ├─> loki-tls
      ├─> minio-api-tls
      ├─> minio-console-tls
      ├─> pgadmin-tls
      └─> prometheus-tls
```

### Automatic Renewal

cert-manager automatically renews certificates:
- Service certificates: Valid for 90 days, renewed when 30 days remain
- Root CA: Valid for 10 years, renewed when 1 year remains

### New Services

For new services that need TLS, add this annotation to the Ingress:

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: tas-ca-issuer
```

## Switching to Let's Encrypt (Optional)

If you later want publicly-trusted Let's Encrypt certificates, you'll need:

1. **Public IP + Port Forwarding** (for HTTP-01 challenges)
   - Update DNS to point to your public IP (76.159.130.76)
   - Forward ports 80 and 443 to 192.168.68.240
   - Use `letsencrypt-prod` ClusterIssuer

2. **DNS Provider API** (for DNS-01 challenges)
   - If Domains Priced Right provides API access
   - Configure appropriate webhook
   - Use DNS-01 solver in ClusterIssuer

For now, self-signed certificates are perfect for:
- Internal services
- Private networks
- Development/staging environments
- Services not exposed to the public internet

## Verification

Test HTTPS is working:
```bash
# Check certificate status
kubectl get certificate -n tas-shared

# Test HTTPS endpoint (with CA verification disabled for testing)
curl -k https://keycloak.tas.scharber.com

# View certificate details
kubectl describe certificate keycloak-tls -n tas-shared

# Check certificate expiration
kubectl get certificate keycloak-tls -n tas-shared -o jsonpath='{.status.notAfter}'
```

## Files Created

- `self-signed-issuer.yaml` - ClusterIssuer and root CA configuration
- `tas-root-ca.crt` - Exported root CA certificate for browser trust
- `TLS-SETUP-COMPLETE.md` - This documentation

## Security Notes

- The root CA private key is stored in Kubernetes secret `tas-root-ca-secret` in the `cert-manager` namespace
- Only administrators with kubectl access can issue certificates
- Consider rotating the root CA every few years for security best practices
- Service certificates automatically rotate every 60 days (renewed at 30 days before expiry)

## Troubleshooting

### Browser still shows security warning
- Ensure you installed the CA certificate in the correct trust store
- Restart your browser completely
- Clear browser cache and SSL state
- Check the certificate was imported: Chrome → Settings → Security → Manage certificates

### Certificate not issuing for new service
```bash
# Check certificate status
kubectl describe certificate <name> -n <namespace>

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager

# Verify issuer is ready
kubectl get clusterissuer tas-ca-issuer
```

### Need to re-issue all certificates
```bash
kubectl delete certificate -n tas-shared --all
# cert-manager will automatically recreate them
```

---

**Status**: ✅ TLS is fully configured and operational
**Next Steps**: Deploy application services (aether-be, tas-agent-builder, tas-llm-router)
