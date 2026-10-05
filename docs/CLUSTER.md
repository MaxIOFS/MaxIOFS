# Multi-Node Cluster Management

**Version**: 1.7.0
**Status**: Production-Ready
**Last Updated**: September 27, 2026

---

## Table of Contents

1. [Overview](#overview)
   - [What happens when a node fails](#what-happens-when-a-node-fails)
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

---

## Overview

MaxIOFS provides complete multi-node cluster support for high availability (HA) and automatic failover. Multiple MaxIOFS instances work together as a unified storage cluster with intelligent request routing, automatic health monitoring, and seamless failover. In v0.9.0-beta, cluster sync was extended to 6 entity types with tombstone-based deletion sync to prevent entity resurrection.

### Key Features

- ✅ Multi-node cluster support with smart routing
- ✅ **Dedicated cluster port 8082** — inter-node traffic is fully separated from S3 (8080) and console (8081)
- ✅ **Inter-node TLS encryption** — automatic, zero-configuration (v0.9.1)
- ✅ HMAC-authenticated node-to-node replication
- ✅ Synchronous replication (factor 1–3): a factor of 2 is a mirror that keeps writing with its peer down; a factor of 3 needs one of its two peers
- ✅ A node that comes back is caught up at once with the writes and deletes it missed
- ✅ Ciphertext replication with a cluster-shared KEK; retention and legal hold travel with the object
- ✅ Elected coordinator for configuration changes; a surviving node takes over
- ✅ Automatic synchronization for 6 entity types (users, tenants, access keys, bucket permissions, IDPs, group mappings)
- ✅ Tombstone-based deletion sync (prevents entity resurrection)
- ✅ Health monitoring (30-second intervals)
- ✅ Bucket location cache (5ms vs 50ms latency)
- ✅ Bucket migration between nodes for capacity rebalancing
- ✅ **Partitions and long absences** — writes and deletions are ordered by time; a deletion is kept until every node has it
- ✅ Web-based cluster management dashboard

### Use Cases

1. **High Availability** - Automatic failover if primary node fails
2. **Geographic Distribution** - Nodes in different regions for low latency
3. **Disaster Recovery** - Replicate data to backup nodes
4. **Load Balancing** - Distribute requests across healthy nodes
5. **Zero-Downtime Maintenance** - Update nodes without service interruption

### What happens when a node fails

A cluster has two planes, and it is worth knowing how each one behaves.

**The data plane (S3 API, port 8080) needs no coordinator at all.** Every node
serves reads and writes on its own. With a replication factor above 1, a write is
copied to the healthy peers before it is acknowledged, and half of the factor's
copies, rounded up and counting the local one, must hold it. A factor of 2 is
therefore a mirror, like RAID 1: with its peer down, the survivor keeps accepting
writes on its own copy. A factor of 3 needs one of its two peers, so it keeps
writing with one node down and answers `503 ServiceUnavailable`
(`Retry-After: 30`) only with both down.

**A node that missed writes is caught up as soon as it is back.** The node that
accepted them records, per peer, the time of the earliest write that peer
missed. When a health check finds the peer healthy again, it sends the peer
every bucket (see [Buckets](#buckets)), compares every key modified since then
with the peer and sends what differs — every version and delete marker the peer
lacks, and the current object when it differs, lock state included — then sends
the deletes the peer missed. A delete of a whole key is sent
only if the peer's copy is not newer than the delete, so a key written on the
other side of a partition is kept. What fails is recorded again and retried at
the next health check. The catch-up runs even with the periodic scrub disabled.

**Metadata-only changes** — tags, ACLs, retention, legal hold, user metadata,
restore status — reach every live node before the request returns, so a
client's successive changes arrive in order. A node that is down, fails, or
still has changes waiting gets the change queued behind them, in SQLite, and the
queue is delivered in order when the node is caught up. A change the node
refuses (the object is gone, or its lock state no longer allows it) is dropped
and logged; any other failure keeps the queue for the next attempt.
With the periodic scrub enabled (the default), a node that restarts also runs a
full anti-entropy cycle within an hour of starting; it compares keys the same
way. A version or delete marker a node deleted is not stored again when a node
that missed the delete sends it, or gives it, a copy.

**The control plane (web console, port 8081) elects a coordinator**, so that two
nodes cannot edit the same entity at the same instant and quietly disagree about
the result. Configuration — users, access keys, policies, roles, tenants, bucket
permissions — is written on the elected node; a change made on any other node is
forwarded to it, which is invisible to whoever is using the console. A bucket's
own settings and objects are kept by the node that holds the bucket: console
requests about them are forwarded to that node and do not need the coordinator.

**When the coordinator dies, the nodes that are still alive hold an election and
one of them takes over.** The majority is counted over the nodes that answer,
not over every node the cluster has ever known, so a node that is gone does not
get a vote in whether the cluster may continue. In a two-node cluster the
survivor elects itself within about twenty seconds — one lease — and from then
on it is the coordinator: every configuration change works normally on it.

**When the failed node comes back, it comes back as a member.** It finds a live
lease under a newer term, follows it, and the synchronisation managers bring it
up to date with everything that changed while it was away. Nothing needs to be
done by hand, and nothing has to be removed or re-joined. If you *do* want to
decommission it permanently, remove it from the console in the ordinary way —
the surviving node is a coordinator, so that works too.

| | 2 nodes, factor 2 | 3 nodes, factor 3 |
|---|---|---|
| Object reads with a node down | ✅ Continue | ✅ Continue |
| Object writes with a node down | ✅ Continue | ✅ Continue |
| Data redundancy (full copy per node) | ✅ Yes | ✅ Yes |
| Configuration changes with a node down | ✅ Continue on the survivor | ✅ Continue |
| Failed node returns | ✅ Rejoins and syncs | ✅ Rejoins and syncs |

> **The trade this makes, stated plainly.** If both nodes are alive but the
> network between them fails, each sees only itself and each becomes the
> coordinator of what it can see, so both accept configuration changes. When the
> link is restored, the synchronisation reconciles them the way it reconciles
> everything else: newest write wins, entity by entity, by `updated_at`. Object
> data is never affected, since it does not go through the coordinator at all.
> The alternative — freezing administration until a strict majority is
> available — was rejected on purpose: it means a two-node cluster locks its
> operator out the moment either node fails, including out of the very
> operations needed to recover.

---

## Architecture

### Port Layout

Each MaxIOFS node exposes three independent ports:

| Port | Purpose | Exposed to |
|------|---------|-----------|
| **8080** | S3 API — object storage operations | S3 clients, load balancer |
| **8081** | Web Console — admin UI and console REST API | Operators, load balancer |
| **8082** | Cluster inter-node — coordination, sync, CSR signing | Other cluster nodes only (firewall off from public) |

Every node of a cluster listens on these default ports. Add Node registers the
new node's S3 API on port 8080 and its cluster port on the cluster port of the
node that adds it: a node on other ports is registered at the wrong addresses,
and its health checks, synchronization and the requests forwarded to it fail.

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
- Routes every S3 and console request about a bucket to the node that holds it
- Bucket names are unique in the cluster: creating a bucket another node holds answers `409`
- Automatic failover to healthy nodes
- Caches the node each remote bucket lives on (5-minute TTL)
- Proxies requests to remote nodes when needed

**3. Health Checker**
- Monitors all nodes every 30 seconds
- Measures network latency
- Updates status: healthy (<1s), degraded (1-5s), unavailable (>5s)

**4. Bucket Location Cache**
- In-memory cache of the node each remote bucket lives on, 5-minute TTL
- A bucket this node holds is served here, whatever the cache says
- A forwarded request answered `503` with `X-MaxIOFS-Bucket-Not-Here` (the bucket left that node) removes the entry; the client's retry is routed again

---

## Quick Start

### Prerequisites

- 2+ MaxIOFS instances on different servers
- Network connectivity on all three ports (8080, 8081, 8082) between all nodes
- Firewall: port 8082 open between node IPs only (not to the public internet)
- Admin access to all nodes

> **Containers**: a multi-node cluster inside a single Docker host provides
> no real HA — every "node" shares the same disk, power supply and kernel,
> so replication multiplies storage cost without adding durability. If you
> run nodes as containers (Docker/Swarm/Kubernetes), place **one node per
> physical host**, pin each container to its host with a **local volume**
> (a rescheduled container without its volume loses the node's identity and
> data), and make port 8082 reachable between hosts. Container multi-host
> orchestration is not part of the officially tested matrix.

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
#   Node IP Address: 10.0.1.20
#   Admin Username:  admin
#   Admin Password:  <node 2 password>
```

The primary node authenticates to Node 2's console API (port 8081), triggers a cluster join, and Node 2 contacts Node 1's cluster port (8082) to register. A node joins only this way: from the console of a node of the cluster, with the new node's administrator credentials.

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
| `cluster_listen` | `:8082` | Bind address for the cluster inter-node server. Keep port 8082 on a node of a cluster; the address can be restricted to one interface. |

```yaml
# config.yaml — cluster port (all other cluster config is managed via the web console)
cluster_listen: ":8082"
```

Environment variable equivalent: `MAXIOFS_CLUSTER_LISTEN=:8082`

> ⚠️ **A node of a cluster listens on the default ports: S3 API 8080, console
> 8081, cluster 8082.**
>
> When you add a node, the primary registers the new node's S3 API on port
> 8080 and its cluster port on the **primary's own** `cluster_listen` port; it
> does not ask the new node for them. A node on other ports is registered at
> the wrong addresses: health checks fail, the node never votes, and requests
> forwarded to it fail.

### Cluster Initialization Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `node_name` | string | Yes | Human-readable name (e.g., "node-east-1") |
| `region` | string | No | Geographic region (e.g., "us-east-1") |
| `local_endpoint` | string | No | Override S3 API endpoint other nodes should use to reach this node. Leave empty to use `public_api_url`. |

### Add Node Parameters (from the primary node's console)

| Field | Default port | Description |
|-------|-------------|-------------|
| **Node IP Address** | 8081 | IP address of the remote node. The primary node authenticates to its console on port 8081, then tells it to join via port 8082. |
| **Admin Username** | — | Admin credentials on the remote node |
| **Admin Password** | — | Admin credentials on the remote node |

### Health Check Configuration

- **Interval**: 30 seconds (hardcoded)
- **Timeout**: 5 seconds
- To modify: Edit `internal/cluster/manager.go` → `healthCheckInterval`

### Cache Configuration

- **TTL**: 5 minutes (hardcoded)
- To modify: Edit `NewRouter` in `internal/cluster/router.go`

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
2. Sign with HMAC-SHA256
3. Send the stored **ciphertext as-is** to Node 2 (plus the object's metadata
   and encryption sidecar — the primary's modification time and client
   checksums travel with it)
4. Node 2 verifies the HMAC signature
5. Node 2 stores a byte-identical copy and decrypts only on read

The object's retention and legal hold travel with it, in this transfer and in the
decrypt/re-encrypt fallback below, so a replica is protected from its first
write. The fallback also carries what its headers cannot: the ETag (a multipart
ETag included), tags, ACL and restore state. The receiving node refuses bytes
that do not hash to that ETag.

The same holds for every other copy between nodes: the initial sync of a new
replica, and the anti-entropy push and pull.

The initial sync of a new replica copies every version of every key, oldest
first and with its version ID — delete markers as delete markers, with their ID
and time, including keys a delete marker hides. A copy that arrives after a
newer version of the same key (an initial sync racing live writes) is stored as
an older version and does not replace the latest; a version the replica already
holds is replaced, not counted twice. In a bucket whose versioning was
suspended, the versions kept from before are copied as versions and the current
object without a version ID last; a copy is stored as the version it copies,
whatever the receiving bucket's versioning status is. An object the sync fails
to send is left to the catch-up: the job says how many, and the replica is
recorded as having missed writes from the oldest of them. A node that joins a
cluster whose replication factor is above 1 starts its initial sync at once.

When fewer peers confirm than the replication factor needs (a factor of 3 with
both peers failing), Node 1 answers `503 ServiceUnavailable` (`Retry-After: 30`)
and removes the version it just wrote, even under the retention or legal hold
that write set: the client was told the write failed. An unversioned write is
kept locally, since deleting it would lose the object it replaced. A peer that
failed is recorded as having missed the write and caught up when it is back.

**Encryption Keys**: all cluster nodes share a **cluster-wide KEK**,
distributed inside the join package and re-synced on every key rotation.
Because every node can unwrap any object's DEK, replication never decrypts
in transit. Objects wrapped with a node-local (pre-join) key transparently
fall back to the legacy decrypt-on-source / re-encrypt-on-destination path
until the background worker re-wraps them to the shared key.

**Encryption secret**: the secret identity provider secrets, replication
destination keys and share link keys are encrypted with is the cluster's on
every node. A node that joins takes it from the join package; it holds no
credentials yet (see [Membership](#membership)). Every minute the coordinator compares each healthy node's
fingerprint of the secret with its own and gives its secret to a node that
differs; a node takes it only from the coordinator it knows. The Nodes page
marks a node that holds another secret.

### Buckets

With a replication factor above 1 every node holds every bucket.

- Creating a bucket, changing its configuration or ACL, and deleting it reach every other node before the request returns. Usage (object count, size) is each node's own and is not sent.
- A node that is down or does not take the change is recorded as having missed a write. When it is caught up, it is sent every bucket and every deletion before the objects. A new replica, and every peer at the start of an anti-entropy cycle and when a node starts, is sent them too. A cluster whose buckets exist only on the node that created them converges this way after the upgrade.
- Two versions of a bucket are ordered by the time of the change; a deletion by the time it was made, to the nanosecond, so a bucket created again right after its deletion exists again.
- A node keeps its copy of a bucket deleted elsewhere when the copy changed after the deletion or holds an object under retention or a legal hold; such a copy is kept and logged. A copy holding objects written after the deletion is kept with those objects only, and sent to the other nodes as created again then. Otherwise the copy is removed with its objects.
- A bucket whose name another tenant holds on a node is refused there and logged.
- A deletion is kept at least 7 days, and until every member of the cluster has been sent it.
- Lifecycle: the coordinator expires objects, and its deletes reach the other nodes. Every node aborts the incomplete multipart uploads started on it.
- Multipart uploads: an upload is held by the node it was started on, and its ID names that node. Its parts, part listing, completion and abort are sent to that node from whichever node they reach, so a load balancer may spread them. A node that does not answer is answered `503` with `Retry-After`; a node no longer in the cluster took its uploads with it (`NoSuchUpload`). Listing the uploads of a bucket asks every healthy node. An upload started before the upgrade has an ID that names no node and is made on the node a request reaches.
- Inventory: the coordinator writes the reports. The report file, its row and the configuration's last run reach the other nodes.
- Scheduled replication rules run on the coordinator. Real-time rules queue each write on the node that took it.

### Bucket rows

With a replication factor above 1 every node also holds the rows a bucket keeps in the node's database: shares, replication rules, inventory configurations and inventory reports.

- A change is sent to every other node before the request returns. A node that is down or does not take it is recorded as having missed a write; it is sent every row, after the buckets, when it is caught up, at the start of an anti-entropy cycle and when a node starts.
- Each row carries the time of its last change, to the nanosecond. A later change wins; at the same time a deletion wins. Rows from before the upgrade carry no time and reach every node.
- Two rows that may not coexist (two shares of one object, two inventory configurations of a tenant's bucket) end as the later one on every node.
- A report whose configuration was deleted is not stored. A row a node cannot store (unknown column, missing tenant) is refused and logged by the sending node.
- Expired shares are not sent: every node removes them.
- With a replication factor of 1 each node keeps the rows of its own buckets; changes are still dated, and reach the other nodes if the factor rises.
- A deletion is kept at least 7 days, and until every member of the cluster has been sent it.
- Stored credentials in these rows are encrypted with the cluster's encryption secret, which every node holds.

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

### Membership

Every node holds the list of nodes. A change to a node (name, region, priority, drained) is stamped with its time and the newest copy wins on every node. The list is sent every 60 seconds and right after a change made in the console.

- **Remove** (console, any other node): every node removes it, and no node that still lists it adds it back: a node joins under a new ID every time, so a removal is final. The removed node is answered `410 Gone` the next time it contacts the cluster, and leaves. Its data stays on its disk.
- **Leave** (console, the node itself): the node tells every node it reaches to remove it, then leaves. The others pass the removal on.
- **Drain** (console, any other node): planned decommissioning. Every node marks the node dead and makes the copies it held again elsewhere. A drained node stays out of service even when it answers. Refused with `409` when it would leave fewer healthy nodes than the replication factor.
- **Dead**: a node unreachable for longer than `ha.dead_node_threshold_hours` (24 hours) is marked dead by each node from its own health checks. When it answers again it is back in service and is caught up from its first missed write.
- **Join**: only a node without data joins a cluster. A node that holds a bucket, a tenant, a user besides its first administrator, an access key, a group, an identity provider, an IAM policy or role created on it, a share or a replication rule is refused with `409`, which names what it holds, and stays as it was. Its first administrator, settings and keys are not data.
- A node that joins a cluster takes the cluster's list of nodes. What it held of a cluster it was in before is dropped.

### Deletions

A deletion is recorded in `cluster_deletion_log` so a node that still holds the entity does not bring it back. It covers users, tenants, access keys, bucket permissions, identity providers, group mappings, groups, STS sessions, IAM policies, roles, attachments and inline policies, objects, and nodes removed from the cluster.

- A deletion keeps the time it was made on the node that made it, dated after the last change that node knew of what it deleted. Two records of the same deletion keep the later time.
- An entity and a deletion of it are ordered by time on every node: a copy changed after the deletion is taken, one changed before it is refused, and a deletion removes a copy only if the copy did not change after it. At the same second the entity is kept. Access keys, bucket permissions and STS sessions keep no change time: any deletion of them wins.
- An object deleted once and written again is kept and sent to the nodes that lack it; a copy written in the second of the deletion counts as written after it.
- Deleting a user, a tenant or a group records the deletion of the IAM policies they held.
- Each node sends every other node the deletions it has not taken yet, in the order they were recorded, every 30 seconds (`POST /api/internal/cluster/deletion-log-sync`). A new node is sent the last 7 days.
- A deletion received by any channel removes the node's copy: the deletion log, the IAM and STS payloads, and each entity's own synchronization.
- A deletion is kept at least 7 days, and until every member of the cluster has been sent it and no catch-up that replays it is pending. A member away, offline or cut off and serving clients, for however long, is sent them when it is back. A node removed from the cluster no longer holds them back.
- Deletions are numbered in a sequence that never goes back.
- Object times are whole seconds. Each copy of an object also carries the time it was written to the nanosecond, which orders two writes of one second, and a write and a deletion of one second. Objects written before the upgrade carry none: within the second, a deletion keeps them and two of them are ordered by ETag. S3 `LastModified` is unchanged.

A bucket deleted by a client takes with it its shares, replication rules, inventory configurations, permissions and the policies naming it; a bucket created by a client starts without what a former bucket of the name left. Every node that removes a bucket, for any reason, drops its notification configuration and integrity scans.

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

A migration moves a bucket from the node it lives on to another node, in a cluster with replication factor 1. With a factor above 1 every node holds every bucket and a migration is refused.

A bucket moves whole:

- every version and delete marker, with its data, version ID, modification time, headers, user metadata, tags, ACL, retention, legal hold and restore state; multipart ETags; the current object without a version ID of a bucket whose versioning is suspended
- the bucket configuration (versioning, Object Lock, policy, lifecycle, CORS, encryption, tags, quota, notification, logging, website, ownership controls) and the bucket ACL
- the bucket's rows in the node database: shares, inventory configurations and reports, replication rules, queue and status

Requirements:

- global administrator
- replication factor 1
- target node healthy
- no multipart upload in progress in the bucket: complete or abort them first (`409` otherwise)

### How It Works

The migration runs on the node the bucket lives on; a console request that reaches another node is forwarded there. The request answers `202 Accepted` and the job runs in the background.

1. **Hold writes.** New writes to the bucket are refused with `503`: S3 `ServiceUnavailable` with `Retry-After: 60`, console code `BUCKET_MIGRATING`. Writes under way finish first; the copy starts after them. Reads continue. Lifecycle skips the bucket.
2. **Stage.** The target creates the bucket with its configuration and ACL, hidden: not listed, not routed to, `404` to peers.
3. **Copy and verify.** Keys in pages of 1,000; each key's versions oldest first, delete markers as markers. After each page the target describes every key it holds: each version's ID and a digest of its ETag, size, modification time, headers, metadata, tags, ACL, lock state and restore state. Any difference fails the migration.
4. **Commit.** Status `committing`. The target stores the bucket's rows and makes the bucket visible. The source hides its copy, deletes the bucket's rows, ACL and data, and stops counting the bucket's bytes in the tenant's usage on that node.
5. **Complete.** The bucket takes writes again, on the target.

A failure before step 4 leaves the bucket on the source unchanged: its writes are admitted again at once, the hidden copy on the target is removed (retried every 30 s until the target answers or leaves the cluster) and the status is `failed` with `error_message`.

A failure during step 4 is retried every 30 s until the hand-over is done. A committed move is never undone.

A restart of the source node:

- `in_progress`: the migration is undone as above.
- `committing`: the bucket's writes are held again before the node serves requests, and the hand-over is finished.

Routing after the move: a node checks its own buckets before any cached location. A node that forwards a request to the node the bucket left receives `503` with `X-MaxIOFS-Bucket-Not-Here`, forgets the cached location, and the client's retry reaches the target.

**Migration States:**

| State | Description |
|-------|-------------|
| `in_progress` | Writes held; copying and verifying |
| `committing` | Copy verified; handing the bucket over. Finished after a restart |
| `completed` | The bucket lives on the target node |
| `failed` | Nothing moved; the bucket stays on the source node (`error_message`) |

### Starting a Migration

**Via Web Console:**

1. Cluster → Migrations → **Migrate Bucket**
2. Select the bucket (the buckets of the node the console is connected to) and the target node (healthy nodes only)
3. **Start Migration**

**Via API:**

```bash
POST /api/v1/cluster/buckets/{bucket}/migrate
{
  "target_node_id": "uuid-target-node"
}

# 202 Accepted
{
  "success": true,
  "data": {
    "id": 1,
    "bucket_name": "my-bucket",
    "source_node_id": "uuid-source-node",
    "target_node_id": "uuid-target-node",
    "status": "in_progress",
    "objects_total": 10000,
    "objects_migrated": 0,
    "bytes_total": 104857600,
    "bytes_migrated": 0,
    "delete_source": true,
    "verify_data": true,
    "started_at": "2026-09-28T10:30:00Z"
  }
}
```

| Status | Cause |
|--------|-------|
| `202` | Migration started |
| `400` | Cluster not enabled, replication factor above 1, target not in the cluster, target is the source, `delete_source: false` |
| `403` | Not a global administrator |
| `404` | The bucket does not live on this node |
| `409` | Target not healthy, bucket already being migrated, multipart uploads in progress |

`delete_source: false` is refused: a bucket lives on one node. The copy is always verified; `verify_data` is ignored.

### Monitoring Migration Progress

```bash
GET /api/v1/cluster/migrations
GET /api/v1/cluster/migrations?bucket=my-bucket
GET /api/v1/cluster/migrations/{id}
```

Global administrators only. Jobs are recorded on the source node.

| Field | Meaning |
|-------|---------|
| `objects_total`, `bytes_total` | The bucket's counters when the migration started; bytes include every version |
| `objects_migrated` | Keys copied whose current version is visible |
| `bytes_migrated` | Bytes of every version copied |
| `error_message` | Cause of a failure; last error while retrying |

### Before Migrating

- Free space on the target ≥ `bytes_total` of the bucket.
- The bucket takes no writes for the duration of the migration; S3 clients retry `503` by default.
- Complete or abort multipart uploads in progress.

### Troubleshooting Migrations

| `error_message` contains | Cause |
|--------------------------|-------|
| `lives on this node` | A bucket of that name lives on the target |
| `differs from this node's` | Verification failed; nothing moved. Check the target's log |
| `multipart uploads in progress` | Complete or abort them |
| `interrupted by a restart` | The source restarted during the copy; start the migration again |

A job that stays `committing` means the target does not answer; the hand-over is retried every 30 s.

**HMAC errors between nodes:**

```bash
# Cluster tokens must match
sqlite3 /data/node1/db/maxiofs.db "SELECT cluster_token FROM cluster_config;"
sqlite3 /data/node2/db/maxiofs.db "SELECT cluster_token FROM cluster_config;"

# Clocks must be synchronized (NTP)
ssh node1 "date -u"
ssh node2 "date -u"
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
- Shows the cluster token: the nodes authenticate to each other with it. Other nodes are added with Add Node

**Add Node** (initiated from the primary node):
- Node IP Address — IP of the remote node; its console is reached on port 8081
- Admin Username and Admin Password — credentials on the remote node
- The primary node handles the full join handshake automatically

**Edit Node:**
- Editable: Name, Region, Priority, Metadata
- Read-only: Endpoint, Node ID (cannot change after join; remove and re-add to change)

---

## API Reference

**Base URL**: `http://localhost:8081/api/v1`
**Authentication**: JWT token required in `Authorization: Bearer <token>` header

This document explains cluster behavior and operations. The canonical endpoint list lives in [API.md](API.md#cluster-management); keep endpoint additions there to avoid drift.

Primary endpoint groups:

- Cluster management: initialize, join, status, nodes, health, cache.
- Cluster replication: rules, bulk replication, status.
- Bucket migration: start migration, list jobs, inspect job details.

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

### 0. "The cluster is choosing a coordinator" on every configuration change

**Symptoms:** The console answers `503` with *"the cluster is choosing a
coordinator for configuration changes"*, or *"this node is no longer the
coordinator"*, on configuration changes. **S3 traffic is unaffected** — objects
upload and download normally, which is the clue that this is the control plane
and not the cluster as a whole. A bucket's settings and objects in the console
are unaffected too.

**Diagnosis:**
```bash
# Who holds the lease, and in which term?
sqlite3 /data/node1/db/maxiofs.db "SELECT leader_id, term, expires_at FROM cluster_leader;"

# The election says why it failed, on every node
grep '"component":"leader"' /var/log/maxiofs/maxiofs.log | tail -20
```

The log names the peer, its answer and its term:

- `reason: unreachable` — the peer's **cluster port** is not answering. Check
  the port is open between nodes and that every node listens on the default
  ports (see [Configuration](#configuration)).
- `reason: voted no: lease still held` — normal for a few seconds after a
  leader changes. Persisting means clocks are far apart, or a node was
  restored from a backup of another node's database.
- `got 1 of 2 votes needed among 2 responding nodes` — both nodes are alive and
  disagree, which is normal for a few seconds during an election. Persisting
  means the two are campaigning against each other; check the clocks and that
  neither database was restored from a copy of the other's.

**Note:** in builds before 1.6.0 a two-node cluster could never elect anybody at
all — the term climbed without bound and configuration changes were refused for
ever, whether or not both nodes were up. Upgrade rather than chasing it.

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
sqlite3 /data/node1/db/maxiofs.db "SELECT id, name, endpoint FROM cluster_nodes;"
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
sqlite3 /data/node1/db/maxiofs.db "SELECT COUNT(*) FROM cluster_replication_queue WHERE status='pending';"

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
sqlite3 /data/node1/db/maxiofs.db "SELECT cluster_token FROM cluster_config;"
sqlite3 /data/node2/db/maxiofs.db "SELECT cluster_token FROM cluster_config;"

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

# Buckets of this node
curl -X GET "http://localhost:8081/api/v1/cluster/buckets" \
  -H "Authorization: Bearer $TOKEN"
```

**Fixes:**
```bash
# Forget the cached location of one bucket
curl -X POST "http://localhost:8081/api/v1/cluster/cache/invalidate" \
  -H "Authorization: Bearer $TOKEN" -d '{"bucket":"my-bucket"}'
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

For cluster test coverage and commands, see [TESTING.md](TESTING.md#internalcluster--28-test-files). The live SQLite schema is maintained by `internal/db/migrations`; avoid duplicating full schema definitions in this guide.

---

**Version**: 1.7.0
**Last Updated**: September 27, 2026
**Documentation Status**: Complete

For questions or issues, see [README.md](../README.md).
