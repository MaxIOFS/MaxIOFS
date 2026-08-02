# MaxIOFS API Reference

**Version**: 1.5.2 | **Last Updated**: July 18, 2026

## Overview

MaxIOFS exposes two HTTP servers:

| Server | Default Port | Purpose | Authentication |
|--------|-------------|---------|----------------|
| **S3 API** | 8080 | AWS S3-compatible REST API | AWS Signature v2/v4 |
| **Console API** | 8081 | Web Console REST API + embedded frontend | JWT / OAuth2 |
| **Cluster (internal)** | 8082 | Inter-node coordination/replication — not a client API | HMAC-SHA256 (node tokens) over cluster TLS |

---

## S3 API (Port 8080)

100% compatible with AWS S3 clients, SDKs, and CLI tools.

### Quick Start

```bash
# Configure AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY

# Use MaxIOFS
aws --endpoint-url=http://localhost:8080 s3 mb s3://my-bucket
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/
aws --endpoint-url=http://localhost:8080 s3 ls s3://my-bucket/
```

### Bucket Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| ListBuckets | GET | `/` |
| CreateBucket | PUT | `/{bucket}` |
| DeleteBucket | DELETE | `/{bucket}` |
| HeadBucket | HEAD | `/{bucket}` |
| GetBucketVersioning | GET | `/{bucket}?versioning` |
| PutBucketVersioning | PUT | `/{bucket}?versioning` |
| GetBucketCORS | GET | `/{bucket}?cors` |
| PutBucketCORS | PUT | `/{bucket}?cors` |
| DeleteBucketCORS | DELETE | `/{bucket}?cors` |
| GetBucketACL | GET | `/{bucket}?acl` |
| PutBucketACL | PUT | `/{bucket}?acl` |
| GetBucketPolicy | GET | `/{bucket}?policy` |
| PutBucketPolicy | PUT | `/{bucket}?policy` |
| DeleteBucketPolicy | DELETE | `/{bucket}?policy` |
| GetBucketTagging | GET | `/{bucket}?tagging` |
| PutBucketTagging | PUT | `/{bucket}?tagging` |
| DeleteBucketTagging | DELETE | `/{bucket}?tagging` |
| GetBucketLifecycle | GET | `/{bucket}?lifecycle` |
| PutBucketLifecycle | PUT | `/{bucket}?lifecycle` |
| DeleteBucketLifecycle | DELETE | `/{bucket}?lifecycle` |
| GetBucketNotification | GET | `/{bucket}?notification` |
| PutBucketNotification | PUT | `/{bucket}?notification` |
| GetObjectLockConfig | GET | `/{bucket}?object-lock` |
| PutObjectLockConfig | PUT | `/{bucket}?object-lock` |
| GetBucketEncryption | GET | `/{bucket}?encryption` |
| PutBucketEncryption | PUT | `/{bucket}?encryption` |
| DeleteBucketEncryption | DELETE | `/{bucket}?encryption` |
| GetBucketLogging | GET | `/{bucket}?logging` |
| PutBucketLogging | PUT | `/{bucket}?logging` |
| GetPublicAccessBlock | GET | `/{bucket}?publicAccessBlock` |
| PutPublicAccessBlock | PUT | `/{bucket}?publicAccessBlock` |
| DeletePublicAccessBlock | DELETE | `/{bucket}?publicAccessBlock` |
| GetBucketOwnershipControls | GET | `/{bucket}?ownershipControls` |
| PutBucketOwnershipControls | PUT | `/{bucket}?ownershipControls` |
| DeleteBucketOwnershipControls | DELETE | `/{bucket}?ownershipControls` |
| ListMultipartUploads | GET | `/{bucket}?uploads` |

