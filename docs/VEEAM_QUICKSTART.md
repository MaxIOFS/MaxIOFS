# Guía Rápida: Configurar Veeam con MaxIOFS

## 📋 Pre-requisitos

✅ MaxIOFS instalado y corriendo
✅ Veeam Backup & Replication 11+ instalado
✅ Bucket con Object Lock creado en MaxIOFS
✅ Credenciales AWS (Access Key / Secret Key) configuradas en MaxIOFS

---

## 🚀 Paso 1: Crear Bucket Inmutable en MaxIOFS

### Opción A: Desde Web UI (Recomendado)

1. Accede a `http://localhost:8081`
2. Ve a **Buckets** > **Create Bucket**
3. Configuración:
   - **Name**: `veeam-backups` (o el nombre que prefieras)
   - **Tab Object Lock**:
     - ✅ Enable Object Lock
     - Mode: **COMPLIANCE** (para inmutabilidad total)
     - Retention: **14 days** (o tu política deseada)
4. Click **Create**

### Opción B: Usando cURL

```bash
curl -X POST http://localhost:8080/api/v1/buckets \
  -H "Content-Type: application/json" \
  -d '{
    "name": "veeam-backups",
    "objectLock": {
      "objectLockEnabled": true,
      "rule": {
        "defaultRetention": {
          "mode": "COMPLIANCE",
          "days": 14
        }
      }
    }
  }'
```

---

## 🔧 Paso 2: Configurar Veeam Backup Repository

### 2.1 Agregar Repositorio

1. Abre **Veeam Backup & Replication Console**
2. Navega a: **Backup Infrastructure** > **Backup Repositories**
3. Click derecho > **Add Backup Repository**

### 2.2 Seleccionar Tipo

1. Selecciona: **Object Storage**
2. Selecciona: **S3 Compatible**
3. Click **Next**

### 2.3 Configurar Conexión

**Name**: `MaxIOFS - Immutable Storage` (o tu nombre preferido)

**Account Settings**:
- Service point: `http://YOUR_MAXIOFS_IP:8080`
  - Ejemplo local: `http://localhost:8080`
  - Ejemplo red: `http://192.168.1.100:8080`
- Access Key: (tu Access Key de MaxIOFS)
- Secret Key: (tu Secret Key de MaxIOFS)
- Region: `us-east-1` (o cualquier región, no importa para MaxIOFS)

**Bucket**:
- Selecciona: `veeam-backups` (el bucket que creaste)
- O usa el botón **Browse** para ver buckets disponibles

### 2.4 Configurar Inmutabilidad ⚠️ **IMPORTANTE**

✅ **Marca la opción**: `Make recent backups immutable for X days`

Configuración recomendada:
- **Immutability period**: `14 days` (debe coincidir con la retention del bucket)
- Esta configuración hace que Veeam use los headers de Object Lock

### 2.5 Mount Server

Selecciona un servidor Windows que actuará como gateway:
- Puede ser el mismo servidor donde está Veeam
- Debe tener acceso de red a MaxIOFS

### 2.6 Revisar y Aplicar

1. Revisa la configuración
2. Click **Apply**
3. Veeam hará pruebas de conectividad y validación

---

## ✅ Paso 3: Validar Configuración

### Pruebas que Veeam realizará automáticamente:

1. ✅ **Conectividad**: Puede alcanzar el endpoint
2. ✅ **Autenticación**: Credenciales válidas
3. ✅ **Bucket Access**: Puede leer/escribir en el bucket
4. ✅ **Object Lock**: Verifica que el bucket tenga Object Lock habilitado
5. ✅ **Retention**: Prueba escribir objeto con retention

### Si todo está correcto:

- Verás el repositorio en estado **Ready**
- Aparecerá un ícono de candado 🔒 indicando inmutabilidad

---

## 🧪 Paso 4: Probar con un Backup

### 4.1 Crear Backup Job

1. Ve a **Home** > **Backup & Replication**
2. Click **Backup Job** > **Virtual Machine** (o el tipo que necesites)
3. Configura el job normalmente
4. En **Storage**:
   - Selecciona el repositorio `MaxIOFS - Immutable Storage`
5. Configura retention points según necesites

### 4.2 Ejecutar Backup

1. Click derecho en el job > **Start**
2. Monitorea el progreso
3. Verifica que complete exitosamente

### 4.3 Validar Inmutabilidad

**Desde Veeam:**
1. Ve al backup creado
2. Click derecho > **Delete from disk**
3. ❌ **Debería fallar** con mensaje: `Cannot delete immutable backup`

**Desde MaxIOFS UI:**
1. Ve a `http://localhost:8081/buckets/veeam-backups`
2. Verás los archivos del backup
3. En la columna **Retention**:
   - ✅ Verás "Expira en X días"
   - 🔒 Badge "WORM" visible

**Desde AWS CLI:**
```bash
# Intentar borrar un archivo del backup
aws s3 rm s3://veeam-backups/backup-file.vbk \
  --endpoint-url http://localhost:8080

# Debería devolver error 403:
# An error occurred (AccessDenied): Object cannot be deleted. 
# Retention period until: 2025-10-20 14:30:00
```

---

## 🔍 Troubleshooting

### Problema: "Cannot connect to service point"

**Causa**: Veeam no puede alcanzar MaxIOFS

**Solución**:
1. Verifica que MaxIOFS esté corriendo: `http://localhost:8080/health`
2. Si MaxIOFS está en otro servidor, verifica firewall
3. Prueba desde el mount server: `curl http://YOUR_IP:8080/health`

