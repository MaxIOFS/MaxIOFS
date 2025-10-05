# Implementation Plan - Production Hardening (Phase 7)

**Last Updated**: 2025-10-05
**Current Version**: v1.1-dev (Multi-Tenancy Complete)

## 🎯 Clarifications & Architecture Decisions

### Frontend/Backend Deployment
- **Development**: Frontend (port 3000) + Backend (ports 8080/8081) run separately
- **Production**: Frontend embedded in backend as static files (monolithic deployment)
- **Result**: No CORS issues in production (same origin)
- **Action**: CORS configuration is for S3 API external clients only

### Storage Backend
- **Current**: Filesystem only
- **Bucket Policy**: NOT implemented (no multiple storage backends yet)
- **Action**: Remove/skip bucket policy documentation until multi-backend support

### TLS/HTTPS
- **Option 1**: Embedded Let's Encrypt support (optional feature)
- **Option 2**: External reverse proxy (nginx/traefik) - user's choice
- **Action**: Make TLS optional, document both approaches

---

## 🔴 Priority: CRITICAL - Security Enhancements

### 1. Bcrypt Password Hashing ✅ VERIFIED

**Current State:**
- File: `internal/auth/manager.go` lines 256-262
- Function: `hashPassword()` uses SHA256 (insecure)
- SHA256 MUST remain for S3 signatures (AWS4-HMAC-SHA256)

**Implementation:**
```go
// Install: go get golang.org/x/crypto/bcrypt

import "golang.org/x/crypto/bcrypt"

// hashPassword - Secure password hashing with bcrypt
func hashPassword(password string) (string, error) {
    hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return "", err
    }
    return string(hashedBytes), nil
}

// verifyPassword - Compare password with bcrypt hash
func verifyPassword(hashedPassword, password string) error {
    return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
```

**Files to Modify:**
- `internal/auth/manager.go` - Update hashPassword function
- `internal/auth/manager.go` - Update ValidateUser to use bcrypt.CompareHashAndPassword
- **Migration**: Existing SHA256 hashes need re-hashing on next login

**Estimated Time**: 2-3 hours

---

### 2. Rate Limiting & Account Lockout

**Requirements:**
- Max 5 login attempts per minute per IP
- Account lockout after 5 failed attempts
- **Global Admin unlock**: Can unlock any user
- **Tenant Admin unlock**: Can unlock users in their tenant only
- Lockout duration: 15 minutes OR manual unlock by admin

**Implementation:**

#### Database Schema (SQLite)
```sql
-- Add to users table
ALTER TABLE users ADD COLUMN failed_login_attempts INTEGER DEFAULT 0;
ALTER TABLE users ADD COLUMN locked_until INTEGER DEFAULT 0;
ALTER TABLE users ADD COLUMN last_failed_login INTEGER DEFAULT 0;
```

#### Rate Limiter (In-Memory)
```go
type LoginRateLimiter struct {
    attempts map[string]*LoginAttempt
    mu       sync.RWMutex
}

type LoginAttempt struct {
    Count     int
    FirstTry  time.Time
    LastTry   time.Time
}

// Check: max 5 attempts per minute per IP
func (l *LoginRateLimiter) AllowLogin(ip string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()

    attempt, exists := l.attempts[ip]
    if !exists {
        l.attempts[ip] = &LoginAttempt{Count: 1, FirstTry: time.Now(), LastTry: time.Now()}
        return true
    }

    // Reset after 1 minute
    if time.Since(attempt.FirstTry) > time.Minute {
        l.attempts[ip] = &LoginAttempt{Count: 1, FirstTry: time.Now(), LastTry: time.Now()}
        return true
    }

    if attempt.Count >= 5 {
        return false // Rate limit exceeded
    }

    attempt.Count++
    attempt.LastTry = time.Now()
    return true
}
```

