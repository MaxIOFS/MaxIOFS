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
DEFAULT_VERSION=v0.3.1-beta
# Try to get VERSION from environment, fallback to DEFAULT_VERSION
ifeq ($(DETECTED_OS),Windows)
	VERSION?=$(if $(VERSION_ENV),$(VERSION_ENV),$(DEFAULT_VERSION))
else
	VERSION?=$(DEFAULT_VERSION)
endif
COMMIT?=$(COMMIT)
BUILD_DATE?=$(BUILD_DATE)
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)"
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
	@if not exist "$(WEB_DIR)\dist" (echo Error: Build directory 'dist' was not created! && exit /b 1)
else
	@rm -rf $(WEB_DIR)/dist
	@echo Installing dependencies...
	@cd $(WEB_DIR) && npm install
	@echo Building Vite production bundle...
	@cd $(WEB_DIR) && NODE_ENV=production npm run build
	@if [ ! -d "$(WEB_DIR)/dist" ]; then \
		echo "Error: Build directory 'dist' was not created!"; \
		exit 1; \
	fi
endif
	@echo Web frontend built successfully (static bundle in $(WEB_DIR)/dist)

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
	@echo.
	@echo ========================================
	@echo Build successful!
	@echo ========================================
	@echo Binary: $(BUILD_DIR)/$(BINARY_NAME)
	@echo Version: $(VERSION) (commit: $(COMMIT))
	@echo Frontend: Embedded in binary
	@echo.
	@echo Usage:
ifeq ($(DETECTED_OS),Windows)
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --data-dir .\data
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --version
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --help
else
	@echo   ./$(BUILD_DIR)/$(BINARY_NAME) --data-dir ./data
	@echo   ./$(BUILD_DIR)/$(BINARY_NAME) --version
	@echo   ./$(BUILD_DIR)/$(BINARY_NAME) --help
endif
	@echo.
	@echo Endpoints:
	@echo   Web Console: http://localhost:8081
	@echo   S3 API:      http://localhost:8080
	@echo.
	@echo TLS Support (optional):
ifeq ($(DETECTED_OS),Windows)
	@echo   .\$(BUILD_DIR)\$(BINARY_NAME) --data-dir .\data --tls-cert cert.pem --tls-key key.pem
else
	@echo   ./$(BUILD_DIR)/$(BINARY_NAME) --data-dir ./data --tls-cert cert.pem --tls-key key.pem
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
endif
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

# Docker build
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t maxiofs:$(VERSION) .
	docker tag maxiofs:$(VERSION) maxiofs:latest

# Docker run
.PHONY: docker-run
docker-run:
	@echo "Running MaxIOFS in Docker..."
	docker run -d \
		--name maxiofs \
		-p 8080:8080 \
		-p 8081:8081 \
		-v maxiofs-data:/data \
		maxiofs:latest

# Release build
.PHONY: release
release: clean test lint build-all
	@echo "Creating release archive..."
	mkdir -p $(BUILD_DIR)/release

	# Create archives for each platform
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-amd64-$(VERSION).tar.gz $(BINARY_NAME)-linux-amd64-$(VERSION)
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-arm64-$(VERSION).tar.gz $(BINARY_NAME)-linux-arm64-$(VERSION)
	cd $(BUILD_DIR) && zip -q release/$(BINARY_NAME)-$(VERSION)-windows-amd64-$(VERSION).zip $(BINARY_NAME)-windows-amd64-$(VERSION).exe
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-amd64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-amd64-$(VERSION)
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-arm64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-arm64-$(VERSION)

	# Create checksums
	cd $(BUILD_DIR)/release && sha256sum * > checksums.txt
	@echo "Release created in $(BUILD_DIR)/release/"

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
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make build VERSION=v1.0.0           - Build with version"
	@echo "  make build-linux                     - Cross-compile for Linux"
	@echo "  make build-all                       - Build for all platforms"
