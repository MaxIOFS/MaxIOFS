# Ejemplos Prácticos de Testing - Veeam Compatibility

## 🧪 Tests Manuales con cURL

### Test 1: Subir objeto con retention usando headers

```bash
# Crear archivo de prueba
echo "Test backup data" > test-backup.vbk

# Calcular fecha de retención (14 días desde ahora)
RETAIN_UNTIL=$(date -u -d "+14 days" +"%Y-%m-%dT%H:%M:%SZ")

# Subir con headers de Object Lock
curl -X PUT "http://localhost:8080/veeam-test/test-backup.vbk" \
  -H "x-amz-object-lock-mode: COMPLIANCE" \
  -H "x-amz-object-lock-retain-until-date: ${RETAIN_UNTIL}" \
  --data-binary "@test-backup.vbk" \
  -v

# Resultado esperado: 200 OK
```

### Test 2: Verificar retention con HEAD

```bash
# Obtener headers del objeto
curl -I "http://localhost:8080/veeam-test/test-backup.vbk"

# Headers esperados en respuesta:
# HTTP/1.1 200 OK
# x-amz-object-lock-mode: COMPLIANCE
# x-amz-object-lock-retain-until-date: 2025-10-17T14:30:00Z
# Content-Length: 16
# ETag: "..."
```

### Test 3: Verificar retention con GET

```bash
# Descargar objeto y ver headers
curl -v "http://localhost:8080/veeam-test/test-backup.vbk"

# Headers esperados (mismos que HEAD) + body del archivo
```

### Test 4: Intentar borrar objeto con retention activa

```bash
# Intentar borrar
curl -X DELETE "http://localhost:8080/veeam-test/test-backup.vbk" -v

# Resultado esperado: 403 Access Denied
# <?xml version="1.0"?>
# <Error>
#   <Code>AccessDenied</Code>
#   <Message>Object cannot be deleted. Retention period until: 2025-10-17 14:30:00</Message>
# </Error>
```

### Test 5: Obtener Object Lock Configuration

```bash
# Obtener configuración del bucket
curl "http://localhost:8080/veeam-test?object-lock"

# Resultado esperado:
# <?xml version="1.0"?>
# <ObjectLockConfiguration>
#   <ObjectLockEnabled>Enabled</ObjectLockEnabled>
#   <Rule>
#     <DefaultRetention>
#       <Mode>COMPLIANCE</Mode>
#       <Days>14</Days>
#     </DefaultRetention>
#   </Rule>
# </ObjectLockConfiguration>
```

---

## 🔐 Tests con AWS CLI

### Configurar AWS CLI

```bash
# Configurar credenciales (usar las de MaxIOFS)
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws configure set region us-east-1
```

### Test 1: Listar buckets

```bash
aws s3 ls --endpoint-url http://localhost:8080

# Resultado esperado:
# 2025-10-03 10:00:00 veeam-test
# 2025-10-03 10:05:00 worm
```

### Test 2: Subir objeto sin retention específica (usa default del bucket)

```bash
echo "Test backup" > backup-daily.vbk

aws s3 cp backup-daily.vbk s3://veeam-test/backups/daily/backup-daily.vbk \
  --endpoint-url http://localhost:8080

# MaxIOFS aplicará retention por defecto del bucket automáticamente
```

### Test 3: Obtener Object Lock Configuration

```bash
aws s3api get-object-lock-configuration \
  --bucket veeam-test \
  --endpoint-url http://localhost:8080

# Resultado esperado:
# {
#     "ObjectLockConfiguration": {
#         "ObjectLockEnabled": "Enabled",
#         "Rule": {
#             "DefaultRetention": {
#                 "Mode": "COMPLIANCE",
#                 "Days": 14
#             }
#         }
#     }
# }
```

### Test 4: Obtener retention de un objeto específico

```bash
aws s3api get-object-retention \
  --bucket veeam-test \
  --key backups/daily/backup-daily.vbk \
  --endpoint-url http://localhost:8080

# Resultado esperado:
# {
#     "Retention": {
#         "Mode": "COMPLIANCE",
#         "RetainUntilDate": "2025-10-17T14:30:00Z"
#     }
# }
```

### Test 5: Intentar borrar objeto con retention

```bash
aws s3 rm s3://veeam-test/backups/daily/backup-daily.vbk \
  --endpoint-url http://localhost:8080

# Resultado esperado:
# delete failed: s3://veeam-test/backups/daily/backup-daily.vbk 
# An error occurred (AccessDenied) when calling the DeleteObject operation: 
# Object cannot be deleted. Retention period until: 2025-10-17 14:30:00
```

### Test 6: Establecer retention en objeto existente

```bash
# Calcular fecha (30 días desde ahora)
RETAIN_DATE=$(date -u -d "+30 days" +"%Y-%m-%dT%H:%M:%SZ")

aws s3api put-object-retention \
  --bucket veeam-test \
  --key backups/daily/backup-daily.vbk \
  --retention '{
    "Mode": "COMPLIANCE",
    "RetainUntilDate": "'${RETAIN_DATE}'"
  }' \
  --endpoint-url http://localhost:8080

# Resultado esperado: Éxito (si el objeto no tiene retention previa o es GOVERNANCE)
```

