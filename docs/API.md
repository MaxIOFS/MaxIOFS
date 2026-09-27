# MaxIOFS API Reference

**Version**: 1.7.0 | **Last Updated**: September 27, 2026

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

**Object key rules**: standard S3 keys up to 1024 characters. A key may not be
empty, start with `/`, or contain a `..` path segment. Objects are stored under a
digest of their key, so every other key is ordinary, including one ending in
`.metadata`.

**Missing bucket**: `DeleteObject` and `DeleteMultipleObjects` on a bucket that
does not exist answer `404 NoSuchBucket` for the whole request.

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

**Lock headers on PutObject**: `x-amz-object-lock-mode`,
`x-amz-object-lock-retain-until-date` (RFC 3339) and `x-amz-object-lock-legal-hold`
are stored in the same metadata write as the object. The PUT is rejected with
`400 InvalidRequest` for an unknown mode, a date not in the future, an unknown
legal-hold status, or any of these headers on a bucket without Object Lock.
Without them, the bucket default retention applies. `CreateMultipartUpload`
takes the same headers, validated when the upload is created and applied when it
completes; without them the completed object gets the bucket default retention.

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
| POST | `/api/v1/auth/refresh` | Exchange a refresh token for a new token pair | Refresh token |
| POST | `/api/v1/auth/logout` | Logout | JWT |
| GET | `/api/v1/auth/me` | Get current user info | JWT |

### Two-Factor Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/2fa/setup` | Generate a TOTP secret and QR code |
| POST | `/api/v1/auth/2fa/enable` | Verify a TOTP code and enable 2FA |
| POST | `/api/v1/auth/2fa/verify` | Verify a 2FA code during login (no JWT) |
| POST | `/api/v1/auth/2fa/disable` | Disable 2FA |
| POST | `/api/v1/auth/2fa/backup-codes` | Regenerate backup codes |
| GET | `/api/v1/auth/2fa/status` | 2FA status for a user |

### OAuth / SSO

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/auth/oauth/providers` | List active OAuth providers | None |
| GET | `/api/v1/auth/oauth/{id}/login` | Start the OAuth flow with a provider | None |
| POST | `/api/v1/auth/oauth/start` | Resolve the provider from a preset and an email, then start the flow | None |
| GET | `/api/v1/auth/oauth/callback` | Callback from the provider; issues a one-time code | None |
| GET | `/api/v1/auth/oauth/exchange-code` | Exchange the one-time code for JWT tokens (single use) | None |

### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List users |
| POST | `/api/v1/users` | Create user |
| GET | `/api/v1/users/{id}` | Get user details |
| PUT | `/api/v1/users/{id}` | Update user |
| DELETE | `/api/v1/users/{id}` | Delete user |
| PUT | `/api/v1/users/{id}/password` | Change password |
| PATCH | `/api/v1/users/{id}/preferences` | Update theme and language preferences |
| POST | `/api/v1/users/{id}/unlock` | Unlock a locked account |
| GET | `/api/v1/users/{id}/permissions` | Permissions the user holds, globally and per bucket |
| PUT | `/api/v1/users/{id}/permissions` | Replace the user's permission selection |

### Access Keys

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/access-keys` | List all access keys |
| GET | `/api/v1/users/{id}/access-keys` | List a user's access keys |
| POST | `/api/v1/users/{id}/access-keys` | Create an access key for a user |
| DELETE | `/api/v1/users/{id}/access-keys/{accessKey}` | Delete an access key |

### IAM Policies and Roles

