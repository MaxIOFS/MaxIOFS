# Compatibilidad con Veeam Backup & Replication - Repositorios Inmutables

## Estado Actual: ⚠️ **Parcialmente Compatible**

MaxIOFS tiene implementada la funcionalidad base de Object Lock/WORM, pero **necesita ajustes específicos** para funcionar completamente con Veeam.

---

## ✅ Lo que YA funciona

### 1. **APIs de Object Lock implementadas**
- ✅ `GET /{bucket}?object-lock` - Obtener configuración de Object Lock
- ✅ `PUT /{bucket}?object-lock` - Establecer configuración de Object Lock
- ✅ `GET /{bucket}/{object}?retention` - Obtener retención del objeto
- ✅ `PUT /{bucket}/{object}?retention` - Establecer/modificar retención
- ✅ `GET /{bucket}/{object}?legal-hold` - Obtener legal hold
- ✅ `PUT /{bucket}/{object}?legal-hold` - Establecer legal hold

### 2. **Enforcement de inmutabilidad**
- ✅ Bloqueo de eliminación durante período de retención
- ✅ COMPLIANCE mode (retención no modificable)
- ✅ GOVERNANCE mode (retención modificable con permisos)
- ✅ Aplicación automática de retention por defecto al subir objetos
- ✅ Validación de fechas de expiración

### 3. **Infraestructura necesaria**
- ✅ Versionado habilitado (requerido por Object Lock)
- ✅ Persistencia de metadatos en archivos JSON
- ✅ Validación en DeleteObject con mensajes detallados

---

## ❌ Lo que FALTA para Veeam

### 1. **Headers de Object Lock en PutObject** 🔴 **CRÍTICO**

**Problema:** Veeam envía headers especiales al subir backups que actualmente ignoramos.

**Headers que Veeam envía:**
```http
PUT /bucket/backup-file.vbk HTTP/1.1
x-amz-object-lock-mode: COMPLIANCE
x-amz-object-lock-retain-until-date: 2025-10-10T00:00:00Z
x-amz-object-lock-legal-hold: ON
```

**Ubicación del código:** `pkg/s3compat/handler.go` línea 273 - función `PutObject`

**Solución necesaria:**
```go
func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
    // ... código existente ...
    
    // AGREGAR: Leer headers de Object Lock
    lockMode := r.Header.Get("x-amz-object-lock-mode")
    retainUntilDate := r.Header.Get("x-amz-object-lock-retain-until-date")
    legalHoldStatus := r.Header.Get("x-amz-object-lock-legal-hold")
    
    // Si se especifican, pasar al objectManager.PutObject
    // y aplicar después de crear el objeto
}
```

---

### 2. **Headers de Object Lock en GetObject/HeadObject** 🔴 **CRÍTICO**

**Problema:** Veeam verifica la retención al leer objetos. Debemos devolver headers indicando el estado.

**Headers que Veeam espera recibir:**
```http
HTTP/1.1 200 OK
x-amz-object-lock-mode: COMPLIANCE
x-amz-object-lock-retain-until-date: 2025-10-10T00:00:00Z
x-amz-object-lock-legal-hold: ON
```

**Ubicación del código:** 
- `pkg/s3compat/handler.go` línea 240 - función `GetObject`
- `pkg/s3compat/handler.go` línea 332 - función `HeadObject`

**Solución necesaria:**
```go
func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
    // ... código existente para obtener obj ...
    
    // AGREGAR: Incluir headers de retention si existen
    if obj.Retention != nil {
        w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
        w.Header().Set("x-amz-object-lock-retain-until-date", 
            obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
    }
    
    // AGREGAR: Incluir header de legal hold si existe
    if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
        w.Header().Set("x-amz-object-lock-legal-hold", "ON")
    }
}
```

---

### 3. **Configuración completa en GetObjectLockConfiguration** 🟡 **IMPORTANTE**

