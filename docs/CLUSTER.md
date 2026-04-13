# Multi-Node Cluster Management

**Version**: 1.2.0
**Status**: Production-Ready
**Last Updated**: April 12, 2026

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Quick Start](#quick-start)
4. [Cluster Setup](#cluster-setup)
5. [Configuration](#configuration)
6. [Cluster Replication](#cluster-replication)
7. [Bucket Migration](#bucket-migration)
8. [Dashboard UI](#dashboard-ui)
9. [API Reference](#api-reference)
10. [Security](#security)
11. [Monitoring & Health](#monitoring--health)
12. [Troubleshooting](#troubleshooting)
13. [Testing](#testing)

---

## Overview

MaxIOFS provides complete multi-node cluster support for high availability (HA) and automatic failover. Multiple MaxIOFS instances work together as a unified storage cluster with intelligent request routing, automatic health monitoring, and seamless failover. In v0.9.0-beta, cluster sync was extended to 6 entity types with tombstone-based deletion sync to prevent entity resurrection.

### Key Features

- ✅ Multi-node cluster support with smart routing
- ✅ **Dedicated cluster port 8082** — inter-node traffic is fully separated from S3 (8080) and console (8081)
- ✅ **Inter-node TLS encryption** — automatic, zero-configuration (v0.9.1)
- ✅ HMAC-authenticated node-to-node replication
- ✅ Automatic synchronization for 6 entity types (users, tenants, access keys, bucket permissions, IDPs, group mappings)
- ✅ Tombstone-based deletion sync (prevents entity resurrection)
- ✅ Health monitoring (30-second intervals)
- ✅ Bucket location cache (5ms vs 50ms latency)
- ✅ Bucket migration between nodes for capacity rebalancing
- ✅ **Stale node reconciler** (v1.0.0) — automatic recovery for nodes offline or network-partitioned
- ✅ Web-based cluster management dashboard

### Use Cases

1. **High Availability** - Automatic failover if primary node fails
2. **Geographic Distribution** - Nodes in different regions for low latency
3. **Disaster Recovery** - Replicate data to backup nodes
4. **Load Balancing** - Distribute requests across healthy nodes
5. **Zero-Downtime Maintenance** - Update nodes without service interruption

---

## Architecture

### Port Layout

Each MaxIOFS node exposes three independent ports:

| Port | Purpose | Exposed to |
|------|---------|-----------|
| **8080** | S3 API — object storage operations | S3 clients, load balancer |
| **8081** | Web Console — admin UI and console REST API | Operators, load balancer |
| **8082** | Cluster inter-node — coordination, sync, CSR signing | Other cluster nodes only (firewall off from public) |

### Cluster Components

```
┌──────────────────────────────────────────────────────┐
│         Load Balancer (HAProxy/Nginx)                │
│  :8080 → S3 API     :8081 → Web Console              │
└──────────────────────┬───────────────────────────────┘
                       │
          ┌────────────┴────────────┐
          │                         │
   ┌──────▼──────┐           ┌──────▼──────┐
   │   Node 1    │◄──:8082──►│   Node 2    │
   │  10.0.1.10  │  cluster   │  10.0.1.20  │
   │  S3  :8080  │  (HMAC+    │  S3  :8080  │
   │  UI  :8081  │   TLS)     │  UI  :8081  │
   │  CL  :8082  │            │  CL  :8082  │
   └─────────────┘            └─────────────┘
```

> **Note**: Port 8082 should **never** be exposed to end users or the public internet. Restrict it at the firewall to cluster node IPs only.

### Core Components

**1. Cluster Manager**
- Manages cluster configuration and state
- Handles node registration/removal
- Tracks nodes in SQLite database (`cluster_config`, `cluster_nodes`)

**2. Smart Router**
- Routes S3 requests to correct node
- Automatic failover to healthy nodes
- Maintains bucket location cache (5-minute TTL)
- Proxies requests to remote nodes when needed

**3. Health Checker**
- Monitors all nodes every 30 seconds
- Measures network latency
- Updates status: healthy (<1s), degraded (1-5s), unavailable (>5s)

**4. Bucket Location Cache**
- In-memory cache with 5-minute TTL
- Cache hit: 5ms latency, Cache miss: 50ms latency
- Automatic invalidation on bucket operations

---

## Quick Start

### Prerequisites

- 2+ MaxIOFS instances on different servers
- Network connectivity on all three ports (8080, 8081, 8082) between all nodes
- Firewall: port 8082 open between node IPs only (not to the public internet)
- Admin access to all nodes

### Setup Steps

**1. Start both nodes**

```bash
# Node 1 (IP: 10.0.1.10)
./maxiofs --data-dir /data/node1

# Node 2 (IP: 10.0.1.20)
./maxiofs --data-dir /data/node2
```

Both nodes start on their default ports: S3 on `:8080`, Console on `:8081`, Cluster on `:8082`.

**2. Initialize the cluster on Node 1**

```
# Open http://10.0.1.10:8081 → Cluster → Initialize Cluster
# Fill in:
#   Node Name: node-1
#   Region: us-east-1 (optional)
# → Cluster token is displayed — COPY AND SAVE IT
```

**3. Add Node 2 from Node 1's console**

This is the recommended method. From Node 1's console:

```
# Cluster → Nodes → Add Node
# Fill in:
#   Node IP Address: 10.0.1.20   (or 10.0.1.20:8081 to use a non-default port)
#   Admin Username:  admin
#   Admin Password:  <node 2 password>
```

The primary node authenticates to Node 2's console API (port 8081), triggers a cluster join, and Node 2 contacts Node 1's cluster port (8082) to register.

**Alternative: Join from Node 2's console**

If you prefer to initiate the join from Node 2 instead:

```
# Open http://10.0.1.20:8081 → Cluster → Join Existing Cluster
# Fill in:
#   Cluster Node IP Address: 10.0.1.10   (or 10.0.1.10:8082 to use a non-default port)
#   Cluster Token: <paste from step 2>
```

**4. Verify cluster**

Check the Cluster page on either node:
- Total Nodes: 2
- Healthy Nodes: 2
- Both nodes showing green status with latency < 10 ms

**5. Configure replication (optional — required for HA)**

Navigate to Cluster → Bucket Replication:
- Select bucket
- Choose destination node
- Set sync interval: 60 s for real-time HA, 300 s for near-real-time, 3600 s for hourly
- Enable "Replicate deletes" and "Replicate metadata"

---

## Cluster Setup

### Production Deployment

```
                ┌──────────────────┐
                │  Load Balancer   │
                │  192.168.1.100   │
                └────────┬─────────┘
                         │
            ┌────────────┴────────────┐
            │                         │
     ┌──────▼──────┐           ┌──────▼──────┐
     │   Node 1    │           │   Node 2    │
     │  10.0.1.10  │◄─────────►│  10.0.1.20  │
     └─────────────┘           └─────────────┘
```

### HAProxy Configuration

> Port 8082 is internal cluster coordination — do **not** route it through the load balancer. Each node must be able to reach the other nodes' port 8082 directly.

```haproxy
# /etc/haproxy/haproxy.cfg
global
    maxconn 4096
    daemon

defaults
    mode http
    timeout connect 5000ms
    timeout client 50000ms
    timeout server 50000ms

# S3 API (Port 8080) — client-facing
frontend s3_frontend
    bind *:8080
    default_backend s3_backend

backend s3_backend
    balance roundrobin
    option httpchk GET /health
    server node1 10.0.1.10:8080 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8080 check inter 10s fall 3 rise 2

# Web Console (Port 8081) — operator-facing
frontend console_frontend
    bind *:8081
    default_backend console_backend

backend console_backend
    balance roundrobin
    option httpchk GET /health
    server node1 10.0.1.10:8081 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8081 check inter 10s fall 3 rise 2

# Port 8082 — cluster inter-node communication
# DO NOT expose via HAProxy — nodes reach each other directly
```

### Network Configuration

**Firewall Rules:**

```bash
# Allow S3 and Console API from anywhere (or your client CIDR)
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -j ACCEPT

# Allow cluster inter-node communication only between cluster node IPs
iptables -A INPUT -s 10.0.1.10 -p tcp --dport 8082 -j ACCEPT  # From Node 1
iptables -A INPUT -s 10.0.1.20 -p tcp --dport 8082 -j ACCEPT  # From Node 2
iptables -A INPUT -p tcp --dport 8082 -j DROP                   # Block all others
```

**DNS Configuration:**

```bash
# /etc/hosts
10.0.1.10  node1
10.0.1.20  node2
192.168.1.100  maxiofs-cluster
```

---

## Configuration

### Server Config Options

| Config key | Default | Description |
|------------|---------|-------------|
| `cluster_listen` | `:8082` | Bind address for the cluster inter-node server. Change the port if 8082 is taken, or use `127.0.0.1:8082` to restrict to loopback (single-machine test clusters). |

```yaml
# config.yaml — cluster port (all other cluster config is managed via the web console)
cluster_listen: ":8082"
```

Environment variable equivalent: `MAXIOFS_CLUSTER_LISTEN=:8082`

### Cluster Initialization Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `node_name` | string | Yes | Human-readable name (e.g., "node-east-1") |
| `region` | string | No | Geographic region (e.g., "us-east-1") |
| `local_endpoint` | string | No | Override S3 API endpoint other nodes should use to reach this node. Leave empty to use `public_api_url`. |

### Add Node Parameters (from the primary node's console)

| Field | Default port | Description |
|-------|-------------|-------------|
| **Node IP Address** | 8081 | IP address (or `IP:port`) of the remote node's **console** port. Port 8081 is used if omitted. The primary node authenticates here, then tells the remote to join via port 8082. |
| **Admin Username** | — | Admin credentials on the remote node |
| **Admin Password** | — | Admin credentials on the remote node |

### Join Cluster Parameters (from the new node's console)

| Field | Default port | Description |
|-------|-------------|-------------|
| **Cluster Node IP Address** | 8082 | IP address (or `IP:port`) of **any existing cluster node's cluster port**. Port 8082 is used if omitted. |
| **Cluster Token** | — | Token displayed when the cluster was initialized |

### Health Check Configuration

- **Interval**: 30 seconds (hardcoded)
- **Timeout**: 5 seconds
- To modify: Edit `internal/cluster/manager.go` → `healthCheckInterval`

### Cache Configuration

- **TTL**: 5 minutes (hardcoded)
- To modify: Edit `internal/cluster/router.go` → `bucketCacheTTL`

---

## Cluster Replication

### Overview

Cluster replication enables **node-to-node replication** for HA. This is separate from user replication (external S3 backup).

**Key Differences:**

| Feature | Cluster Replication | User Replication |
|---------|---------------------|------------------|
| Purpose | HA between MaxIOFS nodes | Backup to external S3 |
| Authentication | HMAC with node_token | S3 access key + secret |
| Credentials | None required | AWS credentials required |
| Tenant Sync | Automatic (30s) | N/A |

### How It Works

1. Object PUT on Node 1
2. Decrypt (if encrypted)
3. Sign with HMAC-SHA256
4. Send to Node 2 (plaintext)
5. Node 2 verifies HMAC signature
6. Re-encrypt with Node 2's master key
7. Store on Node 2

**Encryption Keys**: Each node has its own master key. Objects are decrypted on source, re-encrypted on destination.

### HMAC Authentication

**Message Format:**
```
HMAC-SHA256(node_token, METHOD + PATH + TIMESTAMP + NONCE + BODY)
```

**Request Headers:**
```
X-MaxIOFS-Node-ID: sender-node-id
X-MaxIOFS-Timestamp: <unix-timestamp>
X-MaxIOFS-Nonce: <random-uuid>
X-MaxIOFS-Signature: <hex-encoded-hmac>
```

**Validation:**
- Retrieves node_token from database
- Computes expected signature
- Compares with provided signature (constant-time)
- Checks timestamp skew (max 5 minutes)

### Automatic Entity Synchronization (6 types)

All 6 entity types are **automatically synchronized** across all cluster nodes every 30 seconds:

| Entity Type | Endpoint | What Gets Synced |
|-------------|----------|------------------|
| **Users** | `/api/internal/cluster/user-sync` | Credentials, roles, tenant, preferences |
| **Tenants** | `/api/internal/cluster/tenant-sync` | Quotas, settings, status |
| **Access Keys** | `/api/internal/cluster/access-key-sync` | Key ID, secret, user association |
| **Bucket Permissions** | `/api/internal/cluster/bucket-permission-sync` | ACLs, policies |
| **IDP Providers** | `/api/internal/cluster/idp-provider-sync` | LDAP/OAuth config (encrypted secrets) |
| **Group Mappings** | `/api/internal/cluster/group-mapping-sync` | IDP group → MaxIOFS role mappings |

**How it works:**
- SHA256 checksum-based change detection (only syncs when data changes)
- HMAC-authenticated node-to-node communication
- 30-second sync interval per entity type

**Result:**
- Admin password is identical across all nodes
- Users created on one node are immediately available on all nodes
- User sessions work correctly after node failover
- IDP/SSO configurations available on all nodes

### Tombstone-Based Deletion Sync (v0.9.0-beta)

Deletions are synchronized using a **tombstone-based** approach to prevent entity resurrection in bidirectional sync.

**Problem solved:** Without tombstones, if you delete a user on Node1, Node2 still has it and would push it back to Node1 on the next sync cycle — causing the deleted entity to reappear indefinitely.

**How it works:**
1. When an entity is deleted on any node, a tombstone is recorded in `cluster_deletion_log`
2. Tombstones are synced to all other nodes alongside regular entity sync
3. When a node receives an entity via sync, it checks for a tombstone — if found, the entity is rejected
4. Tombstones are automatically cleaned up after 7 days
5. Endpoint: `POST /api/internal/cluster/deletion-log-sync` (HMAC-authenticated)

**Key design decisions:**
- Tombstones are authoritative: a deletion entry always wins over an item
- Single table for all 6 entity types (not 6 separate tables)
- 7-day TTL is safe because all nodes will have processed the deletion by then

### Stale Node Reconciler (v1.0.0)

When a node reconnects after being offline or network-partitioned for longer than the staleness threshold (7 days), the **Stale Reconciler** runs at startup to restore consistency:

- **Mode Offline**: Node was fully down (no local writes). Fetches a state snapshot from peers and applies it locally. Tombstones are synced bidirectionally.
- **Mode Partition**: Node was isolated but serving clients (divergent state). Uses **last-write-wins (LWW)** for entity timestamps; pushes locally-newer entities to peers; applies remote tombstones. Access keys and bucket permissions always defer to tombstones (no `updated_at`).
- **Detection**: `last_local_write_at` vs `last_seen_at_shutdown` determines which mode applies.

See [OPERATIONS.md](OPERATIONS.md#cluster-incidents) for operator runbooks.

### Configuring Replication

**Via Web Console:**
1. Navigate to Cluster → Bucket Replication
2. Select bucket
3. Click "Configure Replication"
4. Choose destination node
5. Set sync interval: 10-60s (real-time HA), 300s (near-real-time), 3600s (hourly)
6. Enable "Replicate deletes" and "Replicate metadata"

**Via API:**
```bash
POST /api/v1/cluster/replication
{
  "source_bucket": "my-bucket",
  "destination_node_id": "uuid-5678",
  "sync_interval_seconds": 60,
  "enabled": true,
  "replicate_deletes": true,
  "replicate_metadata": true
}
```

### Self-Replication Prevention

- Frontend: Local node filtered from destination dropdown
- Backend: Returns HTTP 400 if `destination_node_id == local_node_id`

---

## Bucket Migration

### Overview

Bucket migration enables **moving entire buckets between cluster nodes** for capacity rebalancing, hardware maintenance, or performance optimization. This feature allows administrators to seamlessly relocate data without service interruption.

**Key Features:**

- ✅ Live bucket migration between nodes
- ✅ Real-time progress tracking (objects and bytes)
- ✅ Optional data integrity verification
- ✅ Automatic bucket location updates
- ✅ Optional source data deletion after successful migration
- ✅ Web-based migration management dashboard

### Use Cases

1. **Capacity Rebalancing** - Move buckets from full nodes to nodes with available space
2. **Hardware Maintenance** - Evacuate data before decommissioning a node
3. **Performance Optimization** - Relocate high-traffic buckets to faster/closer nodes
4. **Geographic Redistribution** - Move data closer to users for better latency
5. **Cost Optimization** - Consolidate data to reduce node count

### How It Works

**Migration Workflow:**

```
┌─────────────────────────────────────────────────────────┐
│ 1. Count Objects & Calculate Total Size                │
│    → Query objects table for bucket                    │
│    → Store counts in migration job                     │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ 2. Copy Objects to Target Node                         │
│    → Iterate through all bucket objects                │
│    → HTTP PUT to target node (HMAC authenticated)      │
│    → Update progress every 10 objects                  │
│    → Allow up to 10 errors before failing              │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ 3. Verify Data Integrity (if enabled)                  │
│    → Validate object count matches                     │
│    → Validate total bytes (1% tolerance)               │
│    → Sample verification: Check first 10 objects       │
│    → Verify ETags match between nodes                  │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ 4. Update Bucket Location                              │
│    → Update Pebble metadata                            │
│    → Update bucket location cache                      │
│    → All future requests route to target node          │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ 5. Delete Source Data (if enabled)                     │
│    → Remove objects from source node                   │
│    → Free up storage space                             │
└─────────────────────────────────────────────────────────┘
```

**Migration States:**

| State | Description |
|-------|-------------|
| `pending` | Migration job created, waiting to start |
| `in_progress` | Actively copying objects to target node |
| `completed` | Successfully migrated all objects |
| `failed` | Migration failed (check error_message) |
| `cancelled` | Migration manually cancelled |

### Configuring Migration

**Via Web Console:**

1. Navigate to Cluster → Migrations tab
2. Click "Migrate Bucket" button
3. Select source bucket from dropdown
4. Select target node (only healthy nodes shown)
5. Configure options:
   - ✅ **Verify data integrity** - Validates ETags after migration (recommended)
   - ✅ **Delete source data** - Removes objects from source after successful migration
6. Click "Start Migration"
7. Monitor progress in Migrations table

**Via API:**

```bash
# Start bucket migration
POST /api/v1/cluster/buckets/{bucket}/migrate
{
  "target_node_id": "uuid-target-node",
  "verify_data": true,
  "delete_source": false
}

# Response: HTTP 202 Accepted
{
  "status": "success",
  "message": "Migration started successfully",
  "data": {
    "id": 1,
    "bucket_name": "my-bucket",
    "source_node_id": "uuid-source-node",
    "target_node_id": "uuid-target-node",
    "status": "pending",
    "objects_total": 0,
    "objects_migrated": 0,
    "bytes_total": 0,
    "bytes_migrated": 0,
    "verify_data": true,
    "delete_source": false,
    "created_at": "2025-12-13T10:30:00Z"
  }
}
```

### Monitoring Migration Progress

**List All Migrations:**

```bash
# Get all migrations
GET /api/v1/cluster/migrations

# Filter by bucket
GET /api/v1/cluster/migrations?bucket=my-bucket

# Response
{
  "status": "success",
  "data": {
    "migrations": [
      {
        "id": 1,
        "bucket_name": "my-bucket",
        "source_node_id": "uuid-source",
        "target_node_id": "uuid-target",
        "status": "in_progress",
        "objects_total": 10000,
        "objects_migrated": 3500,
        "bytes_total": 104857600,
        "bytes_migrated": 36700160,
        "started_at": "2025-12-13T10:30:00Z",
        "updated_at": "2025-12-13T10:35:00Z"
      }
    ],
    "count": 1
  }
}
```

**Get Specific Migration:**

```bash
GET /api/v1/cluster/migrations/{id}

# Response
{
  "status": "success",
  "data": {
    "id": 1,
    "bucket_name": "my-bucket",
    "source_node_id": "uuid-source",
    "target_node_id": "uuid-target",
    "status": "completed",
    "objects_total": 10000,
    "objects_migrated": 10000,
    "bytes_total": 104857600,
    "bytes_migrated": 104857600,
    "verify_data": true,
    "delete_source": false,
    "started_at": "2025-12-13T10:30:00Z",
    "completed_at": "2025-12-13T10:45:00Z",
    "created_at": "2025-12-13T10:30:00Z",
    "updated_at": "2025-12-13T10:45:00Z"
  }
}
```

### Migration Dashboard

**Migrations Table Columns:**

- **ID** - Migration job identifier
- **Bucket** - Bucket being migrated
- **Source → Target** - Node IDs showing migration direction
- **Status** - Current state with color coding (🟢 completed, 🔵 in progress, 🔴 failed)
- **Progress** - Visual progress bar showing percentage and object counts
- **Data Size** - Bytes migrated vs total (human-readable format)
- **Started** - Migration start timestamp
- **Actions** - View details button

**Progress Visualization:**

```
my-bucket    node-1 → node-2    [████████░░] 80%
                                3,500 / 10,000 objects
                                35 MB / 100 MB
```

### Best Practices

**1. Pre-Migration Checklist:**

```bash
# Verify target node has sufficient space
curl -X GET "http://localhost:8081/api/v1/cluster/nodes/{targetNodeId}" \
  -H "Authorization: Bearer $TOKEN"
# Check: capacity_used + bucket_size < capacity_total

# Verify target node is healthy
# Health status should be "healthy" (not degraded/unavailable)

# Stop replication rules for the bucket (optional)
# Prevents conflicts during migration
```

**2. Migration Settings:**

- **Always enable** `verify_data: true` for production migrations
- **Only enable** `delete_source: true` after confirming migration completed successfully
- For large buckets (>100K objects), monitor network bandwidth and node CPU

**3. Performance Considerations:**

| Bucket Size | Expected Duration | Recommendation |
|-------------|-------------------|----------------|
| < 1,000 objects | < 5 minutes | Migrate anytime |
| 1K - 10K objects | 5-30 minutes | Migrate during low-traffic periods |
| 10K - 100K objects | 30m - 3 hours | Schedule during maintenance window |
| > 100K objects | > 3 hours | Consider splitting bucket or increasing worker count |

**4. Error Handling:**

- Migration allows up to **10 errors** before failing
- Check `error_message` field if status is `failed`
- Common errors:
  - Network timeout (check connectivity between nodes)
  - Target node full (check capacity)
  - Permission denied (verify HMAC authentication)

**5. Rollback Plan:**

If migration fails or needs to be reversed:

```bash
# Option 1: Migrate back to original node
POST /api/v1/cluster/buckets/{bucket}/migrate
{
  "target_node_id": "original-node-id",
  "verify_data": true,
  "delete_source": false
}

# Option 2: Update bucket location manually (advanced)
# Use BucketLocationManager to change primary node
```

### Prometheus Metrics

**Migration-Specific Metrics:**

```
cluster_migrations_total
cluster_migrations_active
cluster_migrations_completed_total
cluster_migrations_failed_total
cluster_migration_objects_migrated_total
cluster_migration_bytes_migrated_total
cluster_migration_duration_seconds
```

### Recommended Alerts

```yaml
# alerts.yml
groups:
  - name: maxiofs_migrations
    rules:
      - alert: MigrationFailed
        expr: cluster_migrations_failed_total > 0
        for: 1m
        severity: warning
        annotations:
          summary: "Bucket migration failed"

      - alert: MigrationStalled
        expr: cluster_migrations_active > 0 AND
              increase(cluster_migration_objects_migrated_total[10m]) == 0
        for: 10m
        severity: warning
        annotations:
          summary: "Migration appears stalled"
```

### Troubleshooting Migrations

**Migration Stuck at 0%:**

```bash
# Check source node logs
journalctl -u maxiofs -n 100 | grep "migration"

# Verify bucket exists
curl -X GET "http://source-node:8081/api/v1/buckets/{bucket}" \
  -H "Authorization: Bearer $TOKEN"

# Check migration job status
sqlite3 /data/auth.db "SELECT * FROM cluster_migrations WHERE id=1;"
```

**Migration Failed with HMAC Errors:**

```bash
# Verify cluster tokens match
sqlite3 /data/node1/auth.db "SELECT cluster_token FROM cluster_config;"
sqlite3 /data/node2/auth.db "SELECT cluster_token FROM cluster_config;"

# Ensure clocks are synchronized (NTP)
ssh node1 "date -u"
ssh node2 "date -u"
```

**High Migration Duration:**

```bash
# Test network bandwidth between nodes
scp large-file.bin target-node:/tmp/

# Check if target node is under load
ssh target-node "top -bn1 | grep maxiofs"

# Consider migrating during off-peak hours
```

---

## Dashboard UI

### Accessing Cluster Dashboard

1. Login to web console (http://localhost:8081)
2. Click "Cluster" icon in sidebar (requires global admin)

### Cluster Overview

**Status Cards:**
- Total/Healthy/Degraded/Unavailable Nodes
- Total/Replicated/Local Buckets

**Nodes Table Columns:**
- Name, Endpoint, Health Status (🟢/🟡/🔴/⚪)
- Latency (ms), Capacity (used/total), Buckets count
- Priority, Last Seen, Actions (Edit/Remove)

### Dialogs

**Initialize Cluster:**
- Node Name, Region (optional), Local S3 API Endpoint (optional — leave empty to use `public_api_url`)
- Generates cluster token — copy and save it; you will need it to join other nodes

**Add Node** (initiated from the primary node):
- Node IP Address — IP or IP:port of the remote node's console (default port 8081)
- Admin Username and Admin Password — credentials on the remote node
- The primary node handles the full join handshake automatically

**Join Existing Cluster** (initiated from the new node):
- Cluster Node IP Address — IP or IP:port of any existing node's cluster port (default port 8082)
- Cluster Token — the token generated at cluster initialization

**Edit Node:**
- Editable: Name, Region, Priority, Metadata
- Read-only: Endpoint, Node ID (cannot change after join; remove and re-add to change)

---

## API Reference

**Base URL**: `http://localhost:8081/api/v1`
**Authentication**: JWT token required in `Authorization: Bearer <token>` header

### Cluster Management Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/cluster/initialize` | Initialize cluster, generates token |
| GET | `/api/v1/cluster/config` | Get cluster configuration |
| GET | `/api/v1/cluster/nodes` | List all cluster nodes |
| POST | `/api/v1/cluster/nodes` | Add node to cluster |
| GET | `/api/v1/cluster/nodes/{nodeId}` | Get node details |
| PUT | `/api/v1/cluster/nodes/{nodeId}` | Update node (name, region, priority, metadata) |
| DELETE | `/api/v1/cluster/nodes/{nodeId}` | Remove node from cluster |
| GET | `/api/v1/cluster/health` | Get cluster health summary |
| POST | `/api/v1/cluster/health/refresh` | Trigger manual health check |
| GET | `/api/v1/cluster/cache/stats` | Get cache statistics (hits, misses, ratio) |
| DELETE | `/api/v1/cluster/cache` | Clear bucket location cache |
| GET | `/api/v1/cluster/buckets` | List cluster buckets with replication status |
| GET | `/api/v1/cluster/buckets/{bucketName}/nodes` | Get primary and replica nodes for bucket |

### Cluster Replication Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/cluster/replication` | Create replication rule |
| GET | `/api/v1/cluster/replication` | List replication rules (filter by tenant, bucket) |
| PUT | `/api/v1/cluster/replication/{ruleId}` | Update replication rule |
| DELETE | `/api/v1/cluster/replication/{ruleId}` | Delete replication rule |
| POST | `/api/v1/cluster/replication/bulk` | Bulk replicate all buckets node-to-node |

### Bucket Migration Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/cluster/buckets/{bucket}/migrate` | Migrate bucket to different node |
| GET | `/api/v1/cluster/migrations` | List all migration jobs (optional filter: ?bucket=name) |
| GET | `/api/v1/cluster/migrations/{id}` | Get specific migration job details |

### Example Requests

**Initialize Cluster:**
```json
POST /api/v1/cluster/initialize
{
  "node_name": "node-east-1",
  "region": "us-east-1"
}
// Returns: node_id, cluster_token, is_cluster_enabled
```

**Add Node:**
```json
POST /api/v1/cluster/nodes
{
  "name": "node-west-1",
  "endpoint": "http://10.0.1.20:8080",
  "node_token": "eyJhbGciOi...",
  "region": "us-west-1",
  "priority": 100
}
```

**Create Replication Rule:**
```json
POST /api/v1/cluster/replication
{
  "source_bucket": "my-bucket",
  "destination_node_id": "uuid-5678",
  "destination_bucket": "my-bucket",
  "sync_interval_seconds": 60,
  "enabled": true,
  "replicate_deletes": true,
  "replicate_metadata": true,
  "prefix": ""
}
```

**Migrate Bucket:**
```json
POST /api/v1/cluster/buckets/my-bucket/migrate
{
  "target_node_id": "uuid-target-node",
  "verify_data": true,
  "delete_source": false
}
// Returns HTTP 202 Accepted with migration job details
```

---

## Security

### HMAC-SHA256 Authentication

**Purpose:** Secure node-to-node communication without S3 credentials

**Algorithm:** HMAC-SHA256
**Secret Key:** `node_token` (generated during cluster initialization)

**Signing Process:**
1. Compute message: `METHOD + PATH + TIMESTAMP + NONCE + BODY`
2. Compute HMAC: `HMAC-SHA256(node_token, message)`
3. Hex-encode signature
4. Add headers: `X-MaxIOFS-Node-ID`, `X-MaxIOFS-Timestamp`, `X-MaxIOFS-Nonce`, `X-MaxIOFS-Signature`

**Validation:**
1. Extract headers from request
2. Retrieve `node_token` from database
3. Compute expected signature
4. Compare with provided signature (constant-time)
5. Verify timestamp within ±5 minutes
6. Reject if validation fails (HTTP 401)

### Node Token Security Best Practices

1. **Generate Strong Tokens**: 256 bits of entropy minimum (`openssl rand -hex 32`)
2. **Rotate Regularly**: Every 90 days recommended
3. **Store Securely**: Encrypted in SQLite, never log in plaintext
4. **Network Security**: Use TLS/HTTPS, restrict ports to cluster network only

### Firewall Configuration

```bash
# Allow cluster communication from node subnet only
iptables -A INPUT -s 10.0.1.0/24 -p tcp --dport 8080 -j ACCEPT
iptables -A INPUT -s 10.0.1.0/24 -p tcp --dport 8081 -j ACCEPT

# Block external access
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

---

## Monitoring & Health

### Health Check System

**Automatic Checks:**
- Interval: 30 seconds
- Measures network latency
- Updates status: healthy (<1s), degraded (1-5s), unavailable (>5s), unknown (not checked)

**Health Endpoint:**
```bash
curl http://localhost:8080/health
# Returns: status, timestamp, version, uptime, cluster_enabled, node_id, node_name
```

### Prometheus Metrics

**Cluster-Specific Metrics:**
```
cluster_nodes_total
cluster_nodes_healthy
cluster_nodes_degraded
cluster_nodes_unavailable
cluster_replication_rules_total
cluster_replication_rules_active
cluster_replication_objects_pending
cluster_replication_objects_replicated_total
cluster_replication_bytes_replicated_total
cluster_replication_errors_total
cluster_cache_entries
cluster_cache_hits_total
cluster_cache_misses_total
cluster_cache_hit_ratio
```

### Recommended Alerts

```yaml
# alerts.yml
groups:
  - name: maxiofs_cluster
    rules:
      - alert: ClusterNodeDown
        expr: cluster_nodes_unavailable > 0
        for: 5m
        severity: critical

      - alert: ClusterNodeDegraded
        expr: cluster_nodes_degraded > 0
        for: 10m
        severity: warning

      - alert: ClusterReplicationLag
        expr: cluster_replication_objects_pending > 1000
        for: 15m
        severity: warning

      - alert: ClusterReplicationErrors
        expr: increase(cluster_replication_errors_total[5m]) > 10
        severity: warning

      - alert: ClusterCacheLowHitRatio
        expr: cluster_cache_hit_ratio < 0.7
        for: 30m
        severity: info
```

---

## Troubleshooting

### 1. Node Shows as "Unavailable"

**Symptoms:** Node appears red in dashboard

**Diagnosis:**
```bash
# Test connectivity
ping -c 4 10.0.1.20
telnet 10.0.1.20 8080
curl http://10.0.1.20:8080/health

# Check if MaxIOFS is running
ssh node2 "systemctl status maxiofs"

# Verify endpoint URL in database
sqlite3 /data/node1/auth.db "SELECT id, name, endpoint FROM cluster_nodes;"
```

**Fixes:**
- Check firewall rules (ports 8080/8081 open)
- Start MaxIOFS service if down
- Update endpoint URL if incorrect

### 2. Replication Not Working

**Symptoms:** Objects uploaded to Node 1 don't appear on Node 2

**Diagnosis:**
```bash
# Check replication rule status
curl -X GET "http://localhost:8081/api/v1/cluster/replication?bucket=my-bucket" \
  -H "Authorization: Bearer $TOKEN"

# Verify: enabled=true, last_error=null, reasonable sync_interval

# Check replication queue
sqlite3 /data/node1/auth.db "SELECT COUNT(*) FROM cluster_replication_queue WHERE status='pending';"

# Check tenant sync
curl -X GET "http://node2:8081/api/v1/tenants" \
  -H "Authorization: Bearer $TOKEN"
```

**Fixes:**
- Ensure replication rule is enabled
- Verify tenant exists on destination (automatic sync every 30s)
- Check worker logs: `journalctl -u maxiofs -n 100 | grep "replication worker"`

### 3. HMAC Authentication Errors

**Symptoms:** "Invalid HMAC signature" errors, 401 Unauthorized

**Diagnosis:**
```bash
# Verify cluster tokens match
sqlite3 /data/node1/auth.db "SELECT cluster_token FROM cluster_config;"
sqlite3 /data/node2/auth.db "SELECT cluster_token FROM cluster_config;"

# Check timestamp skew (clocks must be synchronized)
ssh node1 "date -u"
ssh node2 "date -u"
```

**Fixes:**
- Ensure both nodes have same cluster token
- Use NTP to synchronize clocks (max 5 minutes skew allowed)

### 4. Bucket Location Cache Issues

**Symptoms:** Requests routed to wrong node, 404 errors for existing objects

**Diagnosis:**
```bash
# Check cache stats
curl -X GET "http://localhost:8081/api/v1/cluster/cache/stats" \
  -H "Authorization: Bearer $TOKEN"

# Check bucket ownership
curl -X GET "http://localhost:8081/api/v1/cluster/buckets/my-bucket/nodes" \
  -H "Authorization: Bearer $TOKEN"
```

**Fixes:**
```bash
# Clear cache
curl -X DELETE "http://localhost:8081/api/v1/cluster/cache" \
  -H "Authorization: Bearer $TOKEN"
```

### 5. High Replication Lag

**Symptoms:** `objects_pending` count increasing, slow replication

**Diagnosis:**
```bash
# Test network bandwidth between nodes
scp large-file.bin node2:/tmp/

# Check worker count (default: 5 workers)
# internal/cluster/replication_manager.go
```

**Fixes:**
- Increase worker count (code change required)
- Upgrade network or reduce sync frequency
- Increase `sync_interval_seconds` for large object buckets

### 6. Dashboard Not Loading

**Symptoms:** Loading spinner forever, console errors

**Diagnosis:**
- Check browser console (F12 → Console tab)
- Verify API endpoint responds:
```bash
curl -X GET "http://localhost:8081/api/v1/cluster/health" \
  -H "Authorization: Bearer $TOKEN"
```

**Fixes:**
- Clear browser cache
- Check network tab for failed API calls
- Verify JWT token is valid

### Debug Mode

```bash
# Enable debug logging
./maxiofs --data-dir /data/node1 --log-level debug

# Cluster-specific debug output:
# [DEBUG] Cluster Manager: Initialized with node_id=...
# [DEBUG] Health Checker: Node uuid-5678 is healthy (latency=15ms)
# [DEBUG] Replication Worker: Replicating object bucket/file.txt to node uuid-5678
# [DEBUG] HMAC Auth: Signature valid for node uuid-5678
```

### Log Locations

```bash
# systemd
journalctl -u maxiofs -f

# Docker
docker logs -f maxiofs-node1

# Standalone
./maxiofs --data-dir /data 2>&1 | tee maxiofs.log
```

---

## Testing

### Integration Tests

**Location:** `internal/cluster/replication_integration_test.go`

**Infrastructure:** SimulatedNode (in-memory SQLite, HTTP server using `httptest.Server`, HMAC verification)

### Running Tests

```bash
# All cluster tests
go test ./internal/cluster -v

# Specific test
go test ./internal/cluster -v -run TestHMACAuthentication

# With coverage
go test ./internal/cluster -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Test Cases

| Test | Purpose | Coverage |
|------|---------|----------|
| TestHMACAuthentication | Verify HMAC signature validation | Valid/invalid signatures, missing headers, expired timestamp |
| TestTenantSynchronization | Verify tenant sync between nodes | Checksum validation, create/update |
| TestObjectReplication | Verify object replication with HMAC | PUT operations, content verification |
| TestDeleteReplication | Verify delete operations replicate | DELETE operations across nodes |
| TestSelfReplicationPrevention | Verify nodes can't replicate to self | HTTP 400 error validation |

**Test Results:**
- 90+ total cluster tests (management, replication, sync managers, deletion log)
- 100% pass rate
- Pure Go (no CGO dependencies)

---

## SQLite Schema Reference

### cluster_config Table

```sql
CREATE TABLE cluster_config (
    node_id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    cluster_token TEXT NOT NULL,
    is_cluster_enabled INTEGER NOT NULL DEFAULT 0,
    region TEXT,
    created_at INTEGER NOT NULL
);
```

### cluster_nodes Table

```sql
CREATE TABLE cluster_nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    node_token TEXT NOT NULL,
    region TEXT,
    priority INTEGER NOT NULL DEFAULT 100,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    last_health_check INTEGER,
    last_seen INTEGER,
    latency_ms INTEGER DEFAULT 0,
    capacity_total INTEGER DEFAULT 0,
    capacity_used INTEGER DEFAULT 0,
    bucket_count INTEGER DEFAULT 0,
    metadata TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

### cluster_bucket_replication Table

```sql
CREATE TABLE cluster_bucket_replication (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    source_bucket TEXT NOT NULL,
    destination_node_id TEXT NOT NULL,
    destination_bucket TEXT NOT NULL,
    sync_interval_seconds INTEGER NOT NULL DEFAULT 10,
    enabled INTEGER NOT NULL DEFAULT 1,
    replicate_deletes INTEGER NOT NULL DEFAULT 1,
    replicate_metadata INTEGER NOT NULL DEFAULT 1,
    prefix TEXT DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    last_sync_at INTEGER,
    last_error TEXT,
    objects_replicated INTEGER NOT NULL DEFAULT 0,
    bytes_replicated INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (destination_node_id) REFERENCES cluster_nodes(id) ON DELETE CASCADE
);
```

### cluster_replication_queue Table

```sql
CREATE TABLE cluster_replication_queue (
    id TEXT PRIMARY KEY,
    replication_rule_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    source_bucket TEXT NOT NULL,
    object_key TEXT NOT NULL,
    destination_node_id TEXT NOT NULL,
    destination_bucket TEXT NOT NULL,
    operation TEXT NOT NULL DEFAULT 'PUT',
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (replication_rule_id) REFERENCES cluster_bucket_replication(id) ON DELETE CASCADE
);
```

### cluster_deletion_log Table (v0.9.0-beta)

```sql
CREATE TABLE cluster_deletion_log (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,       -- 'user', 'tenant', 'access_key', 'bucket_permission', 'idp_provider', 'group_mapping'
    entity_id TEXT NOT NULL,
    deleted_by_node_id TEXT NOT NULL,
    deleted_at INTEGER NOT NULL,
    UNIQUE(entity_type, entity_id)
);
CREATE INDEX idx_deletion_log_type ON cluster_deletion_log(entity_type);
CREATE INDEX idx_deletion_log_deleted_at ON cluster_deletion_log(deleted_at);
```

### cluster_migrations Table

```sql
CREATE TABLE cluster_migrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL DEFAULT '',
    bucket_name TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    objects_total INTEGER NOT NULL DEFAULT 0,
    objects_migrated INTEGER NOT NULL DEFAULT 0,
    bytes_total INTEGER NOT NULL DEFAULT 0,
    bytes_migrated INTEGER NOT NULL DEFAULT 0,
    delete_source INTEGER NOT NULL DEFAULT 0,
    verify_data INTEGER NOT NULL DEFAULT 1,
    error_message TEXT,
    started_at INTEGER,
    completed_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (source_node_id) REFERENCES cluster_nodes(id),
    FOREIGN KEY (target_node_id) REFERENCES cluster_nodes(id)
);

CREATE INDEX idx_cluster_migrations_bucket ON cluster_migrations(bucket_name);
CREATE INDEX idx_cluster_migrations_status ON cluster_migrations(status);
CREATE INDEX idx_cluster_migrations_tenant ON cluster_migrations(tenant_id);
```

---

**Version**: 1.2.0
**Last Updated**: April 2, 2026
**Documentation Status**: Complete

For questions or issues, see [README.md](../README.md).
