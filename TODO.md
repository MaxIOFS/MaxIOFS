# MaxIOFS - Development Roadmap

**Last Updated**: August 13, 2026

> This file tracks ONLY work in progress and pending work. Completed work lives in [CHANGELOG.md](CHANGELOG.md).

---

## ✅ Closed — the IAM/STS migration

**IAM is the authorization model: one permission system, and nothing in the
request path decides by role, by tenant membership or by ACL for an
authenticated caller.** Everything below was a place the migration had not
reached, or a defect introduced while doing it. All of it is closed; the list
stays as the record of what was checked.

Customers are on **1.5.2**. **1.6.0 is an internal development version** that
has not been released to anyone — whoever uses a nightly build does so at their
own risk.

### Complete authorization bypasses — no check of any kind

- [x] **`SelectObjectContent` authorizes nothing at all** (`pkg/s3compat/select.go`).
  Not one call to `GetUserFromContext`, `userCanPerformS3ActionInTenant` or
  `requireObjectS3Action` exists in the file. It resolves the bucket by bare
  name across the whole metadata store, so there is no tenant boundary either.
  Reproduced: the same credential gets `403` on `GetObject` and `200` with the
  object contents through `POST /{bucket}/{object}?select`. Any authenticated
  credential reads any CSV/JSON object in the deployment.
- [x] **`handleDownloadZip` authorizes nothing at all**
  (`internal/server/download_zip_handler.go`). Reproduced: `403` from
  `handleGetObject`, `200` and a real ZIP of the whole bucket from the
  download-zip route beside it. Every per-action guard added to the console is
  bypassed by the bulk-read path.
- [x] **`HandlePresignedPost` verifies the signature and then writes with no
  permission check** (`pkg/s3compat/presigned.go:876`). It resolves the user
  into the context with a comment saying "for permission checks" and never
  performs one. Any holder of a valid access key can craft a self-signed POST
  policy and upload into any bucket.
- [x] **`handleUpdateBucketOwner` requires only a session**
  (`internal/server/console_api.go:5127`). Reproduced: a user with no
  permissions makes themselves the owner of anyone's bucket.
- [x] **Replication endpoints require only a session**
  (`internal/server/console_api_replication.go`). Reproduced: a user with an
  empty policy set creates a rule with an arbitrary destination, and the worker
  then copies the bucket out with server privileges. The same handlers return
  `destination_secret_key` in plaintext, and `handleGetBucketReplicas`
  (`cluster_handlers.go:661`) leaks those credentials **across tenants** with no
  check at all.

### Privilege escalation

- [x] **A tenant administrator can take over the global administrator account.**
  `handleChangePassword` (`console_api.go:4067`) scans for `role == "admin"`
  with no tenant comparison — the exact thing `isGlobalAdmin` exists to prevent
  — and skips the current-password requirement when the target is someone else.
  `handleCreateUser` performs no role validation, so a tenant admin can mint an
  `admin` inside their own tenant to get there. Reproduced end to end: the
  global administrator's password is changed and `VerifyPassword` confirms it.
- [x] **Multipart authorizes one key and operates on another.** `UploadPart`,
  `ListParts`, `CompleteMultipartUpload` and `AbortMultipartUpload` check the
  URL's bucket and key, then pass only `uploadID` to the object manager, which
  never verifies the upload belongs to them. Reproduced with a caller explicitly
  denied on the victim: list `200`, upload part `200`, abort `204` and the
  victim's upload destroyed. `Complete` writes to the destination recorded in
  the upload metadata — a bucket the caller holds nothing on. A single ownership
  assertion fixes all four.
- [x] **User permissions are readable and writable across tenants**
  (`iam_console_handlers.go:452`). `permissionStore` accepts any admin, and
  neither handler compares the target's tenant to the caller's. A tenant admin
  reads and rewrites another tenant's users' permissions.
- [x] **`handleShareObject` accepts an attacker-supplied `?tenantId`**
  (`console_api.go:2504`) and checks no read permission. A share makes the
  object anonymously readable over S3, so this is a permanent public link to
  another tenant's object. Its two sibling handlers carry the global-admin gate
  this one is missing.

### Authorization still decided by the old model

- [x] **The ACL / `AuthenticatedUsers` cascade still overrides IAM for
  authenticated callers** on DELETE (`handler.go:2996`, used by `DeleteObject`
  and batch `DeleteObjects`) and `HeadBucket` (`:886`). Reproduced: a caller the
  policies explicitly deny is allowed by an `AuthenticatedUsers` grant. Any
  bucket ever set to `authenticated-read` hands delete to every credential in
  the deployment. The same cascade sits in three more validators.