**Problema:** La respuesta actual es un hardcoded simple que no refleja la configuración real.

**Código actual:** `pkg/s3compat/handler.go` línea 373
```go
func (h *Handler) GetObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
    h.writeXMLResponse(w, http.StatusOK, 
        `<ObjectLockConfiguration>
            <ObjectLockEnabled>Enabled</ObjectLockEnabled>
         </ObjectLockConfiguration>`)
}
```

**Solución necesaria:** Cargar la configuración real del bucket y devolver XML completo:
```xml
<ObjectLockConfiguration>
    <ObjectLockEnabled>Enabled</ObjectLockEnabled>
    <Rule>
        <DefaultRetention>
            <Mode>COMPLIANCE</Mode>
            <Days>2</Days>
        </DefaultRetention>
    </Rule>
</ObjectLockConfiguration>
```

---

### 4. **Validación de Object Lock habilitado en el bucket** 🟡 **IMPORTANTE**

**Problema:** Veeam valida que el bucket tenga Object Lock **antes** de configurarlo como repositorio.

**Solución:**
- Asegurar que `GET /{bucket}?object-lock` devuelva error si el bucket NO tiene Object Lock
- Actualmente devuelve siempre "Enabled" (hardcoded)
- Debe consultar `bucketManager.GetBucket()` y verificar `ObjectLockEnabled`

---

### 5. **Soporte para versionId en operaciones** 🟢 **OPCIONAL**

**Contexto:** Veeam puede usar versionado para recuperación incremental.

**Estado:** Tenemos las rutas registradas pero implementación básica.

**Rutas existentes:**
- `GET /{bucket}/{object}?versions`
- `DELETE /{bucket}/{object}?versionId={id}`

---

## 📋 Plan de Implementación

### Fase 1: Crítico - Funcionalidad básica con Veeam (2-3 horas)
1. ✅ Leer headers `x-amz-object-lock-*` en `PutObject`
2. ✅ Devolver headers `x-amz-object-lock-*` en `GetObject`/`HeadObject`
3. ✅ Implementar `GetObjectLockConfiguration` con datos reales del bucket

### Fase 2: Robustez - Testing con Veeam (1-2 horas)
4. ✅ Validar bucket tiene Object Lock antes de permitir operaciones
5. ✅ Testing con Veeam B&R: crear repositorio, hacer backup, intentar borrar
6. ✅ Verificar logs de Veeam para errores restantes

### Fase 3: Optimización (opcional)
7. ⚪ Implementar versionado completo si Veeam lo requiere
8. ⚪ Soporte para bulk operations (batch delete con retention check)
9. ⚪ Métricas específicas de Object Lock

---

## 🔧 Cambios de Código Necesarios

### Archivo 1: `pkg/s3compat/handler.go`

#### Cambio 1.1: PutObject - Leer headers de Object Lock
```go
func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    bucketName := vars["bucket"]
    objectKey := vars["object"]

    logrus.WithFields(logrus.Fields{
        "bucket": bucketName,
        "object": objectKey,
    }).Debug("S3 API: PutObject")

    // Leer headers de Object Lock si están presentes
    lockMode := r.Header.Get("x-amz-object-lock-mode")
    retainUntilDateStr := r.Header.Get("x-amz-object-lock-retain-until-date")
    legalHoldStatus := r.Header.Get("x-amz-object-lock-legal-hold")

    obj, err := h.objectManager.PutObject(r.Context(), bucketName, objectKey, r.Body, r.Header)
    if err != nil {
        if err == object.ErrBucketNotFound {
            h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
            return
        }
        h.writeError(w, "InternalError", err.Error(), objectKey, r)
        return
    }

    // Aplicar retención si se especificó en headers
    if lockMode != "" && retainUntilDateStr != "" {
        retainUntilDate, err := time.Parse(time.RFC3339, retainUntilDateStr)
        if err == nil {
            retention := &object.RetentionConfig{
                Mode:            lockMode,
                RetainUntilDate: retainUntilDate,
            }
            if err := h.objectManager.SetObjectRetention(r.Context(), bucketName, objectKey, retention); err != nil {
                logrus.WithError(err).Warn("Failed to set retention from headers")
            }
        }
    }

    // Aplicar legal hold si se especificó
    if legalHoldStatus == "ON" {
        legalHold := &object.LegalHoldConfig{Status: "ON"}
        if err := h.objectManager.SetObjectLegalHold(r.Context(), bucketName, objectKey, legalHold); err != nil {
            logrus.WithError(err).Warn("Failed to set legal hold from headers")
        }
    }

    w.Header().Set("ETag", obj.ETag)
    w.WriteHeader(http.StatusOK)
}
```

