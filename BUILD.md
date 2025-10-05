# MaxIOFS Build Guide

## Building with Version Information

MaxIOFS injects version information at compile time using Go's `-ldflags`. This displays the correct version when you run `maxiofs.exe --version` or when the application starts.

### Windows Build

**Option 1: Using build.bat (Recommended)**
```cmd
build.bat
```

**Option 2: Manual build with PowerShell**
```powershell
$VERSION = "v1.1.0"
$COMMIT = (git rev-parse --short HEAD)
$DATE = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")

go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" -o maxiofs.exe ./cmd/maxiofs
```

**Option 3: Manual build with cmd**
```cmd
set VERSION=v1.1.0
set COMMIT=ec1cecb
set BUILD_DATE=2025-10-05T14:15:00Z

go build -ldflags "-X main.version=%VERSION% -X main.commit=%COMMIT% -X main.date=%BUILD_DATE%" -o maxiofs.exe ./cmd/maxiofs
```

### Linux/macOS Build

**Using Makefile (Recommended)**
```bash
make build-server VERSION=v1.1.0
```

**Manual build**
```bash
VERSION="v1.1.0"
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" -o maxiofs ./cmd/maxiofs
```

## Verifying the Build

After building, check the version:

```bash
./maxiofs.exe --version
```

You should see output like:
```
v1.1.0 (commit: ec1cecb, built: 2025-10-05T14:15:00Z)
```

When starting the application, you'll see:
```json
{"level":"info","msg":"Starting MaxIOFS","version":"v1.1.0","commit":"ec1cecb","date":"2025-10-05T14:15:00Z"}
```

## Build Without Version Info

If you just want to quickly build for testing (without version information):

```bash
go build -o maxiofs.exe ./cmd/maxiofs
```

This will use default values:
- version: "dev"
- commit: "none"
- date: "unknown"

## Complete Build (Frontend + Backend)

To build everything including the web frontend:

**Windows:**
```cmd
cd web\frontend
npm install
npm run build
cd ..\..
build.bat
```

**Linux/macOS:**
```bash
make build
```

This will:
1. Install frontend dependencies
2. Build the Next.js frontend
3. Build the Go backend with version info
4. Create optimized production binaries

## Build Targets in Makefile

```bash
make build           # Build everything (web + server)
make build-web       # Build only web frontend
make build-server    # Build only Go server
make build-all       # Build for all platforms (Linux, Windows, macOS)
make dev             # Build development version with race detector
make clean           # Remove build artifacts
make test            # Run all tests
```

## Cross-Platform Builds

Build for all platforms at once:

```bash
make build-all VERSION=v1.1.0
```

This creates binaries for:
- Linux AMD64
- Linux ARM64
- Windows AMD64
- macOS AMD64
- macOS ARM64

Output will be in `build/` directory.

## Release Build

Create a complete release with archives and checksums:

```bash
make release VERSION=v1.1.0
```

This will:
1. Run tests
2. Run linters
3. Build for all platforms
4. Create tar.gz/zip archives
5. Generate SHA256 checksums
6. Output everything to `build/release/`

## Troubleshooting

**Problem:** Version shows as "dev" after running the binary

**Solution:** Make sure you're using `-ldflags` when building. Don't use plain `go build`.

**Problem:** Build fails with "package not found"

**Solution:** Download dependencies first:
```bash
go mod download
go mod tidy
```

**Problem:** Frontend build fails

**Solution:**
```bash
cd web/frontend
rm -rf node_modules .next
npm install
npm run build
```

## Development Workflow

For active development:

1. **Backend development:**
   ```bash
   go run ./cmd/maxiofs --data-dir ./data --log-level debug
   ```

2. **Frontend development:**
   ```bash
   cd web/frontend
   npm run dev
   ```
   Frontend will run on port 3000, backend on 8080/8081

3. **Production build:**
   ```bash
   build.bat  # Windows
   make build # Linux/macOS
   ```