- [x] **`ListBuckets` decides by role and tenant membership**
  (`handler.go:478`) — the last `IsAdminUser` in the package — and
  `s3:ListAllMyBuckets` is catalogued but enforced nowhere, so it is grantable
  and grants nothing.
- [x] **Bucket listings never consult the policies.**
  `FilterBucketsByPermissions` (`internal/bucket/filter.go`) decides by role
  `admin`, by ownership and by `CheckBucketAccess`, which reads the legacy
  `bucket_permissions` table that is no longer consulted when authorizing. A
  bucket granted purely through IAM is invisible in both the console and the S3
  listing while its objects are fully accessible.
- [x] **SOSAPI virtual objects**: the tenant guard is one-directional on
  `GetObject` (`handler.go:2835`) and absent on `HeadObject` (`:1723`), with no
  policy check on either. Leaks per-tenant capacity and quota figures.
- [x] **Cross-tenant bucket inventory** (`cluster_handlers.go:593`) reads
  `tenant_id` from a context key the console middleware never sets, so it is
  always empty and the handler lists every tenant's buckets with counts and
  sizes.

### Permissions that do not mean what the console says

- [x] **A policy-only grant cannot read an object.**
  `validateObjectReadPermission` (`handler.go:3395`) still requires an explicit
  ACL for any authenticated caller, after the policy check has already passed.
  Reproduced: a user holding `s3:GetObject` on the bucket gets `403`. The IAM
  read path does not work for anyone who is not the owner. Also breaks
  `CopyObject` and `UploadPartCopy` on their source object.
- [x] **Tenant administrators silently lost permissions, and role policies are
  ignored for them.** `EffectivePolicyDocumentsInTenant` substitutes a hardcoded
  `tenantAdminDocument()` and **skips the role's IAM-attached policies**
  entirely. That hardcoded list omits both Object Lock configuration actions,
  `s3:BypassGovernanceRetention` and `iam:*`. So a tenant admin cannot read or
  set immutability configuration at all, and anything an operator attaches to or
  revokes from a role has no effect on any user who has a tenant. Both halves
  reproduced.
- [x] **Object subresources ignore `versionId`** and check the current-object
  action: attributes, tagging, retention, legal hold and ACL. A grant of
  `s3:GetObject` alone reads any historical version, bypassing
  `s3:GetObjectVersion`. `CopyObject`/`UploadPartCopy` take the source version
  from a header the check never looks at.
- [x] **Bucket subresources borrow unrelated permissions** because no catalogued
  action exists for them: encryption is gated by `s3:PutBucketCORS`; logging,
  replication, website and inventory by lifecycle actions; notification and
  public-access-block by bucket-policy actions; ownership controls by ACL
  actions. One lifecycle grant silently confers four unrelated configurations.
- [x] **Ownership transfer moves no permission.** `handleUpdateBucketOwner`
  writes the owner fields and never touches the owner policy, so the new owner
  gets nothing and the previous one keeps everything.
- [x] **The permissions screen bakes inherited permissions into the user.**
  `GetUserPermissions` resolves *effective* permissions (role, tenant, group,
  owner) while `SetUserPermissions` stores whatever comes back as the user's own
  direct grants. Opening any user's screen and pressing Save converts their
  role-derived permissions into permanent ones that survive losing the role. The
  same call deletes every `bucket-*` grant while leaving the legacy
  `bucket_permissions` row in place, so the console keeps displaying a grant
  that authorizes nothing.
- [x] **Bucket-permission changes do not replicate as authorization.** Grants
  and revocations trigger only the legacy `bucket_permission` sync, which
  carries a table nobody reads at request time, and no tombstone is recorded
  when the inline policy is deleted. Because the IAM receiver upserts
  unconditionally, a peer pushes the revoked policy back and access returns
  cluster-wide. The same mechanism resurrects `owner-<bucket>` after a bucket is
  deleted, which is exactly what `RevokeBucketPolicies` exists to prevent.
  Additionally `IAMSyncManager.Start` returns without starting unless
  `auto_access_key_sync_enabled` is true — authorization data now rides on a
  flag meant for access keys.
- [x] Dead writes to `user_capability_overrides` remain in the user sync path;
  those rows authorize nothing now.

