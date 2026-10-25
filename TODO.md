# TODO

Pending work only. Completed work belongs in `CHANGELOG.md`.

## Later

- [ ] Decide when to drop `internal/layout/`. It reads the previous on-disk layout, so while it ships an installation on 1.6.0 or older can upgrade straight to any release. Removing it makes 1.7.x a required stop.

## Encryption

- [ ] Implement SSE-C.
- [ ] Implement SSE-KMS.
- [ ] Evaluate recovery-key escrow.
- [ ] Evaluate Shamir split for recovery bundles.
- [ ] Evaluate recovery bundle restore from the console.
