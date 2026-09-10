# TODO

Pending work only. Completed work belongs in `CHANGELOG.md`.

## Later

- [ ] Decide when to drop `internal/layout/`. It reads the previous on-disk layout, so while it ships an installation on 1.6.0 or older can upgrade straight to any release. Removing it makes 1.7.x a required stop.

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