#### Cambio 1.2: HeadObject - Devolver headers de Object Lock
```go
func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    bucketName := vars["bucket"]
    objectKey := vars["object"]

    logrus.WithFields(logrus.Fields{
        "bucket": bucketName,
        "object": objectKey,
    }).Debug("S3 API: HeadObject")

    obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketName, objectKey)
    if err != nil {
        if err == object.ErrObjectNotFound {
            h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
            return
        }
        h.writeError(w, "InternalError", err.Error(), objectKey, r)
        return
    }

    w.Header().Set("Content-Type", obj.ContentType)
    w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
    w.Header().Set("ETag", obj.ETag)
    w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))

    // NUEVO: Agregar headers de Object Lock si existen
    if obj.Retention != nil {
        w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
        w.Header().Set("x-amz-object-lock-retain-until-date", 
            obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
    }

    if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
        w.Header().Set("x-amz-object-lock-legal-hold", "ON")
    }

    w.WriteHeader(http.StatusOK)
}
```

#### Cambio 1.3: GetObject - Devolver headers de Object Lock
```go
func (h *Handler) GetObject(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    bucketName := vars["bucket"]
    objectKey := vars["object"]

    // ... código existente para obtener el objeto ...

    w.Header().Set("Content-Type", obj.ContentType)
    w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
    w.Header().Set("ETag", obj.ETag)
    w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))

    // NUEVO: Agregar headers de Object Lock si existen
    if obj.Retention != nil {
        w.Header().Set("x-amz-object-lock-mode", obj.Retention.Mode)
        w.Header().Set("x-amz-object-lock-retain-until-date", 
            obj.Retention.RetainUntilDate.UTC().Format(time.RFC3339))
    }

    if obj.LegalHold != nil && obj.LegalHold.Status == "ON" {
        w.Header().Set("x-amz-object-lock-legal-hold", "ON")
    }

    // ... resto del código ...
}
```

#### Cambio 1.4: GetObjectLockConfiguration - Devolver configuración real
```go
func (h *Handler) GetObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    bucketName := vars["bucket"]

    // Obtener bucket metadata
    bucket, err := h.bucketManager.GetBucket(r.Context(), bucketName)
    if err != nil {
        h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
        return
    }

    // Verificar si tiene Object Lock habilitado
    if bucket.ObjectLock == nil || !bucket.ObjectLock.ObjectLockEnabled {
        h.writeError(w, "ObjectLockConfigurationNotFoundError", 
            "Object Lock configuration does not exist for this bucket", bucketName, r)
        return
    }

    // Construir respuesta XML
    config := ObjectLockConfiguration{
        ObjectLockEnabled: "Enabled",
    }

    if bucket.ObjectLock.Rule != nil && bucket.ObjectLock.Rule.DefaultRetention != nil {
        config.Rule = &ObjectLockRule{
            DefaultRetention: &DefaultRetention{
                Mode: bucket.ObjectLock.Rule.DefaultRetention.Mode,
            },
        }

        if bucket.ObjectLock.Rule.DefaultRetention.Days != nil {
            config.Rule.DefaultRetention.Days = bucket.ObjectLock.Rule.DefaultRetention.Days
        }
        if bucket.ObjectLock.Rule.DefaultRetention.Years != nil {
            config.Rule.DefaultRetention.Years = bucket.ObjectLock.Rule.DefaultRetention.Years
        }
    }

    h.writeXMLResponse(w, http.StatusOK, config)
}
```