### Object Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObject | GET | `/{bucket}/{key+}` |
| PutObject | PUT | `/{bucket}/{key+}` |
| DeleteObject | DELETE | `/{bucket}/{key+}` |
| HeadObject | HEAD | `/{bucket}/{key+}` |
| CopyObject | PUT | `/{bucket}/{key+}` (header: `x-amz-copy-source`) |
| GetObjectAttributes | GET | `/{bucket}/{key+}?attributes` (header: `x-amz-object-attributes`) |
| RestoreObject | POST | `/{bucket}/{key+}?restore` |
| SelectObjectContent | POST | `/{bucket}/{key+}?select&select-type=2` |
| ListObjects | GET | `/{bucket}` |
| ListObjectsV2 | GET | `/{bucket}?list-type=2` |
| DeleteMultipleObjects | POST | `/{bucket}?delete` |

**Object key rules**: standard S3 keys up to 1024 characters. Keys ending in
`.metadata` or `.metadata-staging` are rejected with `InvalidObjectName` —
those suffixes are reserved for the on-disk metadata sidecar files and would
collide with another object's sidecar.

### Multipart Upload Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| CreateMultipartUpload | POST | `/{bucket}/{key+}?uploads` |
| UploadPart | PUT | `/{bucket}/{key+}?partNumber=N&uploadId=ID` |
| CompleteMultipartUpload | POST | `/{bucket}/{key+}?uploadId=ID` |
| AbortMultipartUpload | DELETE | `/{bucket}/{key+}?uploadId=ID` |
| ListParts | GET | `/{bucket}/{key+}?uploadId=ID` |

### Object Lock / Retention

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectRetention | GET | `/{bucket}/{key+}?retention` |
| PutObjectRetention | PUT | `/{bucket}/{key+}?retention` |
| GetObjectLegalHold | GET | `/{bucket}/{key+}?legal-hold` |
| PutObjectLegalHold | PUT | `/{bucket}/{key+}?legal-hold` |

### ACL Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectACL | GET | `/{bucket}/{key+}?acl` |
| PutObjectACL | PUT | `/{bucket}/{key+}?acl` |

### Tagging Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectTagging | GET | `/{bucket}/{key+}?tagging` |
| PutObjectTagging | PUT | `/{bucket}/{key+}?tagging` |
| DeleteObjectTagging | DELETE | `/{bucket}/{key+}?tagging` |

### Additional Features

- **Presigned URLs** — GET/PUT with configurable expiration (S3-compatible paths)
- **Range Requests** — Partial object downloads via `Range` header
- **Conditional Requests** — `If-Match`, `If-None-Match`, `If-Modified-Since`, `If-Unmodified-Since`
- **Conditional Writes** — `PutObject If-None-Match: *` returns 412 `PreconditionFailed` if the object already exists (atomic create-if-absent)
- **SSE Response Headers** — `x-amz-server-side-encryption: AES256` returned on GET/PUT/HEAD when the object is encrypted
- **PublicAccessBlock enforcement** — `IgnorePublicAcls` and `RestrictPublicBuckets` flags deny all public ACL access; configure via `PUT /{bucket}?publicAccessBlock`
- **OwnershipControls** — default `BucketOwnerEnforced`; prevents AWS SDK v2 `OwnershipControlsNotFoundError`; valid values: `BucketOwnerEnforced`, `BucketOwnerPreferred`, `ObjectWriter`
- **RestoreObject** — accepts `<RestoreRequest><Days>N</Days></RestoreRequest>`; returns 409 if restore already in progress; `HeadObject`/`GetObject` return `x-amz-restore: ongoing-request="false", expiry-date="..."` once restored
- **SelectObjectContent** — SQL queries on object data streamed via Amazon Event Stream binary protocol (Records/Stats/End events, CRC32-framed); see section below
- **Server Access Logging** — async delivery to a target bucket in AWS S3 access log format; configure via `PUT /{bucket}?logging`

### S3 Select Reference

`POST /{bucket}/{key}?select&select-type=2`

**Request XML:**

