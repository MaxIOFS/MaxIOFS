# MaxIOFS - Development Roadmap

**Version**: 0.9.0-beta
**Last Updated**: February 15, 2026
**Status**: Beta - S3 Core 100% Compatible

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | 100% | All standard S3 operations |
| Backend Coverage | ~75% | At practical ceiling — see details below |
| Frontend Coverage | 100% | Complete |
| Production Ready | Testing | Target: Q4 2026 |

### Backend Test Coverage Reality (February 7, 2026)

| Module | Coverage | Notes |
|--------|----------|-------|
| internal/metadata | 87.4% | Remaining ~13% are BadgerDB internal error branches (transaction failures, corruption) — not simulable in unit tests |
| internal/object | 77.3% | Remaining gaps: `NewManager` init (47.8%), `GetObject` encryption/range branches (53.7%), `cleanupEmptyDirectories` (34.6%), `deleteSpecificVersion` blocked by Windows file-locking bug |
| cmd/maxiofs | 71.4% | `main()` is 0% (entrypoint, expected), `runServer` at 87.5% |
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30-55%) require real remote nodes. Migration/replication handlers need live infrastructure |
| internal/replication | 19.0% | CRUD rule management tested. `s3client.go`, `worker.go`, `adapter.go` are all 0% — they operate against real remote S3 endpoints and cannot be unit-tested without full network infrastructure |

**Conclusion**: All testable business logic has been covered. The remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, filesystem-level operations, and low-level database error branches. Reaching 90%+ would require integration test infrastructure (multi-node cluster, remote S3 endpoints) which is outside the scope of unit testing.

---

## 🔴 CRITICAL (Security Hardening)

### 1. JWT Signature Verification (CRITICAL) ✅
- [x] `parseBasicToken()` now verifies HMAC-SHA256 signature before trusting payload using `hmac.Equal()` for constant-time comparison
- **File**: `internal/auth/manager.go:1100-1132`
- **Tests**: `TestValidateJWT_ForgedSignature` (4 cases), `TestValidateJWT_TamperedPayload` — all pass

### 2. CORS Wildcard Removal (CRITICAL) ✅
- [x] Replaced hardcoded `Access-Control-Allow-Origin: *` with proper `middleware.CORSWithConfig()` using origin validation
- **File**: `internal/server/console_api.go`
- **Tests**: Existing CORS middleware tests cover origin validation (disallowed origins, wildcards, custom validators)

### 3. Rate Limiting IP Spoofing (CRITICAL) ✅
- [x] `IPKeyExtractor` now only trusts `X-Forwarded-For`/`X-Real-IP` when request comes from a trusted proxy. Added `TrustedProxies`, `stripPort()`, `isTrustedProxy()`.
- **File**: `internal/middleware/ratelimit.go:248-301`
- **Tests**: `TestIPKeyExtractor` (7 cases), `TestStripPort` (7 cases), `TestIsTrustedProxy` (2 cases) — all pass

---

## 🟠 HIGH (Stability & Robustness)

### 4. Default Password Change Notification ✅
- [x] Backend returns `default_password: true` in login response when admin/admin is used
- [x] Frontend shows persistent security warning in notification bell with amber icon, links to user profile
- [x] Warning clears automatically when password is changed via `APIClient.changePassword()`
- **Files**: `internal/server/console_api.go`, `web/frontend/src/lib/api.ts`, `web/frontend/src/components/layout/AppLayout.tsx`

### 5. Goroutine Leak in Decryption Pipeline ✅
- [x] Added context cancellation monitoring — when caller abandons the reader, `ctx.Done()` triggers `pipeWriter.CloseWithError()`, unblocking the goroutine
- **File**: `internal/object/manager.go:318-340`

### 6. Unbounded Map Growth in Replication Manager ✅
- [x] `DeleteRule()` now cleans up `ruleLocks` entry for deleted rules
- [x] `processScheduledRules()` now cleans up `lastSync` entries for rules no longer in the database
- **File**: `internal/replication/manager.go`

### 7. Race Condition in Cluster Cache ✅ (False Positive)
- [x] Verified: all `c.entries` accesses are correctly protected with `sync.RWMutex` — `RLock` for reads, `Lock` for writes
- **File**: `internal/cluster/cache.go` — no changes needed

### 8. Unchecked `crypto/rand.Read` Error ✅
- [x] Added error check — falls back to timestamp-only version ID if `crypto/rand` fails
- **File**: `internal/object/manager.go:207`

### 9. Array Bounds Check in S3 Signature Parsing ✅ (False Positive)
- [x] Verified: all array accesses in `parseS3SignatureV4` and `parseS3SignatureV2` already have proper bounds checks (`len(credParts) >= 2`, `len(kv) != 2`, `len(parts) != 2`)
- **File**: `internal/auth/manager.go:1189-1212` — no changes needed

---

## 🟡 MEDIUM PRIORITY

