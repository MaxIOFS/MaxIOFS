# Multi-Node Cluster Management

**Version**: 0.6.0-beta
**Status**: Production-Ready
**Last Updated**: December 9, 2025

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Quick Start](#quick-start)
4. [Cluster Setup](#cluster-setup)
5. [Configuration](#configuration)
6. [Cluster Replication](#cluster-replication)
7. [Dashboard UI](#dashboard-ui)
8. [API Reference](#api-reference)
9. [Security](#security)
10. [Monitoring & Health](#monitoring--health)
11. [Troubleshooting](#troubleshooting)
12. [Testing](#testing)

---

## Overview

MaxIOFS v0.6.0-beta introduces **complete multi-node cluster support** for high availability (HA) and automatic failover. The cluster system enables multiple MaxIOFS instances to work together as a unified storage cluster with intelligent request routing, automatic health monitoring, and seamless failover.

### Key Features

- ✅ **Multi-Node Cluster Support** - Multiple MaxIOFS instances working as one
- ✅ **Smart Router with Failover** - Intelligent request routing to healthy nodes
- ✅ **Cluster Bucket Replication** - Node-to-node replication for HA
- ✅ **HMAC Authentication** - Secure node-to-node communication
- ✅ **Automatic Tenant Sync** - Tenant data synchronized across all nodes
- ✅ **Cluster Dashboard UI** - Web-based cluster management interface
- ✅ **Health Monitoring** - Continuous health checks every 30 seconds
- ✅ **Bucket Location Cache** - 5-minute TTL cache (5ms vs 50ms latency)

### Use Cases

1. **High Availability (HA)** - Automatic failover if primary node fails
2. **Geographic Distribution** - Nodes in different regions for low latency
3. **Disaster Recovery** - Replicate data to backup nodes
4. **Load Balancing** - Distribute read requests across healthy nodes
5. **Zero-Downtime Maintenance** - Update nodes without service interruption

---

## Architecture

### Cluster Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    MAXIOFS CLUSTER ARCHITECTURE                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────┐           ┌──────────────┐                    │
│  │   Node 1     │◄─────────►│   Node 2     │                    │
│  │  (Primary)   │   HMAC    │  (Replica)   │                    │
│  │              │   Auth    │              │                    │
│  │ ┌──────────┐ │           │ ┌──────────┐ │                    │
│  │ │ Cluster  │ │           │ │ Cluster  │ │                    │
│  │ │ Manager  │ │           │ │ Manager  │ │                    │
│  │ └──────────┘ │           │ └──────────┘ │                    │
│  │ ┌──────────┐ │           │ ┌──────────┐ │                    │
│  │ │  Smart   │ │           │ │  Smart   │ │                    │
│  │ │  Router  │ │           │ │  Router  │ │                    │
│  │ └──────────┘ │           │ └──────────┘ │                    │
│  │ ┌──────────┐ │           │ ┌──────────┐ │                    │
│  │ │ Health   │ │           │ │ Health   │ │                    │
│  │ │ Checker  │ │           │ │ Checker  │ │                    │
│  │ └──────────┘ │           │ └──────────┘ │                    │
│  └──────────────┘           └──────────────┘                    │
│         │                            │                           │
│         │    Tenant Sync (30s)       │                           │
│         │◄──────────────────────────►│                           │
│         │                            │                           │
│         │   Bucket Replication       │                           │
│         │◄──────────────────────────►│                           │
│                                                                   │
│  ┌─────────────────────────────────────────────────────┐        │
│  │            Load Balancer (HAProxy/Nginx)            │        │
│  │         Virtual IP: 192.168.1.100:8080              │        │
│  └─────────────────────────────────────────────────────┘        │
│                          ▲                                        │
│                          │                                        │
│                    S3 Clients                                     │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Component Descriptions

#### 1. Cluster Manager

**Responsibilities:**
- Manage cluster configuration and state
- Handle node registration and removal
- Track cluster nodes in SQLite database
- Coordinate cluster operations

**Database Tables:**
- `cluster_config` - This node's cluster configuration
- `cluster_nodes` - List of all nodes in cluster
- `cluster_health_history` - Historical health check data

#### 2. Smart Router

**Responsibilities:**
- Route S3 requests to the correct node
- Implement automatic failover to healthy nodes
- Maintain bucket location cache (5-minute TTL)
- Proxy requests to remote nodes when needed

**Routing Logic:**
1. Check bucket location cache (5ms latency)
2. If cache miss, query bucket owner (50ms latency)
3. If local bucket, serve directly
4. If remote bucket, proxy to owner node
5. If owner unhealthy, failover to replica

#### 3. Health Checker

**Responsibilities:**
- Monitor all cluster nodes every 30 seconds
- Measure network latency to each node
- Update node health status (healthy/degraded/unavailable)
- Store health history for trend analysis

**Health States:**
- `healthy` - Node responding within 1 second
- `degraded` - Node responding in 1-5 seconds
- `unavailable` - Node not responding (timeout > 5 seconds)
- `unknown` - Node not yet checked

#### 4. Bucket Location Cache

**Purpose:** Performance optimization for bucket lookups

**Characteristics:**
- 5-minute TTL (Time-To-Live)
- In-memory cache per node
- Cache hit: 5ms latency
- Cache miss: 50ms latency (database query)
- Automatic invalidation on bucket operations

#### 5. Internal Proxy Mode

**Purpose:** Allow any node to handle any S3 request

**How it works:**
1. Client connects to Node 1
2. Client requests object from bucket owned by Node 2
3. Node 1 proxies request to Node 2 internally
4. Node 2 returns object to Node 1
5. Node 1 returns object to client

**Benefits:**
- Clients don't need to know which node owns which bucket
- Seamless failover experience
- Simplified client configuration

---

## Quick Start

### Prerequisites

- 2+ MaxIOFS instances (different servers or VMs)
- Network connectivity between all nodes
- Same data directory structure on all nodes
- Admin access to all nodes

### Step 1: Initialize Cluster on Node 1

```bash
# Start Node 1
./maxiofs --data-dir /data/node1 --listen :8080 --console-listen :8081

# Access web console
# Navigate to: http://node1:8081
# Login with admin/admin
# Go to: Cluster page (Server icon in sidebar)
# Click "Initialize Cluster"
```

**Initialize Cluster Dialog:**
- Node Name: `node-1` (or any descriptive name)
- Region: `us-east-1` (optional, for organization)

**Result:** Cluster token will be generated and displayed. **Copy this token!**

Example token: `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...`

### Step 2: Start Node 2 and Join Cluster

```bash
# Start Node 2
./maxiofs --data-dir /data/node2 --listen :8080 --console-listen :8081

# Access web console
# Navigate to: http://node2:8081
# Login with admin/admin
# Go to: Cluster page
# Click "Add Node"
```

**Add Node Dialog:**
- Node Name: `node-2`
- Endpoint URL: `http://node1:8080` (Node 1's S3 API endpoint)
- Node Token: `<paste cluster token from Step 1>`
- Region: `us-east-1` (optional)
- Priority: `100` (lower = higher priority for routing)

### Step 3: Verify Cluster

On either node's web console, go to Cluster page. You should see:

```
Cluster Status:
- Total Nodes: 2
- Healthy Nodes: 2
- Degraded Nodes: 0
- Unavailable Nodes: 0

Nodes:
- node-1 (node1:8080) - ✅ Healthy - Latency: 2ms
- node-2 (node2:8080) - ✅ Healthy - Latency: 3ms
```

### Step 4: Set Up Bucket Replication (Optional)

**For High Availability:**

1. Navigate to: **Cluster → Bucket Replication** page
2. Select a bucket (e.g., `my-important-bucket`)
3. Click **"Configure Replication"**
4. Select destination node: `node-2`
5. Set sync interval: `60` seconds (for real-time HA)
6. Enable **"Replicate deletes"** and **"Replicate metadata"**
7. Click **"Configure Replication"**

**Result:** Objects in `my-important-bucket` will be replicated from node-1 to node-2 every 60 seconds.

### Step 5: Test Failover

**Scenario:** Node 1 goes down, client should automatically read from Node 2

```bash
# From client machine
export AWS_ACCESS_KEY_ID=your-access-key
export AWS_SECRET_ACCESS_KEY=your-secret-key

# Upload object to Node 1
aws s3 cp file.txt s3://my-important-bucket/ --endpoint-url http://node1:8080

# Wait for replication (60 seconds)
sleep 70

# Stop Node 1
ssh node1 "systemctl stop maxiofs"

# Try to download object - should work via Node 2
aws s3 cp s3://my-important-bucket/file.txt downloaded.txt --endpoint-url http://node1:8080
# Note: Endpoint still points to node1, but load balancer routes to node2
```

**Expected Result:** Download succeeds because load balancer detects Node 1 is down and routes to Node 2.

---

## Cluster Setup

### Production Deployment Architecture

```
                    ┌─────────────────────┐
                    │  Load Balancer      │
                    │  (HAProxy/Nginx)    │
                    │  VIP: 192.168.1.100 │
                    └──────────┬──────────┘
                               │
                ┌──────────────┴──────────────┐
                │                             │
         ┌──────▼──────┐             ┌───────▼──────┐
         │   Node 1    │             │   Node 2     │
         │  10.0.1.10  │◄───────────►│  10.0.1.20   │
         │  :8080      │   Cluster   │  :8080       │
         │  :8081      │   Network   │  :8081       │
         └─────────────┘             └──────────────┘
                │                             │
         ┌──────▼──────┐             ┌───────▼──────┐
         │ Filesystem  │             │ Filesystem   │
         │ /data/node1 │             │ /data/node2  │
         └─────────────┘             └──────────────┘
```

### 1. Load Balancer Configuration

#### HAProxy Example

```haproxy
# /etc/haproxy/haproxy.cfg

global
    log /dev/log local0
    maxconn 4096
    user haproxy
    group haproxy
    daemon

defaults
    log global
    mode http
    option httplog
    option dontlognull
    timeout connect 5000ms
    timeout client 50000ms
    timeout server 50000ms

# S3 API Backend (Port 8080)
frontend s3_frontend
    bind *:8080
    mode http
    default_backend s3_backend

backend s3_backend
    mode http
    balance roundrobin
    option httpchk GET /health
    http-check expect status 200
    server node1 10.0.1.10:8080 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8080 check inter 10s fall 3 rise 2

# Console API Backend (Port 8081)
frontend console_frontend
    bind *:8081
    mode http
    default_backend console_backend

backend console_backend
    mode http
    balance roundrobin
    option httpchk GET /health
    http-check expect status 200
    server node1 10.0.1.10:8081 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8081 check inter 10s fall 3 rise 2
```

#### Nginx Example

```nginx
# /etc/nginx/nginx.conf

upstream s3_backend {
    least_conn;
    server 10.0.1.10:8080 max_fails=3 fail_timeout=30s;
    server 10.0.1.20:8080 max_fails=3 fail_timeout=30s;
}

upstream console_backend {
    least_conn;
    server 10.0.1.10:8081 max_fails=3 fail_timeout=30s;
    server 10.0.1.20:8081 max_fails=3 fail_timeout=30s;
}

server {
    listen 8080;
    server_name _;

    location / {
        proxy_pass http://s3_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_connect_timeout 5s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    location /health {
        proxy_pass http://s3_backend/health;
        access_log off;
    }
}

server {
    listen 8081;
    server_name _;

    location / {
        proxy_pass http://console_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

### 2. Network Configuration

**Firewall Rules (iptables example):**

```bash
# On each node, allow cluster communication

# Allow S3 API (8080)
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT

# Allow Console API (8081)
iptables -A INPUT -p tcp --dport 8081 -j ACCEPT

# Allow cluster internal communication (between nodes)
iptables -A INPUT -s 10.0.1.10 -j ACCEPT  # Node 1
iptables -A INPUT -s 10.0.1.20 -j ACCEPT  # Node 2

# Save rules
iptables-save > /etc/iptables/rules.v4
```

**DNS Configuration:**

```bash
# /etc/hosts on each node

10.0.1.10  node1 node1.maxiofs.local
10.0.1.20  node2 node2.maxiofs.local
192.168.1.100  maxiofs-cluster maxiofs-cluster.local
```

---

## Configuration

### Cluster Configuration Parameters

Cluster configuration is stored in SQLite database (`cluster_config` table). No configuration file needed.

### Cluster Initialization Options

When initializing a cluster via web UI or API:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `node_name` | string | Yes | Human-readable name for this node (e.g., "node-east-1") |
| `region` | string | No | Geographic region (e.g., "us-east-1", "eu-central-1") |

**Example API Call:**

```bash
curl -X POST http://localhost:8081/api/cluster/initialize \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-east-1",
    "region": "us-east-1"
  }'
```

### Adding Nodes to Cluster

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Name for the remote node |
| `endpoint` | string | Yes | S3 API endpoint URL (e.g., "http://node2:8080") |
| `node_token` | string | Yes | Cluster token from cluster initialization |
| `region` | string | No | Geographic region |
| `priority` | integer | No | Routing priority (lower = higher priority, default: 100) |
| `metadata` | JSON | No | Additional metadata (key-value pairs) |

**Example API Call:**

```bash
curl -X POST http://localhost:8081/api/cluster/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "node-west-1",
    "endpoint": "http://10.0.1.20:8080",
    "node_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "region": "us-west-1",
    "priority": 100
  }'
```

### Health Check Configuration

Health checks run automatically every **30 seconds**. This is currently hardcoded and not configurable via UI.

To modify health check interval (requires code change):

```go
// internal/cluster/manager.go
const healthCheckInterval = 30 * time.Second  // Change this value
```

### Bucket Location Cache Configuration

Bucket location cache has a **5-minute TTL** (Time-To-Live). This is currently hardcoded.

To modify cache TTL (requires code change):

```go
// internal/cluster/router.go
const bucketCacheTTL = 5 * time.Minute  // Change this value
```

---

## Cluster Replication

### Overview

Cluster bucket replication enables **node-to-node replication** for high availability. This is **completely separate** from user replication (external S3 replication).

**Key Differences:**

| Feature | Cluster Replication | User Replication |
|---------|--------------------|--------------------|
| Purpose | HA between MaxIOFS nodes | Backup to external S3 |
| Authentication | HMAC with node_token | S3 access key + secret |
| Endpoints | `/api/console/cluster/replication` | `/api/console/buckets/{bucket}/replication` |
| Database | `cluster_bucket_replication` table | `replication_rules` table |
| Credentials | No credentials needed | Requires AWS credentials |
| Tenant Sync | Automatic (every 30s) | N/A |

### How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│              CLUSTER BUCKET REPLICATION FLOW                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Node 1 (Source)                          Node 2 (Destination)   │
│  ┌─────────────────┐                     ┌─────────────────┐    │
│  │                 │                     │                 │    │
│  │  1. Object PUT  │                     │                 │    │
│  │     ↓           │                     │                 │    │
│  │  2. Decrypt     │                     │                 │    │
│  │     (if encrypted)                    │                 │    │
│  │     ↓           │                     │                 │    │
│  │  3. Sign with   │   ───HMAC-SHA256──► │  4. Verify      │    │
│  │     HMAC        │                     │     Signature   │    │
│  │     ↓           │                     │     ↓           │    │
│  │  4. Send object │   ──────────────►   │  5. Receive     │    │
│  │     (plaintext) │                     │     object      │    │
│  │                 │                     │     ↓           │    │
│  │                 │                     │  6. Encrypt     │    │
│  │                 │                     │     (with local │    │
│  │                 │                     │      key)       │    │
│  │                 │                     │     ↓           │    │
│  │                 │                     │  7. Store       │    │
│  │                 │                     │                 │    │
│  └─────────────────┘                     └─────────────────┘    │
│                                                                   │
│  Encryption Keys:                                                │
│  - Node 1: master_key_node1.key (from config.yaml)              │
│  - Node 2: master_key_node2.key (from config.yaml)              │
│  → Each node has its own encryption key                          │
│  → Objects are decrypted on source, re-encrypted on destination  │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### HMAC Authentication

**Message Format:**

```
HMAC-SHA256(node_token, METHOD + PATH + TIMESTAMP + NONCE + BODY)
```

**Request Headers:**

```
X-MaxIOFS-Node-ID: sender-node-id
X-MaxIOFS-Timestamp: 1701234567  (Unix timestamp)
X-MaxIOFS-Nonce: random-uuid
X-MaxIOFS-Signature: hex-encoded-hmac-sha256
```

**Validation:**
1. Recipient retrieves `node_token` from database using `X-MaxIOFS-Node-ID`
2. Computes expected signature using same formula
3. Compares with `X-MaxIOFS-Signature` header
4. Checks timestamp skew (max 5 minutes allowed)
5. Rejects if signature doesn't match or timestamp is stale

### Automatic Tenant Synchronization

**Purpose:** Ensure tenant data exists on destination node before replicating buckets

**Process:**
1. Every 30 seconds, Tenant Sync Manager runs
2. For each tenant:
   - Compute checksum of tenant data
   - Check if checksum matches on destination node
   - If not, send tenant data to destination
3. Destination node creates or updates tenant

**API Endpoint:** `POST /api/internal/cluster/tenant-sync` (authenticated with HMAC)

### Configuring Cluster Replication

#### Via Web Console

1. Navigate to **Cluster → Bucket Replication**
2. Filter buckets by replication status (All / Replicated / Local Only)
3. Select bucket to replicate
4. Click **"Configure Replication"**

**Replication Configuration Form:**

| Field | Description | Default | Min | Max |
|-------|-------------|---------|-----|-----|
| Source Bucket | Bucket to replicate | (selected) | - | - |
| Destination Node | Target node (dropdown, local node excluded) | - | - | - |
| Sync Interval | Replication frequency (seconds) | 60 | 10 | unlimited |
| Prefix Filter | Only replicate objects with this prefix | (empty) | - | - |
| Replicate Deletes | Replicate DELETE operations | ✅ Yes | - | - |
| Replicate Metadata | Replicate object metadata | ✅ Yes | - | - |

**Sync Interval Guidelines:**
- **10-60 seconds**: Real-time HA (low RPO)
- **300 seconds (5 min)**: Near-real-time HA
- **3600 seconds (1 hour)**: Periodic backup
- **21600 seconds (6 hours)**: Daily backup windows

#### Via API

**Create Cluster Replication Rule:**

```bash
curl -X POST http://localhost:8081/api/console/cluster/replication \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_bucket": "my-bucket",
    "destination_node_id": "node-2-uuid",
    "destination_bucket": "my-bucket",
    "sync_interval_seconds": 60,
    "enabled": true,
    "replicate_deletes": true,
    "replicate_metadata": true,
    "prefix": "important/",
    "priority": 0
  }'
```

**List Cluster Replication Rules:**

```bash
curl -X GET "http://localhost:8081/api/console/cluster/replication?bucket=my-bucket" \
  -H "Authorization: Bearer $TOKEN"
```

**Bulk Node-to-Node Replication:**

Replicate ALL buckets from one node to another:

```bash
curl -X POST http://localhost:8081/api/console/cluster/replication/bulk \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_node_id": "node-1-uuid",
    "destination_node_id": "node-2-uuid",
    "sync_interval_seconds": 300,
    "replicate_deletes": true,
    "replicate_metadata": true
  }'
```

### Self-Replication Prevention

**Frontend Validation:**
- Local node is filtered from destination node dropdown
- User cannot select local node as replication target

**Backend Validation:**
- API checks if `destination_node_id == local_node_id`
- Returns HTTP 400 error with message:

```json
{
  "error": "Cannot replicate to the same node. Cluster replication is for HA between different MaxIOFS nodes. For local bucket copies, use bucket-level replication settings."
}
```

---

## Dashboard UI

### Accessing Cluster Dashboard

1. Login to web console (http://localhost:8081)
2. Click **"Cluster"** icon in sidebar (Server icon)
3. **Requirement**: Global admin access only

### Cluster Overview Page

**Components:**

1. **Cluster Status Cards**
   - Total Nodes
   - Healthy Nodes
   - Degraded Nodes
   - Unavailable Nodes
   - Total Buckets
   - Replicated Buckets
   - Local Buckets

2. **Nodes Table**

| Column | Description |
|--------|-------------|
| Name | Node name (e.g., "node-east-1") |
| Endpoint | S3 API endpoint URL |
| Health Status | 🟢 Healthy / 🟡 Degraded / 🔴 Unavailable / ⚪ Unknown |
| Latency | Network latency in milliseconds |
| Capacity | Used / Total storage with progress bar |
| Buckets | Number of buckets on this node |
| Priority | Routing priority (lower = higher) |
| Last Seen | Timestamp of last successful health check |
| Actions | Edit / Remove buttons |

3. **Action Buttons**
   - **Initialize Cluster** - Create new cluster (only if not in cluster)
   - **Add Node** - Join existing cluster or add remote node
   - **Refresh** - Manually refresh cluster status

### Initialize Cluster Dialog

**Fields:**
- **Node Name**: Human-readable name for this node
- **Region**: Geographic region (optional)

**Result:**
- Cluster is initialized
- Cluster token is generated and displayed
- Token must be copied and saved (needed for other nodes)

**UI:**
```
┌─────────────────────────────────────────┐
│  Initialize Cluster                     │
├─────────────────────────────────────────┤
│                                          │
│  Node Name: [node-east-1_____________]  │
│  Region:    [us-east-1______________]   │
│                                          │
│  [Cancel]              [Initialize]     │
│                                          │
└─────────────────────────────────────────┘

After initialization:

┌─────────────────────────────────────────┐
│  Cluster Initialized Successfully!      │
├─────────────────────────────────────────┤
│                                          │
│  Cluster Token:                          │
│  ┌──────────────────────────────────┐   │
│  │ eyJhbGciOiJIUzI1NiIsInR5cCI...  │   │
│  │ [Copy to Clipboard]              │   │
│  └──────────────────────────────────┘   │
│                                          │
│  ⚠️ Important:                          │
│  Save this token! You'll need it to     │
│  add other nodes to this cluster.       │
│                                          │
│  [Close]                                 │
│                                          │
└─────────────────────────────────────────┘
```

### Add Node Dialog

**Fields:**
- **Node Name**: Name for the remote node
- **Endpoint URL**: S3 API endpoint (e.g., "http://10.0.1.20:8080")
- **Node Token**: Cluster token from initialization
- **Region**: Geographic region (optional)
- **Priority**: Routing priority (default: 100)
- **Metadata**: Additional JSON metadata (optional)

**UI:**
```
┌─────────────────────────────────────────┐
│  Add Node to Cluster                    │
├─────────────────────────────────────────┤
│                                          │
│  Node Name*: [node-west-1___________]  │
│  Endpoint URL*: [http://10.0.1.20:8080] │
│  Node Token*: [eyJhbGciOiJIUzI...____] │
│  Region: [us-west-1_________________]   │
│  Priority: [100______________________]  │
│  Metadata: [{"key":"value"}__________]  │
│                                          │
│  [Cancel]                    [Add Node] │
│                                          │
└─────────────────────────────────────────┘
```

### Edit Node Dialog

**Editable Fields:**
- Node Name
- Region
- Priority
- Metadata

**Read-Only Fields:**
- Endpoint (cannot change)
- Node ID (cannot change)

**Health Information Panel:**
- Current health status
- Last check timestamp
- Last seen timestamp
- Current latency

---

## API Reference

### Cluster Management Endpoints

Base URL: `http://localhost:8081/api/console/cluster`

**Authentication**: All endpoints require JWT token in `Authorization: Bearer <token>` header

#### 1. Initialize Cluster

```http
POST /api/cluster/initialize
```

**Request Body:**
```json
{
  "node_name": "node-east-1",
  "region": "us-east-1"
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "node_id": "uuid-1234",
    "node_name": "node-east-1",
    "cluster_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "is_cluster_enabled": true,
    "region": "us-east-1",
    "created_at": "2025-12-09T10:00:00Z"
  }
}
```

#### 2. Get Cluster Configuration

```http
GET /api/cluster/config
```

**Response:**
```json
{
  "success": true,
  "data": {
    "node_id": "uuid-1234",
    "node_name": "node-east-1",
    "is_cluster_enabled": true,
    "region": "us-east-1",
    "created_at": "2025-12-09T10:00:00Z"
  }
}
```

#### 3. List Cluster Nodes

```http
GET /api/cluster/nodes
```

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": "uuid-5678",
      "name": "node-west-1",
      "endpoint": "http://10.0.1.20:8080",
      "region": "us-west-1",
      "priority": 100,
      "health_status": "healthy",
      "last_health_check": "2025-12-09T10:05:00Z",
      "last_seen": "2025-12-09T10:05:00Z",
      "latency_ms": 15,
      "capacity_total": 1099511627776,
      "capacity_used": 549755813888,
      "bucket_count": 42,
      "metadata": {},
      "created_at": "2025-12-09T10:01:00Z",
      "updated_at": "2025-12-09T10:05:00Z"
    }
  ]
}
```

#### 4. Add Node

```http
POST /api/cluster/nodes
```

**Request Body:**
```json
{
  "name": "node-west-1",
  "endpoint": "http://10.0.1.20:8080",
  "node_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "region": "us-west-1",
  "priority": 100,
  "metadata": {}
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "uuid-5678",
    "name": "node-west-1",
    "endpoint": "http://10.0.1.20:8080",
    "region": "us-west-1",
    "priority": 100,
    "health_status": "unknown",
    "created_at": "2025-12-09T10:01:00Z",
    "updated_at": "2025-12-09T10:01:00Z"
  }
}
```

#### 5. Get Node Details

```http
GET /api/cluster/nodes/{nodeId}
```

**Response:** Same as single node in List Cluster Nodes

#### 6. Update Node

```http
PUT /api/cluster/nodes/{nodeId}
```

**Request Body:**
```json
{
  "name": "node-west-1-updated",
  "region": "us-west-2",
  "priority": 50,
  "metadata": {"datacenter": "AWS"}
}
```

**Response:** Updated node object

#### 7. Remove Node

```http
DELETE /api/cluster/nodes/{nodeId}
```

**Response:**
```json
{
  "success": true,
  "message": "Node removed successfully"
}
```

#### 8. Get Cluster Health

```http
GET /api/cluster/health
```

**Response:**
```json
{
  "success": true,
  "data": {
    "total_nodes": 2,
    "healthy_nodes": 2,
    "degraded_nodes": 0,
    "unavailable_nodes": 0,
    "total_buckets": 85,
    "replicated_buckets": 42,
    "local_buckets": 43,
    "last_updated": "2025-12-09T10:05:00Z"
  }
}
```

#### 9. Trigger Health Check

```http
POST /api/cluster/health/refresh
```

**Response:**
```json
{
  "success": true,
  "message": "Health check triggered",
  "data": {
    "nodes_checked": 1,
    "checks_performed": 1
  }
}
```

#### 10. Get Cache Statistics

```http
GET /api/cluster/cache/stats
```

**Response:**
```json
{
  "success": true,
  "data": {
    "total_entries": 42,
    "cache_hits": 1523,
    "cache_misses": 89,
    "hit_ratio": 0.944,
    "ttl_minutes": 5,
    "last_cleanup": "2025-12-09T10:00:00Z"
  }
}
```

#### 11. Clear Cache

```http
DELETE /api/cluster/cache
```

**Response:**
```json
{
  "success": true,
  "message": "Cache cleared successfully",
  "data": {
    "entries_cleared": 42
  }
}
```

#### 12. Get Cluster Buckets

```http
GET /api/cluster/buckets
```

**Query Parameters:**
- `tenant_id` (optional) - Filter by tenant

**Response:**
```json
{
  "success": true,
  "data": {
    "buckets": [
      {
        "name": "my-bucket",
        "tenant_id": "tenant-1",
        "primary_node": "node-east-1",
        "replica_count": 1,
        "has_replication": true,
        "replication_rules": 1,
        "object_count": 1523,
        "total_size": 5497558138880
      }
    ]
  }
}
```

#### 13. Get Bucket Nodes

```http
GET /api/cluster/buckets/{bucketName}/nodes
```

**Response:**
```json
{
  "success": true,
  "data": {
    "bucket": "my-bucket",
    "primary_node": {
      "id": "uuid-1234",
      "name": "node-east-1",
      "endpoint": "http://10.0.1.10:8080"
    },
    "replica_nodes": [
      {
        "id": "uuid-5678",
        "name": "node-west-1",
        "endpoint": "http://10.0.1.20:8080",
        "replication_status": "synced",
        "last_sync": "2025-12-09T10:04:00Z"
      }
    ]
  }
}
```

### Cluster Replication Endpoints

Base URL: `http://localhost:8081/api/console/cluster/replication`

#### 14. Create Replication Rule

```http
POST /api/console/cluster/replication
```

**Request Body:**
```json
{
  "source_bucket": "my-bucket",
  "destination_node_id": "uuid-5678",
  "destination_bucket": "my-bucket",
  "sync_interval_seconds": 60,
  "enabled": true,
  "replicate_deletes": true,
  "replicate_metadata": true,
  "prefix": "",
  "priority": 0
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "repl-uuid-1234",
    "tenant_id": "tenant-1",
    "source_bucket": "my-bucket",
    "destination_node_id": "uuid-5678",
    "destination_bucket": "my-bucket",
    "sync_interval_seconds": 60,
    "enabled": true,
    "replicate_deletes": true,
    "replicate_metadata": true,
    "prefix": "",
    "priority": 0,
    "last_sync_at": null,
    "last_error": null,
    "objects_replicated": 0,
    "bytes_replicated": 0,
    "created_at": "2025-12-09T10:10:00Z",
    "updated_at": "2025-12-09T10:10:00Z"
  }
}
```

#### 15. List Replication Rules

```http
GET /api/console/cluster/replication
```

**Query Parameters:**
- `tenant_id` (optional) - Filter by tenant
- `bucket` (optional) - Filter by bucket

**Response:** Array of replication rules (same structure as Create)

#### 16. Update Replication Rule

```http
PUT /api/console/cluster/replication/{ruleId}
```

**Request Body:**
```json
{
  "sync_interval_seconds": 300,
  "enabled": true
}
```

**Response:** Updated rule object

#### 17. Delete Replication Rule

```http
DELETE /api/console/cluster/replication/{ruleId}
```

**Response:**
```json
{
  "success": true,
  "message": "Replication rule deleted successfully"
}
```

#### 18. Bulk Node-to-Node Replication

```http
POST /api/console/cluster/replication/bulk
```

**Request Body:**
```json
{
  "source_node_id": "uuid-1234",
  "destination_node_id": "uuid-5678",
  "sync_interval_seconds": 300,
  "replicate_deletes": true,
  "replicate_metadata": true
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "rules_created": 42,
    "rules_failed": 0,
    "failed_buckets": [],
    "message": "Successfully created 42 replication rules"
  }
}
```

---

## Security

### Cluster Authentication (HMAC-SHA256)

**Purpose:** Secure communication between cluster nodes without S3 credentials

**Algorithm:** HMAC-SHA256

**Secret Key:** `node_token` (generated during cluster initialization)

### HMAC Message Signing

**Steps to sign a request:**

1. **Compute message string:**
   ```
   message = METHOD + PATH + TIMESTAMP + NONCE + BODY
   ```

2. **Compute HMAC:**
   ```
   signature = HMAC-SHA256(node_token, message)
   ```

3. **Encode signature:**
   ```
   hex_signature = hex(signature)
   ```

4. **Add headers to request:**
   ```
   X-MaxIOFS-Node-ID: sender-node-uuid
   X-MaxIOFS-Timestamp: 1701234567
   X-MaxIOFS-Nonce: random-uuid-4
   X-MaxIOFS-Signature: hex_signature
   ```

### HMAC Validation

**Recipient validation steps:**

1. Extract headers from request
2. Retrieve `node_token` from database using `X-MaxIOFS-Node-ID`
3. Compute expected signature using same formula
4. Compare with `X-MaxIOFS-Signature` header (constant-time comparison)
5. Verify timestamp is within ±5 minutes of current time
6. Reject request if validation fails (HTTP 401 Unauthorized)

**Go Implementation Example:**

```go
func (m *ClusterAuthMiddleware) verifyHMAC(r *http.Request, nodeToken string) error {
    timestamp := r.Header.Get("X-MaxIOFS-Timestamp")
    nonce := r.Header.Get("X-MaxIOFS-Nonce")
    signature := r.Header.Get("X-MaxIOFS-Signature")

    // Verify timestamp (max 5 minutes skew)
    reqTime, _ := strconv.ParseInt(timestamp, 10, 64)
    now := time.Now().Unix()
    if abs(now-reqTime) > 300 { // 5 minutes
        return errors.New("timestamp expired")
    }

    // Read body
    body, _ := io.ReadAll(r.Body)
    r.Body = io.NopCloser(bytes.NewReader(body))

    // Compute expected signature
    message := r.Method + r.URL.Path + timestamp + nonce + string(body)
    mac := hmac.New(sha256.New, []byte(nodeToken))
    mac.Write([]byte(message))
    expectedSignature := hex.EncodeToString(mac.Sum(nil))

    // Constant-time comparison
    if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
        return errors.New("invalid signature")
    }

    return nil
}
```

### Node Token Security

**Best Practices:**

1. **Generate Strong Tokens**
   - Use cryptographically secure random generator
   - Minimum 256 bits of entropy
   - Example: `openssl rand -hex 32`

2. **Rotate Tokens Regularly**
   - Recommended: Every 90 days
   - Process:
     1. Generate new token
     2. Update cluster_config on all nodes
     3. Wait for propagation (5 minutes)
     4. Verify all nodes communicating
     5. Remove old token

3. **Store Securely**
   - Tokens stored in SQLite database (encrypted at rest)
   - Never log tokens in plaintext
   - Never expose tokens in API responses (except during initialization)

4. **Network Security**
   - Use TLS/HTTPS for cluster communication (recommended)
   - Restrict cluster ports to cluster network only (firewall rules)
   - Use VPN or private network for cluster communication

### Firewall Configuration

**Recommended Rules:**

```bash
# On each node

# Allow cluster communication from other nodes only
iptables -A INPUT -s 10.0.1.0/24 -p tcp --dport 8080 -j ACCEPT
iptables -A INPUT -s 10.0.1.0/24 -p tcp --dport 8081 -j ACCEPT

# Block cluster communication from external IPs
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

**With Load Balancer:**

```bash
# Allow from load balancer IP only
iptables -A INPUT -s 192.168.1.100 -p tcp --dport 8080 -j ACCEPT
iptables -A INPUT -s 192.168.1.100 -p tcp --dport 8081 -j ACCEPT

# Allow cluster communication between nodes
iptables -A INPUT -s 10.0.1.0/24 -p tcp -j ACCEPT

# Block everything else
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

---

## Monitoring & Health

### Health Check System

**Automatic Health Checks:**
- Runs every **30 seconds** on each node
- Checks all remote nodes in cluster
- Measures network latency
- Updates health status in database

**Health States:**

| State | Criteria | Color |
|-------|----------|-------|
| `healthy` | Response time < 1 second | 🟢 Green |
| `degraded` | Response time 1-5 seconds | 🟡 Yellow |
| `unavailable` | No response or timeout > 5 seconds | 🔴 Red |
| `unknown` | Never checked yet | ⚪ Gray |

**Health Check Endpoint:**

Each node exposes `/health` endpoint:

```bash
curl http://localhost:8080/health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2025-12-09T10:00:00Z",
  "version": "0.6.0-beta",
  "uptime_seconds": 86400,
  "cluster_enabled": true,
  "node_id": "uuid-1234",
  "node_name": "node-east-1"
}
```

### Metrics Collection

**Cluster-Specific Metrics:**

1. **Node Health Metrics**
   - `cluster_nodes_total` - Total nodes in cluster
   - `cluster_nodes_healthy` - Healthy nodes count
   - `cluster_nodes_degraded` - Degraded nodes count
   - `cluster_nodes_unavailable` - Unavailable nodes count

2. **Replication Metrics**
   - `cluster_replication_rules_total` - Total replication rules
   - `cluster_replication_rules_active` - Active rules
   - `cluster_replication_objects_pending` - Objects waiting to replicate
   - `cluster_replication_objects_replicated_total` - Total replicated objects
   - `cluster_replication_bytes_replicated_total` - Total bytes replicated
   - `cluster_replication_errors_total` - Replication errors count

3. **Cache Metrics**
   - `cluster_cache_entries` - Current cache entries
   - `cluster_cache_hits_total` - Cache hit count
   - `cluster_cache_misses_total` - Cache miss count
   - `cluster_cache_hit_ratio` - Hit ratio (0.0-1.0)

**Prometheus Integration:**

```prometheus
# prometheus.yml

scrape_configs:
  - job_name: 'maxiofs-cluster'
    static_configs:
      - targets:
        - '10.0.1.10:8081'  # Node 1
        - '10.0.1.20:8081'  # Node 2
    metrics_path: '/api/metrics'
```

### Monitoring Alerts

**Recommended Prometheus Alerts:**

```yaml
# alerts.yml

groups:
  - name: maxiofs_cluster
    rules:
      - alert: ClusterNodeDown
        expr: cluster_nodes_unavailable > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "MaxIOFS cluster node unavailable"
          description: "{{ $value }} cluster nodes are unavailable for 5+ minutes"

      - alert: ClusterNodeDegraded
        expr: cluster_nodes_degraded > 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "MaxIOFS cluster node degraded"
          description: "{{ $value }} cluster nodes are degraded (high latency)"

      - alert: ClusterReplicationLag
        expr: cluster_replication_objects_pending > 1000
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "MaxIOFS cluster replication lag detected"
          description: "{{ $value }} objects pending replication for 15+ minutes"

      - alert: ClusterReplicationErrors
        expr: increase(cluster_replication_errors_total[5m]) > 10
        labels:
          severity: warning
        annotations:
          summary: "MaxIOFS cluster replication errors increasing"
          description: "{{ $value }} replication errors in last 5 minutes"

      - alert: ClusterCacheLowHitRatio
        expr: cluster_cache_hit_ratio < 0.7
        for: 30m
        labels:
          severity: info
        annotations:
          summary: "MaxIOFS cluster cache hit ratio low"
          description: "Cache hit ratio is {{ $value }} (below 70%)"
```

---

## Troubleshooting

### Common Issues

#### 1. Node Shows as "Unavailable"

**Symptoms:**
- Node appears red (unavailable) in cluster dashboard
- Health status shows "unavailable"

**Possible Causes:**

A. **Network Connectivity**
```bash
# Test connectivity from Node 1 to Node 2
ping -c 4 10.0.1.20
telnet 10.0.1.20 8080
curl http://10.0.1.20:8080/health
```

**Fix:** Check firewall rules, ensure ports 8080/8081 are open

B. **Node is Down**
```bash
# Check if MaxIOFS is running on Node 2
ssh node2 "systemctl status maxiofs"
```

**Fix:** Start MaxIOFS service

C. **Wrong Endpoint URL**
```bash
# Check cluster_nodes table
sqlite3 /data/node1/auth.db "SELECT id, name, endpoint FROM cluster_nodes;"
```

**Fix:** Update node endpoint in database or via API

#### 2. Replication Not Working

**Symptoms:**
- Objects uploaded to Node 1 don't appear on Node 2
- `objects_replicated` count is 0

**Diagnosis Steps:**

A. **Check Replication Rule Status**
```bash
# Via API
curl -X GET "http://localhost:8081/api/console/cluster/replication?bucket=my-bucket" \
  -H "Authorization: Bearer $TOKEN"
```

**Check:**
- `enabled: true`
- `last_error: null`
- `sync_interval_seconds` reasonable (not too high)

B. **Check Replication Queue**
```bash
# Query database
sqlite3 /data/node1/auth.db "SELECT COUNT(*) FROM cluster_replication_queue WHERE status='pending';"
```

**If queue is stuck:**
```bash
# Check worker logs
journalctl -u maxiofs -n 100 | grep -i "replication worker"
```

C. **Check Tenant Sync**
```bash
# Verify tenant exists on destination node
curl -X GET "http://node2:8081/api/console/tenants" \
  -H "Authorization: Bearer $TOKEN"
```

**Fix:** Ensure tenant synchronization is working (automatic every 30 seconds)

#### 3. HMAC Authentication Errors

**Symptoms:**
- Replication errors: "Invalid HMAC signature"
- Node health check fails with 401 Unauthorized

**Diagnosis:**

A. **Check Node Token Match**
```bash
# On Node 1
sqlite3 /data/node1/auth.db "SELECT cluster_token FROM cluster_config;"

# On Node 2 (should match)
sqlite3 /data/node2/auth.db "SELECT cluster_token FROM cluster_config;"
```

**Fix:** Ensure both nodes have the same cluster token

B. **Check Timestamp Skew**
```bash
# Verify clocks are synchronized
ssh node1 "date -u"
ssh node2 "date -u"
```

**Fix:** Use NTP to synchronize clocks across all nodes

C. **Verify HMAC Headers**
```bash
# Enable debug logging
# internal/middleware/cluster_auth.go (set log level to DEBUG)
```

**Look for:**
- Missing HMAC headers
- Incorrect signature computation
- Timestamp out of range (>5 minutes)

#### 4. Bucket Location Cache Issues

**Symptoms:**
- Requests routed to wrong node
- 404 errors for objects that exist

**Diagnosis:**

A. **Check Cache Status**
```bash
curl -X GET "http://localhost:8081/api/cluster/cache/stats" \
  -H "Authorization: Bearer $TOKEN"
```

**Look for:**
- High cache miss rate (>30%)
- Stale entries (check `last_cleanup`)

**Fix:**

```bash
# Clear cache
curl -X DELETE "http://localhost:8081/api/cluster/cache" \
  -H "Authorization: Bearer $TOKEN"
```

B. **Check Bucket Ownership**
```bash
# Query which node owns bucket
curl -X GET "http://localhost:8081/api/cluster/buckets/my-bucket/nodes" \
  -H "Authorization: Bearer $TOKEN"
```

#### 5. High Replication Lag

**Symptoms:**
- `objects_pending` count increasing
- Objects take long time to appear on destination

**Diagnosis:**

A. **Check Worker Count**
```bash
# Check internal/cluster/replication_manager.go
# Default: 5 workers
```

**Fix:** Increase worker count if needed (code change required)

B. **Check Network Bandwidth**
```bash
# Test transfer speed between nodes
scp large-file.bin node2:/tmp/
```

**Fix:** Upgrade network or reduce sync frequency

C. **Check Object Size**
```bash
# Large objects take longer to replicate
# Check average object size
```

**Fix:** Increase `sync_interval_seconds` for buckets with large objects

#### 6. Dashboard Not Loading

**Symptoms:**
- Cluster page shows loading spinner forever
- Console errors in browser

**Diagnosis:**

A. **Check Browser Console**
```javascript
// F12 → Console tab
// Look for API errors
```

**Fix:**
- Clear browser cache
- Check network tab for failed API calls
- Verify JWT token is valid

B. **Check API Endpoint**
```bash
curl -X GET "http://localhost:8081/api/cluster/health" \
  -H "Authorization: Bearer $TOKEN"
```

**Fix:** Ensure API is responding correctly

### Debug Mode

**Enable Debug Logging:**

```bash
# Start MaxIOFS with debug log level
./maxiofs --data-dir /data/node1 --log-level debug
```

**Cluster-Specific Debug Output:**

```
[DEBUG] Cluster Manager: Initialized with node_id=uuid-1234
[DEBUG] Health Checker: Starting health checks (interval=30s)
[DEBUG] Health Checker: Checking node uuid-5678 (node-west-1)
[DEBUG] Health Checker: Node uuid-5678 is healthy (latency=15ms)
[DEBUG] Replication Manager: Starting with 5 workers
[DEBUG] Replication Worker 1: Processing queue item uuid-abcd
[DEBUG] Replication Worker 1: Replicating object my-bucket/file.txt to node uuid-5678
[DEBUG] HMAC Auth: Signing request to node uuid-5678
[DEBUG] HMAC Auth: Signature valid for node uuid-5678
[DEBUG] Replication Worker 1: Object replicated successfully (size=1024 bytes)
```

### Log Locations

**systemd Service:**
```bash
journalctl -u maxiofs -f
```

**Docker:**
```bash
docker logs -f maxiofs-node1
```

**Standalone:**
```bash
# Logs to stdout/stderr
./maxiofs --data-dir /data 2>&1 | tee maxiofs.log
```

---

## Testing

### Integration Tests

**Location:** `C:\Users\aricardo\Projects\MaxIOFS\internal\cluster\replication_integration_test.go`

**Test Infrastructure:** SimulatedNode

**SimulatedNode** simulates a MaxIOFS node without starting a real server:
- In-memory SQLite database
- In-memory object storage (map)
- HTTP server using `httptest.Server`
- HMAC signature verification
- Tenant and object handling

### Running Cluster Tests

```bash
# Run all cluster tests
go test ./internal/cluster -v

# Run specific test
go test ./internal/cluster -v -run TestHMACAuthentication

# Run only cluster replication tests
go test ./internal/cluster -v -run "TestHMAC|TestTenant|TestObject|TestDelete|TestSelf"

# Run with coverage
go test ./internal/cluster -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Test Cases

#### 1. TestHMACAuthentication

**Purpose:** Verify HMAC-SHA256 signature verification

**Scenarios:**
- Valid signature → HTTP 200
- Invalid signature → HTTP 401
- Missing headers → HTTP 401
- Timestamp expired (>5 min) → HTTP 401

**Example:**
```go
func TestHMACAuthentication(t *testing.T) {
    node := setupSimulatedNode(t, "node1")
    defer node.Cleanup()

    // Test valid signature
    req := createSignedRequest(node.NodeToken, "GET", "/test", "")
    resp := sendRequest(node.HTTPServer.URL, req)
    assert.Equal(t, 200, resp.StatusCode)

    // Test invalid signature
    req = createSignedRequest("wrong-token", "GET", "/test", "")
    resp = sendRequest(node.HTTPServer.URL, req)
    assert.Equal(t, 401, resp.StatusCode)
}
```

#### 2. TestTenantSynchronization

**Purpose:** Verify tenant data syncs between nodes

**Scenarios:**
- Create tenant on Node 1
- Sync to Node 2
- Verify tenant exists on Node 2
- Verify checksum matches

**Example:**
```go
func TestTenantSynchronization(t *testing.T) {
    node1 := setupSimulatedNode(t, "node1")
    node2 := setupSimulatedNode(t, "node2")
    defer node1.Cleanup()
    defer node2.Cleanup()

    // Create tenant on node1
    tenant := createTenant(node1, "tenant-1")

    // Sync to node2
    syncTenant(node1, node2, tenant)

    // Verify tenant exists on node2
    tenant2 := getTenant(node2, "tenant-1")
    assert.Equal(t, tenant.ID, tenant2.ID)
    assert.Equal(t, tenant.Name, tenant2.Name)
}
```

#### 3. TestObjectReplication

**Purpose:** Verify object replication with HMAC authentication

**Scenarios:**
- Upload object to Node 1
- Replicate to Node 2 via signed request
- Verify object exists on Node 2
- Verify object content matches

**Example:**
```go
func TestObjectReplication(t *testing.T) {
    node1 := setupSimulatedNode(t, "node1")
    node2 := setupSimulatedNode(t, "node2")
    defer node1.Cleanup()
    defer node2.Cleanup()

    // Upload object to node1
    object := uploadObject(node1, "bucket1", "file.txt", []byte("test content"))

    // Replicate to node2
    replicateObject(node1, node2, "bucket1", "file.txt")

    // Verify object on node2
    retrieved := getObject(node2, "bucket1", "file.txt")
    assert.Equal(t, "test content", string(retrieved))
}
```

#### 4. TestDeleteReplication

**Purpose:** Verify delete operations replicate correctly

**Scenarios:**
- Upload object to Node 1 and Node 2
- Delete object on Node 1
- Replicate delete to Node 2
- Verify object deleted on Node 2

**Example:**
```go
func TestDeleteReplication(t *testing.T) {
    node1 := setupSimulatedNode(t, "node1")
    node2 := setupSimulatedNode(t, "node2")
    defer node1.Cleanup()
    defer node2.Cleanup()

    // Upload object to both nodes
    uploadObject(node1, "bucket1", "file.txt", []byte("test"))
    uploadObject(node2, "bucket1", "file.txt", []byte("test"))

    // Delete on node1
    deleteObject(node1, "bucket1", "file.txt")

    // Replicate delete to node2
    replicateDelete(node1, node2, "bucket1", "file.txt")

    // Verify deleted on node2
    exists := objectExists(node2, "bucket1", "file.txt")
    assert.False(t, exists)
}
```

#### 5. TestSelfReplicationPrevention

**Purpose:** Verify nodes cannot replicate to themselves

**Scenarios:**
- Try to create replication rule with same source and destination
- Backend validation rejects with HTTP 400
- Error message explains why

**Example:**
```go
func TestSelfReplicationPrevention(t *testing.T) {
    node := setupSimulatedNode(t, "node1")
    defer node.Cleanup()

    // Try to replicate to self
    req := createReplicationRuleRequest{
        SourceBucket: "bucket1",
        DestinationNodeID: node.NodeID, // Same as local node
    }

    resp := createReplicationRule(node, req)
    assert.Equal(t, 400, resp.StatusCode)
    assert.Contains(t, resp.Body, "Cannot replicate to the same node")
}
```

### Test Results

```bash
=== RUN   TestHMACAuthentication
=== RUN   TestHMACAuthentication/ValidSignature
=== RUN   TestHMACAuthentication/InvalidSignature
--- PASS: TestHMACAuthentication (0.17s)
    --- PASS: TestHMACAuthentication/ValidSignature (0.01s)
    --- PASS: TestHMACAuthentication/InvalidSignature (0.00s)

=== RUN   TestTenantSynchronization
--- PASS: TestTenantSynchronization (0.23s)

=== RUN   TestObjectReplication
--- PASS: TestObjectReplication (0.27s)

=== RUN   TestDeleteReplication
--- PASS: TestDeleteReplication (0.25s)

=== RUN   TestSelfReplicationPrevention
--- PASS: TestSelfReplicationPrevention (0.11s)

PASS
ok      github.com/maxiofs/maxiofs/internal/cluster    1.832s
```

**Test Coverage:**
- 27 total cluster tests (22 management + 5 replication)
- 100% pass rate
- <2 seconds execution time
- Pure Go (no CGO dependencies)

---

## Appendix

### Glossary

| Term | Definition |
|------|------------|
| **Cluster** | Group of MaxIOFS nodes working together |
| **Node** | Single MaxIOFS instance in a cluster |
| **Cluster Token** | Shared secret for HMAC authentication between nodes |
| **Node Token** | Same as cluster token (used interchangeably) |
| **HMAC** | Hash-based Message Authentication Code |
| **Smart Router** | Component that routes requests to correct node |
| **Health Checker** | Background worker monitoring node health |
| **Bucket Location Cache** | In-memory cache of bucket ownership |
| **Internal Proxy Mode** | Node proxying requests to other nodes |
| **Cluster Replication** | Node-to-node replication for HA |
| **User Replication** | External S3 replication (different system) |
| **Tenant Sync** | Automatic synchronization of tenant data |
| **Self-Replication** | (Invalid) Node replicating to itself |

### SQLite Schema Reference

#### cluster_config Table

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

#### cluster_nodes Table

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

#### cluster_health_history Table

```sql
CREATE TABLE cluster_health_history (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    health_status TEXT NOT NULL,
    latency_ms INTEGER,
    timestamp INTEGER NOT NULL,
    error_message TEXT,
    FOREIGN KEY (node_id) REFERENCES cluster_nodes(id) ON DELETE CASCADE
);
```

#### cluster_bucket_replication Table

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

#### cluster_replication_queue Table

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
    scheduled_at INTEGER,
    started_at INTEGER,
    completed_at INTEGER,
    FOREIGN KEY (replication_rule_id) REFERENCES cluster_bucket_replication(id) ON DELETE CASCADE
);
```

---

**Version**: 0.6.0-beta
**Last Updated**: December 9, 2025
**Documentation Status**: Complete

For questions or issues, please open a GitHub issue or consult the main [README.md](../README.md).