---

## 🧪 Testing con Veeam

### Pre-requisitos
1. ✅ Veeam Backup & Replication instalado
2. ✅ MaxIOFS con cambios aplicados y corriendo
3. ✅ Bucket con Object Lock creado:
   ```bash
   # Crear bucket WORM
   POST http://localhost:8080/api/v1/buckets
   {
     "name": "veeam-immutable",
     "objectLock": {
       "objectLockEnabled": true,
       "rule": {
         "defaultRetention": {
           "mode": "COMPLIANCE",
           "days": 14
         }
       }
     }
   }
   ```

### Pasos de Testing

#### 1. Configurar Veeam
1. Abrir Veeam Backup & Replication
2. Backup Infrastructure → Backup Repositories → Add Repository
3. Seleccionar "Object Storage"
4. Elegir "S3 Compatible"
5. Configurar:
   - Service point: `http://localhost:8080` (o IP del servidor)
   - Bucket: `veeam-immutable`
   - Access Key / Secret Key: credenciales de MaxIOFS
   - ✅ Marcar: "Make recent backups immutable for X days"

#### 2. Validar Configuración
Veeam hará estas llamadas (monitorear logs de MaxIOFS):
```
GET /veeam-immutable?object-lock
GET /veeam-immutable?versioning
GET /veeam-immutable
```

#### 3. Crear Backup de Prueba
1. Crear un backup job simple (VM o archivo)
2. Ejecutar el job
3. Verificar que Veeam suba archivos exitosamente

#### 4. Validar Inmutabilidad
1. En Veeam: intentar borrar el backup antes del período de retención
   - **Esperado**: Veeam muestra error "Cannot delete immutable backup"
2. En AWS CLI: intentar borrar directamente
   ```bash
   aws s3 rm s3://veeam-immutable/backup-file.vbk --endpoint-url=http://localhost:8080
   ```
   - **Esperado**: Error 403 con mensaje de retención

#### 5. Verificar en MaxIOFS UI
1. Ir a `http://localhost:8081/buckets/veeam-immutable`
2. ✅ Verificar badge "WORM" visible
3. ✅ Verificar banner con retention policy
4. ✅ Verificar columna "Retention" muestra días restantes

---

## ⚡ Quick Start - Implementar ahora

Si quieres que implemente estos cambios **AHORA**, dime y actualizo los archivos necesarios. Los cambios son:

1. **3 funciones en `pkg/s3compat/handler.go`** (PutObject, GetObject, HeadObject)
2. **1 función en `pkg/s3compat/handler.go`** (GetObjectLockConfiguration)
3. **Recompilar y reiniciar**
4. **Testing con Veeam**

Estimo **30-45 minutos** para implementación completa + testing inicial.

---

## 📚 Referencias

- [AWS S3 Object Lock Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- [Veeam: Immutable Backup Storage](https://www.veeam.com/blog/how-to-configure-immutable-backup-repository.html)
- [S3 API Reference - Object Lock Headers](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html)

---

## 🎯 Resumen Ejecutivo

**¿Puedes usar MaxIOFS con Veeam AHORA?** 
❌ No, pero estás al 70% del camino.

**¿Qué falta?**
- Headers de Object Lock en PUT/GET/HEAD Object (30 min)
- GetObjectLockConfiguration con datos reales (15 min)

**¿Vale la pena?**
✅ SÍ - Una vez implementado, tendrás un repositorio S3 inmutable **on-premise** compatible con Veeam, sin depender de AWS/Azure/Wasabi.
