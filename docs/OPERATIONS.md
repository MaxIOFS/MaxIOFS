# MaxIOFS Operations Guide

**Version**: 0.9.2-beta  
**Last Updated**: February 28, 2026  
**Audience**: SRE / Ops / On-call engineers

This document describes day‑to‑day operations, runbooks, and best practices for running MaxIOFS in production (single node and clusters).

---

## Table of Contents

1. [Operational Overview](#operational-overview)  
2. [Environments & Responsibilities](#environments--responsibilities)  
3. [Daily Checks](#daily-checks)  
4. [Monitoring & Alerting](#monitoring--alerting)  
5. [Node Lifecycle](#node-lifecycle)  
6. [Cluster Incidents](#cluster-incidents)  
7. [Capacity Management](#capacity-management)  
8. [Backups & Disaster Recovery](#backups--disaster-recovery)  
9. [Maintenance & Upgrades](#maintenance--upgrades)  
10. [Security Operations](#security-operations)  
11. [Data Integrity](#data-integrity)  
12. [Troubleshooting Checklist](#troubleshooting-checklist)

---

## Operational Overview

MaxIOFS is a single‑binary, S3‑compatible object storage server with:

- Local filesystem for object data (`objects/` under `--data-dir`)
- Pebble for metadata (`metadata/` under `--data-dir`)
- SQLite for auth, cluster, replication and settings (`db/maxiofs.db`)
- Separate SQLite database for audit logs (`audit.db`)
- Optional cluster mode with multiple nodes and automatic synchronization

From an operational perspective:

- Treat each **data directory** as a unit: `db/`, `metadata/`, `objects/`, `audit.db` must be kept consistent together.
- In cluster mode, each node has its own local data directory, with **cluster replication** keeping configuration and metadata in sync.
- External replication to S3 (AWS/MinIO/MaxIOFS) is configured per bucket and is independent from the internal cluster replication.

For architecture details see `ARCHITECTURE.md` and `CLUSTER.md`. For deployment details see `DEPLOYMENT.md`.

---

## Environments & Responsibilities

### Typical Environments

- **Development**: Single node, ephemeral data, frequent restarts.
- **Staging**: Single node or small cluster (2–3 nodes), mirrors production configuration as closely as possible.
- **Production**: Cluster (minimum 2 nodes behind a load balancer), dedicated storage, full monitoring, and backup strategy.

### Responsibilities

- **SRE / Ops**
  - Deploy and upgrade MaxIOFS.
  - Manage capacity, scaling and storage.
  - Configure monitoring and alerting.
  - Manage nodes in the cluster (add/remove, maintenance).
  - Operate backup and disaster recovery procedures.

- **Security / IAM**
  - Enforce authentication and authorization policies.
  - Review audit logs and respond to security events.
  - Manage identity providers (LDAP/OIDC) and 2FA policies.

- **Application Owners / Tenants**
  - Manage buckets, data lifecycle and access keys within their tenant.
  - Respond to quota and capacity warnings for their tenant.

---

## Daily Checks

The following checks are recommended at least once per day in production.

### 1. Console Health

- Confirm the **Console** is reachable:
  - `https://<console-endpoint>:8081`
  - Dashboard loads without degraded status indicators.

### 2. Cluster Health (if enabled)

- In the **Cluster** section of the Console:
  - All nodes are `Healthy`.
  - No node is marked as `Stale` or `Out of sync`.
  - Latency and replication delay are within acceptable bounds.

### 3. Disk & Capacity

- Check disk usage on each node:

  ```bash
  df -h /path/to/data-dir
  ```

- Ensure you are comfortably below your alert thresholds (see [Capacity Management](#capacity-management)).

### 4. Alerts & Notifications

- Review:
  - **Disk space alerts** (warning/critical thresholds).
  - **Tenant quota alerts** (80%/90% consumption).
  - **Data integrity alerts** (if enabled).
  - **Replication failures** or lag.

### 5. Errors & Logs

- Check:
  - Application logs (local log files, journald, or external log targets).
  - Prometheus alerts (if configured).
  - External syslog / HTTP log receivers (if configured).

---

## Monitoring & Alerting

MaxIOFS exposes metrics for Prometheus and provides a reference Grafana dashboard (see `PERFORMANCE.md` and `DEPLOYMENT.md`).

### Key Metrics to Monitor

You should monitor at least:

- **System health**
  - Node up/down status (Prometheus `up` metric for `maxiofs`).
  - HTTP 5xx rate for the S3 and Console endpoints.
  - Latency percentiles (p95, p99) for S3 operations.

- **Storage capacity**
  - Disk usage for the MaxIOFS data volume.
  - Per‑bucket and per‑tenant used bytes (exposed via metrics and Console).
  - Number of objects per bucket.

- **Cluster**
  - Node health and last heartbeat.
  - Replication queue length and lag.
  - Stale node detection (any node marked as stale).

- **Security**
  - Count of failed logins.
  - Lockouts due to policy enforcement.
  - Audit events for privileged actions (tenant changes, IDP changes, bucket permissions).

### Recommended Alerts

Indicative thresholds (tune for your environment):

- **Disk space**
  - Warning: `>= 80%` used (`WARNING` event and dashboard banner).
  - Critical: `>= 90%` used (`CRITICAL` event; plan immediate action).

- **Tenant quota**
  - Warning: tenant at `>= 80%` of `MaxStorageBytes`.
  - Critical: tenant at `>= 90%` of `MaxStorageBytes`.

- **Service health**
  - Any node `up == 0` for more than 2 consecutive scrape intervals.
  - S3 API 5xx error rate > 1% over 5 minutes.

- **Security**
  - Excessive failed logins from a single IP over a short period.
  - Sudden spike in delete operations or permission changes.

---

## Node Lifecycle

This section covers how to add, remove, and handle nodes in a cluster.

### Adding a New Node

1. **Provision host**
   - Install OS packages and storage according to `DEPLOYMENT.md`.
   - Ensure network connectivity to all existing nodes and the load balancer.

2. **Deploy MaxIOFS**
   - Install the same **MaxIOFS version** as existing nodes.
   - Configure `config.yaml` with:
     - Cluster enabled.
     - Cluster node token (HMAC secret) shared between nodes.
     - Correct cluster endpoints for peer nodes.

3. **Start the node**
   - Start the MaxIOFS service.
   - Confirm it appears as `Healthy` in the Cluster dashboard.

4. **Integrate with load balancer**
   - Add the node to the load balancer backend pool.
   - Enable traffic gradually if your LB supports slow‑start.

### Planned Node Removal (Decommission)

**Goal**: Safely remove a node from the cluster without losing data or disrupting clients.

1. **Drain traffic**
   - Remove the node from the load balancer or set its weight to 0.
   - Wait for a grace period (e.g., 5–10 minutes) to allow in‑flight requests to finish.

2. **Migrate buckets (if node holds primary buckets)**
   - From the Console:
     - Use the **Bucket Migration** feature to move buckets off the node to other nodes.
     - Wait for each migration to complete successfully.

3. **Verify cluster state**
   - Ensure no buckets remain assigned to the node in the Cluster dashboard.
   - Confirm replication queues are empty or at expected levels.

4. **Remove from cluster configuration**
   - Mark the node as removed in the Cluster settings (see `CLUSTER.md` for exact steps).

5. **Stop the node**
   - Stop the MaxIOFS service on that host.
   - Optionally archive or delete its data directory once you are certain it is no longer needed.

### Handling an Unplanned Node Failure

A node may fail due to host crash, network outage, or hardware issues.

1. **Immediate response**
   - Alerts should fire when a node is `down` or missing heartbeats.
   - Verify whether the problem is **node‑local** (host crash) or **network** (partition).

2. **Client impact**
   - Requests routed via the load balancer should automatically fail over to healthy nodes.
   - If latency or 5xx errors increase, you may need to:
     - Reduce concurrency from clients temporarily.
     - Scale out by adding a replacement node.

3. **Recovery options**
   - **Host can be repaired**:
     - Bring the node back online.
     - Ensure correct configuration and clock.
     - Let the **stale reconciler** run if the node was offline for a long period (see [Cluster Incidents](#cluster-incidents)).
   - **Host is lost permanently**:
     - Remove it from the load balancer.
     - Treat it as a decommissioned node (as above).
     - Rely on remaining nodes and external replication/backup for data durability.

---

## Cluster Incidents

MaxIOFS includes logic to handle **stale nodes** and **network partitions** using a stale reconciler and tombstone‑based deletion sync. Understanding these concepts is key to safe operations.

### Scenario A – Node Offline (No Local Writes)

**Symptoms**:

- Node fully down (process stopped or host offline).
- No client traffic reaches this node while it is offline.

**Behavior**:

- Cluster continues to accept writes on surviving nodes.
- When the node returns:
  - It is detected as **stale** if it has been offline longer than the staleness threshold.
  - The **stale reconciler** fetches a **state snapshot** from peers and:
    - Applies missing configuration and metadata.
    - Applies tombstones for deletions (to prevent entity resurrection).
  - After reconciliation, the node clears the `stale` flag and re‑joins the cluster.

**Operator actions**:

- Confirm the node comes back as `Healthy` and not stale after reconciliation.
- Review logs for any reconciliation errors.

### Scenario B – Network Partition (Node Isolated but Serving Clients)

**Symptoms**:

- Node remains up and serving traffic.
- It is **isolated** from the rest of the cluster (no sync traffic).
- Both sides may receive independent writes.

**Risks**:

- Divergent state between the isolated node and the rest of the cluster.
- Potential conflicts when connectivity is restored.

**Behavior**:

- When connectivity returns and the node is detected as stale:
  - The stale reconciler compares **per‑entity timestamps** and tombstones:
    - Last‑write‑wins (LWW) for entities with `updated_at` timestamps (users, tenants, IDP, group mappings).
    - Tombstones always win for entities without `updated_at` (access keys, bucket permissions).

**Operator actions**:

- Investigate **why** the partition occurred (network incident, misconfiguration).
- After reconciliation:
  - Verify that mission‑critical entities (tenants, users, IDP, bucket permissions) have the expected final state.
  - If you suspect conflicting updates, review audit logs and consider manual correction.

---

## Capacity Management

### Disk Capacity

Each node stores:

- `db/` – SQLite database for core state.
- `audit.db` – Audit log database.
- `metadata/` – Pebble metadata (LSM tree).
- `objects/` – Actual object data (largest by far).

**Best practices**:

- Use dedicated storage (SSD recommended) for `--data-dir`.
- Monitor disk usage and set alerts at 80% and 90%.
- Plan **vertical scaling** (bigger disks) or **horizontal scaling** (more nodes) before you reach 90%.

### Tenant Quotas

- Tenants can have:
  - Maximum storage in bytes.
  - Maximum bucket and access key counts.
- When quotas are close to being reached:
  - Console shows visual indicators.
  - Alerts are sent to tenant and global admins.

**Operator actions**:

- Work with tenant owners to:
  - Increase quota.
  - Clean up old or unneeded data.
  - Implement or tighten lifecycle policies on buckets.

---

## Backups & Disaster Recovery

> **Important**: Backups must be **consistent** across `db/`, `audit.db`, `metadata/`, and `objects/`. Do not back up only one component.

### What to Back Up

On each node:

- `db/maxiofs.db` and related WAL/SHM files.
- `audit.db` and its associated files.
- `metadata/` directory (Pebble).
- `objects/` directory (actual data).
- Configuration files:
  - `/etc/maxiofs/config.yaml` (or equivalent for your deployment).

### Backup Strategies

Recommended approaches:

- **Filesystem‑level snapshots** (LVM, ZFS, cloud volume snapshots):
  - Preferred: create a **crash‑consistent** snapshot of the entire `--data-dir` volume.
  - Coordinate with MaxIOFS:
    - Optionally enable **maintenance mode** (see below) for short snapshot windows.

- **Application‑level cold backup**:
  - Stop MaxIOFS.
  - Copy `db/`, `audit.db`, `metadata/` and `objects/`.
  - Start MaxIOFS again.
  - This gives the strongest consistency guarantees but incurs downtime.

### Restore Procedure (Single Node)

1. **Provision a new host or clean data directory**.
2. **Stop MaxIOFS** if it is running.
3. **Restore backup** of:
   - `db/`, `audit.db`, `metadata/`, `objects/`, and config files.
4. **Start MaxIOFS**.
5. **Verify**:
   - Tenants, users, buckets and objects appear as expected.
   - S3 and Console are functional.

### Restore Procedure (Cluster)

Restores in a cluster are more complex. **Preferred** strategy:

- Avoid partial restores of individual nodes unless strictly necessary.
- Instead:
  - Bring up a **new node** from a backup and join it to the cluster.
  - Use **bucket migration** to move data off old nodes if needed.

If you must restore a specific node:

1. Remove the node from the load balancer.
2. Stop MaxIOFS on that node.
3. Restore its `--data-dir` from a consistent backup.
4. Start MaxIOFS.
5. Allow the stale reconciler and deletion log sync to reconcile state with peers.
6. Carefully verify that the cluster configuration and data view are correct.

---

## Maintenance & Upgrades

### Maintenance Mode

MaxIOFS supports a **maintenance mode** that:

- Allows **read‑only** S3 operations (GET/HEAD).
- Rejects mutating S3 operations (PUT/POST/DELETE) with an appropriate error.
- Blocks Console mutating APIs except for a small set of exempt endpoints (auth, health, settings, internal APIs, notifications).
- Shows a banner in the Console to all users.

**Recommended use cases**:

- Short maintenance windows:
  - Snapshotting or backing up storage volumes.
  - Minor configuration changes that may temporarily affect availability.
- Rolling upgrades where you want to minimize concurrent writes.

### Upgrade Procedure (Single Node)

1. **Plan a maintenance window**.
2. **Enable maintenance mode**.
3. **Deploy new MaxIOFS binary**:
   - Replace the binary or upgrade the package (DEB/RPM).
4. **Restart the service**.
5. **Verify**:
   - Node is healthy.
   - S3 and Console work as expected.
6. **Disable maintenance mode**.

### Rolling Upgrade (Cluster)

1. **One node at a time**:
   - Remove node from load balancer (drain).
   - Optionally enable per‑node maintenance mode.
2. **Upgrade and restart node**.
3. **Verify** the node is healthy and in sync.
4. **Re‑add node to load balancer**.
5. Repeat for remaining nodes.

> Always upgrade all nodes in a cluster to the same version as soon as practical.

---

## Security Operations

### Default Credentials

- Default admin credentials: `admin / admin`.
- **Must be changed immediately** after first login.
- Console shows a warning when the default password is still in use.

### Audit Logs

- Stored in a dedicated `audit.db` database.
- Includes:
  - Authentication attempts and logins.
  - Tenant, user, access key and permission changes.
  - IDP configuration changes.
  - Cluster and replication operations.

**Operational practices**:

- Regularly export or ship audit logs to an external system for long‑term retention.
- Define procedures for:
  - Investigating suspicious activity.
  - Correlating audit events with application and system logs.

### Identity Providers (LDAP / OIDC)

- Changes to IDP configuration can affect many users at once.
- Best practices:
  - Make changes first in a non‑production environment.
  - Use test connections in the Console where available.
  - Ensure at least one local admin account remains usable if IDP is misconfigured.

### Access Keys & Permissions

- Regular rotation of:
  - Access keys.
  - Node tokens used for cluster HMAC authentication.
- Use bucket policies and permission grants to enforce least privilege.
- Monitor permission changes via audit logs.

---

## Data Integrity

MaxIOFS provides mechanisms to verify the integrity of stored objects.

### Background Integrity Scrubber

- Periodically scans objects and verifies:
  - Checksums / ETags.
  - Presence of object data on disk.
- Logs:
  - Corrupted or missing objects as error events.
  - Summary metrics for integrity checks.
- May emit notifications or alerts to admins when issues are found.

### On‑Demand Bucket Integrity Check

- Admins can trigger on‑demand integrity checks for a specific bucket via:
  - Console action (if exposed).
  - HTTP API (`POST /buckets/{bucket}/verify-integrity` with optional query parameters).

**Recommended practice**:

- Run integrity checks:
  - After large migrations.
  - Periodically on critical buckets.
  - After storage incidents (e.g. filesystem or disk errors).

---

## Troubleshooting Checklist

### S3 API Failing or Slow

1. Check node health in Console.
2. Inspect Prometheus metrics:
   - 5xx rate, latency percentiles.
3. Review logs for:
   - Backend errors.
   - Network timeouts or DNS issues.
4. Verify:
   - Disk space is not exhausted.
   - No ongoing maintenance mode unintentionally enabled.

### Console Not Loading

1. Check the HTTP status on the Console port.
2. Inspect browser console and network tab.
3. Confirm:
   - TLS certificates (if used) are valid.
   - Reverse proxy or load balancer configuration is correct.

### Node Shows as Stale

1. Review `CLUSTER.md` and this document’s [Cluster Incidents](#cluster-incidents) section.
2. Inspect logs for stale reconciler activity.
3. Ensure:
   - Node’s time is synchronized (NTP).
   - Network between nodes is stable.

### Quota or Capacity Errors

1. Check tenant quota settings and usage in Console.
2. Verify lifecycle rules are working as expected.
3. Coordinate with tenant owners to clean up or increase quota.

### Integrity Check Failures

1. Identify affected buckets/objects from logs or API responses.
2. Determine if:
   - Data can be recovered from external replicas or backups.
   - The corruption is due to underlying storage issues (check system logs).
3. Consider:
   - Restoring affected objects from backup.
   - Migrating data off problematic storage.

---

For deeper architectural details, always refer back to:

- `ARCHITECTURE.md` – System components and data flow.  
- `CLUSTER.md` – Cluster behavior, replication, and bucket migration.  
- `SECURITY.md` – Security model and best practices.  
- `TESTING.md` – Test coverage and guidelines.