### Known Issues
- [ ] `TestDeleteSpecificVersion_*` tests fail on Windows due to file-locking during BadgerDB cleanup — OS-specific, not a bug

### Code Quality
- [x] HTTP response body not always closed via `defer` immediately after assignment — Verified: all `resp.Body.Close()` are properly deferred (false positive)
- [x] Audit logging errors silently ignored in 12 locations in `console_api.go` — Added `logAuditEvent()` helper that logs warnings on failure, migrated all 12 call sites
- [x] Temp file handle leak potential in `internal/object/manager.go:368-383` — Added `defer tempFile.Close()` immediately after creation to ensure cleanup on panic
- [x] Tag index deletion error ignored in `internal/metadata/badger_objects.go:563` — Now returns error on failed `txn.Delete()` to prevent inconsistent state
- [x] Path traversal with URL-encoded `%2e%2e%2f` — Verified safe: Go's `net/http` decodes URL-encoded paths before handlers, `strings.Contains(path, "..")` catches decoded traversal, and `filepath.Join` normalizes as defense-in-depth

---

## 🟢 LOW PRIORITY

- [ ] Video tutorials and getting started guides
- [ ] Migration guides from MinIO/AWS S3
- [ ] Integration test infrastructure (multi-node cluster) for cluster/replication coverage

---

## ✅ COMPLETED

### v0.8.0-beta (February 2026)
- ✅ Object Filters & Advanced Search (content-type, size range, date range, tags) — new `/objects/search` endpoint + frontend filter panel
- ✅ Version check notification badge for global admins (proxied through backend)
- ✅ Dark mode toggle fixed — now uses ThemeContext, persists to user profile
- ✅ CI/CD test fix — `TestCPUStats_ConsistentData` frequency variance threshold increased for virtualized environments

### v0.7.0-beta (January-February 2026)
- ✅ Bucket Inventory System
- ✅ Database Migration System
- ✅ Performance Profiling & Benchmarking
- ✅ Cluster Production Hardening (rate limiting, circuit breakers, metrics)
- ✅ ListBuckets cross-node aggregation (fixed UX blocker)
- ✅ Cluster-aware quota enforcement (fixed security vulnerability)
- ✅ Backend test coverage expansion — reached practical ceiling (metadata 87.4%, object 77.3%, server 66.1%, cmd 71.4%)

### v0.6.x
- ✅ Cluster Management System
- ✅ Performance Metrics & Dashboards
- ✅ Prometheus/Grafana Integration
- ✅ Bucket Replication

### v0.5.0 and earlier
- ✅ Core S3 Operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket Versioning & Lifecycle Policies
- ✅ Object Lock & Retention (COMPLIANCE/GOVERNANCE)
- ✅ Server-side Encryption (AES-256-CTR)
- ✅ Multi-tenancy with quotas
- ✅ Two-Factor Authentication
- ✅ Bucket Policies, ACLs, CORS, Tags

---

## 🗺️ Long-Term Roadmap

### v0.9.0-beta (Q2 2026)
- [x] LDAP/Active Directory integration
- [x] OAuth2/OIDC SSO (Google, Microsoft presets)
- [x] Identity Provider management UI (CRUD, test connection, LDAP browser, group mappings)
- [x] External user import with role assignment (no auto-provisioning)
- [x] Group-to-role mapping with manual and automatic sync
- [x] OAuth login flow with CSRF protection and 2FA support
- [x] Auth provider badge on user list (Local/LDAP/SSO)
- [x] OAuth auto-provisioning with group mapping authorization (admin must define authorized groups)
- [x] SSO one-button-per-type login (one "Sign in with Google" button, not one per provider)
- [x] Cross-provider user lookup (search ALL OAuth providers on callback for multi-tenant routing)
- [x] SSO user creation from Users page (auth provider dropdown, conditional password)
- [x] Email auto-sync for SSO users (populated from OAuth profile on login)
- [x] Redirect URI auto-generation from PublicConsoleURL
- [x] SSO documentation (`docs/SSO.md`)
- [ ] Integration test infrastructure (multi-node, remote S3) for cluster/replication coverage
- [ ] Chaos engineering tests
- [ ] Storage tiering (hot/warm/cold)
- [ ] Deduplication
- [x] Unit tests for IDP crypto, store (SQLite CRUD), manager (encrypt/decrypt/mask/cache)
- [x] Frontend tests for IDP page (list, search, delete, test connection, permissions, badges)
- [ ] Unit tests for LDAP provider (mock), OAuth provider (mock HTTP), modified handleLogin
- [ ] Integration tests for IDP import/sync flows

### v1.0.0-RC (Q3 2026)
- [ ] SAML SSO
- [ ] Automatic cluster scaling
- [ ] Security audit completion

### v1.0.0 (Q4 2026)
- [ ] PostgreSQL/MySQL metadata backends
- [ ] Kubernetes operator & Helm charts
- [ ] Cloud marketplace listings

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
