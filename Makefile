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
DEFAULT_VERSION=v0.5.0-beta
# Try to get VERSION from environment, fallback to DEFAULT_VERSION
ifeq ($(DETECTED_OS),Windows)
	VERSION?=$(if $(VERSION_ENV),$(VERSION_ENV),$(DEFAULT_VERSION))
else
	VERSION?=$(DEFAULT_VERSION)
endif
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
	@mkdir -p $(BUILD_DIR)/debian-package/lib/systemd/system
	@mkdir -p $(BUILD_DIR)/debian-package/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package/var/log/maxiofs
	
	@echo "Building Linux AMD64 binary..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(FLAGS_RELEASE) -o $(BUILD_DIR)/debian-package/opt/maxiofs/maxiofs ./cmd/maxiofs
	
	@echo "Copying files..."
	@cp config.example.yaml $(BUILD_DIR)/debian-package/etc/maxiofs/config.yaml
	@cp debian/control $(BUILD_DIR)/debian-package/DEBIAN/control
	@cp debian/postinst $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/prerm $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/postrm $(BUILD_DIR)/debian-package/DEBIAN/
	@cp debian/maxiofs.service $(BUILD_DIR)/debian-package/lib/systemd/system/
	
	@echo "Setting permissions..."
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/postinst
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/prerm
	@chmod 755 $(BUILD_DIR)/debian-package/DEBIAN/postrm
	@chmod 755 $(BUILD_DIR)/debian-package/opt/maxiofs/maxiofs
	@chmod 644 $(BUILD_DIR)/debian-package/etc/maxiofs/config.yaml
	@chmod 644 $(BUILD_DIR)/debian-package/lib/systemd/system/maxiofs.service
	
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
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/lib/systemd/system
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/var/lib/maxiofs
	@mkdir -p $(BUILD_DIR)/debian-package-arm64/var/log/maxiofs
	
	@echo "Building Linux ARM64 binary..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(FLAGS_RELEASE) -o $(BUILD_DIR)/debian-package-arm64/opt/maxiofs/maxiofs ./cmd/maxiofs
	
	@echo "Copying files..."
	@cp config.example.yaml $(BUILD_DIR)/debian-package-arm64/etc/maxiofs/config.yaml
	@cp debian/control $(BUILD_DIR)/debian-package-arm64/DEBIAN/control
	@sed -i 's/Architecture: amd64/Architecture: arm64/' $(BUILD_DIR)/debian-package-arm64/DEBIAN/control
	@cp debian/postinst $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/prerm $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/postrm $(BUILD_DIR)/debian-package-arm64/DEBIAN/
	@cp debian/maxiofs.service $(BUILD_DIR)/debian-package-arm64/lib/systemd/system/
	
	@echo "Setting permissions..."
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/postinst
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/prerm
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/DEBIAN/postrm
	@chmod 755 $(BUILD_DIR)/debian-package-arm64/opt/maxiofs/maxiofs
	@chmod 644 $(BUILD_DIR)/debian-package-arm64/etc/maxiofs/config.yaml
	@chmod 644 $(BUILD_DIR)/debian-package-arm64/lib/systemd/system/maxiofs.service
	
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
	@rm -f $(BUILD_DIR)/maxiofs_*.deb
	@echo "Debian artifacts cleaned"

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
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Docker Commands:"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Run MaxIOFS in Docker"
	@echo "  docker-up          - Start services with docker-compose"
	@echo "  docker-down        - Stop services with docker-compose"
	@echo "  docker-logs        - View MaxIOFS logs"
	@echo "  docker-monitoring  - Start with Prometheus + Grafana"
	@echo "  docker-clean       - Remove containers, images and volumes"
	@echo ""
	@echo "Examples:"
	@echo "  make build VERSION=v1.0.0            - Build with version"
	@echo "  make build-linux                     - Cross-compile for Linux"
	@echo "  make build-all                       - Build for all platforms"
	@echo "  make deb VERSION=v0.4.1-beta         - Build Debian AMD64 package"
	@echo "  make deb-arm64 VERSION=v0.4.1-beta   - Build Debian ARM64 package"
	@echo "  make deb-install                     - Build and install package"
	@echo "  make docker-up                       - Start MaxIOFS in Docker"
	@echo "  make docker-monitoring               - Start with monitoring stack"

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
.PHONY: docker-up docker-down docker-logs docker-monitoring docker-clean

docker-up:
	@echo "Starting MaxIOFS with docker-compose..."
	docker-compose up -d

docker-down:
	@echo "Stopping MaxIOFS..."
	docker-compose down

docker-logs:
	@echo "Viewing MaxIOFS logs (Ctrl+C to exit)..."
	docker-compose logs -f maxiofs

docker-monitoring:
	@echo "Starting MaxIOFS with monitoring stack..."
	docker-compose --profile monitoring up -d

docker-clean:
	@echo "Cleaning Docker resources..."
	docker-compose down -v
	docker system prune -f

