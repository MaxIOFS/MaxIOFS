# TODO

Pending work only. Completed work belongs in `CHANGELOG.md`.

## Later

- [ ] Decide when to drop `internal/layout/`. It reads the previous on-disk layout, so while it ships an installation on 1.6.0 or older can upgrade straight to any release. Removing it makes 1.7.x a required stop.

## IAM / STS

- [ ] Test LDAP against a real directory.
- [ ] Test OAuth against a real identity provider.
- [ ] Validate errors when LDAP/OAuth are configured but unavailable.

## Encryption

- [ ] Implement SSE-C.
- [ ] Implement SSE-KMS.
- [ ] Add checkpoint/resume support to `maxiofs recover`.
- [ ] Evaluate recovery-key escrow.
- [ ] Evaluate Shamir split for recovery bundles.
- [ ] Evaluate recovery bundle restore from the console.
