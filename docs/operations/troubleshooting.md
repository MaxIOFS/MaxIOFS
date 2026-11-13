# Troubleshooting

## Where to look

```bash
sudo journalctl -u maxiofs -n 200            # package or systemd install
docker logs --tail 200 maxiofs               # Docker
maxiofs --data-dir ./data --log-level debug  # foreground, verbose
```

The **Audit Logs** page shows refused sign-ins and requests, and the bell in
the console shows alerts.

## The server does not start

| Log says | Fix |
|----------|-----|
| `address already in use` | Another process holds 8080 or 8081: `ss -ltnp \| grep -E '8080\|8081'` |
| `permission denied` on the data directory | `sudo chown -R maxiofs:maxiofs /var/lib/maxiofs` |
| A YAML error | Check `config.yaml` indentation; quote values with `:` or `#` |
| An interrupted write could not be rolled back | Stop it and run [`maxiofs reconcile`](../cli/index.md#reconcile): it reports what it could not resolve and keeps those copies on disk for review |
| `pebble: corruption` | The metadata index is damaged: rebuild it with [`maxiofs recover`](backup-recovery.md#rebuilding-a-lost-index) |

## Cannot reach the console

```bash
curl -i http://127.0.0.1:8081/api/v1/health
```

- **Answers locally, not from outside**: the firewall, or `console_listen`
  bound to `127.0.0.1`.
- **Through a proxy, blank page or errors**: check the proxy forwards to
  8081 and passes `Host` and `X-Forwarded-Proto`.

## Cannot sign in

| Symptom | Cause |
|---------|-------|
| "Account locked" | Too many failed attempts. Wait for `security.lockout_duration`, or an administrator clicks **Unlock account** on the user. |
| Too many attempts from one address | The sign-in rate limit per IP; it counts failures only |
| An identity provider user is refused | They are in no mapped group and were not imported: see [Identity Providers](../console/identity-providers.md) |
| An identity provider is down | Sign-ins through it answer `503`; local accounts still work |

The administrator password can also be changed by another global
administrator. Create a second administrator account and keep it for
emergencies.

## S3 clients fail

| Error | Cause |
|-------|-------|
| `SignatureDoesNotMatch` | Wrong secret key, or a proxy rewrote `Host` |
| `InvalidAccessKeyId` | The key was deleted or belongs to another installation |
| `RequestTimeTooSkewed` | Client and server clocks differ by more than 15 minutes: run NTP on both |
| `AccessDenied` | The user's [permissions](../console/iam.md), the bucket policy, Block Public Access, or Object Lock refuse it. The audit log names which. |
| `QuotaExceeded` | The bucket's or tenant's quota is full |
| `SlowDown` | The server asks the client to back off for a moment; clients retry by themselves |
| `507 InsufficientStorage` | The disk is 99.5% full. Reads and deletes still work: delete data, add a lifecycle rule, or grow the volume |
| `NoSuchBucket` with virtual-hosted addresses | Wildcard DNS or `public_api_url` missing: use path style, see [rclone, s3cmd and mc](../s3/tools.md#path-style-and-virtual-hosted-style) |

## The disk fills up

1. **Dashboard → Top Buckets** shows where the space goes.
2. Old versions are often the cause in versioned buckets: add a lifecycle
   rule with **noncurrent version expiration**.
3. Abandoned multipart uploads hold space too: a rule that **aborts
   incomplete uploads**, or `storage.abort_incomplete_multipart_days` for all
   buckets.

Alerts fire at the thresholds of **Settings → System** before writes stop.

## A cluster node shows unhealthy

- Check the node is running and its port 8082 is reachable from the others.
- Check the clocks: all nodes must run NTP.
- A node that answers again returns to service by itself and is caught up.

The full cluster runbook is in [Operations](index.md#cluster-incidents).