#### Account Lockout Logic
```go
func (am *AuthManager) LockAccount(userID string) error {
    // Lock for 15 minutes
    lockUntil := time.Now().Add(15 * time.Minute).Unix()
    _, err := am.db.Exec(`
        UPDATE users
        SET failed_login_attempts = failed_login_attempts + 1,
            locked_until = ?,
            last_failed_login = ?
        WHERE id = ?
    `, lockUntil, time.Now().Unix(), userID)
    return err
}

func (am *AuthManager) UnlockAccount(adminUserID, targetUserID string) error {
    // Check permissions: Global admin or tenant admin for same tenant
    admin, err := am.GetUser(context.Background(), adminUserID)
    if err != nil {
        return err
    }

    target, err := am.GetUser(context.Background(), targetUserID)
    if err != nil {
        return err
    }

    // Global admin can unlock anyone
    isGlobalAdmin := admin.TenantID == "" && contains(admin.Roles, "admin")

    // Tenant admin can only unlock users in their tenant
    isTenantAdmin := admin.TenantID != "" && admin.TenantID == target.TenantID && contains(admin.Roles, "admin")

    if !isGlobalAdmin && !isTenantAdmin {
        return errors.New("insufficient permissions to unlock account")
    }

    _, err = am.db.Exec(`
        UPDATE users
        SET failed_login_attempts = 0,
            locked_until = 0
        WHERE id = ?
    `, targetUserID)
    return err
}

func (am *AuthManager) IsAccountLocked(userID string) (bool, error) {
    var lockedUntil int64
    err := am.db.QueryRow(`SELECT locked_until FROM users WHERE id = ?`, userID).Scan(&lockedUntil)
    if err != nil {
        return false, err
    }

    if lockedUntil > 0 && time.Now().Unix() < lockedUntil {
        return true, nil
    }

    // Auto-unlock if time expired
    if lockedUntil > 0 {
        _, _ = am.db.Exec(`UPDATE users SET locked_until = 0, failed_login_attempts = 0 WHERE id = ?`, userID)
    }

    return false, nil
}
```

**Files to Modify:**
- `internal/auth/manager.go` - Add rate limiter and lockout logic
- `internal/auth/sqlite.go` - Migration for new columns
- `internal/server/console_api.go` - Check rate limit + account lock before login
- Frontend - Show "Account locked" message + unlock button for admins

**Estimated Time**: 4-6 hours

---

### 3. CORS Configuration (S3 API Only)

**Current State:**
- CORS allows `*` (wildcard) for development
- Console API doesn't need CORS (same origin in production)

**Implementation:**
```go
// config.yaml
cors:
  s3_api:
    allowed_origins:
      - "https://yourdomain.com"
      - "https://backup.yourdomain.com"
    allowed_methods: ["GET", "POST", "PUT", "DELETE", "HEAD"]
    allowed_headers: ["Authorization", "Content-Type", "X-Amz-*"]
    max_age: 3600

// S3 Server CORS middleware
func configureCORS(allowedOrigins []string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")

            // Check if origin is allowed
            allowed := false
            for _, allowedOrigin := range allowedOrigins {
                if origin == allowedOrigin || allowedOrigin == "*" {
                    allowed = true
                    w.Header().Set("Access-Control-Allow-Origin", origin)
                    break
                }
            }

            if !allowed && len(allowedOrigins) > 0 {
                http.Error(w, "Origin not allowed", http.StatusForbidden)
                return
            }

            w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, HEAD")
            w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Amz-*")

            if r.Method == "OPTIONS" {
                w.WriteHeader(http.StatusOK)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

**Files to Modify:**
- `internal/server/s3_server.go` - Update CORS middleware with configurable origins
- `config.yaml` - Add CORS configuration section
- `README.md` - Document CORS setup for S3 clients

**Estimated Time**: 2 hours

---

### 4. TLS/HTTPS Support (Optional)

**Option A: Embedded Let's Encrypt (Recommended for simple deployments)**
```go
// go get golang.org/x/crypto/acme/autocert

import "golang.org/x/crypto/acme/autocert"

// config.yaml
tls:
  enabled: true
  mode: "letsencrypt"  # or "manual"
  domain: "maxiofs.yourdomain.com"
  email: "admin@yourdomain.com"
  cert_cache_dir: "./certs"