The console view of the entities the [AWS IAM protocol](#aws-iam-protocol) manages.
Policies and roles are global admin only.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/iam/policies` | List managed policies with their current document |
| POST | `/api/v1/iam/policies` | Create a managed policy, or add a version to an existing one |
| DELETE | `/api/v1/iam/policies/{name}` | Delete a managed policy |
| GET | `/api/v1/iam/roles` | List roles with their attached policies |
| POST | `/api/v1/iam/roles` | Create a role, or replace an existing role's trust policy |
| DELETE | `/api/v1/iam/roles/{name}` | Delete a role; sessions issued from it stop working immediately |
| GET | `/api/v1/iam/permissions` | Catalogue of assignable permissions, grouped |

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
**S3 API port** — where AWS SDKs and S3-compatible tooling expect it. Point
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
| GET | `/api/v1/tenants/{id}/users` | List a tenant's users |

### Buckets

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets` | List buckets |
| POST | `/api/v1/buckets` | Create bucket |
| GET | `/api/v1/buckets/{name}` | Get bucket details |
| DELETE | `/api/v1/buckets/{name}` | Delete bucket |
| PUT | `/api/v1/buckets/{name}/owner` | Change the bucket owner |
| GET | `/api/v1/buckets/{name}/versions` | All object versions and delete markers (`?prefix=`, `?maxKeys=`) |
| GET | `/api/v1/buckets/{name}/folder-size?prefix={prefix}` | Total size (bytes) and object count under a prefix |
| POST | `/api/v1/buckets/{name}/download-zip-token` | Mint a download token for a folder archive |
| GET | `/api/v1/buckets/{name}/download-zip?prefix={prefix}` | Stream objects under a prefix as a ZIP archive (max 10,000 objects / 10 GB); accepts `?downloadToken=` |
| POST | `/api/v1/buckets/{name}/recalculate-stats` | Recalculate object count and size (admin only) |
| POST | `/api/v1/buckets/{name}/verify-integrity` | Verify object integrity (global admin only, rate-limited) |
| GET | `/api/v1/buckets/{name}/integrity-status` | Last integrity scan results, newest first |
| POST | `/api/v1/buckets/{name}/integrity-status` | Save a manual scan result |

### Bucket Configuration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/permissions` | List bucket permissions |
| POST | `/api/v1/buckets/{name}/permissions` | Grant a permission to a user, group or tenant |
| DELETE | `/api/v1/buckets/{name}/permissions/revoke` | Revoke a permission (`?userId=`, `?groupId=` or `?tenantId=`) |
| DELETE | `/api/v1/buckets/{name}/permissions/{id}` | Revoke a permission by ID (legacy) |
| GET | `/api/v1/buckets/{name}/versioning` | Get versioning config |
| PUT | `/api/v1/buckets/{name}/versioning` | Set versioning config |
| GET | `/api/v1/buckets/{name}/object-lock` | Get Object Lock config (enabled flag, default retention) |
| PUT | `/api/v1/buckets/{name}/object-lock` | Set Object Lock config |
| GET | `/api/v1/buckets/{name}/lifecycle` | Get lifecycle rules |
| PUT | `/api/v1/buckets/{name}/lifecycle` | Set lifecycle rules |
| DELETE | `/api/v1/buckets/{name}/lifecycle` | Delete lifecycle rules |
| GET | `/api/v1/buckets/{name}/quota` | Get the bucket quota and current usage |
| PUT | `/api/v1/buckets/{name}/quota` | Set the bucket quota (size, object count) |
| DELETE | `/api/v1/buckets/{name}/quota` | Remove the bucket quota |
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
| GET | `/api/v1/buckets/{name}/notification` | Get notification config |
| PUT | `/api/v1/buckets/{name}/notification` | Set notification config |
| DELETE | `/api/v1/buckets/{name}/notification` | Delete notification config |
| GET | `/api/v1/buckets/{name}/encryption` | Get SSE config |
| PUT | `/api/v1/buckets/{name}/encryption` | Set SSE config |
| DELETE | `/api/v1/buckets/{name}/encryption` | Remove SSE config (server default applies) |
| GET | `/api/v1/buckets/{name}/public-access-block` | Get public-access-block config |
| PUT | `/api/v1/buckets/{name}/public-access-block` | Set public-access-block config |
| DELETE | `/api/v1/buckets/{name}/public-access-block` | Remove public-access-block config (all flags false) |
| GET | `/api/v1/buckets/{name}/website` | Get static website config |
| PUT | `/api/v1/buckets/{name}/website` | Set static website config |
| DELETE | `/api/v1/buckets/{name}/website` | Remove static website config |
| GET | `/api/v1/buckets/{name}/inventory` | Get inventory config |
| PUT | `/api/v1/buckets/{name}/inventory` | Set inventory config |
| DELETE | `/api/v1/buckets/{name}/inventory` | Delete inventory config |
| GET | `/api/v1/buckets/{name}/inventory/reports` | List inventory reports |

### Bucket Replication (External S3)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/replication/rules` | List replication rules |
| POST | `/api/v1/buckets/{name}/replication/rules` | Create replication rule |
| GET | `/api/v1/buckets/{name}/replication/rules/{ruleId}` | Get rule details |
| PUT | `/api/v1/buckets/{name}/replication/rules/{ruleId}` | Update rule |
| DELETE | `/api/v1/buckets/{name}/replication/rules/{ruleId}` | Delete rule |
| GET | `/api/v1/buckets/{name}/replication/rules/{ruleId}/metrics` | Replication metrics for a rule |
| POST | `/api/v1/buckets/{name}/replication/rules/{ruleId}/sync` | Trigger a manual sync |

### Objects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{bucket}/objects` | List objects |
| GET | `/api/v1/buckets/{bucket}/objects/search` | Search objects (filters) |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/download-token` | Mint a download token for one object (valid 120 s) |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}` | Download object; accepts `?downloadToken=` |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}` | Upload object |
| DELETE | `/api/v1/buckets/{bucket}/objects/{key+}` | Delete object (`404` when the bucket does not exist) |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Get object ACL |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Set object ACL |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Get legal hold |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Set legal hold |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/versions` | List object versions |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/restore` | Restore a version: re-promote it, or remove a delete marker |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/rename` | Rename object — body `{"newKey":"..."}`. Blocked for COMPLIANCE retention or active Legal Hold. |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/tags` | Get object tags |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/tags` | Set object tags — body `{"tags":[{"key":"...","value":"..."}]}` |

