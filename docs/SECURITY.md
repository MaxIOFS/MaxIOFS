# MaxIOFS Security Guide

**Version**: 1.8.0 | **Last Updated**: October 13, 2026

## Security Overview

| Feature | Status | Details |
|---------|--------|---------|
| Password hashing | ✅ | Bcrypt (Go DefaultCost = 10) |
| JWT sessions | ✅ | Configurable timeout, idle timeout |
| Two-Factor Authentication | ✅ | TOTP (Google Authenticator, Authy) |
| S3 Signature auth | ✅ | AWS v2 and v4 |
| OAuth2/OIDC SSO | ✅ | Google, Microsoft, custom OIDC |
| LDAP/AD integration | ✅ | Bind authentication |
| Cluster HMAC auth | ✅ | HMAC-SHA256 with timestamp/nonce |
| Inter-node TLS | ✅ | Auto-generated internal CA, ECDSA P-256 |
| Role-Based Access Control | ✅ | 5 roles (admin, tenant-admin, user, readonly, guest) |
| Rate limiting | ✅ | IP-based login throttling |
| Account lockout | ✅ | Configurable threshold and duration |
| Encryption at rest | ✅ | AES-256-GCM authenticated encryption (64 KB chunks) |
| IDP secrets encryption | ✅ | AES-256-GCM for stored OAuth secrets |
| Object Lock (WORM) | ✅ | COMPLIANCE and GOVERNANCE modes |
| ACLs | ✅ | S3-compatible canned + custom ACLs |
| PublicAccessBlock | ✅ | Per-bucket flags enforced on every request |
| Audit logging | ✅ | 20+ event types, SQLite storage |
| Real-time notifications | ✅ | SSE push for security events |
| Multi-tenant isolation | ✅ | Complete data and API isolation |

---

## Authentication

### Console Authentication (JWT)

1. User submits username + password
2. If 2FA enabled: the server answers with a `two_factor_token` valid for 5 minutes instead of a session; the console sends it with the TOTP or backup code to `/api/v1/auth/2fa/verify`. The token is not a credential.
3. Server validates credentials → issues JWT token
4. Token stored in browser localStorage
5. Included in all API requests via `Authorization: Bearer <token>`

In a cluster any node serves a sign-in, with or without a coordinator.

**Session settings** (configurable at runtime):
- `security.session_timeout` — Refresh-token lifetime / inactivity timeout (default: 24h)
- `security.access_token_lifetime` — Access-token lifetime before refresh (default: 15 minutes)

### S3 API Authentication

AWS Signature v2 and v4 — compatible with all S3 clients, SDKs, and tools.

```bash
# AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws --endpoint-url=http://localhost:8080 s3 ls
```

Access keys are generated via Web Console (Users → Access Keys) or API. The default `admin / admin` credentials are only for the initial Web Console login; no S3 access key is created by default.

