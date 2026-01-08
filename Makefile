# MaxIOFS Makefile
# Cross-platform build system (Windows/Linux/macOS)
# 
# Windows Requirements: PowerShell, Go, Node.js, npm, git
# Linux/macOS Requirements: bash, Go, Node.js, npm, git, make
#
# Usage:
#   make build              - Build everything (frontend + backend)
#   make build-server       - Build only the backend
#   make VERSION=v1.0.0     - Build with specific version
#   make help               - Show all available targets

# Detect OS
ifeq ($(OS),Windows_NT)
	DETECTED_OS := Windows
	BINARY_EXT := .exe
	RM := cmd /c "rmdir /s /q"
	MKDIR := cmd /c "if not exist"
	MKDIR_CMD := mkdir
	SHELL := cmd.exe
	.SHELLFLAGS := /c
	# For Windows, we need to get commit and date differently
	COMMIT := $(shell git rev-parse --short HEAD 2>nul || echo unknown)
	BUILD_DATE := $(shell powershell -Command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ'")
else
	DETECTED_OS := $(shell uname -s)
	BINARY_EXT :=
	RM := rm -rf
	MKDIR := mkdir -p
	MKDIR_CMD :=
	COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
	BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
endif

# Build variables
BINARY_NAME=maxiofs$(BINARY_EXT)
# Default version - update this when releasing new versions
DEFAULT_VERSION=v0.7.0-beta
# Try to get VERSION from environment, fallback to DEFAULT_VERSION
ifeq ($(DETECTED_OS),Windows)
	VERSION?=$(if $(VERSION_ENV),$(VERSION_ENV),$(DEFAULT_VERSION))
else
	VERSION?=$(DEFAULT_VERSION)
endif
# Clean version for RPM (remove v prefix and -beta/-alpha/-nightly suffix)
VERSION_CLEAN=$(shell echo $(VERSION) | sed 's/^v//' | sed 's/-beta$$//' | sed 's/-alpha$$//' | sed 's/-nightly-.*//')
COMMIT?=$(COMMIT)
BUILD_DATE?=$(BUILD_DATE)
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)"
LDFLAGS_RELEASE=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE) -s -w"
BUILD_FLAGS=-buildvcs=false

# Go variables
GOCMD=go
GOBUILD=$(GOCMD) build $(BUILD_FLAGS)
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Directories
BUILD_DIR=build
WEB_DIR=web/frontend
DIST_DIR=web/dist

# Kill hanging processes (Windows only)
.PHONY: kill-processes
kill-processes:
ifeq ($(DETECTED_OS),Windows)
	@echo Stopping any running Node processes...
#	@taskkill /F /IM node.exe /T 2>nul || echo No Node processes found
#	@timeout /t 1 /nobreak >nul
endif

# Default target
.PHONY: all
all: build

# Build the complete application
.PHONY: build
build: kill-processes build-web build-server

# Build the web frontend
.PHONY: build-web
build-web:
	@echo Building web frontend...
	@echo Cleaning previous build...
ifeq ($(DETECTED_OS),Windows)
	@if exist "$(WEB_DIR)\dist" rmdir /s /q "$(WEB_DIR)\dist"
	@echo Installing dependencies...
	@cd $(WEB_DIR) && npm install
	@echo Building Vite production bundle...
	@cd $(WEB_DIR) && npm run build
	@if not exist "$(WEB_DIR)\dist" echo Error: Build directory 'dist' was not created! && exit /b 1