---

## 💻 Tests desde PowerShell

### Test 1: Crear bucket con Object Lock

```powershell
$bucketConfig = @{
    name = "veeam-production"
    objectLock = @{
        objectLockEnabled = $true
        rule = @{
            defaultRetention = @{
                mode = "COMPLIANCE"
                days = 30
            }
        }
    }
} | ConvertTo-Json -Depth 10

Invoke-RestMethod -Uri "http://localhost:8080/api/v1/buckets" `
    -Method POST `
    -Body $bucketConfig `
    -ContentType "application/json"
```

### Test 2: Listar objetos con retention

```powershell
$objects = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/buckets/veeam-test/objects" `
    -Method GET

# Ver objetos con retention
$objects.objects | ForEach-Object {
    Write-Host "Object: $($_.key)"
    Write-Host "  Retention Mode: $($_.retention.mode)"
    Write-Host "  Retain Until: $($_.retention.retainUntilDate)"
    Write-Host ""
}
```

### Test 3: Verificar estado de bucket

```powershell
$bucket = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/buckets/veeam-test" `
    -Method GET

Write-Host "Bucket: $($bucket.name)"
Write-Host "Object Lock Enabled: $($bucket.objectLock.objectLockEnabled)"
Write-Host "Default Retention Mode: $($bucket.objectLock.rule.defaultRetention.mode)"
Write-Host "Default Retention Days: $($bucket.objectLock.rule.defaultRetention.days)"
```

---

## 🎮 Tests con Veeam Backup & Replication

### Test 1: Agregar Repositorio

1. **Abrir Veeam B&R Console**

2. **Navegar a repositorios**:
   - Backup Infrastructure → Backup Repositories

3. **Agregar nuevo repositorio**:
   - Add Backup Repository → Object Storage → S3 Compatible

4. **Configurar conexión**:
   ```
   Name: MaxIOFS Immutable
   Service Point: http://YOUR_SERVER_IP:8080
   Access Key: [tu access key]
   Secret Key: [tu secret key]
   ```

5. **Seleccionar bucket**:
   - Click "Browse"
   - Debe aparecer: veeam-test
   - Seleccionar

6. **Configurar inmutabilidad**:
   - ✅ Make recent backups immutable for: 14 days

7. **Seleccionar mount server**

8. **Aplicar y esperar validación**

**Resultado esperado**: ✅ Repository added successfully

### Test 2: Crear Backup Job

1. **Crear job de prueba**:
   - Home → Backup & Replication
   - Backup Job → VMware vSphere (o Hyper-V)

2. **Configurar job**:
   ```
   Name: Test Immutable Backup
   Objects: [Seleccionar una VM pequeña de test]
   Storage: MaxIOFS Immutable
   Retention: 7 restore points
   Schedule: Daily at 10 PM
   ```

3. **Ejecutar job manualmente**:
   - Click derecho en job → Start

4. **Monitorear ejecución**:
   - Debe completar exitosamente
   - Verificar que aparezca en MaxIOFS UI

**Logs esperados en MaxIOFS**:
```
INFO: Applied Object Lock retention from headers
  - bucket: veeam-test
  - object: Test-Immutable-Backup-2025-10-03-Full.vbk
  - mode: COMPLIANCE
  - until: 2025-10-17
```

### Test 3: Validar Inmutabilidad

1. **Desde Veeam Console**:
   - Ve al backup reciente
   - Click derecho → Delete from disk
   - **Esperado**: ❌ Error - "Cannot delete immutable backup"

2. **Desde MaxIOFS UI**:
   ```
   http://localhost:8081/buckets/veeam-test
   ```
   - Deberías ver el archivo .vbk
   - Columna "Retention": ✅ "Expira en 14 días"
   - Badge: 🔒 WORM

3. **Intentar desde AWS CLI**:
   ```bash
   aws s3 rm s3://veeam-test/Test-Immutable-Backup-2025-10-03-Full.vbk \
     --endpoint-url http://localhost:8080
   ```
   - **Esperado**: ❌ AccessDenied error

### Test 4: Restore

1. **Verificar restore funciona**:
   - En Veeam: Click derecho en backup → Restore
   - Restaurar VM completa o archivos
   - **Esperado**: ✅ Restore exitoso

2. **Confirmar objeto no se modificó**:
   - Verificar en MaxIOFS UI que el backup sigue intacto
   - ETag no cambió
   - Retention date no cambió

### Test 5: Esperar Expiración

**Nota**: Solo para testing con retention corta (ej: 1 día)

1. **Después de que expire la retention**:
   ```bash
   # Intentar borrar nuevamente
   aws s3 rm s3://veeam-test/expired-backup.vbk \
     --endpoint-url http://localhost:8080
   ```
   - **Esperado**: ✅ Borrado exitoso (ya expiró)

2. **Desde Veeam**:
   - Delete from disk debería funcionar ahora

---

## 📊 Validación de Logs

### Logs de MaxIOFS a Monitorear

Durante testing, busca estas líneas en los logs:

```bash
# Cuando Veeam sube un backup
INFO: S3 API: PutObject - bucket: veeam-test, object: backup.vbk
INFO: Applied Object Lock retention from headers
  - bucket: veeam-test
  - object: backup.vbk
  - mode: COMPLIANCE
  - until: 2025-10-17 14:30:00

