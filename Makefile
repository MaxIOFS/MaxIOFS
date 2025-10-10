# MaxIOFS Makefile

# Build variables
BINARY_NAME=maxiofs
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)"

# Go variables
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Directories
BUILD_DIR=build
WEB_DIR=web/frontend
DIST_DIR=web/dist

# Default target
.PHONY: all
all: clean build

# Build the complete application
.PHONY: build
build: build-web build-server

# Build the web frontend
.PHONY: build-web
build-web:
	@echo "Building web frontend..."
	@echo "Cleaning previous build..."
	rm -rf $(WEB_DIR)/.next
	rm -rf $(WEB_DIR)/out
	@echo "Installing dependencies..."
	cd $(WEB_DIR) && npm ci
	@echo "Building Next.js static export..."
	cd $(WEB_DIR) && NODE_ENV=production npm run build
	@if [ ! -d "$(WEB_DIR)/out" ]; then \
		echo "Error: Static export directory 'out' was not created!"; \
		exit 1; \
	fi
	@echo "Web frontend built successfully (static export in $(WEB_DIR)/out)"

# Build the Go server
.PHONY: build-server
build-server:
	@echo "Building MaxIOFS server..."
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/maxiofs
	@echo "Server built successfully"

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
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)
	rm -rf $(WEB_DIR)/.next
	rm -rf $(WEB_DIR)/out
	rm -f coverage.out

# Download Go dependencies
.PHONY: deps
deps:
	@echo "Downloading Go dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Build for all platforms
.PHONY: build-all
build-all: clean build-web
	@echo "Building for all platforms..."
	mkdir -p $(BUILD_DIR)

	# Linux AMD64
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/maxiofs

	# Linux ARM64
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/maxiofs

	# Windows AMD64
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/maxiofs

	# macOS AMD64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/maxiofs

	# macOS ARM64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/maxiofs

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
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64
	cd $(BUILD_DIR) && zip -q release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64

	# Create checksums
	cd $(BUILD_DIR)/release && sha256sum * > checksums.txt
	@echo "Release created in $(BUILD_DIR)/release/"

# Help target
.PHONY: help
help:
	@echo "MaxIOFS Build System"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Clean and build everything"
	@echo "  build            - Build web frontend and server"
	@echo "  build-web        - Build only the web frontend"
	@echo "  build-server     - Build only the Go server"
	@echo "  build-all        - Build for all platforms"
	@echo "  dev              - Build development version"
	@echo "  dev-server       - Start development server"
	@echo "  dev-web          - Start web development server"
	@echo "  test             - Run all tests"
	@echo "  test-unit        - Run unit tests only"
	@echo "  test-integration - Run integration tests only"
	@echo "  lint             - Lint code"
	@echo "  fmt              - Format code"
	@echo "  clean            - Clean build artifacts"
	@echo "  deps             - Download dependencies"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Run in Docker"
	@echo "  release          - Create release build"
	@echo "  help             - Show this help message"