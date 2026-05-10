param(
  [int]$FrontendPort = 5177,
  [string]$Project = "",
  [switch]$UseEmbeddedFrontend
)

$ErrorActionPreference = "Stop"

$frontendDir = Split-Path -Parent $PSScriptRoot
$repoRoot = Resolve-Path (Join-Path $frontendDir "..\..")
$dataDir = Join-Path $repoRoot "build\playwright-data"
$backendOut = Join-Path $repoRoot "build\playwright-backend.out.log"
$backendErr = Join-Path $repoRoot "build\playwright-backend.err.log"
$viteOut = Join-Path $repoRoot "build\playwright-vite.out.log"
$viteErr = Join-Path $repoRoot "build\playwright-vite.err.log"

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

function Wait-HttpOk {
  param(
    [string]$Url,
    [int]$TimeoutSeconds = 45
  )

  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ((Get-Date) -lt $deadline) {
    try {
      $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 3
      if ($response.StatusCode -eq 200) {
        return
      }
    } catch {
      Start-Sleep -Milliseconds 500
    }
  }

  throw "Timed out waiting for $Url"
}

function Stop-ProcessTree {
  param([System.Diagnostics.Process]$Process)
  if ($null -eq $Process) {
    return
  }

  $processId = $Process.Id
  if (-not (Get-Process -Id $processId -ErrorAction SilentlyContinue)) {
    return
  }

  Get-CimInstance Win32_Process -Filter "ParentProcessId = $processId" -ErrorAction SilentlyContinue |
    ForEach-Object {
      $child = Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue
      if ($null -ne $child) {
        Stop-ProcessTree -Process $child
      }
    }

  Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
}

function Stop-PortListener {
  param([int]$Port)

  Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique |
    ForEach-Object {
      Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue
    }
}

function Assert-PortAvailable {
  param([int]$Port)

  $listener = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($null -ne $listener) {
    throw "Port $Port is already in use by process $($listener.OwningProcess)"
  }
}

$backend = $null
$vite = $null

try {
  $backend = Start-Process `
    -FilePath (Join-Path $repoRoot "maxiofs.exe") `
    -ArgumentList @("--data-dir", $dataDir, "--log-level", "warn") `
    -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $backendOut `
    -RedirectStandardError $backendErr `
    -PassThru `
    -WindowStyle Hidden

  Wait-HttpOk -Url "http://localhost:8081/login"

  if ($UseEmbeddedFrontend) {
    $env:PLAYWRIGHT_BASE_URL = "http://localhost:8081"
  } else {
    Assert-PortAvailable -Port $FrontendPort

    $vite = Start-Process `
      -FilePath "npm.cmd" `
      -ArgumentList @("run", "dev", "--", "--host", "127.0.0.1", "--port", "$FrontendPort", "--strictPort") `
      -WorkingDirectory $frontendDir `
      -RedirectStandardOutput $viteOut `
      -RedirectStandardError $viteErr `
      -PassThru `
      -WindowStyle Hidden

    Wait-HttpOk -Url "http://127.0.0.1:$FrontendPort/login"
    $env:PLAYWRIGHT_BASE_URL = "http://127.0.0.1:$FrontendPort"
  }

  Push-Location $frontendDir
  try {
    if ($Project) {
      npm run test:e2e -- --project=$Project
    } else {
      npm run test:e2e
    }
    exit $LASTEXITCODE
  } finally {
    Pop-Location
  }
} finally {
  Stop-ProcessTree -Process $vite
  if (-not $UseEmbeddedFrontend) {
    Stop-PortListener -Port $FrontendPort
  }
  Stop-ProcessTree -Process $backend
}