### Fixed in this pass, recorded for the record

- [x] **The console refused a global administrator every write, everywhere.**
  The per-action console guard applied its "may only read" rule to every global
  administrator rather than only to one reaching INTO a tenant, so creating or
  deleting a bucket in the global namespace answered 403 — and
  `TestHandleCreateBucket` had its assertion changed from 200 to 403 in the same
  commit, with a comment rationalising it, instead of the guard being fixed.
  Restored, and the two surfaces are now explicitly different: the console is
  the administration surface and a global administrator administers it, while
  the S3 path keeps the read-only-when-crossing rule that
  `tenant_boundary_test.go` pins.

### Regressions introduced while doing the migration

- [x] **Two of three cluster proxy entry points lost every timeout.** Removing
  `http.Client.Timeout` from the shared client was correct — it was a file-size
  limit in disguise — but only `ProxyRequest` was converted to the progress
  watchdog. `ProxyToNodeAPIURL` (the S3 data path) and `DoAuthenticatedRequest`
  (~45 call sites: HA sync, anti-entropy, migration, reconciliation) now call
  `.Do()` with **no bound at all**. `ResponseHeaderTimeout` covers only the
  headers, so a peer that answers `200` and then falls silent wedges the caller
  forever, on contexts that carry no deadline.
- [x] **`proxyBucketRequest` falls through to local handling with a consumed
  body** (`pkg/s3compat/handler.go:340`) — the defect that was fixed in the
  console proxy and left here. Reproduced: after a failed forward the body reads
  zero bytes with a `nil` error, so `PutObject` **stores a 0-byte object and
  returns 200**. The client is told the upload succeeded.
- [x] **A download token is accepted on four sibling GET routes** when the
  object key is literally `acl`, `tags`, `versions` or `legal-hold`
  (`download_token.go:62`). The suffix test was written as though it matched the
  route; it matches the path. Bounded — same bucket, same key, GET only, and
  those handlers still check permissions — but wider than documented.
- [x] **The `versionId` fix was applied to one of two call sites.**
  `ObjectVersionsModal.tsx` was corrected; the identical download button on the
  bucket page's Versions tab (`pages/buckets/[bucket]/index.tsx:1720`) still
  downloads the current version under the older version's name.
- [x] `transfer.Transport` leaves `req.GetBody` pointing at the unwrapped body,
  so a transport-level retry uploads without stamping the progress clock.
- [x] `APIClient.fetchObjectBlob` has no callers and drops `versionId`, while
  its comment advertises reading a specific version.
- [x] `downloadFolderAsZip` still buffers the whole archive in memory — the case
  the single-object download was converted away from, and typically larger.
- [x] Orphaned i18n keys `downloadingFile` / `downloadingKey` in all 9 locales.

### Not from this migration — pre-existing, surfaced in the same pass

Unrelated to IAM/STS, recorded here so they are not lost.

- [x] **A lost `.metadata` sidecar makes `GetObject` serve raw ciphertext with
  HTTP 200.** `GetMetadata` falls back to `generateBasicMetadata`, which has no
  `encrypted` key, so decryption is skipped and the ciphertext is streamed with
  a `nil` error under the plaintext `Size`/`ETag`. Reproduced: 1312 bytes of
  AES-GCM served under `Content-Length: 1280`. Reachable when
  `repairStagedCommit`'s roll-forward rename fails persistently — its own log
  says the object "stays unreadable", but it is readable as garbage. Must fail
  closed: basic metadata cannot distinguish a legacy plaintext object from an
  encrypted one that lost its DEK.
- [x] **The storage backend never calls `fsync`.** `Sync()` appears zero times
  in `internal/storage`: not on the data file, not on the sidecar, not on the
  directory after either rename, and `tempFile.Close()`'s error is discarded.
  Since v1.5.2 Pebble fsyncs its WAL every second, so metadata is durable while
  the bytes and the sidecar carrying the DEK are not — after a power cut Pebble
  confidently reports objects whose data may be zero-length, which is the
  finding above at scale. The two-phase sidecar commit protects ordering and
  assumes a durability nothing provides.
- [x] **Data race on `cluster.Manager.tlsConfig` / `clusterHTTPClient`**
  (`manager.go:978` writes; `:907`, `:927` and `health.go:212` read).
  Reproduced under `-race`. `loadTLSConfig` runs from the init/join HTTP
  handlers while ~20 dynamic proxy clients read the config on every request.
  `ProxyClient` guards its own rebuild with a mutex, but its input is
  unsynchronised.