```xml
<SelectObjectContentRequest>
  <Expression>SELECT s.name, s.age FROM S3Object s WHERE s.age > 25</Expression>
  <ExpressionType>SQL</ExpressionType>
  <InputSerialization>
    <CompressionType>NONE</CompressionType>
    <CSV>
      <FileHeaderInfo>USE</FileHeaderInfo>   <!-- USE | IGNORE | NONE -->
      <FieldDelimiter>,</FieldDelimiter>
    </CSV>
    <!-- or: <JSON><Type>LINES</Type></JSON> -->
  </InputSerialization>
  <OutputSerialization>
    <CSV>
      <FieldDelimiter>,</FieldDelimiter>
    </CSV>
    <!-- or: <JSON></JSON> -->
  </OutputSerialization>
</SelectObjectContentRequest>
```

**Supported input formats:** CSV, JSON Lines
**Supported output formats:** CSV, JSON (one object per line, column order preserved)
**SQL engine:** SQLite — supports SELECT, WHERE, GROUP BY, ORDER BY, aggregate functions (COUNT, SUM, AVG, MIN, MAX)
**Not supported:** compressed input (GZIP/BZIP2), Parquet format

**Response:** `application/vnd.amazon.eventstream` — standard Amazon Event Stream binary format

### Health Endpoints (No Auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |

---

## Console API (Port 8081)

REST API for web console management. All endpoints prefixed with `/api/v1` unless noted. JWT authentication required (via `Authorization: Bearer <token>` header).

### Authentication

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/api/v1/auth/login` | Login (username + password + optional TOTP) | None |
| POST | `/api/v1/auth/logout` | Logout | JWT |
| GET | `/api/v1/auth/me` | Get current user info | JWT |

### Two-Factor Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/2fa/setup` | Start 2FA setup (returns QR code) |
| POST | `/api/v1/auth/2fa/verify` | Verify 2FA setup with TOTP code |
| POST | `/api/v1/auth/2fa/disable` | Disable 2FA |
| POST | `/api/v1/auth/2fa/validate` | Validate a TOTP code |
| POST | `/api/v1/auth/2fa/backup-codes` | Regenerate backup codes |
| GET | `/api/v1/auth/2fa/backup-codes` | Get backup codes |

### OAuth / SSO

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/auth/oauth/providers` | List active OAuth providers | None |
| POST | `/api/v1/auth/oauth/login` | Start OAuth login flow | None |
| GET | `/api/v1/auth/oauth/callback` | OAuth callback from provider | None |
| GET | `/api/v1/auth/oauth/complete` | Complete OAuth login | None |

### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List users |
| POST | `/api/v1/users` | Create user |
| GET | `/api/v1/users/{id}` | Get user details |
| PUT | `/api/v1/users/{id}` | Update user |
| DELETE | `/api/v1/users/{id}` | Delete user |
| PUT | `/api/v1/users/{id}/password` | Change password |
| PATCH | `/api/v1/users/{id}/status` | Update user status (activate/deactivate) |
| POST | `/api/v1/users/{id}/unlock` | Unlock locked account |

### Access Keys

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/access-keys` | List all access keys |
| GET | `/api/v1/access-keys/user/{userId}` | List user's access keys |
| POST | `/api/v1/access-keys` | Create access key |
| DELETE | `/api/v1/access-keys/{id}` | Delete access key |

### Temporary Credentials (STS)