A download token is bound to one object or one folder archive and is not a
session: it opens only the resource it was minted for.

### Shares & Presigned URLs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{bucket}/shares` | List active share links for a bucket |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/share` | Create a share link |
| DELETE | `/api/v1/buckets/{bucket}/objects/{key+}/share` | Revoke a share link |
| POST | `/api/v1/buckets/{bucket}/objects/{key+}/presigned-url` | Generate a presigned URL |

### Metrics & Monitoring

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics` | Dashboard metrics |
| GET | `/api/v1/metrics/system` | System metrics (CPU, memory, disk) |
| GET | `/api/v1/metrics/s3` | S3 operation metrics |
| GET | `/api/v1/metrics/history` | Metrics history |
| GET | `/api/v1/metrics/history/stats` | Metrics history statistics |
| GET | `/api/v1/metrics/performance/latencies` | Latency statistics per operation |
| GET | `/api/v1/metrics/performance/throughput` | Current throughput |
| GET | `/api/v1/metrics/performance/history?operation={op}` | Latency history for one operation |
| POST | `/api/v1/metrics/performance/reset` | Reset performance counters |

### Audit Logs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/audit-logs` | List audit logs (with filtering) |
| GET | `/api/v1/audit-logs/{id}` | Get specific audit log entry |

**Query parameters**: `tenant_id`, `user_id`, `event_type`, `resource_type`, `action`, `status`, `start_date`, `end_date`, `page`, `page_size`

### Settings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | List settings (`?category=`) |
| GET | `/api/v1/settings/categories` | List setting categories |
| GET | `/api/v1/settings/{key}` | Get a setting |
| PUT | `/api/v1/settings/{key}` | Update a setting — body `{"value":"..."}` |
| POST | `/api/v1/settings/bulk` | Update several settings — body `{"settings":{"key":"value"}}` |
| POST | `/api/v1/settings/email/test` | Send a test email to the requesting admin |
| GET | `/api/v1/settings/encryption/recovery-status` | KEK version and whether the recovery bundle was downloaded |
| POST | `/api/v1/settings/encryption/recovery-bundle` | Export the KEK as a passphrase-encrypted recovery bundle |
| GET | `/api/v1/settings/encryption/worker-status` | Encryption worker progress (global admin only) |
| POST | `/api/v1/settings/encryption/worker-run` | Start an encryption pass now |
| POST | `/api/v1/settings/encryption/rotate-kek` | Create a new current KEK version |