- [x] **Lost update between `UpdateBucket` and `UpdateBucketMetrics`.**
  `UpdateBucketMetrics` serialises under the bucket-metrics mutex;
  `UpdateBucket` writes the same key with no lock. Reproduced: 4-5 of 200
  increments lost, and in the other direction a bucket setting (quota,
  versioning, Object Lock) is silently reverted.
- [x] **Delete paths take no per-key lock**, unlike `PutObject`. Reproduced:
  bucket size and tenant storage drift after concurrent overwrite/delete. Tenant
  storage has no clamp and drifts **upward**, consuming real quota until a
  manual recount — exactly what a retrying backup client produces.
  `deletePermanently` also swallows `ErrObjectNotFound` and decrements anyway,
  so two concurrent deletes debit twice.
- [x] **`DeleteObjectVersion` leaves the main entry pointing at the deleted
  version** and takes no mutation mutex. `deleteSpecificVersion` repairs it
  best-effort and returns `nil` on all three failure paths, so the client is
  told the delete succeeded. The two halves also disagree on durability
  (`pebble.Sync` vs `commitNoSync`), so a hard kill between them makes the
  dangling pointer permanent, and non-destructive `Reconcile` will never correct
  it.
- [x] **Tenant storage quota is never returned** on versioned deletes or
  `ForceDeleteBucket` — `DecrementTenantStorage` has exactly one caller, on the
  non-versioned path — while every new version adds its full size. A tenant
  using versioning eventually locks itself out at 100% with a fraction actually
  used.
- [x] **Goroutine leak on the encrypted-upload error path**
  (`manager.go:2875`, `:3196`): `FilesystemBackend.Put` returns without draining
  or closing the pipe, leaving the encryptor blocked in `Write` forever. One
  leak per upload that fails mid-copy — that is, during a disk-full episode.
- [x] `FilesystemBackend.List` returns in-flight and orphaned temp files as
  objects (`.tmp_*`, `.metadata-tmp-*`, `maxiofs-upload-*`); `Reconcile` already
  skips all six prefixes. Walk errors are discarded, so an unreadable subtree
  silently shortens the listing.
- [x] Zero-byte objects never have their data file deleted: the delete-marker
  guard tests `Size > 0` instead of `isMetadataDeleteMarker`.
- [x] Unsynchronised `proxyClient` writes in ten `Start()` methods called from
  the init/join handlers while both servers are already serving;
  `LeaderManager.Stop()` panics if called twice; `s.clusterServer` is replaced
  without synchronisation.
- [x] `handleGeneratePresignedURL` has no authorization check, tests for a role
  `system_admin` that exists nowhere in the repo, and leaks a file handle.
- [x] `coordinatorExemptPath` matches substrings rather than path segments, so a
  bucket named `download` skips the coordinator gate.
- [x] TOCTOU on the tenant bucket count, and `UpdateTenant` overwrites the
  atomic usage counters from a stale snapshot.
- [x] Minor: integrity-history lost update; `GetAllLatencyStats` re-locking mid
  iteration; `metrics.collector` Start/Stop channel race (no production caller);
  background loops with no stop path in `cluster/cache.go`, `cluster/health.go`
  (a fresh `http.Transport` per probe, never closed) and
  `replication/manager.go`.

### Test-harness trap worth removing

- [x] The s3compat harnesses wire bucket ownership through a type assertion on
  an anonymous interface. When a method's signature changes, the assertion
  simply stops matching: no compile error, no warning — the owner policy is
  never written, and ten unrelated tests fail as "the owner cannot read its own
  bucket". It cost a confusing debugging round this pass. An explicit named
  interface would fail to compile instead.

### Observed, not yet explained

- [x] The intermittent failures under parallel load were **not** timing. Root
  cause: the SQLite DSN. The driver is `modernc.org/sqlite`, whose parameters
  are written `_pragma=name(value)`; seven test files and three production
  databases used the OTHER driver's form (`_busy_timeout=`, `_journal_mode=`),
  which modernc accepts without complaint and applies not at all — leaving
  rollback-journal mode and a zero busy timeout, so a second writer got
  SQLITE_BUSY instead of waiting. Fixed everywhere, pinned by
  `internal/db/dsn_test.go`, and three consecutive full-suite runs are clean.

### Found only by running a real two-node cluster (August 12, 2026)

