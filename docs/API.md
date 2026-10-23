# MaxIOFS API Reference

**Version**: 1.8.0 | **Last Updated**: October 13, 2026

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

**Bucket name rules**: as S3 names general purpose buckets, 3 to 63 characters
of lowercase letters, numbers, periods and hyphens, from and to a letter or a
number; not two periods in a row, not an IP address, not starting with `xn--`,
`sthree-` or `amzn-s3-demo-`, not ending with `-s3alias`, `--ol-s3`, `.mrap`,
`--x-s3` or `--table-s3` (`400 InvalidBucketName`). A bucket is reached
path-style or by its host, periods in it too: `{bucket}.{host of public_api_url}`,
and `{bucket}.{website_hostname}` as a website. With one host under the other,
the longer is the one a request is for. Over HTTPS a wildcard certificate covers
one label, so a bucket with periods is sent path-style.

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

**Object key rules**: any key S3 takes, up to 1024 bytes (`400 KeyTooLongError`);
an empty key is `400 InvalidArgument`. A key is taken as it is: `a//b`, `a/./b`,
`/a` and `a/../b` are keys of their own. Objects are stored under a digest of
their key, so no key names a file, including one ending in `.metadata`. A client
sends a `..` segment as it is only if it does not normalize the path: botocore
(aws-cli, boto3) and the Go SDK do not, browsers do.

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
CopyObject applies them to the copy. As in AWS S3, a mode or retain-until date
needs `s3:PutObjectRetention` and a legal hold `s3:PutObjectLegalHold`, besides
`s3:PutObject`; without them the request answers `403 AccessDenied`.

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
- **Range Requests** — Partial object downloads via `Range` header; a range is sent without the object's checksum. `partNumber` on GET and HEAD reads one part of an object written in parts (`206`, `Content-Range`, `x-amz-mp-parts-count`); an object written whole, or in parts by an earlier version, is its only part (`416 InvalidPartNumber` for another)
- **Object headers** — `Cache-Control`, `Content-Disposition`, `Content-Encoding`, `Content-Language` and `Expires` are kept and given back, for an object written whole, in parts or copied; `x-amz-tagging` on CreateMultipartUpload tags the object written. `response-content-type`, `response-content-disposition`, `response-cache-control`, `response-content-language`, `response-content-encoding` and `response-expires` on GET and HEAD set the headers sent; an anonymous GET with them is refused (`400 InvalidRequest`)
- **Limits** — user metadata up to 2 KB (`MetadataTooLarge`); 10 tags, keys up to 128 characters, values up to 256, each key once (`InvalidTag`); parts other than the last of 5 MB at least (`EntityTooSmall`); a copy of an object onto itself must change its metadata, class, tags or encryption (`InvalidRequest`)
- **Conditional Requests** — `If-Match`, `If-None-Match`, `If-Modified-Since`, `If-Unmodified-Since`
- **ETags** — sent between double quotes, as S3 sends them, in headers and in XML (`GetObjectAttributes`, inventory reports and event notifications give them without, as S3 does); conditions and completed parts take them quoted or not
- **Versioning** — an object written before versioning was enabled, or while it is suspended, is the version `null`: kept when a version replaces it, read, copied and deleted with `versionId=null`, and listed by `ListObjectVersions`. While versioning is suspended a write replaces the null version and keeps the others, and a delete puts a null delete marker
- **Conditional Writes** — `PutObject` with `If-None-Match: *` returns 412 `PreconditionFailed` if the object already exists; with `If-Match`, 412 if its ETag is another and 404 `NoSuchKey` if there is no object. Both are decided under the key's lock: of concurrent writes of a new key with `If-None-Match: *`, one is taken
- **Integrity** — a write is checked against its `Content-MD5`, an `x-amz-checksum-*` checksum (CRC32, CRC32C, CRC64NVME, SHA1, SHA256) in a header or in the trailer of an aws-chunked body, the `x-amz-content-sha256` it was signed with, and the signature of each chunk and of the trailers of a body sent in signed chunks (`SignatureDoesNotMatch`); one that does not match is refused (`BadDigest`, `InvalidDigest`, `XAmzContentSHA256Mismatch`) and nothing is stored, on PUT and on each part. A checksum is kept and returned. Only the headers say a body is aws-chunked
- **SSE Response Headers** — `x-amz-server-side-encryption: AES256` returned on GET/PUT/HEAD when the object is encrypted; `GetBucketEncryption` gives `AES256` for a bucket without a configuration of its own, as every object is encrypted
- **PublicAccessBlock enforcement** — `IgnorePublicAcls` voids public ACLs and `RestrictPublicBuckets` public bucket policies for requests without credentials; `BlockPublicAcls` and `BlockPublicPolicy` refuse a request carrying a public ACL or a public policy (`403 AccessDenied`); configure via `PUT /{bucket}?publicAccessBlock`
- **Bucket policies** — evaluated with the caller's IAM policies, AWS-style; principals `"*"`, `arn:aws:iam:::user/<name>` or a user ID; conditions on `aws:SourceIp` and `aws:SecureTransport`; a policy using anything else is refused with `MalformedPolicy` (see SECURITY.md)
- **OwnershipControls** — default `BucketOwnerEnforced`; prevents AWS SDK v2 `OwnershipControlsNotFoundError`; valid values: `BucketOwnerEnforced`, `BucketOwnerPreferred`, `ObjectWriter`
- **Lifecycle** — `Expiration` (days, a date, or delete markers alone), `Transition` and `NoncurrentVersionTransition` to a colder storage class, `NoncurrentVersionExpiration` with `NewerNoncurrentVersions`, `AbortIncompleteMultipartUpload`; filters by prefix, tags (`Tag`, `And`) and size (`ObjectSizeGreaterThan`, `ObjectSizeLessThan`). A version is noncurrent from when the next one was written. A rule's prefix reads back in the form it was set, `Filter` or the rule's `Prefix`. A rule S3 would refuse is refused with its error: `MalformedXML`, `InvalidArgument`, `InvalidRequest` or `InvalidStorageClass`. In a cluster a transition moves the object to the nodes of its class (see CLUSTER.md)
- **Storage classes** — `x-amz-storage-class` on PUT, CreateMultipartUpload and CopyObject (a copy without one is STANDARD); returned on GET, HEAD and listings; a class S3 does not have is refused (`InvalidStorageClass`)
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
| POST | `/api/v1/auth/login` | Login (username + password). With 2FA, answers `requires_2fa` and a `two_factor_token` valid for 5 minutes instead of a session | None |
| POST | `/api/v1/auth/refresh` | Exchange a refresh token for a new token pair | Refresh token |
| POST | `/api/v1/auth/logout` | Logout | JWT |
| GET | `/api/v1/auth/me` | Get current user info | JWT |

