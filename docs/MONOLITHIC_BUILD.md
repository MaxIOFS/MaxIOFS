# MaxIOFS Monolithic Build Guide

## Overview

MaxIOFS can be built as a single monolithic binary with the Next.js frontend embedded. This eliminates the need to run a separate frontend development server in production.

## How It Works

1. **Frontend Build**: Next.js compiles to static HTML/CSS/JS files in `web/frontend/out/`
2. **Go Embed**: The `embed` package includes these files in the Go binary
3. **Static Server**: The backend serves the embedded files on port 8081
4. **SPA Routing**: Custom handler manages client-side routing (fallback to index.html)

## Build Process

### Windows

```bash
# Automated build (recommended)
build.bat

# Manual steps
cd web\frontend
npm install
npm run build
cd ..\..
go build -o maxiofs.exe ./cmd/maxiofs
```

### Linux/macOS

```bash
# Automated build (recommended)
make build

# Manual steps
cd web/frontend
npm install
npm run build
cd ../..
go build -o maxiofs ./cmd/maxiofs
```

## Architecture

```
┌─────────────────────────────────────┐
│         maxiofs.exe                 │
│  ┌──────────────────────────────┐   │
│  │  Go Binary                   │   │
│  │  ┌────────────────────────┐  │   │
│  │  │  Embedded Frontend     │  │   │
│  │  │  (Next.js static)      │  │   │
│  │  │  - index.html          │  │   │
│  │  │  - *.js, *.css         │  │   │
│  │  │  - images, fonts       │  │   │
│  │  └────────────────────────┘  │   │
│  │                              │   │
│  │  Console API (port 8081)     │   │
│  │  S3 API (port 8080)          │   │
│  └──────────────────────────────┘   │
└─────────────────────────────────────┘
```

## File Locations

### Development

```
web/frontend/
├── src/              # Source code
├── .next/            # Next.js build cache (gitignored)
├── out/              # Static export (gitignored)
└── node_modules/     # Dependencies (gitignored)
```

### Production Binary

Frontend files are embedded from `web/frontend/out/` into the Go binary at compile time.

## Embedding Mechanism

### embed.go

```go
//go:embed all:../../web/frontend/out
var embeddedAssets embed.FS

func GetWebAssets() http.FileSystem {
    webFS, err := fs.Sub(embeddedAssets, "web/frontend/out")
    if err != nil {
        return http.Dir("./web/frontend/out") // Fallback
    }
    return http.FS(webFS)
}
```

### SPA Handler

```go
type SPAHandler struct {
    staticFS http.FileSystem
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Try to open requested file
    // If not found, serve index.html (for client-side routing)
    // Handles: /, /buckets, /users, etc.
}
```

## Build Scripts

### build.bat (Windows)

```batch
[1/2] Building frontend...
  - Clean out/ and .next/
  - npm install
  - npm run build (creates out/ directory)

[2/2] Building backend...
  - go build with embedded frontend
  - Result: maxiofs.exe (single binary)
```

### Makefile (Linux/macOS)

```makefile
build: build-web build-server

build-web:
  - Clean previous build
  - Install dependencies
  - Build static export

build-server:
  - Go build with ldflags
  - Embed frontend from out/
```

## Next.js Configuration

### next.config.js

```javascript
const nextConfig = {
  output: 'export',           // Static export mode
  images: {
    unoptimized: true        // No image optimization
  },
  trailingSlash: false,       // Clean URLs
  basePath: '',               // Root serving
}
```

### Key Settings

- **output: 'export'** - Generates static HTML files
- **images.unoptimized** - Required for static export
- **trailingSlash: false** - Cleaner URLs without trailing /
- **basePath: ''** - Serve from root, not subdirectory

## Running the Binary

### With Frontend Embedded

```bash
# Run the monolithic binary
./maxiofs.exe --data-dir ./data

# Web Console automatically available at:
http://localhost:8081

# S3 API at:
http://localhost:8080
```

### Without Frontend (Development)

If frontend is not built, the server shows a helpful placeholder:

```html
⚠️ MaxIOFS Console - Assets Not Built

The web console frontend has not been compiled.

To build:
1. cd web/frontend && npm install && npm run build
2. go build -o maxiofs ./cmd/maxiofs
3. Restart server

For development:
- Run: cd web/frontend && npm run dev (port 3000)
```

## Development Workflow

### Option 1: Monolithic (Production-like)

