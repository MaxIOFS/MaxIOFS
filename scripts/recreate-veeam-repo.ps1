<#
.SYNOPSIS
    Elimina el repositorio Veeam existente y crea uno nuevo para que respete SOSAPI

.DESCRIPTION
    Este script:
    1. Elimina el repositorio S3 existente en Veeam
    2. Crea un nuevo repositorio S3 con los mismos parámetros
    3. Veeam leerá el SOSAPI durante la creación y respetará IsMultiBucketModeRequired=false

.PARAMETER RepositoryName
    Nombre del nuevo repositorio (por defecto: MaxIOFS-Single)

.PARAMETER ServicePoint
    URL del endpoint S3 de MaxIOFS

.PARAMETER AccessKey
    Access Key del usuario S3

.PARAMETER SecretKey
    Secret Key del usuario S3

.PARAMETER Bucket
    Nombre del bucket S3

.PARAMETER Folder
    Carpeta dentro del bucket para almacenar backups

.PARAMETER OldRepositoryName
    Nombre del repositorio existente a eliminar (opcional)

.EXAMPLE
    .\recreate-veeam-repo.ps1 -RepositoryName "MaxIOFS-Single" -ServicePoint "http://10.200.1.135:8080" -AccessKey "fJoPSQZxRK1VE4NpK8CS" -SecretKey "TU_SECRET_KEY" -Bucket "backups" -Folder "salvas" -OldRepositoryName "ObjectSR"
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$RepositoryName,
    
    [Parameter(Mandatory=$true)]
    [string]$ServicePoint,
    
    [Parameter(Mandatory=$true)]
    [string]$AccessKey,
    
    [Parameter(Mandatory=$true)]
    [string]$SecretKey,
    
    [Parameter(Mandatory=$true)]
    [string]$Bucket,
    
    [Parameter(Mandatory=$true)]
    [string]$Folder,
    
    [Parameter(Mandatory=$false)]
    [string]$OldRepositoryName,
    
    [Parameter(Mandatory=$false)]
    [string]$GatewayServer
)

# Import Veeam PowerShell module
Write-Host "Cargando módulo Veeam Backup & Replication..." -ForegroundColor Cyan
try {
    Add-PSSnapin -Name VeeamPSSnapIn -ErrorAction Stop
} catch {
    Write-Error "No se pudo cargar el módulo Veeam PSSnapIn. ¿Está Veeam B&R instalado?"
    exit 1
}

# Función para eliminar repositorio existente
function Remove-VeeamRepository {
    param([string]$Name)
    
    if ([string]::IsNullOrEmpty($Name)) {
        return
    }
    
    Write-Host "`n=== PASO 1: Eliminando repositorio existente ===" -ForegroundColor Yellow
    
    $repo = Get-VBRBackupRepository -Name $Name -ErrorAction SilentlyContinue
    
    if ($null -eq $repo) {
        Write-Host "El repositorio '$Name' no existe. Continuando..." -ForegroundColor Green
        return
    }
    
    # Verificar si hay backups en el repositorio
    $backups = Get-VBRBackup | Where-Object { $_.RepositoryId -eq $repo.Id }
    
    if ($backups) {
        Write-Warning "El repositorio '$Name' contiene los siguientes backups:"
        $backups | ForEach-Object { Write-Host "  - $($_.Name)" -ForegroundColor Yellow }
        
        $confirm = Read-Host "`n¿ELIMINAR el repositorio y TODOS sus backups? (escribe 'SI' para confirmar)"
        
        if ($confirm -ne "SI") {
            Write-Error "Operación cancelada por el usuario"
            exit 1
        }
        
        # Remover backups primero
        Write-Host "Eliminando backups del repositorio..." -ForegroundColor Cyan
        $backups | ForEach-Object {
            Remove-VBRBackup -Backup $_ -Confirm:$false -ErrorAction Continue
        }
    }
    
    # Eliminar el repositorio
    Write-Host "Eliminando repositorio '$Name'..." -ForegroundColor Cyan
    try {
        Remove-VBRBackupRepository -Repository $repo -Confirm:$false -ErrorAction Stop
        Write-Host "✓ Repositorio eliminado exitosamente" -ForegroundColor Green
    } catch {
        Write-Error "Error al eliminar repositorio: $_"
        exit 1
    }
}

# Función para crear nuevo repositorio
function New-VeeamS3Repository {
    Write-Host "`n=== PASO 2: Creando nuevo repositorio S3 ===" -ForegroundColor Yellow
    
    # Crear cuenta Amazon (credenciales S3)
    Write-Host "Creando credenciales S3..." -ForegroundColor Cyan
    try {
        $account = Add-VBRAmazonAccount -AccessKey $AccessKey -SecretKey $SecretKey -Description "$RepositoryName Credentials" -ErrorAction Stop
        Write-Host "✓ Credenciales creadas" -ForegroundColor Green
    } catch {
        Write-Error "Error al crear credenciales: $_"
        exit 1
    }
    
    # Crear repositorio S3
    Write-Host "Creando repositorio S3..." -ForegroundColor Cyan
    try {
        $repoParams = @{
            AmazonS3Account = $account
            Name = $RepositoryName
            AmazonS3ServicePoint = $ServicePoint
            AmazonS3Region = "us-east-1"
            AmazonS3Bucket = $Bucket
            AmazonS3Folder = $Folder
            ErrorAction = "Stop"
        }
        
        # Agregar GatewayServer si se especificó
        if (-not [string]::IsNullOrEmpty($GatewayServer)) {
            $gateway = Get-VBRServer -Name $GatewayServer -ErrorAction Stop
            $repoParams['GatewayServer'] = $gateway
        }
        
        $repo = Add-VBRAmazonS3Repository @repoParams
        Write-Host "✓ Repositorio creado" -ForegroundColor Green
    } catch {
        Write-Error "Error al crear repositorio: $_"
        # Limpiar cuenta creada
        Remove-VBRAmazonAccount -Account $account -Confirm:$false -ErrorAction SilentlyContinue
        exit 1
    }
    
    return $repo
}

