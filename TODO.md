# MaxIOFS - Development Roadmap

**Version**: 1.0.0-rc1
**Last Updated**: March 4, 2026
**Status**: Beta

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | ~99% | Full compatibility audit completed — 20 issues identified and resolved (March 2026) |
| Backend Coverage | ~75% | At practical ceiling — see details below |
| Frontend Coverage | 100% | Complete |
| Production Ready | Testing | Target: Q4 2026 |

### Backend Test Coverage Reality (February 7, 2026)

| Module | Coverage | Notes |
|--------|----------|-------|
| internal/metadata | 87.4% | Remaining ~13% are Pebble internal error branches (WAL failures, I/O errors) — not simulable in unit tests |
| internal/object | 77.3% | Remaining gaps: `NewManager` init (47.8%), `GetObject` encryption/range branches (53.7%), `cleanupEmptyDirectories` (34.6%), `deleteSpecificVersion` blocked by Windows file-locking bug |
| cmd/maxiofs | 71.4% | `main()` is 0% (entrypoint, expected), `runServer` at 87.5% |
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30–55%) require real remote nodes. Migration/replication handlers need live infrastructure |
| internal/replication | 19.0% | CRUD rule management tested. `s3client.go`, `worker.go`, `adapter.go` are all 0% — they operate against real remote S3 endpoints and cannot be unit-tested without full network infrastructure |

**Conclusion**: All testable business logic has been covered. The remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, filesystem-level operations, and low-level database error branches. Reaching 90%+ would require integration test infrastructure (multi-node cluster, remote S3 endpoints) which is outside the scope of unit testing.

---

## 🟡 LOW PRIORITY (Optional / Nice-to-Have)

### 1. Operational Runbook

- [ ] `docs/OPERATIONS.md` — runbook for production operators:
  - What to do when a cluster node goes down
  - How to safely remove a node from the cluster
  - How to recover from a Pebble crash (WAL recovery is automatic, but document the indicators)
  - How to interpret audit logs for security incidents
  - Recommended monitoring alerts for Prometheus/Grafana
  - Disk space management (what to do when approaching capacity)

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
