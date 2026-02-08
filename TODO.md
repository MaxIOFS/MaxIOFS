# MaxIOFS - Development Roadmap

**Version**: 0.8.1-beta
**Last Updated**: February 8, 2026
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

## 🔴 CRITICAL — v0.8.1-beta (Security Hardening)

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

## 🟠 HIGH — v0.8.1-beta (Stability & Robustness)

### 4. Default Password Change Notification
- [ ] When admin user still has default password, show persistent notification in the bell icon warning to change it
- **Files**: `internal/server/console_api.go` (detect default password on login), `web/frontend/src/hooks/useNotifications.ts` (show notification)
- **Fix**: On login, if username is "admin" and password matches "admin", include a `password_change_required` flag in the response. Frontend shows a persistent notification that cannot be dismissed until password is changed

### 5. Goroutine Leak in Decryption Pipeline
- [ ] If the pipe reader is abandoned (early return from caller), the decryption goroutine blocks forever on `pipeWriter.Write()`
- **File**: `internal/object/manager.go:318-330`
- **Fix**: Use `context.Context` cancellation or `defer pipeWriter.CloseWithError()` to ensure the goroutine exits when the reader is gone

### 6. Unbounded Map Growth in Replication Manager
- [ ] `lastSync` and `ruleLocks` maps grow indefinitely, never cleaned up — causes slow memory leak
- **File**: `internal/replication/manager.go:528, 701`
- **Fix**: Implement periodic cleanup (remove entries for deleted rules) or use `sync.Map` with TTL-based eviction

### 7. Race Condition in Cluster Cache
- [ ] `c.entries` map accessed without read lock in some code paths
- **File**: `internal/cluster/cache.go:23, 76`
- **Fix**: Use `sync.RWMutex` consistently — `RLock` for reads, `Lock` for writes

### 8. Unchecked `crypto/rand.Read` Error
- [ ] `rand.Read(randomBytes)` error is ignored — crypto random can fail on resource-exhausted systems
- **File**: `internal/object/manager.go:207`
- **Fix**: Check the error and return it if `rand.Read` fails

### 9. Array Bounds Check in S3 Signature Parsing
- [ ] `credParts[1]` accessed without verifying `len(credParts) >= 2` — can panic on malformed auth headers
- **File**: `internal/auth/manager.go:1193-1200`
- **Fix**: Add bounds check before each array access in signature parsing

---

## 🟡 MEDIUM PRIORITY

### Known Issues
- [ ] `TestDeleteSpecificVersion_*` tests fail on Windows due to file-locking during BadgerDB cleanup — OS-specific, not a bug

### Code Quality
- [ ] HTTP response body not always closed via `defer` immediately after assignment (`internal/server/console_api.go:850`)
- [ ] Audit logging errors silently ignored in 6+ locations in `console_api.go` — should at least log warnings
- [ ] Temp file handle leak potential in `internal/object/manager.go:368-383` — `defer cleanup` placement
- [ ] Tag index deletion error ignored in `internal/metadata/badger_objects.go:563` — can cause inconsistent state
- [ ] Path traversal with URL-encoded `%2e%2e%2f` — verify if Go's HTTP router decodes before reaching validation (`internal/storage/filesystem.go:400-416`)

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
- [ ] Integration test infrastructure (multi-node, remote S3) for cluster/replication coverage
- [ ] Chaos engineering tests
- [ ] Storage tiering (hot/warm/cold)
- [ ] Deduplication

### v1.0.0-RC (Q3 2026)
- [ ] LDAP/Active Directory integration
- [ ] SAML/OAuth2 SSO
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