Both were invisible to the whole test suite, which exercises the election with
in-process fakes and never with two nodes holding two databases.

- [x] **A two-node cluster could never elect a coordinator, so every
  configuration change failed permanently.** `campaign` recorded its vote for
  itself in `cluster_leader` — the lease table — *before* knowing whether it had
  won. That speculative row is indistinguishable from a real lease, so each node
  refused the other ("lease still held" / "term already granted"), neither ever
  reached a majority, and the term climbed without bound: observed at 99 after
  ten minutes, with `POST /users/{u}/access-keys` answering "This node is no
  longer the coordinator" the whole time. Two further defects fed the same
  livelock: a freshly joined node knows nothing about the current leader, so it
  campaigned blindly and fenced out a healthy one, and every node retried on the
  same fixed five-second tick, so they all stood up in the same instant for ever.
  Fixed by separating the vote from the lease (new `cluster_vote` table; the
  lease is written only after a majority answers), adding a **pre-vote** round
  that asks without persisting anything, and randomising the campaign delay.
  Verified: one term, one election, stable for the whole session.
- [x] **Every immediate cross-node push after a configuration change was
  cancelled by its own HTTP response.** `TriggerSync` received `r.Context()` and
  handed it to a goroutine that outlives the request, so the push died the
  moment the handler answered — "failed to query access keys: context canceled"
  in the logs, and convergence left to the periodic ticker up to a minute later.
  Seven of the nine sync managers were affected (IAM and STS already detached).
  All nine now go through `runDetached` in `internal/cluster/sync_trigger.go`,
  which keeps the request's values, drops its cancellation and bounds the work.
- [x] **Undocumented constraint, not a defect**: the remote cluster port is
  derived from the *local* node's configuration, so every node must use the same
  `cluster_listen` port. Documented in `docs/CLUSTER.md` (Configuration), along
  with the symptom it produces — a node registered at a port nothing listens on,
  which reads as an unexplained unhealthy node.
- [x] **A cluster that loses a node now re-elects among the living.** The
  majority was counted over every node ever known, so a two-node cluster whose
  master died left the survivor unable to elect anybody and unable to change any
  configuration — permanently, since removing the dead node needs a coordinator
  too. The majority is counted over the nodes that answer the round instead: an
  absent node gets no vote on whether the cluster may continue. Verified on the
  lab cluster — master killed, survivor took over in one lease and served access
  key, user and IAM policy writes; master restarted, rejoined as a member and
  synchronised everything it had missed. Pinned by `TestQuorum_*` in
  `internal/cluster/leader_test.go`. The partition trade (both halves lead what
  they can see; reconciled by `updated_at` on heal) is deliberate and documented
  in `docs/CLUSTER.md`.

### CI

- [x] The test step runs `go test $PKGS -v` with **no `-timeout`**, so the
  default 600 s per-package budget applies. Locally `internal/cluster`,
  `internal/server` and `internal/auth` each take 170-190 s without `-race`, and
  a shared runner is easily 3x slower. This is the most likely cause of the CI
  failure that could not be reproduced locally. Add `-timeout 20m` and re-run.

---

## 🔵 In Progress — IAM/STS

MaxIOFS speaks both AWS identity protocols on `POST /` of the S3 endpoint:
**STS** issues short-lived credentials (`ASIA` keys, server-enforced expiry,
revocable, cluster-replicated) and **IAM** manages identities, credentials,
managed and inline policies, and roles. Details in [CHANGELOG.md](CHANGELOG.md);
the model is described in [docs/SECURITY.md](docs/SECURITY.md) and the wire
protocol in [docs/API.md](docs/API.md).

**The design rule that governs both**: a policy attached to a pre-existing user
can only *restrict* — the normal pipeline still decides and the policy
intersects with it. Only identities created through the IAM API are governed by
their policies as a grant. Nothing that already worked changes behaviour.

### Before this can ship