---

### Problema: "Authentication failed"

**Causa**: Credenciales incorrectas

**Solución**:
1. Verifica Access Key y Secret Key en MaxIOFS
2. Asegúrate de copiarlas correctamente (sin espacios)
3. Prueba con AWS CLI primero:
   ```bash
   aws configure set aws_access_key_id YOUR_KEY
   aws configure set aws_secret_access_key YOUR_SECRET
   aws s3 ls --endpoint-url http://localhost:8080
   ```

---

### Problema: "Object Lock not enabled"

**Causa**: El bucket no tiene Object Lock habilitado

**Solución**:
1. Verifica en MaxIOFS UI que el bucket tenga badge "WORM"
2. Si no lo tiene, debes **recrear el bucket** (Object Lock solo se puede habilitar en creación)
3. O crea un nuevo bucket con Object Lock habilitado

---

### Problema: "Immutability validation failed"

**Causa**: MaxIOFS no está aplicando retention correctamente

**Solución**:
1. Verifica que MaxIOFS sea la versión más reciente con soporte Veeam
2. Revisa logs de MaxIOFS: busca "Applied Object Lock retention from headers"
3. Prueba manualmente con curl:
   ```bash
   # Subir archivo con retention
   curl -X PUT http://localhost:8080/veeam-backups/test.txt \
     -H "x-amz-object-lock-mode: COMPLIANCE" \
     -H "x-amz-object-lock-retain-until-date: 2025-10-20T00:00:00Z" \
     -d "test data"
   
   # Verificar que tiene retention
   curl -I http://localhost:8080/veeam-backups/test.txt
   # Debería incluir headers:
   # x-amz-object-lock-mode: COMPLIANCE
   # x-amz-object-lock-retain-until-date: 2025-10-20T00:00:00Z
   ```

---

### Problema: Veeam puede borrar backups antes de expirar

**Causa**: Modo GOVERNANCE en lugar de COMPLIANCE

**Solución**:
1. GOVERNANCE permite bypass con permisos especiales
2. Usa **COMPLIANCE** para inmutabilidad total
3. Recrea el bucket con COMPLIANCE mode

---

## 📊 Monitoreo

### Logs de MaxIOFS

Busca estas entradas en los logs:

```
INFO: Applied Object Lock retention from headers - bucket: veeam-backups, object: backup.vbk, mode: COMPLIANCE, until: 2025-10-20
INFO: Returning Object Lock configuration - bucket: veeam-backups, enabled: Enabled, hasRule: true
INFO: Object deletion blocked by retention - bucket: veeam-backups, object: backup.vbk, expires: 2025-10-20
```

### Métricas en MaxIOFS UI

1. Ve a `http://localhost:8081/metrics`
2. Verifica:
   - **API Requests**: Deberías ver PUTs exitosos
   - **Object Lock Operations**: Contador de aplicaciones de retention
   - **Blocked Deletes**: Intentos de borrado bloqueados

---

## 🎯 Configuración Recomendada para Producción

### Retention Periods

| Tipo de Backup | Retention Recomendada |
|----------------|----------------------|
| Daily Backups  | 14-30 días          |
| Weekly Backups | 60-90 días          |
| Monthly Backups| 365 días (1 año)    |
| Archival       | 2555 días (7 años)  |

### Modo de Object Lock

- **COMPLIANCE**: Para cumplimiento regulatorio (recomendado para producción)
  - Nadie puede borrar o modificar, ni siquiera root/admin
  - Solo se puede borrar después de expirar
  
- **GOVERNANCE**: Para flexibilidad operacional
  - Se puede borrar con permisos especiales
  - Útil para testing o ambientes de desarrollo

### Arquitectura de Buckets

**Opción 1: Un bucket por tipo de backup**
```
veeam-daily-backups     (14 days retention)
veeam-weekly-backups    (90 days retention)
veeam-monthly-backups   (365 days retention)
```

**Opción 2: Un bucket con prefijos**
```
veeam-backups/daily/    (objeto con 14 days retention)
veeam-backups/weekly/   (objeto con 90 days retention)
veeam-backups/monthly/  (objeto con 365 days retention)
```

---

## 🔐 Security Checklist

Antes de usar en producción:

- [ ] MaxIOFS corre en HTTPS (no HTTP)
- [ ] Credenciales únicas por cliente/aplicación
- [ ] Firewall configurado (solo permitir Veeam server → MaxIOFS)
- [ ] Bucket con Object Lock habilitado
- [ ] Modo COMPLIANCE para backups críticos
- [ ] Retention period alineado con políticas de la empresa
- [ ] Logs habilitados y monitoreados
- [ ] Backups del storage de MaxIOFS (para disaster recovery)
- [ ] Testing regular de restore

---

## 📚 Referencias Adicionales

- [Documentación completa de compatibilidad](./VEEAM_COMPATIBILITY.md)
- [Arquitectura de MaxIOFS](./ARCHITECTURE.md)
- [Veeam Best Practices - Immutable Backup](https://www.veeam.com/blog/how-to-configure-immutable-backup-repository.html)
- [AWS S3 Object Lock Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)

---

## 🎉 ¡Listo!

Ahora tienes:
- ✅ Repositorio S3 inmutable on-premise
- ✅ Backups protegidos contra ransomware
- ✅ Cumplimiento de políticas de retención
- ✅ Compatible con Veeam Backup & Replication
- ✅ Sin dependencia de cloud providers externos

**Siguiente paso**: Configura tu primer backup job y valida que la inmutabilidad funcione correctamente.