# Función para verificar configuración multi-bucket
function Test-MultiBucketConfiguration {
    param($Repository)
    
    Write-Host "`n=== PASO 3: Verificando configuración multi-bucket ===" -ForegroundColor Yellow
    
    $options = $Repository.Options.MultiBucketOptions
    
    Write-Host "`nConfiguración Multi-Bucket:" -ForegroundColor Cyan
    Write-Host "  IsEnabled: $($options.IsEnabled)" -ForegroundColor $(if ($options.IsEnabled) { "Red" } else { "Green" })
    Write-Host "  BackupsPerBucket: $($options.BackupsPerBucket)" -ForegroundColor Gray
    
    if ($options.IsEnabled) {
        Write-Warning "⚠ Multi-bucket está HABILITADO"
        Write-Warning "Esto significa que Veeam NO respetó el SOSAPI IsMultiBucketModeRequired=false"
        Write-Warning "`nPosibles causas:"
        Write-Warning "  1. El SOSAPI no está accesible en: $ServicePoint/$Bucket/.system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/system.xml"
        Write-Warning "  2. El XML del SOSAPI no está en el formato correcto"
        Write-Warning "  3. MaxIOFS no se está identificando como 'MinIO' en el SOSAPI"
        Write-Warning "`nVerifica el SOSAPI con:"
        Write-Warning "  curl $ServicePoint/$Bucket/.system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/system.xml"
        
        return $false
    } else {
        Write-Host "`n✓✓✓ ¡ÉXITO! Multi-bucket está DESHABILITADO ✓✓✓" -ForegroundColor Green
        Write-Host "Veeam respetó correctamente el SOSAPI de MaxIOFS" -ForegroundColor Green
        return $true
    }
}

# ====================
# EJECUCIÓN PRINCIPAL
# ====================

Write-Host @"
╔═══════════════════════════════════════════════════════════════╗
║  Recreación de Repositorio Veeam S3 para MaxIOFS             ║
║  Este script eliminará y recreará el repositorio para que    ║
║  Veeam respete el SOSAPI y NO habilite multi-bucket         ║
╚═══════════════════════════════════════════════════════════════╝
"@ -ForegroundColor Cyan

Write-Host "`nParámetros de configuración:" -ForegroundColor Cyan
Write-Host "  Repositorio nuevo: $RepositoryName" -ForegroundColor White
Write-Host "  Service Point: $ServicePoint" -ForegroundColor White
Write-Host "  Bucket: $Bucket" -ForegroundColor White
Write-Host "  Folder: $Folder" -ForegroundColor White
Write-Host "  Access Key: $AccessKey" -ForegroundColor White
if ($OldRepositoryName) {
    Write-Host "  Repositorio a eliminar: $OldRepositoryName" -ForegroundColor Yellow
}

Write-Host "`n⚠ ADVERTENCIA: Esta operación eliminará el repositorio existente y todos sus backups" -ForegroundColor Red
$confirm = Read-Host "`n¿Continuar? (S/N)"
if ($confirm -ne "S") {
    Write-Host "Operación cancelada" -ForegroundColor Yellow
    exit 0
}

try {
    # Paso 1: Eliminar repositorio existente (si se especificó)
    if (-not [string]::IsNullOrEmpty($OldRepositoryName)) {
        Remove-VeeamRepository -Name $OldRepositoryName
    }
    
    # Esperar un momento para asegurar limpieza
    Start-Sleep -Seconds 2
    
    # Paso 2: Crear nuevo repositorio
    $newRepo = New-VeeamS3Repository
    
    # Esperar un momento para que Veeam procese la configuración
    Start-Sleep -Seconds 3
    
    # Refrescar objeto del repositorio
    $newRepo = Get-VBRBackupRepository -Name $RepositoryName
    
    # Paso 3: Verificar configuración
    $success = Test-MultiBucketConfiguration -Repository $newRepo
    
    if ($success) {
        Write-Host "`n═══════════════════════════════════════════════" -ForegroundColor Green
        Write-Host "✓ Repositorio creado exitosamente" -ForegroundColor Green
        Write-Host "✓ Multi-bucket DESHABILITADO correctamente" -ForegroundColor Green
        Write-Host "═══════════════════════════════════════════════" -ForegroundColor Green
    } else {
        Write-Host "`n═══════════════════════════════════════════════" -ForegroundColor Red
        Write-Host "✗ Repositorio creado pero multi-bucket sigue HABILITADO" -ForegroundColor Red
        Write-Host "✗ Revisa el SOSAPI de MaxIOFS" -ForegroundColor Red
        Write-Host "═══════════════════════════════════════════════" -ForegroundColor Red
        exit 1
    }
    
} catch {
    Write-Error "Error durante la ejecución: $_"
    exit 1
}