```bash
# Build once
build.bat  # or make build

# Run
./maxiofs.exe --data-dir ./data

# Frontend changes require rebuild
```

### Option 2: Separate Servers (Fast Development)

```bash
# Terminal 1: Backend
go run ./cmd/maxiofs --data-dir ./data

# Terminal 2: Frontend (with hot reload)
cd web/frontend
npm run dev

# Frontend: http://localhost:3000
# Backend: http://localhost:8080, http://localhost:8081
```

**Recommended**: Use Option 2 during development, Option 1 for testing production build.

## Deployment

### Single Binary Deployment

```bash
# Copy binary to server
scp maxiofs user@server:/opt/maxiofs/

# Run
./maxiofs --data-dir /var/lib/maxiofs
```

**Advantages:**
- ✅ Single file to deploy
- ✅ No CORS issues (same origin)
- ✅ Simplified configuration
- ✅ Faster startup

### Docker Deployment

```dockerfile
FROM alpine:latest
COPY maxiofs /app/maxiofs
RUN chmod +x /app/maxiofs

# Frontend is embedded - no need to COPY it separately
CMD ["/app/maxiofs", "--data-dir", "/data"]
```

## Troubleshooting

### Frontend Not Embedded

**Symptoms:**
- Placeholder page instead of web console
- "Assets not found" log message

**Solution:**
```bash
cd web/frontend
npm run build
cd ../..
go build -o maxiofs.exe ./cmd/maxiofs
```

### 404 on Client-Side Routes

**Symptoms:**
- `/buckets` works on initial load
- Refresh gives 404

**Cause:** SPA handler not configured

**Solution:** Ensure `SPAHandler` is being used (should be automatic)

### Build Fails: "pattern all:../../web/frontend/out: no matching files"

**Cause:** Frontend not built yet

**Solution:**
```bash
cd web/frontend
npm run build
cd ../..
# Now Go build will work
```

### Assets Not Loading

**Check:**
1. Frontend built: `ls web/frontend/out/`
2. Go embed directive: `//go:embed all:../../web/frontend/out`
3. Rebuild binary after frontend changes

## CI/CD Integration

### GitHub Actions

```yaml
- name: Build Frontend
  run: |
    cd web/frontend
    npm ci
    npm run build

- name: Build Backend
  run: |
    go build -ldflags "-X main.version=$VERSION" -o maxiofs ./cmd/maxiofs
```

### GitLab CI

```yaml
build:
  script:
    - cd web/frontend && npm ci && npm run build
    - go build -o maxiofs ./cmd/maxiofs
  artifacts:
    paths:
      - maxiofs
```

## Binary Size

Typical binary sizes:

- **Backend only**: ~15 MB
- **With embedded frontend**: ~20-25 MB
  - HTML/CSS/JS: ~5 MB
  - Dependencies: ~3 MB
  - Compression: Gzip during build

## Performance

**Serving Embedded Assets:**
- Cached in memory after first access
- No disk I/O after initial load
- Faster than separate file serving
- Gzip compression automatic

**Benchmarks:**
- Embedded: ~0.1ms response time
- File system: ~0.5ms response time
- Network overhead: Same (HTTP)

## Security Considerations

1. **No Separate Frontend Server**: Eliminates CORS attack surface
2. **Single Authentication**: JWT tokens valid for both APIs
3. **Same Origin Policy**: Browser security simplified
4. **Embedded Assets**: Cannot be tampered with (part of binary)

## Comparison: Monolithic vs Separate

| Aspect | Monolithic | Separate |
|--------|-----------|----------|
| Deployment | 1 binary | Backend + Frontend files |
| Ports | 2 (8080, 8081) | 3 (8080, 8081, 3000) |
| CORS | Not needed | Required |
| Hot Reload | No (rebuild) | Yes (npm run dev) |
| Production | ✅ Recommended | ❌ Not recommended |
| Development | ⚠️ Slower | ✅ Faster |
| CI/CD | Simpler | More complex |

## Best Practices

1. **Always build frontend before backend** in production
2. **Use separate servers** during active development
3. **Test monolithic build** before release
4. **Automate with build.bat or Makefile**
5. **Version frontend and backend together**
6. **Include frontend in Git** (src/, not out/)
7. **Gitignore build outputs** (out/, .next/)

## See Also

- [BUILD.md](BUILD.md) - Build instructions
- [DEPLOYMENT.md](DEPLOYMENT.md) - Deployment guide
- [CONFIGURATION.md](CONFIGURATION.md) - Configuration options
