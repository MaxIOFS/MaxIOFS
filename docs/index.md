---
hide:
  - navigation
---

# MaxIOFS

**Self-hosted, S3-compatible object storage in a single binary.** The web
console, multi-tenancy, IAM, SSO, Object Lock, replication and clustering are
built in: no external database, no separate console process, no cloud account.

![The MaxIOFS console](assets/screenshots/dashboard-light.png#only-light)
![The MaxIOFS console](assets/screenshots/dashboard-dark.png#only-dark)

## Start here

<div class="grid cards" markdown>

-   :material-download: **Install**

    ---

    Binary, Debian/RPM package or Docker image. Running in two minutes.

    [:octicons-arrow-right-24: Installation](getting-started/installation.md)

-   :material-rocket-launch: **First steps**

    ---

    Sign in, secure the administrator, create a bucket and upload your first
    object with the AWS CLI.

    [:octicons-arrow-right-24: First Steps](getting-started/first-steps.md)

-   :material-monitor-dashboard: **Use the console**

    ---

    Every page of the web console, with screenshots: buckets, objects, users,
    IAM, identity providers, settings and the cluster.

    [:octicons-arrow-right-24: Console Guide](console/index.md)

-   :material-console: **Use S3 tools**

    ---

    The AWS CLI, rclone, s3cmd, SDKs, presigned URLs and temporary credentials.

    [:octicons-arrow-right-24: S3 & Tools](s3/aws-cli.md)

-   :material-server-network: **Deploy for production**

    ---

    systemd, Docker, TLS behind a reverse proxy, and a cluster of up to five
    nodes with copies or parity.

    [:octicons-arrow-right-24: Deployment](deployment/single-node.md)

-   :material-wrench: **Operate it**

    ---

    Backups, disaster recovery, integrity scans, key rotation, monitoring and
    troubleshooting.

    [:octicons-arrow-right-24: Operations](operations/index.md)

</div>

## What you get

| Area | Included |
|------|----------|
| **S3 API** | Buckets and objects, multipart uploads, presigned URLs (V2 and V4), versioning, Object Lock, lifecycle, CORS, bucket policies, ACLs, tagging, notifications, static websites, S3 Select, replication |
| **Access control** | Users, groups and tenants, the AWS IAM protocol (policies, roles, `AssumeRole`), STS temporary credentials, two-factor authentication |
| **Identity providers** | LDAP / Active Directory and OAuth2 / OIDC (Keycloak, Google, Microsoft), group mappings, hourly Auto Sync |
| **Data protection** | Encryption at rest always on (AES-256-GCM, per-object keys), SSE-C, background integrity scans, a recovery tool that rebuilds the index from disk |
| **Cluster** | Up to five nodes, 1 to 3 copies or Reed-Solomon parity, storage classes for tiering, any node serves any bucket |
| **Operations** | Audit log with CSV export and syslog, Prometheus metrics and a Grafana dashboard, email alerts, maintenance mode |

## Ports

| Port | Serves |
|------|--------|
| `8080` | The S3 API (`aws`, SDKs, backup software) |
| `8081` | The web console and its API |
| `8082` | Traffic between the nodes of a cluster, only once a cluster is set up. Never expose it to clients. |

!!! tip "Default sign-in"
    A new installation signs in with **admin / admin**. Change the password
    and download the encryption recovery bundle right after the first sign-in:
    [First Steps](getting-started/first-steps.md) shows how.