Short-lived S3 credentials that carry the requesting user's own permissions and
expire automatically, so applications never need to hold permanent keys.

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/sts/session-token` | Issue temporary credentials for the calling user |
| GET | `/api/v1/sts/sessions` | List own active sessions (`?all=true` — global admin: everyone's) |
| DELETE | `/api/v1/sts/sessions/{keyId}` | Revoke a session (own; global admins: any) |
| POST | `/api/v1/sts/ldap-identity` | Exchange LDAP credentials for temporary credentials (no JWT) |
| POST | `/api/v1/sts/web-identity` | Exchange an OAuth access token for temporary credentials (no JWT) |

**Request** (body optional — omit for the default duration):

```json
{ "durationSeconds": 3600 }
```

Duration must be between 900 s (15 min) and `security.sts_max_session_duration`
(default 43200 s = 12 h); values outside the range are rejected with 400. A user
may hold at most 100 active sessions (429 beyond that).

**Optional session policy** — attach a policy to narrow the credential:

```json
{
  "durationSeconds": 3600,
  "sessionPolicy": {
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": ["s3:GetObject", "s3:ListBucket"],
        "Resource": ["arn:aws:s3:::backups", "arn:aws:s3:::backups/*"]
      }
    ]
  }
}
```

`sessionPolicy` accepts a JSON object or a string containing the document. It can
only **remove** permissions: a request is served when the normal authorization
pipeline allows it *and* the policy allows it, so a session never gets more than
its user already has.

Supported: `Effect` (`Allow`/`Deny`), `Action`, `Resource` — string or list, with
`*`/`?` wildcards; explicit `Deny` wins. Resources may omit the `arn:aws:s3:::`
prefix. **Rejected with 400 at issuance**: `Principal` (a session policy always
applies to its own user), `Condition` (not evaluated yet — accepting it would
silently widen the policy), an empty `Statement` list, documents over 2048 bytes
or 20 statements. A denial at request time returns `403 AccessDenied`; an expired
session returns `403 ExpiredToken`.

**Response** — the secret and session token are returned **once** and are never
retrievable afterwards; listings show neither:

```json
{
  "accessKeyId": "ASIA…",
  "secretAccessKey": "…",
  "sessionToken": "…",
  "expiresAt": 1785000000
}
```

**Using the credentials** — sign S3 requests with SigV4 as usual, sending the
session token in `X-Amz-Security-Token`. The token **must be listed in
`SignedHeaders`** (SDKs do this automatically); an unsigned token is rejected so
it cannot be stripped or swapped in transit. Presigned URLs work too, with the
token as a query parameter. SigV2 is rejected for temporary credentials, as on AWS.

Permissions are evaluated per request against the base user's live state: roles,
capabilities, bucket permissions, bucket policies and ACLs all apply unchanged.
Suspending or deleting the user invalidates every session of theirs immediately.

#### Federation — credentials for headless clients

The two endpoints below need **no console session**: they authenticate the caller
against an identity provider directly, so an application holding LDAP credentials
or an OAuth access token can obtain S3 credentials without anyone creating
permanent keys for it.

They are **disabled by default**. Set `security.sts_federation_enabled` to `true`
first (Settings → Security); until then they answer `403`.

```json
POST /api/v1/sts/ldap-identity
{ "providerId": "idp-…", "username": "svc-backup", "password": "…",
  "durationSeconds": 3600, "sessionPolicy": { } }

POST /api/v1/sts/web-identity
{ "providerId": "idp-…", "token": "<OAuth access token>",
  "durationSeconds": 3600, "sessionPolicy": { } }
