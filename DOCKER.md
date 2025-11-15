# MaxIOFS Docker Compose

Este archivo proporciona la configuración de Docker Compose para ejecutar MaxIOFS con servicios opcionales de monitoreo.

## Inicio Rápido

### Usando el Script de Build (Recomendado)

El script `docker-build.ps1` configura automáticamente la versión, commit hash y fecha de build:

```powershell
# Solo build
.\docker-build.ps1

# Build y levantar servicios
.\docker-build.ps1 -Up

# Build y levantar con monitoreo
.\docker-build.ps1 -ProfileName monitoring -Up

# Levantar sin rebuild
.\docker-build.ps1 -NoBuild -Up

# Detener servicios
.\docker-build.ps1 -Down

# Limpiar todo
.\docker-build.ps1 -Clean
```

### Usando Make

```bash
# Build de la imagen
make docker-build

# Build y start
make docker-run

# Start servicios (sin rebuild)
make docker-up

# Stop servicios
make docker-down

# Ver logs
make docker-logs

# Start con monitoreo (Prometheus + Grafana)
make docker-monitoring

# Limpiar
make docker-clean
```

### Docker Compose Manual

Si prefieres usar docker-compose directamente, primero configura las variables de entorno:

```powershell
# PowerShell
$env:VERSION = "0.4.0-beta"
$env:GIT_COMMIT = (git rev-parse --short HEAD)
$env:BUILD_DATE = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

docker-compose build
docker-compose up -d
```

```bash
# Linux/macOS
export VERSION="0.4.0-beta"
export GIT_COMMIT=$(git rev-parse --short HEAD)
export BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

docker-compose build
docker-compose up -d
```

## Servicios

### MaxIOFS (Principal)
- **Puerto S3 API**: 8080
- **Puerto Web Console**: 8081
- **Health Check**: http://localhost:8081/api/v1/health
- **Volumen de datos**: `maxiofs-data`

### Prometheus (Opcional - perfil monitoring)
- **Puerto**: 9091
- **URL**: http://localhost:9091
- **Configuración**: `docker/prometheus.yml`

### Grafana (Opcional - perfil monitoring)
- **Puerto**: 3000
- **URL**: http://localhost:3000
- **Usuario**: admin
- **Password**: admin (cambiar en producción)

## Configuración

### Variables de Entorno

Las principales variables están definidas en el `docker-compose.yaml`:

```yaml
MAXIOFS_PUBLIC_API_URL: "http://localhost:8080"      # URL pública para S3 API
MAXIOFS_PUBLIC_CONSOLE_URL: "http://localhost:8081"  # URL pública para Web Console
MAXIOFS_JWT_SECRET: "change-this-secret-key"         # ⚠️ CAMBIAR EN PRODUCCIÓN
```

### Archivo de Configuración

Para usar un archivo `config.yaml` personalizado:

1. Copia el archivo de ejemplo:
```bash
cp config.example.yaml config.yaml
```

2. Edita `config.yaml` con tu configuración

3. En `docker-compose.yaml`, descomenta la línea:
```yaml
volumes:
  - maxiofs-data:/data
  # - ./config.yaml:/app/config.yaml:ro  # <-- Quitar el comentario
```

**Nota para Windows**: Docker Desktop en Windows puede tener problemas compartiendo archivos individuales. Si te sale un error de "user declined directory sharing", usa las variables de entorno en lugar del archivo config.yaml.

## Construcción de la Imagen

### Build con versión específica:
```bash
docker-compose build --build-arg VERSION=0.4.0-beta
```

### Build sin cache:
```bash
docker-compose build --no-cache
```

## Comandos Útiles

### Ver logs en tiempo real:
```bash
docker-compose logs -f maxiofs
```

### Reiniciar servicio:
```bash
docker-compose restart maxiofs
```

### Detener y eliminar contenedores:
```bash
docker-compose down
```

### Detener y eliminar contenedores + volúmenes:
```bash
docker-compose down -v
```

## Volúmenes

- **maxiofs-data**: Almacena buckets, objetos y metadatos de BadgerDB/SQLite
- **prometheus-data**: Base de datos de métricas de Prometheus
- **grafana-data**: Configuración y dashboards de Grafana

## Producción

### Recomendaciones de seguridad:

1. **Cambiar JWT Secret**:
```yaml
MAXIOFS_JWT_SECRET: "usar-secreto-largo-y-aleatorio-aqui"
```

2. **Usar HTTPS con proxy reverso** (Nginx/Traefik):
```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.maxiofs.rule=Host(`s3.midominio.com`)"
  - "traefik.http.routers.maxiofs.tls=true"
```

3. **Backup de volúmenes**:
```bash
docker run --rm -v maxiofs-data:/data -v $(pwd):/backup alpine tar czf /backup/maxiofs-backup.tar.gz -C /data .
```

4. **Límites de recursos**:
```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 2G
    reservations:
      cpus: '1'
      memory: 1G
```

## Acceso Inicial

1. Acceder a la Web Console: http://localhost:8081
2. Crear usuario administrador desde la UI
3. Configurar tenants y buckets según necesidad

## Troubleshooting

### Error "user declined directory sharing" en Windows

Este es un problema conocido de Docker Desktop en Windows con archivos individuales. **Soluciones:**

1. **Usar variables de entorno** (recomendado):
   - La configuración ya usa variables de entorno en `docker-compose.yaml`
   - No necesitas montar `config.yaml`
   - Edita las variables en la sección `environment:` del compose

2. **Si necesitas config.yaml**:
   - Crea un directorio: `mkdir docker/config`
   - Mueve tu config: `copy config.yaml docker/config/`
   - Cambia el volumen a: `- ./docker/config:/app/config:ro`
   - Dentro del contenedor estará en `/app/config/config.yaml`

3. **Compartir el directorio en Docker Desktop**:
   - Settings → Resources → File Sharing
   - Agrega `C:\Users\aricardo\Projects\MaxIOFS`
   - Reinicia Docker Desktop

### Ver estado de los servicios:
```bash
docker-compose ps
```

### Verificar health check:
```bash
curl http://localhost:8081/api/v1/health
```

### Inspeccionar volúmenes:
```bash
docker volume inspect maxiofs-data
```

### Ver logs de errores:
```bash
docker-compose logs --tail=100 maxiofs | grep -i error
```
