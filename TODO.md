# TODO

Pending work only. Completed work belongs in `CHANGELOG.md`.

## 1.7.0

### Storage layout

- [ ] Flat hash-sharded files: `<bucket>/<aa>/<bb>/<sha256>`, no directories built from key components.
- [ ] Buckets at the storage root, no tenant directories.
- [ ] `Backend` takes bucket, key and versionID instead of a joined path.
- [ ] Sidecar carries bucket, key and versionID; `recover` and `reconcile` read them from there instead of parsing the path.
- [ ] Migration driven by Pebble: non-destructive, resumable, aborts on duplicate bucket names.
- [ ] `.maxiofs-layout` marker; refuse to start on a newer layout than the binary knows.
- [ ] Reject a bucket name already used in another tenant. `GetBucketByName` resolves by name alone, so a duplicate silences one bucket over S3.

### Folder markers

Closed by the storage layout change; keep until it lands.

- [ ] Review and fix S3 folder marker compatibility.
- [ ] Prevent `list-object-versions` from exposing internal folder markers.
- [ ] Define cleanup behavior for orphan folder markers.
- [ ] Add regression tests for versioned buckets with prefixes.
- [ ] Turn the current AWS CLI battery into a repeatable script.

## IAM / STS

- [ ] Test LDAP against a real directory.
- [ ] Test OAuth against a real identity provider.
- [ ] Validate errors when LDAP/OAuth are configured but unavailable.

## S3

- [ ] Review compatibility with Hadoop/Spark S3A.

## Encryption

- [ ] Implement SSE-C.
- [ ] Implement SSE-KMS.
- [ ] Add checkpoint/resume support to `maxiofs recover`.
- [ ] Evaluate recovery-key escrow.
- [ ] Evaluate Shamir split for recovery bundles.
- [ ] Evaluate recovery bundle restore from the console.

## Performance / Metrics

- [ ] Review chunk-level bandwidth throttling.
- [ ] Review quota delta calculation during concurrent overwrites.

## Erasure Coding

After the storage layout change.

- [ ] Define the scope of the first erasure coding release.
- [ ] Add erasure coding configuration.
- [ ] Integrate a Reed-Solomon library.
- [ ] Implement erasure-coded writes.
- [ ] Implement erasure-coded reads with reconstruction.
- [ ] Add per-object EC layout metadata.
- [ ] Adapt anti-entropy and repair for EC.
- [ ] Implement replication -> EC migration.
- [ ] Implement EC -> replication migration.
- [ ] Add basic console controls.
