# Script para deshabilitar multi-bucket en repositorios S3 de Veeam
# Ejecutar como administrador con Veeam PowerShell snap-in cargado

param(
    [Parameter(Mandatory=$true)]
    [string]$RepositoryName
)

# Cargar Veeam PowerShell snap-in si no está cargado
if ((Get-PSSnapin -Name VeeamPSSnapIn -ErrorAction SilentlyContinue) -eq $null) {
    Add-PSSnapin VeeamPSSnapIn
}

Write-Host "Buscando repositorio: $RepositoryName" -ForegroundColor Cyan

# Obtener el repositorio
$repo = Get-VBRBackupRepository -Name $RepositoryName
if ($null -eq $repo) {
    Write-Host "ERROR: No se encontró el repositorio '$RepositoryName'" -ForegroundColor Red
    exit 1
}

Write-Host "Repositorio encontrado: $($repo.Name)" -ForegroundColor Green
Write-Host "Tipo: $($repo.TypeDisplay)" -ForegroundColor Gray

# Verificar que es un repositorio S3
if ($repo.Type -ne "AmazonS3Compatible") {
    Write-Host "ADVERTENCIA: Este repositorio no es del tipo S3 Compatible" -ForegroundColor Yellow
}

# Mostrar configuración actual
Write-Host "`nConfiguración actual de Multi-Bucket:" -ForegroundColor Cyan
$repo.Options.MultiBucketOptions | Format-List

# Deshabilitar multi-bucket
Write-Host "`nDeshabilitando Multi-Bucket Mode..." -ForegroundColor Yellow

try {
    # Obtener las opciones actuales
    $options = $repo.Options
    
    # Modificar la configuración de multi-bucket
    $options.MultiBucketOptions.IsEnabled = $false
    
    # Aplicar los cambios
    Set-VBRRepositoryOptions -Repository $repo -Options $options
    
    Write-Host "✓ Multi-Bucket Mode deshabilitado correctamente" -ForegroundColor Green
    
    # Verificar los cambios
    Write-Host "`nNueva configuración:" -ForegroundColor Cyan
    $repoUpdated = Get-VBRBackupRepository -Name $RepositoryName
    $repoUpdated.Options.MultiBucketOptions | Format-List
    
    Write-Host "`n✓ Configuración actualizada. Ahora Veeam usará un solo bucket." -ForegroundColor Green
    Write-Host "Todos los backups irán a: $($repo.AmazonS3Folder)" -ForegroundColor Gray
    
} catch {
    Write-Host "ERROR al modificar la configuración: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
