#!/usr/bin/env pwsh
# MaxIOFS Docker Build Script
# This script sets up environment variables for the Docker build

param(
    [string]$ProfileName = "",
    [switch]$NoBuild,
    [switch]$Up,
    [switch]$Down,
    [switch]$Clean
)

# Colors for output
function Write-Info { Write-Host $args -ForegroundColor Cyan }
function Write-Success { Write-Host $args -ForegroundColor Green }
function Write-Error { Write-Host $args -ForegroundColor Red }

# Get version from go.mod or default
$VERSION = "0.3.1-beta"
if (Test-Path "go.mod") {
    $goMod = Get-Content "go.mod" -Raw
    if ($goMod -match 'module.*v(\d+\.\d+\.\d+)') {
        $VERSION = $matches[1]
    }
}

# Get git commit hash
$GIT_COMMIT = "unknown"
try {
    $GIT_COMMIT = git rev-parse --short HEAD 2>$null
    if (-not $GIT_COMMIT) {
        $GIT_COMMIT = "dev"
    }
} catch {
    $GIT_COMMIT = "dev"
}

# Get build date in ISO 8601 format
$BUILD_DATE = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

# Check if working directory is clean
$GIT_STATUS = ""
try {
    $GIT_STATUS = git status --porcelain 2>$null
    if ($GIT_STATUS) {
        $GIT_COMMIT = "${GIT_COMMIT}"
    }
} catch {
    # Git not available or not a git repo
}

# Export environment variables
$env:VERSION = $VERSION
$env:GIT_COMMIT = $GIT_COMMIT
$env:BUILD_DATE = $BUILD_DATE

Write-Info "==================================="
Write-Info "MaxIOFS Docker Build"
Write-Info "==================================="
Write-Info "Version:    $VERSION"
Write-Info "Commit:     $GIT_COMMIT"
Write-Info "Build Date: $BUILD_DATE"
Write-Info "==================================="
Write-Host ""

# Handle different operations
if ($Clean) {
    Write-Info "Cleaning up Docker resources..."
    docker-compose down -v
    docker rmi maxiofs:$VERSION -f 2>$null
    Write-Success "Cleanup complete!"
    exit 0
}

if ($Down) {
    Write-Info "Stopping services..."
    if ($ProfileName) {
        docker-compose --profile $ProfileName down
    } else {
        docker-compose down
    }
    Write-Success "Services stopped!"
    exit 0
}

# Build the image
if (-not $NoBuild) {
    Write-Info "Building Docker image..."
    $buildArgs = @(
        "--build-arg", "VERSION=$VERSION",
        "--build-arg", "COMMIT=$GIT_COMMIT",
        "--build-arg", "BUILD_DATE=$BUILD_DATE"
    )
    
    docker-compose build @buildArgs
    
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Build failed!"
        exit 1
    }
    
    Write-Success "Build complete!"
    Write-Host ""
}

# Start services if requested
if ($Up) {
    Write-Info "Starting services..."
    
    $composeArgs = @("up", "-d")
    
    if ($ProfileName) {
        $composeArgs = @("--profile", $ProfileName) + $composeArgs
        Write-Info "Using profile: $ProfileName"
    }
    
    docker-compose @composeArgs
    
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to start services!"
        exit 1
    }
    
    Write-Host ""
    Write-Success "Services started!"
    Write-Host ""
    Write-Info "Access the application at:"
    Write-Host "  - Web Console: http://localhost:8081" -ForegroundColor Yellow
    Write-Host "  - S3 API:      http://localhost:8080" -ForegroundColor Yellow
    
    if ($ProfileName -eq "monitoring") {
        Write-Host "  - Prometheus:  http://localhost:9091" -ForegroundColor Yellow
        Write-Host "  - Grafana:     http://localhost:3000 (admin/admin)" -ForegroundColor Yellow
    }
    
    Write-Host ""
    Write-Info "View logs with: docker-compose logs -f"
}

Write-Host ""
Write-Success "Done!"