### Logging Configuration

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/logs/frontend` | Receive frontend logs |
| POST | `/api/v1/logs/reconfigure` | Reapply logging settings |
| GET | `/api/v1/logs/targets` | List log targets |
| POST | `/api/v1/logs/targets` | Create a log target |
| POST | `/api/v1/logs/targets/test` | Test a target configuration without saving it |
| GET | `/api/v1/logs/targets/{id}` | Get a log target |
| PUT | `/api/v1/logs/targets/{id}` | Update a log target |
| DELETE | `/api/v1/logs/targets/{id}` | Delete a log target |
| POST | `/api/v1/logs/targets/{id}/test` | Test a saved log target |

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
| POST | `/api/v1/identity-providers/{id}/group-members` | List the members of a provider group |
| POST | `/api/v1/identity-providers/{id}/import-users` | Import users from provider |
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
| POST | `/api/v1/cluster/join` | Receive the join package from an existing cluster node |
| POST | `/api/v1/cluster/leave` | Leave cluster |
| GET | `/api/v1/cluster/status` | Get cluster status |
| GET | `/api/v1/cluster/config` | Get this node's cluster configuration |
| GET | `/api/v1/cluster/token` | Get the cluster token (global admin only) |
| GET | `/api/v1/cluster/nodes` | List all nodes |
| POST | `/api/v1/cluster/nodes` | Add a standalone node to the cluster |
| GET | `/api/v1/cluster/nodes/{id}` | Get node details |
| PUT | `/api/v1/cluster/nodes/{id}` | Update node |
| DELETE | `/api/v1/cluster/nodes/{id}` | Remove node |
| GET | `/api/v1/cluster/nodes/{id}/health` | Run a health check on one node |
| POST | `/api/v1/cluster/nodes/{id}/drain` | Mark a remote node dead immediately and start the dead-node reconciler (global admin only) |
| GET | `/api/v1/cluster/cache/stats` | Bucket location cache statistics |
| POST | `/api/v1/cluster/cache/invalidate` | Invalidate the bucket location cache |
| GET | `/api/v1/cluster/buckets` | Buckets with replication information |
| GET | `/api/v1/cluster/buckets/{bucket}/replicas` | Replication information for one bucket |

### High Availability

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cluster/ha` | Replication factor and node status |
| PUT | `/api/v1/cluster/ha` | Set the replication factor — body `{"factor":2}` (global admin only) |
| GET | `/api/v1/cluster/ha/sync-jobs` | Initial-sync and delta-sync jobs |
| GET | `/api/v1/cluster/ha/scrub-status` | Recent anti-entropy runs and the checkpoint of a running cycle |
| GET | `/api/v1/cluster/ha/degraded-state` | Cluster degraded reason (empty when healthy) |

The factor is 1, 2 or 3. Setting it requires as many healthy nodes as the factor,
and on every node free space of at least the current data divided by the factor,
plus 20%.

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
| GET | `/api/v1` | Console API information and endpoint index | None |
| GET | `/api/v1/version` | Server version info | None |
| GET | `/api/v1/config` | Server configuration (includes `maintenanceMode`) | JWT |
| GET | `/api/v1/version-check` | Latest-release check, proxied to maxiofs.com | JWT |
| GET | `/api/v1/security/status` | Security status overview | JWT |
| GET | `/api/v1/health` | Console API health check | None |
| GET | `/health` | Health check | None |

### Profiling

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/profiling/stats` | Real-time system statistics (global admin only) |
| GET | `/debug/pprof/*` | Go pprof endpoints (global admin only) |

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

Common codes: `NoSuchBucket`, `NoSuchKey`, `BucketAlreadyExists`, `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `InvalidRequest`, `QuotaExceeded`, `SlowDown` (rate limited — a client should back off and retry, unlike a bare `429`), `ServiceUnavailable` (cluster write quorum unavailable — a replication factor of 3 with both peers down — with `Retry-After: 30`). A delete blocked by retention or a legal hold answers `AccessDenied`, as in AWS S3.

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