```

Both return the same payload as `/sts/session-token`, and both accept the same
`durationSeconds` and `sessionPolicy` fields.

- **LDAP**: the user must already exist in MaxIOFS and be linked to that provider
  (`auth_provider = ldap:{providerId}`) — the exchange authenticates, it does not
  create accounts.
- **Web identity**: the token is validated by calling the provider's userinfo
  endpoint, so expired or revoked tokens are rejected. A user unknown to MaxIOFS
  is auto-provisioned through group mappings exactly as in browser SSO login.
- The caller needs the `keys:manage_own` capability (or admin), the account must
  be active and unlocked, and the provider must be `active`.
- Attempts are rate-limited per IP on the same budget as console login (`429`)
  and recorded in the audit log. Every rejection answers an opaque `403 Access
  denied` — the endpoints do not reveal which usernames exist.

#### AWS STS protocol (for SDKs)

The same credentials are available through the AWS STS query protocol, on the
**S3 API port** — the location AWS SDKs and MinIO-oriented tooling expect. Point
the SDK's STS endpoint override (`AWS_STS_ENDPOINT` or equivalent) at the S3
endpoint:

```
POST /                                    (S3 API port, e.g. http://maxiofs:8080)
Content-Type: application/x-www-form-urlencoded

Action=GetSessionToken&DurationSeconds=3600
```

| Action | Authentication | Parameters |
|--------|----------------|------------|
| `GetSessionToken` | SigV4 with **permanent** credentials | `DurationSeconds`, `Policy` |
| `AssumeRole` | SigV4 with **permanent** credentials | `RoleArn`, `RoleSessionName` (required with a role), `DurationSeconds`, `Policy` |
| `AssumeRoleWithWebIdentity` | The token itself | `WebIdentityToken`, `ProviderId` (only if several OAuth providers exist), `DurationSeconds`, `Policy` |
| `AssumeRoleWithLDAPIdentity` | The credentials themselves | `LDAPUsername`, `LDAPPassword`, `DurationSeconds`, `Policy` |

Responses are the standard AWS XML documents; `Policy` is an optional session
policy and `DurationSeconds` follows the same bounds as the JSON API.

- **`AssumeRole` resolves `RoleArn` against the role table.** A role that does
  not exist returns `NoSuchEntity`, and a role whose trust policy does not name
  the caller returns `AccessDenied`. The credentials carry the **role's**
  permissions, not the caller's, bounded by the role's `MaxSessionDuration`.
  Omitting `RoleArn` falls back to `GetSessionToken` semantics — the caller's
  own permissions — which keeps working for tools that use `AssumeRole` as a
  synonym for "temporary credentials".
- **Temporary credentials cannot call these actions**: a session signed with an
  `ASIA` key is refused, so a leaked credential cannot renew itself past its
  expiry.
- The two federated actions obey `security.sts_federation_enabled` exactly like
  their JSON counterparts.

## AWS IAM protocol

Served on the **same** `POST /` of the S3 API port, dispatched by `Action`.
Point an AWS IAM client at the S3 endpoint and it works unmodified:

```
aws iam --endpoint-url http://maxiofs:8080 create-user --user-name backup-agent
aws iam --endpoint-url http://maxiofs:8080 put-user-policy         --user-name backup-agent --policy-name job         --policy-document file://policy.json
aws iam --endpoint-url http://maxiofs:8080 create-access-key --user-name backup-agent
```

| Group | Actions |
|-------|---------|
| Identities | `CreateUser`, `DeleteUser`, `GetUser`, `ListUsers` |
| Credentials | `CreateAccessKey`, `DeleteAccessKey`, `ListAccessKeys` |
| Managed policies | `CreatePolicy`, `GetPolicy`, `ListPolicies`, `DeletePolicy` |
| Policy versions | `CreatePolicyVersion`, `GetPolicyVersion`, `ListPolicyVersions`, `DeletePolicyVersion`, `SetDefaultPolicyVersion` |
| Inline policies | `Put`/`Get`/`Delete`/`List` + `UserPolicy` / `RolePolicy` / `GroupPolicy` |
| Attachments | `Attach`/`Detach`/`ListAttached` + `UserPolicies` / `RolePolicies` / `GroupPolicies` |
| Roles | `CreateRole`, `GetRole`, `ListRoles`, `DeleteRole`, `UpdateAssumeRolePolicy` |

- **Authentication**: SigV4 with **permanent** credentials of a user holding the
  `iam:manage` capability (administrators have it by default). Temporary
  credentials are refused — a session must not be able to create identities that
  outlive it.
- **Enabled by** `security.iam_api_enabled` (default `true`). Turning it off also
  stops IAM/STS being advertised to Veeam.
- **New identities land in the caller's tenant.** The AWS protocol has no field
  for a tenant, and an integration must not be able to create identities outside
  the boundary it was given.
- **Policy documents** are stored and returned verbatim (raw JSON, not
  URL-encoded). Documents using `Condition`, or `Principal` on an identity
  policy, are rejected at write time rather than accepted and partly ignored.
- **Errors** use the AWS IAM codes: `NoSuchEntity`, `EntityAlreadyExists`,
  `InvalidInput`, `LimitExceeded`, `DeleteConflict`, `AccessDenied`.

Because both protocols share the S3 endpoint, SOSAPI reports `IAMSTS=true` to
Veeam with `IAMEndpoint` and `STSEndpoint` both set to that URL. It is advertised
only while the IAM surface is enabled and `PublicAPIURL` is configured.

### Groups

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/groups` | List groups (global admin: all; tenant admin: own tenant) |
| POST | `/api/v1/groups` | Create group |
| GET | `/api/v1/groups/{id}` | Get group details |
| PUT | `/api/v1/groups/{id}` | Update group (displayName, description) |
| DELETE | `/api/v1/groups/{id}` | Delete group |
| GET | `/api/v1/groups/{id}/members` | List group members |
| POST | `/api/v1/groups/{id}/members` | Add member to group |
| DELETE | `/api/v1/groups/{id}/members/{userId}` | Remove member from group |
| GET | `/api/v1/users/{userId}/groups` | List groups a user belongs to |

**Query parameters for `GET /api/v1/groups`:**
- `?tenantId={id}` — filter by tenant (global admin only)
- `?scope=global` — return only global (no-tenant) groups

**Bucket permission grants now support groupId** in addition to userId/tenantId:
- `POST /api/v1/buckets/{name}/permissions` — body may include `groupId`
- `DELETE /api/v1/buckets/{name}/permissions/revoke?groupId={id}` — revoke group permission

### Tenants

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/tenants` | List tenants |
| POST | `/api/v1/tenants` | Create tenant |
| GET | `/api/v1/tenants/{id}` | Get tenant details |
| PUT | `/api/v1/tenants/{id}` | Update tenant |
| DELETE | `/api/v1/tenants/{id}` | Delete tenant |
| GET | `/api/v1/tenants/{id}/stats` | Get tenant statistics |

### Buckets

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets` | List buckets |
| POST | `/api/v1/buckets` | Create bucket |
| GET | `/api/v1/buckets/{name}` | Get bucket details |
| DELETE | `/api/v1/buckets/{name}` | Delete bucket |

### Bucket Configuration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/permissions` | List bucket permissions |
| POST | `/api/v1/buckets/{name}/permissions` | Add permission |
| DELETE | `/api/v1/buckets/{name}/permissions/{id}` | Remove permission |
| PUT | `/api/v1/buckets/{name}/permissions/{id}` | Update permission |
| GET | `/api/v1/buckets/{name}/versioning` | Get versioning config |
| PUT | `/api/v1/buckets/{name}/versioning` | Set versioning config |
| GET | `/api/v1/buckets/{name}/lifecycle` | Get lifecycle rules |
| PUT | `/api/v1/buckets/{name}/lifecycle` | Set lifecycle rules |
| DELETE | `/api/v1/buckets/{name}/lifecycle` | Delete lifecycle rules |
| GET | `/api/v1/buckets/{name}/cors` | Get CORS config |
| PUT | `/api/v1/buckets/{name}/cors` | Set CORS config |
| DELETE | `/api/v1/buckets/{name}/cors` | Delete CORS config |
| GET | `/api/v1/buckets/{name}/acl` | Get bucket ACL |
| PUT | `/api/v1/buckets/{name}/acl` | Set bucket ACL |
| GET | `/api/v1/buckets/{name}/policy` | Get bucket policy |
| PUT | `/api/v1/buckets/{name}/policy` | Set bucket policy |
| DELETE | `/api/v1/buckets/{name}/policy` | Delete bucket policy |
| GET | `/api/v1/buckets/{name}/tagging` | Get bucket tags |
| PUT | `/api/v1/buckets/{name}/tagging` | Set bucket tags |
| DELETE | `/api/v1/buckets/{name}/tagging` | Delete bucket tags |
| GET | `/api/v1/buckets/{name}/notifications` | Get notification config |
| PUT | `/api/v1/buckets/{name}/notifications` | Set notification config |
| DELETE | `/api/v1/buckets/{name}/notifications` | Delete notification config |
| PUT | `/api/v1/buckets/{name}/object-lock` | Enable object lock |
| GET | `/api/v1/buckets/{name}/inventory` | Get inventory config |
| PUT | `/api/v1/buckets/{name}/inventory` | Set inventory config |
| DELETE | `/api/v1/buckets/{name}/inventory` | Delete inventory config |
| GET | `/api/v1/buckets/{name}/inventory/reports` | List inventory reports |
| POST | `/api/v1/buckets/{name}/verify-integrity` | Verify bucket object integrity (admin only, rate-limited) |
| POST | `/api/v1/buckets/{name}/recalculate-stats` | Recalculate bucket object count and size (admin only) |

### Bucket Replication (External S3)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/replication` | List replication rules |
| POST | `/api/v1/buckets/{name}/replication` | Create replication rule |
| GET | `/api/v1/buckets/{name}/replication/{id}` | Get rule details |
| PUT | `/api/v1/buckets/{name}/replication/{id}` | Update rule |
| DELETE | `/api/v1/buckets/{name}/replication/{id}` | Delete rule |
| GET | `/api/v1/buckets/{name}/replication/{id}/status` | Get replication status |
| POST | `/api/v1/buckets/{name}/replication/{id}/sync` | Trigger manual sync |

### Objects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{bucket}/objects` | List objects |
| GET | `/api/v1/buckets/{bucket}/objects/search` | Search objects (filters) |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}` | Download object |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}` | Upload object |
| DELETE | `/api/v1/buckets/{bucket}/objects/{key+}` | Delete object |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Get object ACL |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Set object ACL |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Get legal hold |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Set legal hold |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/versions` | List object versions |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/rename` | Rename object — body `{"newKey":"..."}`. Blocked for COMPLIANCE retention or active Legal Hold. |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/tags` | Get object tags |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/tags` | Set object tags — body `{"tags":[{"key":"...","value":"..."}]}` |
| GET | `/api/v1/buckets/{bucket}/folder-size?prefix={prefix}` | Total size (bytes) and object count under prefix |
| GET | `/api/v1/buckets/{bucket}/download-zip?prefix={prefix}` | Stream objects under prefix as ZIP archive (max 10,000 objects / 10 GB) |