# Cuando Veeam verifica el objeto
INFO: S3 API: HeadObject - bucket: veeam-test, object: backup.vbk

# Cuando Veeam verifica Object Lock config
INFO: S3 API: GetObjectLockConfiguration - bucket: veeam-test
INFO: Returning Object Lock configuration
  - bucket: veeam-test
  - enabled: Enabled
  - hasRule: true

# Cuando se intenta borrar (bloqueado)
INFO: S3 API: DeleteObject - bucket: veeam-test, object: backup.vbk
INFO: Object deletion blocked by retention
  - bucket: veeam-test
  - object: backup.vbk
  - expires: 2025-10-17 14:30:00
```

### Logs de Veeam a Monitorear

En Veeam Job logs, busca:

```
[INFO] Connecting to S3 compatible storage: http://your-server:8080
[INFO] Validating Object Lock configuration
[INFO] Object Lock enabled: COMPLIANCE mode, 14 days retention
[INFO] Uploading backup file with immutability enabled
[INFO] Successfully applied retention to backup file
[INFO] Backup completed and protected until: 2025-10-17
```

---

## ✅ Checklist de Validación Completa

Antes de usar en producción, verifica:

- [ ] MaxIOFS corre sin errores
- [ ] Bucket con Object Lock creado exitosamente
- [ ] GetObjectLockConfiguration devuelve XML correcto
- [ ] Subida con headers aplica retention correctamente
- [ ] HEAD Object devuelve headers de retention
- [ ] GET Object devuelve headers de retention
- [ ] DELETE Object bloqueado durante retention
- [ ] DELETE Object funciona después de expirar
- [ ] Veeam repositorio agregado exitosamente
- [ ] Veeam backup job completa exitosamente
- [ ] Veeam no puede borrar backup antes de expirar
- [ ] Veeam restore funciona correctamente
- [ ] MaxIOFS UI muestra retention correctamente
- [ ] Logs de MaxIOFS sin errores
- [ ] Logs de Veeam sin errores

---

## 🚨 Troubleshooting Common Issues

### Issue: "Failed to set retention from headers"

**Causa**: Bucket no tiene Object Lock habilitado

**Solución**:
```bash
# Verificar bucket
curl "http://localhost:8080/api/v1/buckets/veeam-test"

# Si objectLockEnabled = false, recrear bucket con Object Lock
```

### Issue: Headers de retention no aparecen en respuesta

**Causa**: Objeto no tiene retention aplicada

**Solución**:
```bash
# Verificar retention del objeto
aws s3api get-object-retention \
  --bucket veeam-test \
  --key backup.vbk \
  --endpoint-url http://localhost:8080

# Si no tiene, aplicarla manualmente
aws s3api put-object-retention \
  --bucket veeam-test \
  --key backup.vbk \
  --retention '{"Mode":"COMPLIANCE","RetainUntilDate":"2025-10-17T00:00:00Z"}' \
  --endpoint-url http://localhost:8080
```

### Issue: Veeam says "Object Lock not supported"

**Causa**: GetObjectLockConfiguration falla o devuelve error

**Solución**:
```bash
# Test manual
curl "http://localhost:8080/veeam-test?object-lock"

# Debe devolver XML con ObjectLockEnabled=Enabled
# Si falla, verificar que MaxIOFS sea la versión con Veeam support
```

---

## 📈 Performance Testing

### Test de carga con múltiples uploads

```bash
#!/bin/bash
# upload_test.sh

BUCKET="veeam-test"
ENDPOINT="http://localhost:8080"
RETAIN_UNTIL=$(date -u -d "+14 days" +"%Y-%m-%dT%H:%M:%SZ")

for i in {1..100}; do
  echo "Upload $i" > "test-file-$i.vbk"
  
  curl -X PUT "${ENDPOINT}/${BUCKET}/test-file-$i.vbk" \
    -H "x-amz-object-lock-mode: COMPLIANCE" \
    -H "x-amz-object-lock-retain-until-date: ${RETAIN_UNTIL}" \
    --data-binary "@test-file-$i.vbk" \
    --silent --show-error \
    --write-out "%{http_code}\n"
  
  rm "test-file-$i.vbk"
done

# Verificar que todos sean 200 OK
```

### Test de concurrencia

```bash
# Ejecutar múltiples uploads en paralelo
for i in {1..10}; do
  (./upload_test.sh &)
done

# Monitorear CPU/memoria de MaxIOFS
# Verificar no hay errores en logs
```

---

## 🎯 Conclusión

Con estos tests puedes validar completamente que:

1. ✅ MaxIOFS maneja headers de Object Lock correctamente
2. ✅ Veeam puede usar MaxIOFS como repositorio inmutable
3. ✅ La inmutabilidad se aplica y respeta correctamente
4. ✅ Los backups están protegidos según la política configurada

**Ready for Production!** 🚀