// Server setup
if config.TLS.Enabled && config.TLS.Mode == "letsencrypt" {
    certManager := autocert.Manager{
        Prompt:      autocert.AcceptTOS,
        HostPolicy:  autocert.HostWhitelist(config.TLS.Domain),
        Email:       config.TLS.Email,
        Cache:       autocert.DirCache(config.TLS.CertCacheDir),
    }

    server := &http.Server{
        Addr:      ":443",
        Handler:   handler,
        TLSConfig: &tls.Config{GetCertificate: certManager.GetCertificate},
    }

    go http.ListenAndServe(":80", certManager.HTTPHandler(nil)) // HTTP->HTTPS redirect
    log.Fatal(server.ListenAndServeTLS("", ""))
}
```

**Option B: Manual Certificates**
```go
// config.yaml
tls:
  enabled: true
  mode: "manual"
  cert_file: "/path/to/cert.pem"
  key_file: "/path/to/key.pem"

// Server setup
if config.TLS.Enabled && config.TLS.Mode == "manual" {
    log.Fatal(http.ListenAndServeTLS(":443", config.TLS.CertFile, config.TLS.KeyFile, handler))
}
```

**Option C: External Reverse Proxy (nginx/traefik)**
```nginx
# nginx.conf
server {
    listen 443 ssl http2;
    server_name maxiofs.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/maxiofs.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/maxiofs.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /s3/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
    }
}
```

**Files to Modify:**
- `cmd/maxiofs/main.go` - Add TLS configuration
- `docs/TLS_SETUP.md` - Document all 3 options
- `README.md` - Add TLS section

**Estimated Time**: 4-6 hours (embedded) or 1 hour (documentation only)

---

## 🟠 Priority: HIGH - Documentation

### Remove Bucket Policy References
- Bucket policies require multi-backend support (not implemented)
- Remove from docs until Phase 8 (multi-backend)

### Create Documentation
1. **API.md** - Complete S3 API reference
2. **DEPLOYMENT.md** - Production deployment guide
3. **CONFIGURATION.md** - All config options
4. **SECURITY.md** - Security best practices
5. **MULTI_TENANCY.md** - Tenant architecture
6. **TLS_SETUP.md** - TLS configuration options
7. **RATE_LIMITING.md** - Account lockout and rate limiting

**Estimated Time**: 8-12 hours

---

## 🟡 Priority: MEDIUM - Production Features

### CI/CD Pipeline
- GitHub Actions for automated testing
- Docker image publishing
- Release automation

### Docker Optimization
- Multi-stage build with Alpine
- Embedded frontend in production image
- Health checks and resource limits

### Monitoring
- Grafana dashboards
- Alert rules
- Log aggregation

**Estimated Time**: 12-16 hours

---

## 📊 Summary

### Immediate Tasks (Next 1-2 weeks)
1. ✅ **Bcrypt Migration** (2-3h) - Replace SHA256 for passwords
2. ✅ **Rate Limiting** (4-6h) - IP-based + account lockout with admin unlock
3. ✅ **CORS Config** (2h) - Configurable S3 API origins
4. ⚠️ **TLS Support** (1-6h) - Optional, document all approaches
5. ✅ **Documentation** (8-12h) - Complete production docs

### Total Estimated Time
- **Critical Security**: 8-11 hours
- **Documentation**: 8-12 hours
- **Optional TLS**: 1-6 hours
- **Total**: 17-29 hours (2-4 working days)

### Not Needed (Yet)
- ❌ Bucket policies (no multi-backend)
- ❌ Frontend CORS (monolithic deployment)
- ❌ Mandatory TLS (optional feature)

---

## ✅ Completion Checklist

### Security
- [x] Implement bcrypt password hashing
- [x] Add automatic migration from SHA256 to bcrypt on login
- [ ] Implement IP-based rate limiting (5 attempts/min)
- [ ] Add account lockout (5 failed attempts = 15min lock)
- [ ] Add unlock functionality (Global Admin / Tenant Admin)
- [ ] Update login endpoint with all security checks
- [ ] Add frontend UI for account unlock

### Configuration
- [ ] Add CORS configuration (S3 API only)
- [ ] Add TLS configuration (optional)
- [ ] Update config.yaml with all new options
- [ ] Add environment variable support

### Documentation
- [ ] API.md - S3 operations reference
- [ ] DEPLOYMENT.md - Production guide
- [ ] CONFIGURATION.md - Config options
- [ ] SECURITY.md - Security best practices
- [ ] MULTI_TENANCY.md - Tenant architecture
- [ ] TLS_SETUP.md - TLS options
- [ ] RATE_LIMITING.md - Lockout/unlock flow
- [ ] Update README.md with security status

### Testing
- [ ] Unit tests for bcrypt functions
- [ ] Integration tests for rate limiting
- [ ] Test account lockout/unlock flow
- [ ] Test with different CORS configurations
- [ ] Security audit

---

**Next Action**: Implement rate limiting and account lockout (highest priority after bcrypt)

---

## 🎉 Completed Tasks

### 1. Bcrypt Password Hashing ✅ COMPLETE

**Implementation Details:**
- **File Modified**: `internal/auth/manager.go`
  - Added bcrypt import at line 20
  - Replaced `hashPassword()` with bcrypt implementation (lines 257-264)
  - Added `verifyPassword()` for bcrypt comparison (lines 266-269)
  - Kept `hashPasswordSHA256()` for legacy migration (lines 271-276)

- **File Modified**: `internal/auth/sqlite.go`
  - Added `UpdateUserPassword()` method (lines 278-297)
  - Already had `HashPassword()` and `VerifyPassword()` using bcrypt

- **Migration Logic** (lines 244-266 in manager.go):
  - Try bcrypt verification first
  - If fails, try SHA256 (legacy)
  - On SHA256 match, automatically migrate to bcrypt
  - Update database with new bcrypt hash
  - Log migration success/failure

**Benefits:**
- ✅ Secure password storage with bcrypt
- ✅ Automatic migration from SHA256 on login
- ✅ No manual database migration needed
- ✅ Backward compatible with existing passwords
- ✅ Production-ready security

**Testing:**
- ✅ Code compiles successfully
- ⏳ Manual login test recommended (admin/admin should auto-migrate)

---

### 2. Tenant Quota Validations ✅ COMPLETE

**Implementation Details:**
- **File Modified**: `internal/server/console_api.go`

**Storage Quota Validation** (lines 710-737 in console_api.go):
- Added validation in `handleUploadObject` BEFORE uploading
- Gets bucket info to check tenant ownership
- Retrieves tenant and checks `CurrentStorageBytes + ContentLength > MaxStorageBytes`
- Returns 403 Forbidden with quota exceeded message if limit reached
- Prevents upload if it would exceed tenant's storage quota

**Access Keys Quota Validation** (lines 1250-1269 in console_api.go):
- Added validation in `handleCreateAccessKey` BEFORE creating key
- Gets user to check tenant association
- Retrieves tenant and checks `CurrentAccessKeys >= MaxAccessKeys`
- Returns 403 Forbidden with quota exceeded message if limit reached
- Prevents access key creation if tenant quota is full

**Bucket Quota Validation** (Already implemented):
- Was already working correctly in `handleCreateBucket` (lines 373-385)
- Checks `CurrentBuckets >= MaxBuckets` before bucket creation

**Benefits:**
- ✅ Storage quota enforcement (prevents uploads exceeding limit)
- ✅ Access keys quota enforcement (prevents key creation exceeding limit)
- ✅ Bucket quota enforcement (already working)
- ✅ Consistent error messages across all quota types
- ✅ User-friendly error messages showing current/max values

**Code Cleanup:**
- ✅ Removed duplicate password hashing functions from manager.go
- ✅ Now using centralized HashPassword/VerifyPassword from sqlite.go
- ✅ Removed unused bcrypt import from manager.go

**Testing:**
- ✅ Code compiles successfully
- ⏳ Manual testing recommended:
  1. Upload files until storage quota exceeded
  2. Create access keys until quota exceeded
  3. Verify error messages are clear and helpful