### Shares & Presigned URLs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/shares` | List active share links |
| POST | `/api/v1/shares` | Create share link |
| DELETE | `/api/v1/shares/{id}` | Revoke share link |
| POST | `/api/v1/presign` | Generate presigned URL |

### Metrics & Monitoring

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics` | Dashboard metrics |
| GET | `/api/v1/metrics/system` | System metrics (CPU, memory, disk) |
| GET | `/api/v1/metrics/storage` | Storage metrics |
| GET | `/api/v1/metrics/performance` | Performance metrics |
| GET | `/api/v1/metrics/history` | Metrics history |
| GET | `/api/v1/performance/overview` | Performance overview |
| GET | `/api/v1/performance/operations` | Operation-level metrics |
| GET | `/api/v1/performance/history` | Performance history |
| POST | `/api/v1/performance/reset` | Reset performance counters |

### Audit Logs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/audit-logs` | List audit logs (with filtering) |
| GET | `/api/v1/audit-logs/{id}` | Get specific audit log entry |

**Query parameters**: `tenant_id`, `user_id`, `event_type`, `resource_type`, `action`, `status`, `start_date`, `end_date`, `page`, `page_size`

### Settings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | List all settings |
| GET | `/api/v1/settings/{key}` | Get setting value |
| GET | `/api/v1/settings/category/{category}` | List settings by category |
| PUT | `/api/v1/settings/{key}` | Update setting |
| POST | `/api/v1/settings/reset` | Reset all to defaults |