else
	@rm -rf $(WEB_DIR)/dist
	@echo Installing dependencies...
	@cd $(WEB_DIR) && npm ci --legacy-peer-deps || npm install
	@echo Fixing executable permissions...
	@chmod -R +x $(WEB_DIR)/node_modules/.bin/* 2>/dev/null || true
	@echo Building Vite production bundle...
	@cd $(WEB_DIR) && NODE_ENV=production npm run build
	@test -d "$(WEB_DIR)/dist" || exit 1
endif
	@echo Web frontend built successfully - static bundle in $(WEB_DIR)/dist

# Build the Go server
.PHONY: build-server
build-server:
	@echo Building MaxIOFS server...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
else
	@mkdir -p $(BUILD_DIR)
endif
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/maxiofs
ifeq ($(DETECTED_OS),Windows)
	@echo.
	@echo ========================================
	@echo Build successful!
	@echo ========================================
	@echo Binary: $(BUILD_DIR)/$(BINARY_NAME)
	@echo Version: $(VERSION) (commit: $(COMMIT))
	@echo Frontend: Embedded in binary
	@echo.
	@echo Usage:
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --data-dir .\data
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --version
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --help
	@echo.
	@echo Endpoints:
	@echo   Web Console: http://localhost:8081
	@echo   S3 API:      http://localhost:8080
	@echo.
	@echo TLS Support (optional):
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --data-dir .\data --tls-cert cert.pem --tls-key key.pem
else
	@echo ""
	@echo "========================================"
	@echo "Build successful!"
	@echo "========================================"
	@echo "Binary: $(BUILD_DIR)/$(BINARY_NAME)"
	@echo "Version: $(VERSION) (commit: $(COMMIT))"
	@echo "Frontend: Embedded in binary"
	@echo ""
	@echo "Usage:"
	@echo "  ./$(BUILD_DIR)/$(BINARY_NAME) --data-dir ./data"
	@echo "  ./$(BUILD_DIR)/$(BINARY_NAME) --version"
	@echo "  ./$(BUILD_DIR)/$(BINARY_NAME) --help"
	@echo ""
	@echo "Endpoints:"
	@echo "  Web Console: http://localhost:8081"
	@echo "  S3 API:      http://localhost:8080"
	@echo ""
	@echo "TLS Support (optional):"
	@echo "  ./$(BUILD_DIR)/$(BINARY_NAME) --data-dir ./data --tls-cert cert.pem --tls-key key.pem"
endif

# Development build (without optimizations)
.PHONY: dev
dev:
	@echo "Building for development..."
	$(GOBUILD) -race -o $(BUILD_DIR)/$(BINARY_NAME)-dev ./cmd/maxiofs

# Install web dependencies
.PHONY: install-web
install-web:
	@echo "Installing web dependencies..."
	cd $(WEB_DIR) && npm ci

# Development server
.PHONY: dev-server
dev-server: build-web
	@echo "Starting development server..."
	$(BUILD_DIR)/$(BINARY_NAME)-dev --log-level=debug

# Development web server (separate process)
.PHONY: dev-web
dev-web:
	@echo "Starting web development server..."
	cd $(WEB_DIR) && npm run dev

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

# Run unit tests only
.PHONY: test-unit
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -race -short ./...

# Run integration tests
.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -race -run Integration ./tests/integration/...

# Run benchmarks
.PHONY: bench
bench:
ifeq ($(DETECTED_OS),Windows)
	@echo Running performance benchmarks...
	@if not exist "bench-results" mkdir bench-results
	@echo === Storage Benchmarks === > bench-results\benchmarks.txt
	$(GOTEST) ./internal/storage -bench=. -benchmem -benchtime=3s >> bench-results\benchmarks.txt
	@echo. >> bench-results\benchmarks.txt
	@echo === Encryption Benchmarks === >> bench-results\benchmarks.txt
	$(GOTEST) ./pkg/encryption -bench=. -benchmem -benchtime=3s >> bench-results\benchmarks.txt
	@echo.
	@echo Benchmarks completed
	@echo Results saved to bench-results\benchmarks.txt
else
	@echo "Running performance benchmarks..."
	@mkdir -p bench-results
	@echo "=== Storage Benchmarks ===" | tee bench-results/benchmarks.txt
	$(GOTEST) ./internal/storage -bench=. -benchmem -benchtime=3s | tee -a bench-results/benchmarks.txt
	@echo "" | tee -a bench-results/benchmarks.txt
	@echo "=== Encryption Benchmarks ===" | tee -a bench-results/benchmarks.txt
	$(GOTEST) ./pkg/encryption -bench=. -benchmem -benchtime=3s | tee -a bench-results/benchmarks.txt
	@echo ""
	@echo "✅ Benchmarks completed"
	@echo "📊 Results saved to bench-results/benchmarks.txt"
endif

# Run benchmarks with CPU profiling
.PHONY: bench-profile
bench-profile:
ifeq ($(DETECTED_OS),Windows)
	@echo Running benchmarks with CPU profiling...
	@if not exist "bench-results" mkdir bench-results
	$(GOTEST) ./internal/storage -bench=. -benchmem -cpuprofile=bench-results\cpu-storage.prof -benchtime=5s
	$(GOTEST) ./pkg/encryption -bench=. -benchmem -cpuprofile=bench-results\cpu-encryption.prof -benchtime=5s
	@echo.
	@echo Benchmarks with profiling completed
	@echo CPU profiles saved to bench-results\*.prof
	@echo Analyze with: go tool pprof bench-results\cpu-storage.prof
else
	@echo "Running benchmarks with CPU profiling..."
	@mkdir -p bench-results
	$(GOTEST) ./internal/storage -bench=. -benchmem -cpuprofile=bench-results/cpu-storage.prof -benchtime=5s
	$(GOTEST) ./pkg/encryption -bench=. -benchmem -cpuprofile=bench-results/cpu-encryption.prof -benchtime=5s
	@echo ""
	@echo "✅ Benchmarks with profiling completed"
	@echo "📊 CPU profiles saved to bench-results/*.prof"
	@echo "💡 Analyze with: go tool pprof bench-results/cpu-storage.prof"
endif

# ============================================================================
# Performance Testing (k6)
# ============================================================================
# Requires k6 to be installed: https://k6.io/docs/get-started/installation/
#
# Environment variables:
#   S3_ENDPOINT      - S3 API endpoint (default: http://localhost:8080)
#   CONSOLE_ENDPOINT - Console API endpoint (default: http://localhost:8081)
#   ACCESS_KEY       - S3 access key (create via web console)
#   SECRET_KEY       - S3 secret key
#   TEST_BUCKET      - Test bucket name (default: perf-test-bucket)
#
# Example:
#   S3_ENDPOINT=http://server:8080 ACCESS_KEY=test SECRET_KEY=test123 make perf-test-upload

# Check if k6 is installed
.PHONY: check-k6
check-k6:
	@which k6 >/dev/null 2>&1 || (echo "Error: k6 not found. Install from https://k6.io/docs/get-started/installation/" && exit 1)

# Upload performance test (ramp-up to 50 VUs over 2 minutes)
.PHONY: perf-test-upload
perf-test-upload: check-k6
	@echo "Running upload performance test..."
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	k6 run tests/performance/upload_test.js

# Download performance test (sustained 100 VUs for 3 minutes)
.PHONY: perf-test-download
perf-test-download: check-k6
	@echo "Running download performance test..."
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	k6 run tests/performance/download_test.js

# Mixed workload test (spike from 25 to 100 VUs)
.PHONY: perf-test-mixed
perf-test-mixed: check-k6
	@echo "Running mixed workload test..."
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	k6 run tests/performance/mixed_workload.js

# Quick smoke test (5 VUs for 30 seconds)
.PHONY: perf-test-quick
perf-test-quick: check-k6
	@echo "Running quick performance smoke test..."
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	k6 run --vus 5 --duration 30s tests/performance/mixed_workload.js

# Stress test (ramp up to find breaking point)
.PHONY: perf-test-stress
perf-test-stress: check-k6
	@echo "Running stress test (WARNING: may cause service degradation)..."
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	k6 run --vus 200 --duration 5m tests/performance/mixed_workload.js

# Run all performance tests sequentially
.PHONY: perf-test-all
perf-test-all: check-k6
	@echo "Running all performance tests..."
	@echo "This will take approximately 10-15 minutes"
	@echo "NOTE: Set ACCESS_KEY and SECRET_KEY environment variables"
	@echo ""
	@echo "=== Test 1/3: Upload Performance ==="
	k6 run tests/performance/upload_test.js
	@echo ""
	@echo "=== Test 2/3: Download Performance ==="
	k6 run tests/performance/download_test.js
	@echo ""
	@echo "=== Test 3/3: Mixed Workload ==="
	k6 run tests/performance/mixed_workload.js
	@echo ""
	@echo "All performance tests completed!"

# Performance test with custom VUs and duration
# Usage: make perf-test-custom VUS=50 DURATION=2m SCRIPT=upload_test.js
.PHONY: perf-test-custom
perf-test-custom: check-k6
	@echo "Running custom performance test..."
	k6 run --vus $(VUS) --duration $(DURATION) tests/performance/$(SCRIPT)

# Lint code
.PHONY: lint
lint:
	@echo "Linting Go code..."
	golangci-lint run ./...
	@echo "Linting web code..."
	cd $(WEB_DIR) && npm run lint

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	$(GOCMD) fmt ./...
	@echo "Formatting web code..."
	cd $(WEB_DIR) && npm run format

# Clean build artifacts
.PHONY: clean
clean:
	@echo Cleaning build artifacts...
	$(GOCLEAN)
ifeq ($(DETECTED_OS),Windows)
	@if exist "$(BUILD_DIR)" rmdir /s /q "$(BUILD_DIR)"
	@if exist "$(DIST_DIR)" rmdir /s /q "$(DIST_DIR)"
	@if exist "$(WEB_DIR)\dist" rmdir /s /q "$(WEB_DIR)\dist"
	@if exist "coverage.out" del /q "coverage.out"
else
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -rf $(WEB_DIR)/dist
	@rm -f coverage.out
endif

# Download Go dependencies
.PHONY: deps
deps:
	@echo "Downloading Go dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Build for all platforms
.PHONY: build-all
build-all: kill-processes clean build-web
	@echo Building for all platforms...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@echo Building Linux AMD64...
	@set GOOS=linux&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
	@echo Building Linux ARM64...
	@set GOOS=linux&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
	@echo Building Windows AMD64...
	@set GOOS=windows&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
	@echo Building macOS AMD64...
	@set GOOS=darwin&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
	@echo Building macOS ARM64...
	@set GOOS=darwin&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
	@echo.
	@echo ========================================
	@echo Multi-platform build complete!
	@echo ========================================
	@echo Binaries created in $(BUILD_DIR)/:
	@echo   - maxiofs-linux-amd64-$(VERSION)
	@echo   - maxiofs-linux-arm64-$(VERSION)
	@echo   - maxiofs-windows-amd64-$(VERSION).exe
	@echo   - maxiofs-darwin-amd64-$(VERSION)
	@echo   - maxiofs-darwin-arm64-$(VERSION)
else
	@mkdir -p $(BUILD_DIR)
	@echo "Building Linux AMD64..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
	@echo "Building Linux ARM64..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
	@echo "Building Windows AMD64..."
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
	@echo "Building macOS AMD64..."
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
	@echo "Building macOS ARM64..."
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
	@echo ""
	@echo "========================================"
	@echo "Multi-platform build complete!"
	@echo "========================================"
	@echo "Binaries created in $(BUILD_DIR)/:"
	@echo "  - maxiofs-linux-amd64-$(VERSION)"
	@echo "  - maxiofs-linux-arm64-$(VERSION)"
	@echo "  - maxiofs-windows-amd64-$(VERSION).exe"
	@echo "  - maxiofs-darwin-amd64-$(VERSION)"
	@echo "  - maxiofs-darwin-arm64-$(VERSION)"
endif

# Build for specific platforms (cross-compilation)
.PHONY: build-linux
build-linux:
	@echo Building for Linux AMD64...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@set GOOS=linux&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
else
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
endif
	@echo Linux AMD64 binary created: $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION)

.PHONY: build-linux-arm64
build-linux-arm64:
	@echo Building for Linux ARM64...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@set GOOS=linux&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
else
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
endif
	@echo Linux ARM64 binary created: $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION)

.PHONY: build-windows
build-windows:
	@echo Building for Windows AMD64...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@set GOOS=windows&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
else
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
endif
	@echo Windows AMD64 binary created: $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe

.PHONY: build-darwin
build-darwin:
	@echo Building for macOS AMD64...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@set GOOS=darwin&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
else
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
endif
	@echo macOS AMD64 binary created: $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION)

.PHONY: build-darwin-arm64
build-darwin-arm64:
	@echo Building for macOS ARM64...
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@set GOOS=darwin&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
else
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
endif
	@echo macOS ARM64 binary created: $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION)

# Docker build (basic)
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t maxiofs:$(VERSION) .
	docker tag maxiofs:$(VERSION) maxiofs:latest

# Docker run (basic)
.PHONY: docker-run
docker-run:
	@echo "Running MaxIOFS in Docker..."
	docker run -d \
		--name maxiofs \
		-p 8080:8080 \
		-p 8081:8081 \
		-v maxiofs-data:/data \
		maxiofs:latest

# Release build (optimized binaries with -s -w flags, no compression)
.PHONY: release
release: clean build-web
	@echo "Building optimized release binaries for all platforms..."
ifeq ($(DETECTED_OS),Windows)
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	@echo Building Linux AMD64 (optimized)...
	@set GOOS=linux&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
	@echo Building Linux ARM64 (optimized)...
	@set GOOS=linux&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
	@echo Building Windows AMD64 (optimized)...
	@set GOOS=windows&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
	@echo Building macOS AMD64 (optimized)...
	@set GOOS=darwin&& set GOARCH=amd64&& go build $(BUILD_FLAGS) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
	@echo Building macOS ARM64 (optimized)...
	@set GOOS=darwin&& set GOARCH=arm64&& go build $(BUILD_FLAGS) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
	@echo.
	@echo ========================================
	@echo Release build complete!
	@echo ========================================
	@echo Optimized binaries ready in $(BUILD_DIR)\:
	@dir /b "$(BUILD_DIR)\maxiofs-*"
	@echo.
	@echo Optimization: Built with -s -w flags (20-30%% smaller, no debug symbols)
	@echo Ready to upload to GitHub releases
else
	@mkdir -p $(BUILD_DIR)
	@echo "Building Linux AMD64 (optimized)..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-linux-amd64-$(VERSION) ./cmd/maxiofs
	@echo "Building Linux ARM64 (optimized)..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-linux-arm64-$(VERSION) ./cmd/maxiofs
	@echo "Building Windows AMD64 (optimized)..."
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-windows-amd64-$(VERSION).exe ./cmd/maxiofs
	@echo "Building macOS AMD64 (optimized)..."
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-darwin-amd64-$(VERSION) ./cmd/maxiofs
	@echo "Building macOS ARM64 (optimized)..."
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-darwin-arm64-$(VERSION) ./cmd/maxiofs
	@echo ""
	@echo "========================================"
	@echo "Release build complete!"
	@echo "========================================"
	@echo "Optimized binaries ready in $(BUILD_DIR)/:"
	@ls -lh $(BUILD_DIR)/maxiofs-*
	@echo ""
	@echo "Optimization: Built with -s -w flags (20-30% smaller, no debug symbols)"
	@echo "Ready to upload to GitHub releases"
endif

# Debian package build
# IMPORTANT: This target only installs config.example.yaml in the package.
# The actual config.yaml is created by the postinst script ONLY if it doesn't exist.
# This preserves the encryption key when upgrading packages.
.PHONY: deb
deb: build-web
	@echo "Building Debian package..."
ifneq ($(DETECTED_OS),Windows)
	@echo "Checking for required tools..."
	@which dpkg-deb >/dev/null || (echo "Error: dpkg-deb not found. Install with: sudo apt-get install dpkg-dev" && exit 1)
	@echo "Creating package structure..."
	@rm -rf $(BUILD_DIR)/debian-package
	@mkdir -p $(BUILD_DIR)/debian-package/DEBIAN
	@mkdir -p $(BUILD_DIR)/debian-package/opt/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package/etc/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package/etc/logrotate.d
	@mkdir -p $(BUILD_DIR)/debian-package/lib/systemd/system
	@mkdir -p $(BUILD_DIR)/debian-package/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package/var/log/maxiofs
	
	@echo "Building Linux AMD64 binary..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(FLAGS_RELEASE) -o $(BUILD_DIR)/debian-package/opt/maxiofs/maxiofs ./cmd/maxiofs
	
	@echo "Copying files..."
	@cp config.example.yaml $(BUILD_DIR)/debian-package/etc/maxiofs/config.example.yaml
	@sed -i 's|data_dir: "./data"|data_dir: "/var/lib/maxiofs"|' $(BUILD_DIR)/debian-package/etc/maxiofs/config.example.yaml
	@cp debian/control $(BUILD_DIR)/debian-package/DEBIAN/control
	@cp debian/postinst $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/prerm $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/postrm $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/maxiofs.service $(BUILD_DIR)/debian-package/lib/systemd/system/
	@cp debian/maxiofs.logrotate $(BUILD_DIR)/debian-package/etc/logrotate.d/maxiofs

	@echo "Setting permissions..."
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/postinst
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/prerm
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/postrm
	@chmod 755 $(BUILD_DIR)/debian-package/opt/maxiofs/maxiofs
	@chmod 644 $(BUILD_DIR)/debian-package/etc/maxiofs/config.example.yaml
	@chmod 644 $(BUILD_DIR)/debian-package/lib/systemd/system/maxiofs.service
	@chmod 644 $(BUILD_DIR)/debian-package/etc/logrotate.d/maxiofs

	@echo "Building .deb package..."
	@dpkg-deb --build $(BUILD_DIR)/debian-package $(BUILD_DIR)/maxiofs_$(VERSION)_amd64.deb
	
	@echo ""
	@echo "=========================================="
	@echo "Debian package created successfully!"
	@echo "=========================================="
	@echo "Package: $(BUILD_DIR)/maxiofs_$(VERSION)_amd64.deb"
	@echo ""
	@echo "To install:"
	@echo "  sudo dpkg -i $(BUILD_DIR)/maxiofs_$(VERSION)_amd64.deb"
	@echo ""
	@echo "To remove:"
	@echo "  sudo apt-get remove maxiofs"
	@echo ""
	@echo "To purge (remove data):"
	@echo "  sudo apt-get purge maxiofs"
	@echo ""
	@echo "After installation:"
	@echo "  1. Edit /etc/maxiofs/config.yaml"
	@echo "  2. Start service: sudo systemctl start maxiofs"
	@echo "  3. Check status: sudo systemctl status maxiofs"
	@echo "  4. View logs: sudo journalctl -u maxiofs -f"
else
	@echo "Error: Debian package building is only supported on Linux"
	@echo "Please run this target on a Linux system with dpkg-dev installed"
	@exit 1
endif

# Debian package build for ARM64
# IMPORTANT: This target only installs config.example.yaml in the package.
# The actual config.yaml is created by the postinst script ONLY if it doesn't exist.
# This preserves the encryption key when upgrading packages.
.PHONY: deb-arm64
deb-arm64: build-web
	@echo "Building Debian package for ARM64..."
ifneq ($(DETECTED_OS),Windows)
	@echo "Checking for required tools..."
	@which dpkg-deb >/dev/null || (echo "Error: dpkg-deb not found. Install with: sudo apt-get install dpkg-dev" && exit 1)
	@echo "Creating package structure..."
	@rm -rf $(BUILD_DIR)/debian-package-arm64
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/DEBIAN
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/opt/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/etc/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/etc/logrotate.d
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/lib/systemd/system
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/var/log/maxiofs
	
	@echo "Building Linux ARM64 binary..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(FLAGS_RELEASE) -o $(BUILD_DIR)/debian-package-arm64/opt/maxiofs/maxiofs ./cmd/maxiofs
	
	@echo "Copying files..."
	@cp config.example.yaml $(BUILD_DIR)/debian-package-arm64/etc/maxiofs/config.example.yaml
	@sed -i 's|data_dir: "./data"|data_dir: "/var/lib/maxiofs"|' $(BUILD_DIR)/debian-package-arm64/etc/maxiofs/config.example.yaml
	@cp debian/control $(BUILD_DIR)/debian-package-arm64/DEBIAN/control
	@sed -i 's/Architecture: amd64/Architecture: arm64/' $(BUILD_DIR)/debian-package-arm64/DEBIAN/control
	@cp debian/postinst $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/prerm $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/postrm $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/maxiofs.service $(BUILD_DIR)/debian-package-arm64/lib/systemd/system/
	@cp debian/maxiofs.logrotate $(BUILD_DIR)/debian-package-arm64/etc/logrotate.d/maxiofs

	@echo "Setting permissions..."
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/postinst
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/prerm
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/postrm
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/opt/maxiofs/maxiofs
	@chmod 644 $(BUILD_DIR)/debian-package-arm64/etc/maxiofs/config.example.yaml
	@chmod 644 $(BUILD_DIR)/debian-package-arm64/lib/systemd/system/maxiofs.service
	@chmod 644 $(BUILD_DIR)/debian-package-arm64/etc/logrotate.d/maxiofs

	@echo "Building .deb package..."
	@dpkg-deb --build $(BUILD_DIR)/debian-package-arm64 $(BUILD_DIR)/maxiofs_$(VERSION)_arm64.deb
	
	@echo ""
	@echo "=========================================="
	@echo "Debian ARM64 package created successfully!"
	@echo "=========================================="
	@echo "Package: $(BUILD_DIR)/maxiofs_$(VERSION)_arm64.deb"
	@echo ""
	@echo "To install on ARM64 system:"
	@echo "  sudo dpkg -i $(BUILD_DIR)/maxiofs_$(VERSION)_arm64.deb"
	@echo ""
	@echo "Compatible with:"
	@echo "  - Raspberry Pi 3/4/5 (64-bit OS)"
	@echo "  - AWS Graviton instances"
	@echo "  - Oracle Cloud Ampere"
	@echo "  - Any ARM64 Linux system"
else
	@echo "Error: Debian package building is only supported on Linux"
	@echo "Please run this target on a Linux system with dpkg-dev installed"
	@exit 1
endif

# Install Debian package locally (for testing)
.PHONY: deb-install
deb-install: deb
	@echo "Installing Debian package locally..."
ifneq ($(DETECTED_OS),Windows)
	sudo dpkg -i $(BUILD_DIR)/maxiofs_$(VERSION)_amd64.deb || sudo apt-get install -f -y
	@echo ""
	@echo "Package installed! Service status:"
	sudo systemctl status maxiofs --no-pager || true
	@echo ""
	@echo "Configuration file: /etc/maxiofs/config.yaml"
	@echo "Data directory: /var/lib/maxiofs"
	@echo "Logs: /var/log/maxiofs or 'sudo journalctl -u maxiofs -f'"
	@echo ""
	@echo "To start: sudo systemctl start maxiofs"
	@echo "To enable on boot: sudo systemctl enable maxiofs"
else
	@echo "Error: Installation is only supported on Linux"
	@exit 1
endif

# Uninstall Debian package (for testing)
.PHONY: deb-uninstall
deb-uninstall:
	@echo "Uninstalling MaxIOFS..."
ifneq ($(DETECTED_OS),Windows)
	sudo systemctl stop maxiofs || true
	sudo apt-get remove maxiofs -y || true
	@echo "MaxIOFS uninstalled (data preserved in /var/lib/maxiofs)"
	@echo "To completely remove data: sudo apt-get purge maxiofs -y"
else
	@echo "Error: Uninstallation is only supported on Linux"
	@exit 1
endif

# Clean Debian build artifacts
.PHONY: deb-clean
deb-clean:
	@echo "Cleaning Debian package artifacts..."
	@rm -rf $(BUILD_DIR)/debian-package
	@rm -rf $(BUILD_DIR)/debian-package-arm64
	@rm -f $(BUILD_DIR)/maxiofs_*.deb
	@echo "Debian artifacts cleaned"

# ============================================================================
# RPM Package Targets (RHEL/CentOS/Fedora/Rocky/Alma)
# ============================================================================

# RPM package build for AMD64
# IMPORTANT: This target only installs config.example.yaml in the package.
# The actual config.yaml is created by the %post script ONLY if it doesn't exist.
# This preserves the encryption key when upgrading packages.
.PHONY: rpm
rpm: build-web rpm-binary

# Internal target for RPM AMD64 build (without build-web prerequisite)
.PHONY: rpm-binary
rpm-binary:
	@echo "Building RPM package for AMD64..."
ifneq ($(DETECTED_OS),Windows)
	@echo "Checking for required tools..."
	@which rpmbuild >/dev/null || (echo "Error: rpmbuild not found. Install with: sudo dnf install rpm-build rpmdevtools" && exit 1)
	
	@echo "Creating build structure..."
	@mkdir -p $(BUILD_DIR)/rpm-build/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
	@mkdir -p $(BUILD_DIR)/rpm-package/opt/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package/etc/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package/var/log/maxiofs
	
	@echo "Building Linux AMD64 binary..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs ./cmd/maxiofs
	
	@echo "Creating tarball..."
	@echo "VERSION=$(VERSION), VERSION_CLEAN=$(VERSION_CLEAN)"
	@mkdir -p $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/build
	@echo "Copying source files to $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/"
	@cp -r cmd config.example.yaml go.mod go.sum internal pkg web rpm docs README.md CHANGELOG.md TODO.md LICENSE $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/ 2>/dev/null || true
	@cp $(BUILD_DIR)/maxiofs $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/build/maxiofs
	@echo "Creating tarball: $(BUILD_DIR)/rpm-build/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz"
	@ls -la $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN) || echo "Directory listing failed"
	tar -czf $(BUILD_DIR)/rpm-build/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz -C $(BUILD_DIR) maxiofs-$(VERSION_CLEAN)
	@echo "Tarball created successfully"
	@ls -lh $(BUILD_DIR)/rpm-build/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz || echo "Tarball not found!"
	@rm -rf $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)
	
	@echo "Building RPM package..."
	@rpmbuild --define "_topdir $(shell pwd)/$(BUILD_DIR)/rpm-build" \
		--define "version $(VERSION_CLEAN)" \
		--define "_builddir $(shell pwd)/$(BUILD_DIR)" \
		-bb rpm/maxiofs.spec
	
	@echo "Moving RPM to build directory..."
	@mv $(BUILD_DIR)/rpm-build/RPMS/x86_64/maxiofs-*.rpm $(BUILD_DIR)/ 2>/dev/null || true
	
	@echo ""
	@echo "=========================================="
	@echo "RPM package created successfully!"
	@echo "=========================================="
	@find $(BUILD_DIR) -name "maxiofs-*.rpm" -type f -exec echo "Package: {}" \;
	@echo ""
	@echo "To install:"
	@echo "  sudo rpm -ivh $(BUILD_DIR)/maxiofs-*.x86_64.rpm"
	@echo "  or"
	@echo "  sudo dnf install $(BUILD_DIR)/maxiofs-*.x86_64.rpm"
	@echo ""
	@echo "To upgrade:"
	@echo "  sudo rpm -Uvh $(BUILD_DIR)/maxiofs-*.x86_64.rpm"
	@echo ""
	@echo "Compatible with:"
	@echo "  - RHEL 8, 9"
	@echo "  - Rocky Linux 8, 9"
	@echo "  - AlmaLinux 8, 9"
	@echo "  - CentOS Stream 8, 9"
	@echo "  - Fedora 38+"
	@echo "  - Oracle Linux 8, 9"
else
	@echo "Error: RPM package building is only supported on Linux"
	@echo "Use 'make rpm-docker' to build in a container"
	@exit 1
endif

# RPM package build for ARM64
.PHONY: rpm-arm64
rpm-arm64: build-web rpm-arm64-binary

# Internal target for RPM ARM64 build (without build-web prerequisite)
.PHONY: rpm-arm64-binary
rpm-arm64-binary:
	@echo "Building RPM package for ARM64..."
ifneq ($(DETECTED_OS),Windows)
	@echo "Checking for required tools..."
	@which rpmbuild >/dev/null || (echo "Error: rpmbuild not found. Install with: sudo dnf install rpm-build rpmdevtools" && exit 1)
	
	@echo "Creating build structure..."
	@mkdir -p $(BUILD_DIR)/rpm-build-arm64/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
	@mkdir -p $(BUILD_DIR)/rpm-package-arm64/opt/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package-arm64/etc/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package-arm64/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/rpm-package-arm64/var/log/maxiofs
	
	@echo "Building Linux ARM64 binary..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BUILD_DIR)/maxiofs-arm64 ./cmd/maxiofs

	@echo "Creating tarball..."
	@echo "VERSION=$(VERSION), VERSION_CLEAN=$(VERSION_CLEAN)"
	@mkdir -p $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/build
	@echo "Copying source files to $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/"
	@cp -r cmd config.example.yaml go.mod go.sum internal pkg web rpm docs README.md CHANGELOG.md TODO.md LICENSE $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/ 2>/dev/null || true
	@cp $(BUILD_DIR)/maxiofs-arm64 $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)/build/maxiofs
	@echo "Creating tarball: $(BUILD_DIR)/rpm-build-arm64/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz"
	@ls -la $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN) || echo "Directory listing failed"
	tar -czf $(BUILD_DIR)/rpm-build-arm64/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz -C $(BUILD_DIR) maxiofs-$(VERSION_CLEAN)
	@echo "Tarball created successfully"
	@ls -lh $(BUILD_DIR)/rpm-build-arm64/SOURCES/maxiofs-$(VERSION_CLEAN).tar.gz || echo "Tarball not found!"
	@rm -rf $(BUILD_DIR)/maxiofs-$(VERSION_CLEAN)
	
	@echo "Building RPM package..."
	@rpmbuild --define "_topdir $(shell pwd)/$(BUILD_DIR)/rpm-build-arm64" \
		--define "version $(VERSION_CLEAN)" \
		--define "_builddir $(shell pwd)/$(BUILD_DIR)" \
		--target aarch64 \
		-bb rpm/maxiofs.spec
	
	@echo "Moving RPM to build directory..."
	@mv $(BUILD_DIR)/rpm-build-arm64/RPMS/aarch64/maxiofs-*.rpm $(BUILD_DIR)/ 2>/dev/null || true
	
	@echo ""
	@echo "=========================================="
	@echo "RPM ARM64 package created successfully!"
	@echo "=========================================="
	@find $(BUILD_DIR) -name "maxiofs-*.aarch64.rpm" -type f -exec echo "Package: {}" \;
	@echo ""
	@echo "To install on ARM64 system:"
	@echo "  sudo rpm -ivh $(BUILD_DIR)/maxiofs-*.aarch64.rpm"
	@echo ""
	@echo "Compatible with:"
	@echo "  - Raspberry Pi 4/5 (64-bit)"
	@echo "  - AWS Graviton"
	@echo "  - Oracle Cloud Ampere"
	@echo "  - Any ARM64 RHEL/Rocky/Alma system"
else
	@echo "Error: RPM package building is only supported on Linux"
	@echo "Use 'make rpm-docker' to build in a container"
	@exit 1
endif

# Build RPM for both AMD64 and ARM64
.PHONY: rpm-all
rpm-all: build-web
	@$(MAKE) rpm-binary
	@$(MAKE) rpm-arm64-binary
	@echo ""
	@echo "=========================================="
	@echo "All RPM packages created successfully!"
	@echo "=========================================="
	@find $(BUILD_DIR) -name "maxiofs-*.rpm" -type f -exec echo "Package: {}" \;
	@echo ""
	@echo "AMD64 package: maxiofs-*.x86_64.rpm"
	@echo "ARM64 package: maxiofs-*.aarch64.rpm"

# Internal target for RPM AMD64 build (without build-web prerequisite)
.PHONY: rpm-binary
rpm-binary:

# Build RPM using Docker (works on any platform)
.PHONY: rpm-docker
rpm-docker: build-web
	@echo "Building RPM packages (AMD64 + ARM64) using Docker..."
	@docker build -f Dockerfile.rpm-builder -t maxiofs-rpm-builder .
	@docker run --rm -v $(shell pwd):/workspace maxiofs-rpm-builder
	@echo ""
	@echo "RPM packages built successfully using Docker!"

# Install RPM package locally (for testing)
.PHONY: rpm-install
rpm-install: rpm
	@echo "Installing RPM package locally..."
ifneq ($(DETECTED_OS),Windows)
	@if command -v dnf >/dev/null 2>&1; then \
		sudo dnf install -y $(BUILD_DIR)/maxiofs-*.x86_64.rpm; \
	elif command -v yum >/dev/null 2>&1; then \
		sudo yum install -y $(BUILD_DIR)/maxiofs-*.x86_64.rpm; \
	else \
		sudo rpm -ivh $(BUILD_DIR)/maxiofs-*.x86_64.rpm; \
	fi
	@echo ""
	@echo "Package installed! Service status:"
	sudo systemctl status maxiofs --no-pager || true
	@echo ""
	@echo "Configuration file: /etc/maxiofs/config.yaml"
	@echo "Data directory: /var/lib/maxiofs"
	@echo "Logs: /var/log/maxiofs or 'sudo journalctl -u maxiofs -f'"
	@echo ""
	@echo "To start: sudo systemctl start maxiofs"
	@echo "To enable on boot: sudo systemctl enable maxiofs"
else
	@echo "Error: Installation is only supported on Linux"
	@exit 1
endif

# Uninstall RPM package (for testing)
.PHONY: rpm-uninstall
rpm-uninstall:
	@echo "Uninstalling MaxIOFS RPM..."
ifneq ($(DETECTED_OS),Windows)
	sudo systemctl stop maxiofs || true
	sudo rpm -e maxiofs || true
	@echo "MaxIOFS uninstalled (data preserved in /var/lib/maxiofs)"
	@echo "To completely remove data: sudo rm -rf /etc/maxiofs /var/lib/maxiofs /var/log/maxiofs"
else
	@echo "Error: Uninstallation is only supported on Linux"
	@exit 1
endif

# Clean RPM build artifacts
.PHONY: rpm-clean
rpm-clean:
	@echo "Cleaning RPM package artifacts..."
	@rm -rf $(BUILD_DIR)/rpm-build
	@rm -rf $(BUILD_DIR)/rpm-build-arm64
	@rm -rf $(BUILD_DIR)/rpm-package
	@rm -rf $(BUILD_DIR)/rpm-package-arm64
	@rm -f $(BUILD_DIR)/maxiofs-*.rpm
	@rm -f $(BUILD_DIR)/maxiofs-*.src.rpm
	@echo "RPM artifacts cleaned"

# Help target
.PHONY: help
help:
	@echo "MaxIOFS Build System (Cross-Platform)"
	@echo ""
	@echo "Detected OS: $(DETECTED_OS)"
	@echo ""
	@echo "Available targets:"
	@echo "  all                - Clean and build everything"
	@echo "  build              - Build web frontend and server (current platform)"
	@echo "  build-web          - Build only the web frontend"
	@echo "  build-server       - Build only the Go server (current platform)"
	@echo "  build-all          - Build for all platforms (cross-compile)"
	@echo "  build-linux        - Build for Linux AMD64"
	@echo "  build-linux-arm64  - Build for Linux ARM64"
	@echo "  build-windows      - Build for Windows AMD64"
	@echo "  build-darwin       - Build for macOS AMD64 (Intel)"
	@echo "  build-darwin-arm64 - Build for macOS ARM64 (Apple Silicon)"
	@echo "  dev                - Build development version"
	@echo "  dev-server         - Start development server"
	@echo "  dev-web            - Start web development server"
	@echo "  test               - Run all tests"
	@echo "  test-unit          - Run unit tests only"
	@echo "  test-integration   - Run integration tests only"
	@echo "  perf-test-upload   - Run k6 upload performance test"
	@echo "  perf-test-download - Run k6 download performance test"
	@echo "  perf-test-mixed    - Run k6 mixed workload test"
	@echo "  perf-test-quick    - Run quick k6 smoke test (30s)"
	@echo "  perf-test-stress   - Run k6 stress test (WARNING: intensive)"
	@echo "  perf-test-all      - Run all k6 performance tests (10-15min)"
	@echo "  perf-test-custom   - Run custom k6 test (set VUS, DURATION, SCRIPT)"
	@echo "  lint               - Lint code"
	@echo "  fmt                - Format code"
	@echo "  clean              - Clean build artifacts"
	@echo "  deps               - Download dependencies"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Run in Docker"
	@echo "  release            - Create release build"
	@echo "  deb                - Build Debian package AMD64 (Linux only)"
	@echo "  deb-arm64          - Build Debian package ARM64 (Linux only)"
	@echo "  deb-install        - Build and install Debian package locally"
	@echo "  deb-uninstall      - Uninstall Debian package"
	@echo "  deb-clean          - Clean Debian build artifacts"
	@echo "  rpm                - Build RPM package AMD64 (Linux only)"
	@echo "  rpm-arm64          - Build RPM package ARM64 (Linux only)"
	@echo "  rpm-docker         - Build RPM using Docker (any platform)"
	@echo "  rpm-install        - Build and install RPM package locally"
	@echo "  rpm-uninstall      - Uninstall RPM package"
	@echo "  rpm-clean          - Clean RPM build artifacts"
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Docker Commands:"
	@echo "  docker-build              - Build Docker image"
	@echo "  docker-run                - Run MaxIOFS in Docker (standalone)"
	@echo "  docker-up                 - Start basic MaxIOFS (docker-compose)"
	@echo "  docker-monitoring         - Start with Prometheus + Grafana"
	@echo "  docker-cluster            - Start 3-node cluster"
	@echo "  docker-cluster-monitoring - Start cluster + monitoring (full stack)"
	@echo "  docker-ps                 - Show running containers"
	@echo "  docker-logs               - View MaxIOFS logs"
	@echo "  docker-down               - Stop all services"
	@echo "  docker-clean              - Remove containers, volumes, images"
	@echo ""
	@echo "Examples:"
	@echo "  make build VERSION=v1.0.0                      - Build with version"
	@echo "  make build-linux                               - Cross-compile for Linux"
	@echo "  make build-all                                 - Build for all platforms"
	@echo "  make deb VERSION=v0.4.1-beta                   - Build Debian AMD64 package"
	@echo "  make deb-arm64 VERSION=v0.4.1-beta             - Build Debian ARM64 package"
	@echo "  make rpm VERSION=v0.7.0                        - Build RPM AMD64 package"
	@echo "  make rpm-arm64 VERSION=v0.7.0                  - Build RPM ARM64 package"
	@echo "  make rpm-docker                                - Build RPM using Docker"
	@echo "  make deb-install                               - Build and install package"
	@echo "  make docker-up                                 - Start MaxIOFS in Docker"
	@echo "  make docker-monitoring                         - Start with monitoring stack"
	@echo "  ACCESS_KEY=test SECRET_KEY=pass make perf-test-upload  - Run upload test"
	@echo "  make perf-test-custom VUS=100 DURATION=5m SCRIPT=mixed_workload.js"

# Docker targets (PowerShell-based for Windows)
.PHONY: docker-build-ps docker-run-ps docker-up-ps docker-down-ps docker-monitoring-ps docker-clean-ps

docker-build-ps:
	@echo "Building Docker image with PowerShell script..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1

docker-run-ps:
	@echo "Building and starting MaxIOFS in Docker..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1 -Up

docker-up-ps:
	@echo "Starting MaxIOFS with docker-compose..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1 -NoBuild -Up

docker-down-ps:
	@echo "Stopping MaxIOFS..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1 -Down

docker-monitoring-ps:
	@echo "Starting MaxIOFS with monitoring stack..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1 -ProfileName monitoring -Up

docker-clean-ps:
	@echo "Cleaning Docker resources..."
	@pwsh -ExecutionPolicy Bypass -File docker-build.ps1 -Clean

# Docker Compose targets (cross-platform)
.PHONY: docker-up docker-down docker-logs docker-monitoring docker-cluster docker-cluster-monitoring docker-ps docker-clean

# Start basic MaxIOFS (single node)
docker-up:
	@echo "Starting MaxIOFS (basic)..."
	docker-compose up -d

# Stop all services
docker-down:
	@echo "Stopping all MaxIOFS services..."
	docker-compose --profile monitoring --profile cluster down

# View logs (basic MaxIOFS)
docker-logs:
	@echo "Viewing MaxIOFS logs (Ctrl+C to exit)..."
	docker-compose logs -f maxiofs

# Start with Prometheus + Grafana monitoring
docker-monitoring:
	@echo "Starting MaxIOFS with Prometheus + Grafana..."
	@echo "Access:"
	@echo "  - MaxIOFS Console: http://localhost:8081 (admin/admin)"
	@echo "  - Prometheus:      http://localhost:9091"
	@echo "  - Grafana:         http://localhost:3000 (admin/admin)"
	docker-compose --profile monitoring up -d

# Start 3-node cluster setup
docker-cluster:
	@echo "Starting MaxIOFS 3-node cluster..."
	@echo "Access:"
	@echo "  - Node 1 Console:  http://localhost:8081 (admin/admin)"
	@echo "  - Node 2 Console:  http://localhost:8083 (admin/admin)"
	@echo "  - Node 3 Console:  http://localhost:8085 (admin/admin)"
	docker-compose --profile cluster up -d

# Start cluster with monitoring (full stack)
docker-cluster-monitoring:
	@echo "Starting MaxIOFS 3-node cluster with monitoring..."
	@echo "Access:"
	@echo "  - Node 1 Console:  http://localhost:8081"
	@echo "  - Node 2 Console:  http://localhost:8083"
	@echo "  - Node 3 Console:  http://localhost:8085"
	@echo "  - Prometheus:      http://localhost:9091"
	@echo "  - Grafana:         http://localhost:3000"
	docker-compose --profile monitoring --profile cluster up -d

# Show running containers
docker-ps:
	@echo "MaxIOFS Docker containers:"
	@docker-compose ps

# Clean up everything (containers, volumes, images)
docker-clean:
	@echo "Cleaning Docker resources (containers, volumes, images)..."
	docker-compose --profile monitoring --profile cluster down -v
	docker system prune -f
	docker rmi maxiofs:latest maxiofs:$(VERSION) 2>/dev/null || true
	@echo "Cleanup complete!"