- [x] Manual end-to-end check with a real S3 client (AWS CLI) using temporary credentials, with and without a session policy — **done August 13, 2026** against a running deployment. `ASIA` key signs SigV4 with the session token; the same key without the token is refused; a temporary credential cannot mint another (`AccessDenied`); the session is listed and revocable from the console.
- [x] Manual check of the AWS STS surface with a real SDK — **done**. `aws sts get-session-token` and `aws sts assume-role` both work unmodified. An unknown `RoleArn` answers `NoSuchEntity`, and a role whose trust policy does not name the caller answers `AccessDenied` rather than assuming silently. A session policy narrows a role session and cannot widen it: with `s3:GetObject` only, `GetObject` succeeds and `ListBuckets` is denied even though the role allows it.
- [x] Manual check of the AWS IAM surface with `aws iam --endpoint-url` — **done**: create-user, get-user, list-users, create-access-key, put-user-policy, get-user-policy, list-user-policies, create-policy, create-policy-version (`--set-as-default`), list-policy-versions, attach-user-policy, list-attached-user-policies, create-role, put-role-policy, and every delete. The created credential does exactly what its policy says: with a read-only inline policy it reads the named bucket and its object, while `PutObject`, `DeleteObject` and `CreateBucket` all answer `AccessDenied` and nothing is written. Another bucket is denied. With no policy at all it can do nothing.

  > One deliberate difference from AWS: `ListAllMyBuckets` returns only the buckets the caller's policies reach, not every bucket in the deployment. AWS lists them all because a bucket list is per account; here it is filtered, which is what a multi-tenant deployment needs.
- [ ] Veeam interop: confirm it discovers the endpoints from `system.xml` (`IAMSTS=true`) and completes its create-user → put-user-policy → create-access-key flow
- [x] Multi-node check: issue on node A, use on node B; revoke on A, confirm B rejects; create a policy on A and confirm it applies on B; delete it on A and confirm it does not come back — **done on a real two-node cluster (August 12, 2026)**; all four pass, and it uncovered two defects that no test in the repository could see, described below
- [ ] Federation against a real directory / identity provider: the LDAP bind and the OAuth userinfo call are the two paths automated tests cannot reach

### The tenant boundary is structural, not written in the policies

An ARN names a bucket by its bare name, so `arn:aws:s3:::backups` matches every
bucket called that, in every tenant. What keeps one tenant's grant from reaching
another's bucket is `userCanPerformS3ActionInTenant`, the way an AWS account
bounds what a policy saying `*` can reach — not the documents themselves.

This is a deliberate shape, but it means nothing in a policy expresses it, so
only `pkg/s3compat/tenant_boundary_test.go` holds it in place. Two consequences
worth keeping in mind before extending the model:

- [ ] Every new path that consults a `PolicySet` against a bucket has to go
  through the boundary check. A direct `set.Allows(...)` is unbounded. The rule
  to follow is the standard one, not the convenient one: AWS resolves the
  account boundary structurally, at resource resolution, and never inside the
  policy document. That is the shape to keep.
- [x] **Decided: no tenant-qualified ARNs.** `arn:aws:s3:::<tenant>/<bucket>`
  would move the boundary into the documents, but it is not an ARN any AWS SDK
  or tool understands — S3 ARNs carry no account precisely because the bucket
  name is global. Inventing a format breaks compatibility with the clients this
  server exists to serve. The boundary stays structural, which is also what AWS
  does.

### Known limitations (deliberate)

- No `Condition` support in any policy kind — documents that use one are rejected at write time rather than silently evaluated without it.
- Policy evaluation covers S3 actions. There are no `iam:*` action-level policies: the IAM surface as a whole is gated by the `iam:manage` capability.
- POST form uploads reject temporary credentials outright (fail-closed).
- Session policies cannot be edited: revoke and re-issue.
- LDAP federation does not just-in-time provision users, matching console login.
- Federation endpoints are disabled by default (`security.sts_federation_enabled`).
- IAM-created identities never get console access; they exist to hold S3 credentials.

> **No new migrations for further IAM/STS work.** All of its schema lives in
> `migration18_v160_IAMSTS`; extend that function instead of adding migration 19,
> 20, … One migration per feature, not one per increment. Valid until v1.6.0
> ships, after which the function is frozen and any further schema change needs
> its own migration.

---

## 🔐 Backlog — Encryption: SSE-C / SSE-KMS (Phase 5)

The envelope system (KEK in DB, per-object DEK, worker, rotation, ciphertext HA replication, recovery bundle + `maxiofs recover`) shipped in the v1.5.0 batch. What remains is real per-request key support on top of it:

- **SSE-C**: the KEK is the customer key from the request headers (over TLS); store only the key MD5 + the wrapped DEK — the server never persists the customer key.
- **SSE-KMS**: the KEK lives in an external KMS via a pluggable provider (Vault Transit / AWS KMS).