### Two-Factor Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/2fa/setup` | Generate a TOTP secret and QR code |
| POST | `/api/v1/auth/2fa/enable` | Verify a TOTP code and enable 2FA |
| POST | `/api/v1/auth/2fa/verify` | Complete a 2FA login: `{"two_factor_token", "code"}` (no JWT). A wrong code counts as a failed login |
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
| GET | `/api/v1/auth/oauth/exchange-code` | Exchange the one-time code for JWT tokens (single use); for a user with two-factor authentication, `{"requires_2fa": true, "two_factor_token"}` for `/api/v1/auth/2fa/verify`. In a cluster any node exchanges it: the code names the node that issued it | None |

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

Permissions are evaluated per request against the base user's live state: their
policies and the bucket policy apply unchanged.
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
| GET | `/api/v1/buckets/{name}/download-zip?prefix={prefix}` | Stream objects under a prefix as a ZIP archive (max 10,000 objects / 10 GB); accepts `?downloadToken=`. An entry is the key after the prefix, as a relative path: empty and `.` segments dropped, `..` and a leading drive letter percent-encoded (`%2E%2E`, `C%3A`) |
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
| PUT | `/api/v1/buckets/{name}/lifecycle` | Set the lifecycle rules sent; a stored rule of another ID stays |
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
| GET | `/api/v1/buckets/{bucket}/search` | Search objects (filters) |
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

