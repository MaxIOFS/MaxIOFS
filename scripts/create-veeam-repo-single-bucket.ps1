# Script para crear repositorio S3 en Veeam con Multi-Bucket DESHABILITADO
# Ejecutar como administrador en el servidor de Veeam

param(
    [Parameter(Mandatory=$true)]
    [string]$RepositoryName,
    
    [Parameter(Mandatory=$true)]
    [string]$ServicePoint,  # Ejemplo: http://10.200.1.135:8080
    
    [Parameter(Mandatory=$true)]
    [string]$AccessKey,
    
    [Parameter(Mandatory=$true)]
    [string]$SecretKey,
    
    [Parameter(Mandatory=$true)]
    [string]$Bucket,
    
    [string]$Folder = "",
    
    [string]$GatewayServer = $null  # Opcional: nombre del servidor gateway
)

# Cargar Veeam PowerShell snap-in
if ((Get-PSSnapin -Name VeeamPSSnapIn -ErrorAction SilentlyContinue) -eq $null) {
    Add-PSSnapin VeeamPSSnapIn
}

Write-Host "Creando repositorio S3 Compatible: $RepositoryName" -ForegroundColor Cyan
Write-Host "Endpoint: $ServicePoint" -ForegroundColor Gray
Write-Host "Bucket: $Bucket" -ForegroundColor Gray
Write-Host "Folder: $Folder" -ForegroundColor Gray

try {
    # Crear credenciales
    Write-Host "`nCreando credenciales..." -ForegroundColor Yellow
    $securePassword = ConvertTo-SecureString $SecretKey -AsPlainText -Force
    $creds = Add-VBRAmazonAccount -AccessKey $AccessKey -SecretKey $securePassword -Description "$RepositoryName Credentials"
    Write-Host "✓ Credenciales creadas" -ForegroundColor Green
    
    # Obtener servidor gateway (opcional)
    $gateway = $null
    if ($GatewayServer) {
        $gateway = Get-VBRServer -Name $GatewayServer
        if ($null -eq $gateway) {
            Write-Host "ADVERTENCIA: No se encontró el servidor gateway '$GatewayServer', se usará conexión directa" -ForegroundColor Yellow
        } else {
            Write-Host "✓ Servidor gateway: $($gateway.Name)" -ForegroundColor Green
        }
    }
    
    # Crear repositorio
    Write-Host "`nCreando repositorio..." -ForegroundColor Yellow
    
    $repoParams = @{
        AmazonS3Account = $creds
        Name = $RepositoryName
        AmazonS3ServicePoint = $ServicePoint
        AmazonS3Bucket = $Bucket
        AmazonS3Folder = $Folder
        AmazonS3RegionType = "Other"
        AmazonS3CustomRegion = "us-east-1"
    }
    
    if ($gateway) {
        $repoParams.Add("Gateway", $gateway)
    }
    
    $repo = Add-VBRAmazonS3Repository @repoParams
    Write-Host "✓ Repositorio creado: $($repo.Name)" -ForegroundColor Green
    
    # CRÍTICO: Deshabilitar multi-bucket INMEDIATAMENTE
    Write-Host "`nDeshabilitando Multi-Bucket Mode..." -ForegroundColor Yellow
    $options = $repo.Options
    $options.MultiBucketOptions.IsEnabled = $false
    Set-VBRRepositoryOptions -Repository $repo -Options $options
    Write-Host "✓ Multi-Bucket Mode DESHABILITADO" -ForegroundColor Green
    
    # Verificar configuración final
    Write-Host "`nConfiguración final del repositorio:" -ForegroundColor Cyan
    $repoFinal = Get-VBRBackupRepository -Name $RepositoryName
    Write-Host "Nombre: $($repoFinal.Name)" -ForegroundColor White
    Write-Host "Tipo: $($repoFinal.TypeDisplay)" -ForegroundColor White
    Write-Host "Bucket: $Bucket" -ForegroundColor White
    Write-Host "Folder: $Folder" -ForegroundColor White
    
    Write-Host "`nMulti-Bucket Options:" -ForegroundColor Cyan
    $repoFinal.Options.MultiBucketOptions | Format-List
    
    if ($repoFinal.Options.MultiBucketOptions.IsEnabled -eq $false) {
        Write-Host "`n✓✓✓ ÉXITO: Repositorio creado con Multi-Bucket DESHABILITADO ✓✓✓" -ForegroundColor Green
    } else {
        Write-Host "`n⚠ ADVERTENCIA: Multi-Bucket sigue habilitado, intente deshabilitar manualmente" -ForegroundColor Yellow
    }
    
} catch {
    Write-Host "`nERROR: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host $_.ScriptStackTrace -ForegroundColor Red
    exit 1
}