Context that still applies:
- The reader stays multi-format — (1) plaintext → as-is; (2) legacy direct-encrypted → KEK-v1; (3) envelope → unwrap DEK. None can be dropped or existing data is lost.
- Old KEK versions are kept forever by design (tiny, included in the bundle, deleting one could orphan sidecar-only files that reference it).
- Cluster known limitation: a node that already holds a conflicting KEK version number (e.g. ex-member of another cluster) cannot join without recovery.

### Smaller encryption/recovery follow-ups

- **`maxiofs recover` checkpoint/resume**: the current implementation is a single pass (safe: output store is fresh and non-empty output is refused, so a crash = delete the partial out-db and re-run). For multi-million-object deployments a checkpoint would avoid restarting the walk.
- **Recovery-bundle stronger variants** (optional): recovery-key **escrow** (wrap the KEK with a separately-held break-glass key) and **Shamir** split (N shares, K to reconstruct).
- **Admin restore endpoint** (optional): upload a bundle through the console for the "fresh install after disaster" flow — must replace the freshly-generated KEK before any new objects are written. Today this is covered by `maxiofs recover` offline.

---

## 🔧 Follow-up — Minor items deliberately deferred (July 2026 bug-hunt)

Two low-impact items from the full-project review were left as-is on purpose:

- **Bandwidth throttling spends tokens after the read** (`internal/bandwidth`): each 32 KB chunk is delivered, then waited for — transfers can run up to one chunk ahead of budget. Cosmetic smoothing only.
- **Quota delta computed outside the per-key lock** (`object.PutObject`): two concurrent overwrites of the same key can compute the same delta, causing transient metrics drift. Fixing it would hold the sharded per-key lock across encryption + disk IO, serialising unrelated keys that share a shard — not worth it for a metrics-only drift.

---

## 🟣 Planned — Erasure Coding (replace N-way replication for large objects)

**Goal**: cut disk overhead from `N×` (N-way replication) to `~1.5×` while preserving the same failure tolerance. Today a 1 GB object with `factor=3` consumes 3 GB cluster-wide; with EC `4+2` it consumes 1.5 GB and tolerates the same 2 node failures.

**Rationale**: erasure coding deserves its own release. It changes the on-disk layout, the metadata schema, and the read/write paths. The HA durability primitives shipped earlier (quorum, read fallback, anti-entropy) are prerequisites — without them, EC just multiplies the existing data-loss windows across more shards.

---

### Storage model

Reed-Solomon `K + M`:
- `K` data shards: the object split into K equal parts.
- `M` parity shards: computed from the K data shards.
- Object reconstructible from **any K of the K+M shards**.
- Tolerates loss of `M` nodes simultaneously.
- Disk overhead: `(K+M)/K`.

| Scheme | Nodes needed | Overhead | Tolerates |
|--------|--------------|----------|-----------|
| `4+2` | 6 | 1.5× | 2 nodes |
| `6+3` | 9 | 1.5× | 3 nodes |
| `8+4` | 12 | 1.5× | 4 nodes |

For comparison, current `factor=3` replication is 3× overhead and tolerates 2 nodes — EC `4+2` is the same tolerance at half the disk cost.

**Hybrid model**: small objects (< `ec.min_object_size`, default 1 MB) keep using N-way replication. Reed-Solomon has fixed per-object overhead (shard headers, metadata) that dominates for small files. MinIO does the same.

**Encryption interaction**: EC shards carry ciphertext (objects are envelope-encrypted before sharding), so the shard distribution path reuses the raw-transfer machinery from ciphertext HA replication.

---

### Work items

#### 1. EC config + library integration (~3 days)

- New cluster global config: `ec.enabled`, `ec.data_shards` (K, default 4), `ec.parity_shards` (M, default 2), `ec.min_object_size` (default 1 MB).
- Validate at config-set time: `K + M ≤ healthy_node_count`.
- Add dependency: `github.com/klauspost/reedsolomon` (the canonical Go EC library, well-maintained, used by MinIO, SeaweedFS, etc.).
- Files: `internal/cluster/sync_schema.go`, `internal/cluster/manager.go`, `go.mod`.

#### 2. EC writer (~1 week)

New module `internal/storage/ec/writer.go`:

- Buffer the input stream into chunks (configurable, default 4 MB per stripe).
- For each stripe: split into K data shards, compute M parity shards via reedsolomon library, send each shard to a different cluster node in parallel.
- Same quorum semantics as the current write path: client gets 200 only when all `K+M` shards are written. Tolerate up to `M` failures (we still have K to reconstruct), but mark failed nodes `stale` for repair.
- Anti-loop: same `X-MaxIOFS-HA-Replica` header pattern.