`{key+}` is the key with each `/` sent as `%2F`: routes match the path as sent,
so `objects/x%2Ftags` is the object `x/tags` and `objects/x/tags` its tags.

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
| POST | `/api/v1/cluster/join` | Receive the join package from an existing cluster node; `409` when this node holds data |
| POST | `/api/v1/cluster/leave` | Leave the cluster; the other nodes it reaches remove it |
| GET | `/api/v1/cluster/status` | Get cluster status |
| GET | `/api/v1/cluster/config` | Get this node's cluster configuration |
| GET | `/api/v1/cluster/token` | Get the cluster token (global admin only) |
| GET | `/api/v1/cluster/nodes` | List all nodes; `is_coordinator` marks the coordinator as the answering node knows it, none while the nodes elect one |
| POST | `/api/v1/cluster/nodes` | Add a standalone node to the cluster; `409` when that node holds data |
| GET | `/api/v1/cluster/nodes/{id}` | Get node details |
| PUT | `/api/v1/cluster/nodes/{id}` | Update node |
| DELETE | `/api/v1/cluster/nodes/{id}` | Remove a node from every node; the node leaves when it next contacts one |
| GET | `/api/v1/cluster/nodes/{id}/health` | Run a health check on one node |
| POST | `/api/v1/cluster/nodes/{id}/drain` | Start the drain of a node, this one included: it takes no new data, its objects are moved to the other nodes, and once it holds none it serves no S3 request and is out of service on every node; `409` when the other nodes that take data would be fewer than the most demanding storage class needs (its factor, or three with parity) (global admin only) |
| PUT | `/api/v1/cluster/nodes/{id}/storage-class` | Put a node in a storage class kept by the cluster, STANDARD included — body `{"storage_class":"GLACIER"}`; `409` when the other nodes of its class that take data would be fewer than its protection needs (global admin only) |
| GET | `/api/v1/cluster/buckets/{bucket}/replicas` | Replication information for one bucket |

### High Availability

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cluster/ha` | Replication factor or parity of STANDARD (`parity`, `data_shards`, `parity_shards` of a write now), its usable capacity, tolerated failures and nodes (`standard_nodes`), the classes kept (`storage_classes`), and node status with the class of each |
| PUT | `/api/v1/cluster/ha` | Set the replication factor — body `{"factor":2}` — or turn parity on — body `{"parity":true}`, 3 healthy nodes at least; a factor turns it off (global admin only) |
| GET | `/api/v1/cluster/ha/sync-jobs` | Initial-sync jobs; a done job's `error_message` says how many objects it left to the catch-up |
| GET | `/api/v1/cluster/ha/scrub-status` | Recent anti-entropy runs and the checkpoint of a running cycle |
| GET | `/api/v1/cluster/ha/degraded-state` | Cluster degraded reason, computed for the nodes as they are (empty when healthy) |
| GET | `/api/v1/cluster/storage-classes` | The storage classes kept, STANDARD first: protection, layout with parity, nodes, room, tolerated failures, `below_protection`; the nodes with their class; the classes that can be added (global admin only) |
| PUT | `/api/v1/cluster/storage-classes/{name}` | Keep a storage class on nodes of its own, or change its protection — body `{"factor":2}` or `{"parity":true}`; STANDARD's is set with `/cluster/ha` (global admin only) |
| DELETE | `/api/v1/cluster/storage-classes/{name}` | Stop keeping a storage class: its nodes go back to STANDARD and its objects are kept there (global admin only) |

The factor is 1, 2 or 3. Setting it requires as many healthy nodes of STANDARD as
the factor, and on every node free space of at least the current data divided by
the factor, plus 20%.

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

Common codes: `NoSuchBucket`, `NoSuchKey`, `BucketAlreadyExists`, `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `InvalidRequest`, `QuotaExceeded`, `SlowDown` (rate limited — a client should back off and retry, unlike a bare `429`), `ServiceUnavailable` (cluster write quorum unavailable — a replication factor of 3 with both peers down — with `Retry-After: 30`; a request forwarded to a node that does not hold the bucket, with `Retry-After: 1`). A delete blocked by retention or a legal hold answers `AccessDenied`, as in AWS S3.

### Console API (JSON)