### Logging Configuration

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/logging/test-syslog` | Test syslog output |
| POST | `/api/v1/logging/test-http` | Test HTTP log output |
| POST | `/api/v1/logging/test-file` | Test file log output |

### Identity Providers (IDP)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/identity-providers` | List all providers |
| POST | `/api/v1/identity-providers` | Create provider |
| GET | `/api/v1/identity-providers/{id}` | Get provider details |
| PUT | `/api/v1/identity-providers/{id}` | Update provider |
| DELETE | `/api/v1/identity-providers/{id}` | Delete provider |
| POST | `/api/v1/identity-providers/{id}/test` | Test provider connection |
| POST | `/api/v1/identity-providers/{id}/search-users` | Search users in provider |
| POST | `/api/v1/identity-providers/{id}/search-groups` | Search groups in provider |
| POST | `/api/v1/identity-providers/{id}/import-user` | Import user from provider |
| POST | `/api/v1/identity-providers/{id}/sync` | Sync all group memberships |

### Group Mappings (IDP)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/identity-providers/{id}/group-mappings` | List group mappings |
| POST | `/api/v1/identity-providers/{id}/group-mappings` | Create group mapping |
| PUT | `/api/v1/identity-providers/{id}/group-mappings/{mapId}` | Update mapping |
| DELETE | `/api/v1/identity-providers/{id}/group-mappings/{mapId}` | Delete mapping |
| POST | `/api/v1/identity-providers/{id}/group-mappings/{mapId}/sync` | Sync specific mapping |