Edge cases:
- Object size not a multiple of stripe size: pad the last stripe, store the original size in metadata so reads truncate correctly.
- Multipart upload: each part goes through its own EC encoding. Part metadata records the shard layout per part.
- Concurrent writes to the same key: same versioning rules as today, but each version is its own EC layout.

Files: `internal/storage/ec/writer.go` (new), `internal/object/manager.go` (route to EC for `size >= ec.min_object_size`), `internal/server/cluster_object_handlers.go` (shard receiver endpoint).

#### 3. EC reader (~1 week)

New module `internal/storage/ec/reader.go`:

- Read object metadata to learn the shard layout `[(NodeID, ShardIdx, Size)]`.
- Request K shards in parallel (try data shards first; fall back to parity shards if any data shard node is unavailable).
- Reconstruct the original stream via `reedsolomon.Reconstruct`.
- Streaming: produce output as soon as the first K shards arrive — don't buffer the whole object.

Edge cases:
- More than M nodes down → object unrecoverable, return 503 with which shards are missing (admin needs to know what to repair).
- Partial shard corruption (checksum mismatch on a shard): treat that shard as missing, fall back to another.
- Range requests: compute which stripes are needed, fetch only those shards. Saves bandwidth on large objects with small ranges.

Files: `internal/storage/ec/reader.go` (new), `internal/object/manager.go`.

#### 4. EC metadata in Pebble (~3 days)

Extend object metadata to store EC layout. New fields on `metadata.Object`:

```go
EncodingType  string  // "replication" | "ec"
ECDataShards  int     // K
ECParityShards int     // M
ECStripeSize  int     // bytes per stripe
ECShards      []ECShardLocation  // per-shard: NodeID, ShardIdx, Checksum
```

Existing replicated objects keep `EncodingType = "replication"` and the new fields stay zero-valued. Reader picks the path based on `EncodingType`.

Files: `internal/metadata/types.go`, `internal/metadata/pebble_objects.go`, `internal/object/adapter.go`.

#### 5. EC-aware anti-entropy and repair (~3 days)

Extend the existing scrubber to also check shard health:

- For EC objects, check each shard's existence and checksum on its assigned node.
- If a shard is missing or corrupted: read K healthy shards, reconstruct the missing/bad one, write it to a healthy node (the original or a new one if the original is dead).
- If `M` shards are missing simultaneously, the object is on the edge of unrecoverable — escalate to a critical SSE alert immediately.

Files: `internal/cluster/anti_entropy.go`.

#### 6. Migration: replication → EC (~1 week)

Background worker that converts existing replicated objects (size ≥ `ec.min_object_size`) to EC layout:

- Reads the object once from any replica.
- Writes new EC shards to K+M nodes via the new EC writer.
- Updates Pebble metadata atomically (`EncodingType` flips from `replication` → `ec`, `ECShards` populated).
- Deletes the old replica copies only after the EC layout is verified readable.
- Crash-safe: checkpoint last-migrated key in Pebble.
- Throttled and pausable from the admin UI.

Reverse migration (EC → replication) supported for the same case the user wants to roll back. Same worker, opposite direction.

Files: `internal/cluster/ec_migration_worker.go` (new), `internal/server/cluster_ha_handlers.go`.

#### 7. Frontend — EC controls (~3 days)

`web/frontend/src/pages/cluster/HA.tsx`:

- New section "Storage encoding" with Replication / Erasure Coding toggle.
- K and M sliders, with live disk-overhead and tolerance preview.
- Migration progress bar (per-bucket: how many objects migrated).
- Per-object inspector: show shard layout for debugging.

---

### Estimated effort
- Total: ~4 weeks focused engineering
- Critical path: 1 → 2+3 (writer/reader in parallel) → 4 → 5
- 6 (migration) and 7 (UI) ship after the core path is stable

### Consistency model
- Same as the current cluster: AP, quorum-based.
- EC writes require all K+M shards to be acked or the write fails — there is no "EC quorum" partial-write mode (you cannot reconstruct without K shards, period).

### Upgrade path
- Ships with `ec.enabled = false` by default. Existing deployments are unaffected.
- Admin enables EC → migration worker starts converting objects in background. Cluster stays operational throughout.
- Rollback: set `ec.enabled = false` and run reverse migration.

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