```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

HTTP status codes: 200 (success), 202 (accepted, runs in the background), 400 (bad request), 401 (unauthorized), 403 (forbidden), 404 (not found), 409 (conflict), 429 (rate limited), 500 (server error), 503 (maintenance mode: code `MAINTENANCE_MODE`)

---

## Prometheus Metrics

Prometheus format at `/metrics` on the S3 port, without authentication, while `metrics.enable` (configuration) and the `metrics.enabled` setting are on. `/api/v1/metrics` on the console port is the console's JSON and needs a session. Each node of a cluster exports its own. `docker/grafana/dashboards/maxiofs.json` shows every family below.

| Metric | Labels | Counts |
|--------|--------|--------|
| `maxiofs_http_requests_total` | `method`, `operation`, `status` | S3 API requests, by S3 operation and HTTP status |
| `maxiofs_http_request_duration_seconds` | `method`, `operation` | S3 API request duration (histogram) |
| `maxiofs_http_request_size_bytes`, `maxiofs_http_response_size_bytes` | `method`, `operation` | Request and response body sizes (histograms) |
| `maxiofs_s3_operations_total` | `operation`, `bucket`, `status` | S3 operations by outcome: `success` below HTTP 400, `refused` 4xx, `error` 5xx |
| `maxiofs_s3_operation_duration_seconds` | `operation` | S3 operation duration (histogram) |
| `maxiofs_s3_errors_total` | `operation`, `bucket`, `error_type` | S3 requests the server failed (5xx), by S3 error code; a HEAD, which has no body, by HTTP status |
| `maxiofs_s3_refused_total` | `operation`, `bucket`, `error_type` | S3 requests refused as the client's (4xx), such as a probe for a key that is not there or an access denied, by S3 error code; a HEAD by HTTP status |
| `maxiofs_storage_operations_total` | `operation`, `status` | Storage backend operations: `success`, `not_found` or `failure` |
| `maxiofs_storage_operation_duration_seconds` | `operation` | Storage backend operation duration; a read's time is the time to open the object (histogram) |
| `maxiofs_object_operations_total` | `operation`, `bucket` | Object operations completed from any entry point: S3 API, console, background tasks |
| `maxiofs_object_operation_duration_seconds`, `maxiofs_object_size_bytes` | `operation` | Object operation duration and object size (histograms) |
| `maxiofs_auth_attempts_total` | `method`, `status` | Sign-ins: `s3_sigv4`, `s3_sigv2`, `s3_presigned`, `s3_post_policy`, `console_password`, `console_2fa`, `console_oauth` |
| `maxiofs_auth_failures_total` | `method`, `reason` | Failed sign-ins by reason: an S3 error code, or `invalid_credentials`, `account_locked`, `rate_limited`, `sso_required`, an OAuth error |
| `maxiofs_bucket_objects_total`, `maxiofs_bucket_bytes_total` | `bucket` | What this node holds of each bucket, read at each scrape; bytes include versions |
| `maxiofs_bucket_operations_total` | `operation`, `bucket`, `status` | Bucket creations, deletions and configuration changes |
| `maxiofs_object_lock_operations_total` | `operation`, `bucket`, `status` | Retention, legal hold and Object Lock configuration changes |
| `maxiofs_object_lock_retention_objects_total` | `bucket`, `mode` | Objects whose current version a retention mode holds, counted every 15 minutes |
| `maxiofs_background_tasks_total` | `task_type`, `status` | Runs of `lifecycle`, `inventory`, `replication`, `stats_reconciler`, `integrity_scrub`, `encryption`, `audit_retention`, `sts_session_sweep`, `anti_entropy`, `dead_node_check`, `ha_sync` |
| `maxiofs_background_task_duration_seconds` | `task_type` | Background task run duration (histogram) |
| `maxiofs_system_events_total` | `event_type` | Events sent to the console: disk, quota, node, integrity and account alerts, configuration changes |
| `maxiofs_system_cpu_usage_percent`, `maxiofs_system_memory_usage_percent`, `maxiofs_system_disk_usage_percent`, `maxiofs_system_disk_used_bytes`, `maxiofs_system_disk_total_bytes` | | Node resources |
| `maxiofs_cache_hit_rate`, `maxiofs_cache_size_bytes` | | Metadata (Pebble) block cache hit rate since start, and size |
| `maxiofs_operation_latency_{p50,p95,p99,mean}_milliseconds`, `maxiofs_operation_success_rate_percent`, `maxiofs_operation_count_total` | `operation` | Per-node latency, success rate and count over the last hour |
| `maxiofs_throughput_{requests,bytes,objects}_per_second` | | Current throughput |

A refused request does not name its bucket (`bucket=""`): anyone can name any bucket. No label carries an object key.

---

**See also**: [ARCHITECTURE.md](ARCHITECTURE.md) · [CLUSTER.md](CLUSTER.md) · [OPERATIONS.md](OPERATIONS.md) · [SSO.md](SSO.md) · [SECURITY.md](SECURITY.md)