### Cluster Management

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/cluster/initialize` | Initialize cluster on this node |
| POST | `/api/v1/cluster/join` | Join existing cluster |
| POST | `/api/v1/cluster/leave` | Leave cluster |
| GET | `/api/v1/cluster/status` | Get cluster status |
| GET | `/api/v1/cluster/config` | Get cluster configuration |
| GET | `/api/v1/cluster/nodes` | List all nodes |
| POST | `/api/v1/cluster/nodes` | Add node |
| GET | `/api/v1/cluster/nodes/{id}` | Get node details |
| PUT | `/api/v1/cluster/nodes/{id}` | Update node |
| DELETE | `/api/v1/cluster/nodes/{id}` | Remove node |
| GET | `/api/v1/cluster/health` | Cluster health summary |
| GET | `/api/v1/cluster/health/history` | Health check history |
| POST | `/api/v1/cluster/health/refresh` | Trigger manual health check |
| GET | `/api/v1/cluster/cache/stats` | Cache statistics |
| DELETE | `/api/v1/cluster/cache` | Clear bucket location cache |

### Cluster Replication

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cluster/replication` | List replication rules |
| POST | `/api/v1/cluster/replication` | Create replication rule |
| PUT | `/api/v1/cluster/replication/{id}` | Update rule |
| DELETE | `/api/v1/cluster/replication/{id}` | Delete rule |
| POST | `/api/v1/cluster/replication/bulk` | Bulk replicate all buckets |

### Cluster Migrations

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/cluster/buckets/{bucket}/migrate` | Start bucket migration |
| GET | `/api/v1/cluster/migrations` | List migrations |
| GET | `/api/v1/cluster/migrations/{id}` | Get migration details |

### Notifications (SSE)

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/notifications/stream` | SSE event stream | JWT |

### System

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/version` | Server version info | None |
| GET | `/api/v1/config` | Public server configuration (includes `maintenanceMode`) | JWT |
| GET | `/health` | Health check | None |
| GET | `/api/v1/security/status` | Security status overview | JWT |

### Profiling

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/profiling` | Go pprof data (global admin only) |

---

## Error Responses

### S3 API (XML)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
  <Resource>/my-bucket</Resource>
</Error>
```

Common codes: `NoSuchBucket`, `NoSuchKey`, `BucketAlreadyExists`, `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `QuotaExceeded`, `ObjectLocked`

### Console API (JSON)

```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

HTTP status codes: 200 (success), 400 (bad request), 401 (unauthorized), 403 (forbidden), 404 (not found), 409 (conflict), 429 (rate limited), 500 (server error)

---

## Prometheus Metrics

Available at `/metrics` on both ports. Key metrics:

```
maxiofs_s3_operations_total{operation, status}
maxiofs_s3_operation_duration_seconds{operation}
maxiofs_storage_used_bytes{tenant}
maxiofs_objects_total{tenant}
maxiofs_buckets_total{tenant}
maxiofs_api_requests_total{method, endpoint}
cluster_nodes_total
cluster_nodes_healthy
cluster_replication_objects_pending
cluster_cache_hit_ratio
```

---

**See also**: [ARCHITECTURE.md](ARCHITECTURE.md) · [CLUSTER.md](CLUSTER.md) · [OPERATIONS.md](OPERATIONS.md) · [SSO.md](SSO.md) · [SECURITY.md](SECURITY.md)