**The body is the one signed.** A SigV4 request whose `x-amz-content-sha256` is
the hash of its body is refused (`XAmzContentSHA256Mismatch`) when the body
is another. A body sent in signed chunks (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`,
as minio-go sends over HTTP) is checked chunk by chunk, the last chunk and the
signed trailers too, against the signatures chained from the request's: a
mismatch is refused with `SignatureDoesNotMatch`. Either way nothing of the
write is stored. `UNSIGNED-PAYLOAD` bodies are covered by TLS only.

### OAuth2/OIDC SSO

SSO via Google Workspace, Microsoft Entra ID (Azure AD), or custom OIDC providers. Users are auto-provisioned based on group-to-role mappings.

- An email belongs to one user, as its username or its email, whichever way it signs in. A user with an email another user has is refused.
- A provider signs in its own users only, and provisions with its own group mappings. A tenant's provider gives users of its tenant only.
- An email the provider says it did not verify (`email_verified` false) signs nobody in, by the console or by STS: on a provider where users may set or register any email, it would sign one in as another user. An email the provider says nothing about is taken.
- A tenant administrator reaches its own tenant's providers only.

> Complete SSO guide: [SSO.md](SSO.md)

### LDAP/Active Directory

Bind authentication against LDAP/AD directories. Configured via Web Console → Users → Identity Providers.

- Bind with a read-only service account; MaxIOFS only searches and binds.
- Test Connection warns of a directory reached without TLS: the service account's and the users' passwords cross the network in clear.
- A user signs in with the DN the console imported or a group sync gave, or with the name typed on the users page. The entry has to match the user filter.
- A directory that does not answer within 10 seconds to connect, or 15 seconds to a request, fails the sign-in with `503`. The failure is not counted against the account or the address.
- A user's groups are those its `memberOf` names and those that name it as `member` or `uniqueMember`.
- At each renewal of a console session the service account looks the user up again: a user gone from the directory, or disabled in Active Directory, loses the session.

### Cluster Authentication & Encryption

Inter-node communication is both **encrypted** (TLS) and **authenticated** (HMAC-SHA256):

**TLS Encryption (automatic):**
- On cluster initialization, an internal CA (ECDSA P-256, 10-year validity) is generated
- Each node gets a certificate signed by the CA (1-year validity, auto-renewed)
- All inter-node HTTP communication uses TLS; the calling node verifies the other node's certificate against the internal CA. The caller is authenticated by the HMAC signature below, not by a client certificate
- No configuration required — fully automatic and transparent
- Certificate auto-renewal runs monthly; hot-swapped without restart

**Joining a node:** the join package carries the CA's key, the cluster token, the JWT secret, the cluster KEK and the encryption secret. It crosses the network sealed with a join code the new node's administrator opened (160 random bits, good once for 15 minutes, closed by five packages it does not open): AES-256-GCM, with a key derived from the code. The new node seals its answer with the same key. A node in the middle, or at a mistyped address, reads nothing and cannot pass for the new node; no credentials of the new node are sent.

**HMAC-SHA256 Authentication:**
```
Signature = HMAC-SHA256(node_token, METHOD + PATH + TIMESTAMP + NONCE + BODY)
```

- Timestamp validation: ±5 minutes maximum clock skew
- Constant-time signature comparison (timing attack prevention)
- Nonce for replay attack prevention

> Complete cluster security: [CLUSTER.md](CLUSTER.md#security)

---

## Two-Factor Authentication (2FA)

TOTP-based 2FA compatible with Google Authenticator, Authy, and similar apps.

**Setup:**
1. Navigate to Profile → Security
2. Click "Enable 2FA"
3. Scan QR code with authenticator app
4. Enter verification code to confirm
5. Save backup codes (10 single-use recovery codes)

**Recovery:** Use backup codes if authenticator device is lost.

---

## Authorization (RBAC)

Default grants per role. Since 1.6.0 each role is a set of IAM policies, evaluated
together with bucket permissions and attached policies — see [IAM](#iam).

| Action | Global Admin | Tenant Admin | User | Read-Only |
|--------|:---:|:---:|:---:|:---:|
| Manage tenants | ✅ | ❌ | ❌ | ❌ |
| Manage cluster | ✅ | ❌ | ❌ | ❌ |
| Modify security settings | ✅ | ❌ | ❌ | ❌ |
| Manage IDP providers | ✅ | ✅ | ❌ | ❌ |
| Manage tenant users | ✅ | ✅ | ❌ | ❌ |
| View audit logs | ✅ | ✅ (own tenant) | ❌ | ❌ |
| Create/delete buckets | ✅ | ✅ | ✅ | ❌ |
| Upload/download objects | ✅ | ✅ | ✅ | ⬇️ only |
| Manage own access keys | ✅ | ✅ | ✅ | ❌ |

---

## Temporary Credentials (STS)

Users can issue short-lived S3 credentials (access key ID prefixed `ASIA`, secret,
and session token) so that applications and scripts never store permanent keys.

> Sessions issued by `AssumeRole` are governed by the role instead — see
> [IAM](#iam) below.

**Security model** — a temporary credential is a *projection* of an existing user,
never a separate identity:

- Permissions are **not** copied into the session. Every request re-evaluates the
  base user's live roles, capabilities, bucket permissions, bucket policies and
  ACLs, so a permission change takes effect on in-flight sessions immediately.
- The session token must be present **and covered by the SigV4 signature**
  (listed in `SignedHeaders`, or in the query string for presigned URLs). An
  unsigned token is rejected, so it cannot be stripped or substituted in transit.
- Expiry is enforced server-side on every request; client clocks are not trusted.
  Maximum lifetime is `security.sts_max_session_duration` (default 12 h, minimum
  15 min). A user may hold at most 100 active sessions.
- Session secrets are encrypted at rest with the same AES-256-GCM scheme used for
  permanent access keys.
- **Revocation is immediate**: deleting the session in Settings → Access Keys
  kills the credential. Suspending or deleting the user invalidates all of their
  sessions at once, on every cluster node, with no propagation delay.
- Temporary credentials are **S3-only** — they grant no console access — and are
  **SigV4-only**, matching AWS.

**Session policies** — a credential may carry an optional IAM-style policy that
narrows it further:

- The policy is an **intersection, never a grant**: a request is served only when
  the normal authorization pipeline allows it *and* the policy allows it. No
  policy can widen a session beyond its base user, so issuing one is never a
  privilege-escalation vector.
- Standard IAM precedence — explicit `Deny` beats `Allow`, and an explicit
  `Allow` is required (no implicit allow).
- Requests that do not map to a known S3 action are **denied** while a policy is
  attached, rather than guessed at.
- `Condition` and `Principal` are rejected at issuance instead of ignored: a
  policy this server cannot enforce exactly as written never becomes a credential
  that is broader than its author believed.
- A stored policy that no longer parses denies every request for that session.
- Denials return `403 AccessDenied`; an expired session returns `403 ExpiredToken`.

**Federation** (`/sts/ldap-identity`, `/sts/web-identity`) lets a headless client
trade LDAP credentials or an OAuth access token for temporary credentials without
a console session:

- **Disabled by default** (`security.sts_federation_enabled`). These endpoints
  accept credentials without a session, so enabling them is an explicit admin
  decision, not a side effect of configuring an identity provider.
- Authentication is delegated: LDAP binds against the directory, and an OAuth
  token is validated by the provider's own userinfo endpoint, so revoked and
  expired tokens fail.
- The caller still needs `keys:manage_own` and an active, unlocked account. Note
  it does **not** need `console:access` — the credentials grant S3 access only.
- Rate-limited per IP on the console login budget, and every attempt (success or
  denial) is audited. Denials are opaque, so the endpoints cannot be used to
  enumerate usernames.
- Failed exchanges deliberately do **not** count toward the console account
  lockout: a script with a stale password could otherwise lock a person out of
  the console. Directories enforce their own lockout policy.

**AWS STS protocol surface** (`POST /` on the S3 API) exposes the same issuance
paths to AWS SDKs. It adds no new way to obtain credentials:

- `GetSessionToken` and `AssumeRole` require a valid SigV4 signature made with
  **permanent** credentials. `GetSessionToken` issues for the signing user only;
  `AssumeRole` resolves `RoleArn` and issues for the role, if the role's trust
  policy names the caller.
- **A temporary credential cannot mint another one.** Requests signed with an
  `ASIA` key are refused, so a leaked session cannot renew itself indefinitely
  and expiry always holds.
- The federated actions carry the federation gates unchanged.

In a cluster, sessions replicate on the same interval as access keys with an
immediate push on issue and revoke. If a node is unreachable at that moment, a
revoked session can survive there until the next sync cycle; its expiry remains
the hard upper bound.

---

## IAM

MaxIOFS serves the AWS IAM query protocol on the S3 endpoint, so a client can
manage identities, credentials, policies and roles with the API it already
speaks. The wire details are in [API.md](API.md); what matters for security is
how IAM policies combine with the model described above.

### One model

There is one permission system. What a user may do is the set of policies that
apply to them — their own, their groups', their tenant's and their roles' —
evaluated AWS-style: default deny, an explicit `Allow` is required, an explicit
`Deny` anywhere wins, and a request that maps to no known action is denied.

The bucket's policy takes part in the same decision, as in AWS: a `Deny` there
refuses, an `Allow` there grants a user of the bucket's tenant. See
[Bucket Policies](#bucket-policies).

Nothing else is consulted. The capability and bucket-permission tables are not
read at request time; the permissions an installation already had were converted
into policies once, on the boot that upgraded it, and every permission written
since goes straight to a policy.

A user is a user. Their access keys carry their permissions — a request is the
same request whether it arrived on a console session or signed with a key.

**The account boundary is not a policy.** A principal that belongs to a tenant
never leaves it, whatever its own policies say. A global administrator reaches
another tenant to read — the audit view — and not to write: on a tenant's bucket
the console refuses to change lifecycle, tagging, CORS, ACL, policy, versioning,
object lock, notifications, encryption, public access block, website and
inventory and bucket quotas. Global administrators retain full access to global
buckets. Bucket quotas use the resource-scoped actions
`maxiofs:GetBucketQuota` and `maxiofs:PutBucketQuota`; the latter covers both
setting and removing a quota. Tenant administrators hold both actions.

**Roles** are named permission sets. A role is assigned to users; if it also
carries a trust policy it can be assumed through `AssumeRole`, and a role with
no trust policy cannot be assumed by anyone. There is one role concept, not two.

**Actions** are namespaced by the service they belong to — `s3:*` for object and
bucket operations, `console:*` for what only exists in the console, `iam:*` for
managing identities. The namespace is not a division between kinds of user: it
exists because a policy has to be able to name every operation, and without it
the operations outside S3 would need a second mechanism.

Tenant scoping always applies. An entity created through the IAM API belongs to
its creator's tenant, and a caller reaches only what its own tenant owns: a role,
policy or group of another tenant answers as if it did not exist. A group name
resolves inside the caller's tenant.

A role or managed policy with no tenant belongs to the deployment. Every tenant
reads those — the built-in policies to attach them, the roles to assume them —
and only a global administrator changes them. A global administrator reaches
every tenant.

### Roles

A role is an assumable identity with:

- a **trust policy** (required, never defaulted) naming who may assume it — by
  user ARN, user name, tenant, MaxIOFS role, or `*`;
- its own attached and inline policies, which are what a role session may do;
- an optional `MaxSessionDuration`, itself bounded by
  `security.sts_max_session_duration`.

`AssumeRole` refuses an unknown `RoleArn` and refuses a caller the trust policy
does not name. A role session carries the **role's** permissions, not the
caller's; an optional session policy narrows them further. Role policies are read
live on every request, so editing or deleting a role takes effect on sessions
already issued from it — deleting a role stops its sessions rather than leaving
them running until they expire. A tenant-scoped role cannot be assumed from
another tenant regardless of what its trust policy says.

### Access to the IAM surface itself

Every IAM call needs a SigV4 signature made with **permanent** credentials of a
user holding the `iam:manage` capability, which no role grants by default —
administrators have it through the admin safety net and anyone else needs an
explicit override. Temporary credentials are refused: a session able to create
identities would outlive its own expiry. The surface can be switched off entirely
with `security.iam_api_enabled`.

In a cluster, IAM entities replicate on the access-key interval with an immediate
push after every change. Entities are upserted rather than replaced, edits
converge on their update timestamp, and deletions travel as tombstones so a node
that has not yet heard about a delete cannot resurrect the entity.

---

## Password Security

- **Hashing**: Bcrypt with Go's `bcrypt.DefaultCost` (cost factor 10)
- **Storage**: SQLite database (`maxiofs.db`), never stored in plaintext
- **Minimum length**: 8 characters (configurable via `security.password_min_length`)
- **Recommendation**: 12+ characters with mixed case, numbers, and symbols

---

## Console Sessions

- A sign-in starts a session kept on the server and replicated to every node. Its access token lasts `security.access_token_lifetime` (15 minutes) and its refresh token `security.session_timeout` (24 hours), renewed at each refresh.
- A sign-out ends the session on every node: its access and refresh tokens are refused from then on.
- A session of an identity provider's user is asked about again at each renewal, and ended by the provider's back-channel logout; see [SSO.md](SSO.md#sessions-follow-the-provider). A provider that cannot be reached ends no session.
- The users of an LDAP provider with a mapping set to Auto Sync are asked about every hour, signed in or not: one gone from the directory or disabled there is deactivated, its access keys refused; see [SSO.md](SSO.md#directory-sync).
- The provider's refresh token kept with a session is encrypted with the encryption secret.
- In a cluster only the node that started an OAuth session uses the provider's refresh token: another node passes the renewal there over the cluster port, and while that node does not answer keeps the session without asking the provider. The coordinator renews the sessions of a node removed from the cluster. Renewals that arrive together share one question to the provider.
- A session ended during a renewal stays ended.

---

## Rate Limiting & Account Protection

### IP Rate Limiting

Prevents brute-force attacks from a single IP address:
- Only failed sign-ins count: a wrong password, an unknown user, a wrong two-factor token, a refused STS federation. A sign-in that succeeds never counts, so many users behind one NAT, or software that signs in on every request, are not held up.
- Default: 5 failed sign-ins per minute per IP
- Configurable: `security.ratelimit_login_per_minute`
- Exceeded: HTTP 429 Too Many Requests

### Account Lockout

Protects individual accounts after repeated failed logins:
- Default threshold: 5 failed attempts (`security.max_failed_attempts`)
- Default duration: 15 minutes (`security.lockout_duration`)
- Admin notification: Real-time SSE push when account is locked
- Manual unlock: Admin can unlock via Web Console (Users → Unlock)
- Counter reset: Successful login resets the failed attempts counter
- A wrong two-factor code counts as a failed attempt
- In a cluster each node counts the failed attempts it serves and locks the account on that node: behind a load balancer, an account takes up to `max_failed_attempts` failures per node before every node has locked it. The limit per IP is per node too. An administrator's unlock reaches every node.

---

## Encryption at Rest

### Object Encryption (AES-256-GCM, envelope — always on)

Server-side encryption is **always active** (like AWS S3): every new object is
encrypted with its own random Data Encryption Key (DEK) using authenticated
AES-256-GCM in 64 KB chunks — any tampered chunk is detected and rejected on
read. Each object's DEK is wrapped with the Key Encryption Key (KEK) and
stored in the object's on-disk `.metadata` sidecar, so objects remain
recoverable from the filesystem alone given a KEK backup. The parts of a
multipart upload in progress are stored encrypted with a key of the upload,
wrapped with the KEK.

**Key management:**
- The KEK lives in the database and is **generated automatically** on first
  start — no configuration needed. (Pre-v1.5 deployments with
  `storage.encryption_key` in config.yaml: that key is seeded into the
  database as version 1 on the first start and never read again; existing
  objects keep decrypting.)
- **Recovery bundle**: download it from Settings → Security (passphrase-
  encrypted export of every key version) and store it OUTSIDE the server —
  the console shows a banner until you do. Without a bundle, losing the
  database means losing every encrypted object.
- **Key rotation**: Settings → Security → Rotate key (or
  `POST /api/v1/settings/encryption/rotate-kek`). Object data is never
  re-encrypted — the background worker re-wraps each object's DEK to the new
  version. After rotating, download a fresh bundle.
- Objects written before encryption became mandatory (plaintext or legacy
  direct-encrypted) are converted in the background when server load is low;
  progress is visible in Settings → Security.

**Cluster**: nodes share a cluster-wide KEK (distributed on join and on
rotation), so HA replication moves ciphertext as-is — no decrypt/re-encrypt
per hop.

**Disaster recovery**: if the metadata database is lost but the object files
survive, `maxiofs recover --data-dir <dir> --recovery-bundle <bundle>`
rebuilds the metadata store from the filesystem and restores the keys —
original modification times included, so lifecycle timers and cluster
reconciliation stay correct, and each version's Object Lock retention and
legal hold, which its sidecar keeps. Two deliberate biases towards recovering data:
versioned objects whose latest "version" was a deletion come back visible
(delete markers exist only in the lost database — re-delete if the deletion
should stand), and objects whose key material cannot be verified are still
indexed with the failure reported instead of silently disappearing from
listings. See `maxiofs recover --help` and the runbook in
[OPERATIONS.md](OPERATIONS.md#backups--disaster-recovery).

**Characteristics:**
- Transparent: Automatic encrypt on upload, decrypt on download
- Mixed mode: pre-existing plaintext/legacy objects keep reading correctly
- Performance: ~5-10% latency increase for large files
- S3 API compatible: No client changes needed
- External KMS (Vault, AWS KMS) is planned on top of the envelope

### Customer-Provided Keys (SSE-C)

A client can send its own key with each request. The object's DEK is wrapped
with that key instead of the KEK, in its entry and sidecar; MaxIOFS keeps no
copy of the key.

- Per bucket, blocked by default as in AWS since April 2026:
  `PutBucketEncryption` with `BlockedEncryptionTypes` `NONE` allows it, `SSE-C`
  blocks it; in the console, Bucket → Security → Customer-provided keys.
  Blocking stops writes; objects written stay readable with their key.
- Over HTTPS only: TLS on the S3 port, or `X-Forwarded-Proto: https` from a
  trusted proxy. A node that forwards a request to another passes on how it
  was received.
- The server cannot read such an object without its key: the console does not
  download, share or rename it, a folder ZIP leaves it out, and bucket
  replication does not copy it. The integrity scrub checks its stored bytes
  against its ETag, their MD5; KEK rotation leaves it as it is; `maxiofs
  recover` restores its entry.
- In a cluster its bytes move as stored: whole copies are copied and parity
  cuts them in shards without the key. A node that holds none decrypts what it
  reads from another with the key of the request.
- A multipart upload's key stays wrapped with the KEK until the upload is
  completed or aborted, as `CompleteMultipartUpload` carries no customer key;
  the object keeps only the wrap with the customer's key.
- Logs give the key headers and the STS session token as `[redacted]`.

### Stored Credentials Encryption (AES-256-GCM)

OAuth client secrets, LDAP bind passwords, replication destination secret keys and share link keys are encrypted at rest in the SQLite database with AES-256-GCM, with a key derived from the encryption secret.

- The secret lives in the database (`encryption_secret` table), like the KEK. It is stored on the first start from `auth.encryption_secret`, or else from an explicit `auth.jwt_secret`, or else generated; the configuration is not read again, and a configured value that differs is reported at startup.
- At every start the server names, in its log, the stored credentials the secret does not decrypt: they have to be entered again.
- In a cluster every node holds the cluster's secret: a node that joins takes it, and the coordinator gives it to a node that holds another. The node re-encrypts what it stores in the same transaction.
- The secret is never returned by the API. Neither is the JWT signing secret: the settings endpoints do not list, read or change it.

---

## Cluster Replication Security

When objects are replicated between cluster nodes:

1. The stored **ciphertext is sent as-is** — objects are never decrypted in
   transit (all nodes share a cluster-wide KEK distributed on join/rotation,
   so every node can unwrap the object's DEK at read time)
2. The transfer additionally travels over the **TLS-encrypted** inter-node
   connection (automatic)
3. HMAC-SHA256 signature authenticates the transfer
4. The destination stores a byte-identical copy (metadata, timestamps and
   client checksums included)

Objects wrapped with a node-local (pre-join) key fall back to a decrypt/
re-encrypt transfer until the background worker re-wraps them to the shared
key. Inter-node TLS is enabled automatically using the cluster's internal
CA — no manual configuration needed.

---

## Audit Logging

SQLite-based immutable audit trail tracking 20+ event types.

### Event Categories

| Category | Events |
|----------|--------|
| Authentication | Login success/failure, logout, token issued/expired |
| 2FA | 2FA enabled/disabled, verification success/failure |
| User Management | User created/updated/deleted, password changed |
| Access Keys | Key generated/revoked |
| Bucket Operations | Created/deleted, versioning/policy/CORS/ACL changed |
| Object Operations | Uploaded/downloaded/deleted, multipart, lock/retention |
| Security | Account locked/unlocked, rate limit triggered |
| System | Config changed, server started, encryption status |
| IDP | Provider created/updated/deleted, group mapping changed |
| Cluster | Node added/removed, replication rule changed |

### Access Control

- **Global admins**: View all audit logs across all tenants
- **Tenant admins**: View only their tenant's audit logs
- **Users**: No audit log access

### Configuration

- Retention: `audit.retention_days` (default: 90 days, configurable at runtime)
- Storage: Separate SQLite database (`audit.db`) for isolation
- Export: CSV export via Web Console with date/event/user/tenant filters
- API: `GET /api/v1/audit-logs` with query parameters for filtering

---

## Real-Time Security Notifications

Server-Sent Events (SSE) push notifications for security events:

- Account lockouts → instant admin notification
- Topbar bell icon with unread count badge
- Persistent storage (survives page reloads)
- Tenant isolation (global admins see all, tenant admins see own tenant)
- Automatic SSE connection on admin login

No configuration required — enabled automatically for admin users.

---

## Object Lock (WORM)

Write-Once-Read-Many compliance for regulatory requirements.

| Mode | Protection | Override |
|------|-----------|---------|
| **GOVERNANCE** | Prevents accidental deletion | Admin can override with special permission |
| **COMPLIANCE** | Strict immutability | Cannot be deleted until retention period expires |

A retention in force only grows stricter. A COMPLIANCE one is extended and
nothing else: it is not shortened, removed or made GOVERNANCE, with
`x-amz-bypass-governance-retention` or without. A GOVERNANCE one is extended or
made COMPLIANCE freely, and shortened or removed only with that header and
`s3:BypassGovernanceRetention`. In a cluster every node applies the same rules.

**Legal Hold**: Independent of retention — can be applied/removed anytime, prevents deletion regardless of retention settings.

**Protection from the first write**: retention and legal hold sent with a PUT
(`x-amz-object-lock-mode`, `x-amz-object-lock-retain-until-date`,
`x-amz-object-lock-legal-hold`), or the bucket default retention, are stored in
the same metadata write as the object, so no copy of it exists unprotected.
Invalid values reject the PUT with `InvalidRequest`. Setting them on a write needs
the same permissions as the dedicated APIs, as in AWS S3: `s3:PutObjectRetention`
for a mode or retain-until date, `s3:PutObjectLegalHold` for a legal hold — on
PUT, `CreateMultipartUpload`, CopyObject and console uploads. The bucket default
retention needs neither. HA replicas receive the same
retention and legal hold with the object. `CreateMultipartUpload` takes the same
headers, validated when the upload is created and applied when it completes.

**One exception**: a write that misses the HA quorum removes the version it just
created, even under the retention or legal hold it set. The client was told the
write failed, so that version never existed for it. No client request can delete
a protected version.

```bash
# Enable on bucket
aws s3api create-bucket --bucket compliance \
  --endpoint-url http://localhost:8080 --object-lock-enabled-for-bucket

# Set retention
aws s3api put-object-retention --bucket compliance --key doc.pdf \
  --endpoint-url http://localhost:8080 \
  --retention Mode=COMPLIANCE,RetainUntilDate=2027-01-01T00:00:00Z
```

---

## Bucket Policies

A bucket policy decides together with the caller's IAM policies, AWS-style: an
explicit `Deny` in either refuses, an `Allow` in either grants. The tenant
boundary and a session policy bound both: a bucket policy grants nothing to a
user of another tenant, nor beyond what a session policy allows.

- **Principals**: `"*"` (everyone, including requests without credentials), a
  user ARN `arn:aws:iam:::user/<name>`, or a user ID, alone or in a list under
  `"AWS"`.
- **Requests without credentials**: only a statement naming `"*"` applies. A
  `Deny` refuses them even when the bucket ACL is public.
- **Conditions**: `aws:SourceIp` and `aws:SecureTransport`. The source address
  is the client address the server logs: behind a trusted proxy, the rightmost
  `X-Forwarded-For` entry past the proxies of `trusted_proxies`; TLS includes a
  trusted proxy's `X-Forwarded-Proto: https`. Trusted proxies are those of
  `trusted_proxies` and, unless `trust_private_networks` is `false`, every
  loopback and private address. A request a cluster node forwards to another
  is evaluated with its client's address and transport, carried signed.
- **Refused when saved** (`MalformedPolicy`): `NotPrincipal`, `NotAction`,
  `NotResource`, other principal types or ARNs, actions outside S3, resources
  outside the bucket, other condition keys or operators. A policy is never read
  differently from what it says.
- **Its own management**: `s3:GetBucketPolicy`, `s3:PutBucketPolicy` and
  `s3:DeleteBucketPolicy` are decided by the caller's own permissions, so a
  policy that denies everything can still be fixed.
- A bucket granted only by its policy is reachable by name; the bucket list
  shows what the caller's own permissions grant.

---

## S3 ACLs

S3-compatible Access Control Lists for bucket and object-level permissions.

**Supported canned ACLs:**
- `private` (default)
- `public-read`
- `public-read-write`
- `authenticated-read`
- `bucket-owner-read`
- `bucket-owner-full-control`

Custom ACLs with grant-based permissions (READ, WRITE, READ_ACP, WRITE_ACP, FULL_CONTROL) are also supported.

An object ACL belongs to the version it was set on. The next object written at the key, by overwrite, as a new version or after a delete, starts `private`.

A request without credentials reads, heads and lists a bucket whose ACL grants READ to everyone (`public-read`), unless its PublicAccessBlock ignores public ACLs or its bucket policy denies it; it needs no share link or presigned URL. A signed request is decided by its IAM policies and the bucket policy; the ACL plays no part.

A read without credentials, a share link's included, cannot choose the headers it is sent: a GET with `response-content-type` or another `response-*` parameter is refused (`400 InvalidRequest`), as in S3, so a public object cannot be served as a page of another type.

---

## PublicAccessBlock

Per-bucket public access controls that override ACLs. Configure via the S3 API or the bucket's Security & Access tab in the console. Buckets created in the console have every flag on.

| Flag | Effect |
|------|--------|
| `BlockPublicAcls` | Refuses a request carrying an ACL that grants anything to `AllUsers` or `AuthenticatedUsers` (`403 AccessDenied`): PutBucketAcl, PutObjectAcl, and `x-amz-acl` on PutObject, CopyObject and CreateMultipartUpload. Nothing is stored. ACLs already set are kept |
| `IgnorePublicAcls` | Public ACLs, set before or after, grant nothing to requests without credentials |
| `BlockPublicPolicy` | Refuses a public bucket policy (`403 AccessDenied`). A policy already set is kept |
| `RestrictPublicBuckets` | A public bucket policy grants nothing to requests without credentials |

A bucket policy is public when an `Allow` names `"*"` and no `aws:SourceIp` condition limits it to ranges of /8 or narrower (IPv4) or /32 or narrower (IPv6), as AWS defines it. Share links, presigned URLs and static website hosting are not affected.

```bash
# Block all public access
aws s3api put-public-access-block \
  --endpoint-url http://localhost:8080 \
  --bucket my-bucket \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

# Read current configuration
aws s3api get-public-access-block \
  --endpoint-url http://localhost:8080 \
  --bucket my-bucket
```

---

## Standards: ISO/IEC 27001, 27017, 27018 and ISO 9001

MaxIOFS is not certified against any of these standards. Certification is given to an organization's management system, not to a product. The tables list what MaxIOFS provides as evidence for an operator's certification, and what the operator does. Organizational, people and physical controls are the operator's.

### ISO/IEC 27001:2022 Annex A (controls of ISO/IEC 27002:2022)

| Control | MaxIOFS | Operator |
|---------|---------|----------|
| 5.15 Access control | IAM policies (managed, inline, roles), bucket policies, ACLs, Block Public Access | Writes the access policy |
| 5.16 Identity management | Local, LDAP and OIDC users; group mappings; LDAP directory sync deactivates users gone from the directory; one email belongs to one user | Runs the joiner, mover and leaver steps |
| 5.17 Authentication information | Bcrypt; password policy (`security.password_*`); identity-provider secrets and replication keys encrypted at rest; a warning while `admin` keeps its default password | Changes the default `admin` password |
| 5.18 Access rights | Access keys revocable; a sign-out ends the session on every node; STS credentials expire | Reviews rights periodically |
| 5.33 Protection of records | Object Lock (COMPLIANCE, GOVERNANCE, legal hold); versioning | Sets retention per bucket |
| 8.2 Privileged access rights | Global administrator, tenant administrator and capabilities; `security.require_2fa_admin` | Keeps few administrators, with 2FA |
| 8.3 Information access restriction | Tenants isolated in data and API | — |
| 8.5 Secure authentication | TOTP 2FA; account lockout; limit of failed sign-ins per IP; 15-minute access tokens; AWS Signature V4 | Tunes `security.*` |
| 8.8 Technical vulnerabilities | Each nightly scans the dependencies (govulncheck) and the code (gosec) | Applies releases |
| 8.9 Configuration management | Changes to settings are audited | Keeps the configuration file under change control |
| 8.10 Information deletion | Lifecycle expiration; deletion of objects and versions | Sets lifecycle rules |
| 8.12 Data leakage prevention | Block Public Access on for buckets created in the console | Reviews public buckets |
| 8.13 Information backup | Bucket replication to another S3 endpoint; KEK recovery bundle and `maxiofs recover` | Keeps the recovery bundle offline; tests restores |
| 8.14 Redundancy | Cluster replication factor; erasure coding; storage classes | Sizes the cluster |
| 8.15 Logging | Audit log of 20+ event types, on by default (`audit.enabled`), read-only through the API, kept `audit.retention_days` (90); syslog and HTTP shipping | Keeps it on; ships logs to its SIEM and reviews them |
| 8.16 Monitoring activities | Prometheus metrics; console alerts (lockouts, disk, quota); email alerts (disk, quota) | Sets alert rules |
| 8.17 Clock synchronization | Signatures and tokens are checked against the clock | Runs NTP on every node |
| 8.20 Network security | Inter-node TLS 1.2+ with the cluster's CA; HMAC-signed inter-node requests | Firewalls the ports |
| 8.24 Use of cryptography | AES-256-GCM envelope encryption, always on; KEK rotation; SSE-C, blocked per bucket until enabled, HTTPS only; TLS (`cert_file`) | Keeps the KEK backup; clients keep their SSE-C keys |
| 8.26 Application security requirements | Console Content-Security-Policy without foreign inline script; HSTS; `X-Frame-Options`; `nosniff` | Serves over HTTPS |
| 8.25, 8.28, 8.29 Secure development and testing | Go and frontend test suites, govulncheck and gosec on each nightly | — |
| 8.32 Change management | Versioned releases; [CHANGELOG.md](../CHANGELOG.md) | Tests a release before upgrading |

### ISO/IEC 27017:2015 (cloud services)

| Control | MaxIOFS | Operator |
|---------|---------|----------|
| CLD.6.3.1 Shared roles and responsibilities | The Operator columns of these tables | Tells its customers which controls are its own |
| CLD.8.1.5 Removal of customer assets | A tenant is deleted only once it holds no buckets | Empties the tenant's buckets when the contract ends |
| CLD.9.5.1 Segregation in virtual computing environments | Per tenant: data path, users, identity providers, quotas and bandwidth caps | — |
| CLD.12.1.5 Administrator's operational security | Administrative actions audited; maintenance mode blocks writes | Documents its procedures |
| CLD.12.4.5 Monitoring of cloud services | A tenant administrator sees the tenant's audit log | — |

### ISO/IEC 27018:2019 (personal data in the cloud)

MaxIOFS keeps these personal data: usernames, email addresses and display names; in the audit log and the server logs, IP addresses and user agents. Object content is the customer's and is not read.

| Need | MaxIOFS | Operator |
|------|---------|----------|
| Protection at rest | Objects always encrypted; stored credentials encrypted; passwords as bcrypt hashes. User records in `maxiofs.db` are not encrypted | Encrypts the disks |
| Retention | Audit records deleted after `audit.retention_days`; the username of a deleted user stays in them until then | Sets the retention its policy needs |
| Access | Global administrators see all tenants; tenant administrators only theirs | — |
| Requests from a person | Audit log export to CSV, filtered by user; user records in the console and the API | Answers within its legal terms |
| Disclosure to third parties | The console's version check asks `https://maxiofs.com/version.json` and sends no user data. Every other outbound connection is configured by the operator: identity providers, SMTP, syslog and HTTP log shipping, replication targets | Lists its subprocessors |
| Location | Data stays on the operator's nodes; replication goes only where configured | Chooses where nodes run |

### ISO 9001 (quality)

ISO 9001 certifies the organization that makes the product. The development of MaxIOFS keeps this evidence:

- Each user-visible change is recorded in [CHANGELOG.md](../CHANGELOG.md), by version.
- Each nightly runs the Go and frontend test suites, govulncheck and gosec. A failure stops the build and publishes nothing.
- Binaries, `.deb` and `.rpm` packages and the Docker image are built by CI from the commit they name.
- Documentation in `docs/` changes with the feature.
- Security issues are reported privately; see [Reporting Security Issues](#reporting-security-issues).

---

## Security Best Practices

1. **Change default credentials immediately** (admin/admin)
2. **Use HTTPS** — reverse proxy with TLS for all traffic
3. **Enable 2FA** for all admin accounts
4. **Configure rate limiting** — adjust thresholds for your environment
5. **Run as non-root** — use dedicated service account
6. **Restrict file permissions** — `chmod 700` on data directory
7. **Configure firewall** — expose only necessary ports
8. **Keep audit logging on** (`audit.enabled`) — review logs regularly
9. **Inter-node TLS is automatic** — cluster nodes encrypt all communication using auto-generated certificates
10. **Back up encryption keys** — data is irrecoverable without them
11. **Enable PublicAccessBlock** — turn on all four flags on any bucket that should never be public; buckets created in the console have them on
12. **Monitor** — set up Prometheus alerts for security events

---

## Known Limitations

1. **No SOC 2 or ISO certification** — no third-party assessment; what MaxIOFS provides to an operator's certification is in [Standards](#standards-isoiec-27001-27017-27018-and-iso-9001)
2. **Single master encryption key** — no per-tenant keys, no HSM integration
3. **No SAML SSO** — OAuth2/OIDC recommended instead
4. **Basic session management** — no device tracking or geographic restrictions
5. **SQLite audit storage** — may have scale limits at very high event volumes
6. **External log shipping via syslog/HTTP only** — no built-in managed SIEM integrations or log analytics backends
7. **Manual key rotation** — rotation is admin-initiated (no automatic schedule); object data is never re-encrypted, the background worker re-wraps each object's DEK to the new key version

---

## Reporting Security Issues

**DO NOT** open public GitHub issues for security vulnerabilities.
Contact the maintainers directly via the security contact in the repository.

---

**See also**: [SSO.md](SSO.md) · [CLUSTER.md](CLUSTER.md#security) · [CONFIGURATION.md](CONFIGURATION.md)
