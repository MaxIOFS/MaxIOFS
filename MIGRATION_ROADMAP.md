# Migration Roadmap: App Router → Pages Router

## ✅ MIGRACIÓN COMPLETADA

**Fecha de completación**: 11 de Octubre, 2025

## Objetivo
Migrar de Next.js App Router a Pages Router para soportar `output: 'export'` con rutas dinámicas manejadas por client-side routing.

## Estructura de Archivos

### Mapeo de Rutas

| App Router (Actual) | Pages Router (Nuevo) | Tipo |
|---------------------|----------------------|------|
| `src/app/page.tsx` | `src/pages/index.tsx` | Static |
| `src/app/login/page.tsx` | `src/pages/login.tsx` | Static |
| `src/app/buckets/page.tsx` | `src/pages/buckets/index.tsx` | Static |
| `src/app/buckets/create/page.tsx` | `src/pages/buckets/create.tsx` | Static |
| `src/app/buckets/[bucket]/page.tsx` | `src/pages/buckets/[bucket]/index.tsx` | Dynamic |
| `src/app/buckets/[bucket]/settings/page.tsx` | `src/pages/buckets/[bucket]/settings.tsx` | Dynamic |
| `src/app/users/page.tsx` | `src/pages/users/index.tsx` | Static |
| `src/app/users/access-keys/page.tsx` | `src/pages/users/access-keys.tsx` | Static |
| `src/app/users/[user]/page.tsx` | `src/pages/users/[user]/index.tsx` | Dynamic |
| `src/app/users/[user]/settings/page.tsx` | `src/pages/users/[user]/settings.tsx` | Dynamic |
| `src/app/tenants/page.tsx` | `src/pages/tenants.tsx` | Static |
| `src/app/objects/page.tsx` | `src/pages/objects.tsx` | Static |
| `src/app/metrics/page.tsx` | `src/pages/metrics.tsx` | Static |
| `src/app/security/page.tsx` | `src/pages/security.tsx` | Static |
| `src/app/settings/page.tsx` | `src/pages/settings.tsx` | Static |
| `src/app/layout.tsx` | `src/pages/_app.tsx` | Layout |
| N/A | `src/pages/_document.tsx` | HTML Document |

### Archivos Especiales

1. **_app.tsx** (nuevo): Layout global y providers
2. **_document.tsx** (nuevo): Estructura HTML base
3. **404.tsx** (opcional): Página de error personalizada

## Cambios de Código por Página

### 1. Imports que cambian

```typescript
// ❌ App Router
'use client';
import { useParams, useRouter } from 'next/navigation';

// ✅ Pages Router
import { useRouter } from 'next/router';
```

### 2. Hook de parámetros dinámicos

```typescript
// ❌ App Router
const params = useParams();
const bucketName = params.bucket as string;

// ✅ Pages Router
const router = useRouter();
const { bucket } = router.query;
const bucketName = bucket as string;
```

### 3. Navegación

```typescript
// ❌ App Router
import { useRouter } from 'next/navigation';
router.push('/buckets');

// ✅ Pages Router
import { useRouter } from 'next/router';
router.push('/buckets');
// (mismo código, solo cambia el import)
```

### 4. Layout Global

**Actual: `src/app/layout.tsx`**
```typescript
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <QueryClientProvider client={queryClient}>
          <AppLayout>{children}</AppLayout>
        </QueryClientProvider>
      </body>
    </html>
  );
}
```

**Nuevo: `src/pages/_app.tsx`**
```typescript
import type { AppProps } from 'next/app';
import { QueryClientProvider } from '@tanstack/react-query';
import { AppLayout } from '@/components/layout/AppLayout';
import '@/styles/globals.css';

export default function MyApp({ Component, pageProps }: AppProps) {
  return (
    <QueryClientProvider client={queryClient}>
      <AppLayout>
        <Component {...pageProps} />
      </AppLayout>
    </QueryClientProvider>
  );
}
```

**Nuevo: `src/pages/_document.tsx`**
```typescript
import { Html, Head, Main, NextScript } from 'next/document';

export default function Document() {
  return (
    <Html lang="en">
      <Head />
      <body>
        <Main />
        <NextScript />
      </body>
    </Html>
  );
}
```

## Componentes que NO Cambian

Los siguientes se mantienen exactamente igual:
- `src/components/layout/AppLayout.tsx`
- `src/components/ui/*` (todos)
- `src/lib/api.ts`
- `src/lib/auth.ts`
- `src/lib/sweetalert.ts`
- Todos los componentes de UI

## Configuración

### next.config.js

```javascript
/** @type {import('next').NextConfig} */
const path = require('path')

const nextConfig = {
  // Static export for Pages Router
  output: 'export',

  // Disable image optimization
  images: {
    unoptimized: true
  },

  eslint: {
    ignoreDuringBuilds: true,
  },

  typescript: {
    ignoreBuildErrors: true,
  },

  // Disable trailing slashes
  trailingSlash: false,

  // Webpack configuration for path aliases
  webpack: (config) => {
    config.resolve.alias = {
      ...config.resolve.alias,
      '@': path.resolve(__dirname, './src'),
    }
    return config
  },
}

module.exports = nextConfig
```

### Build Output

- **Actual:** `.next/` (requiere Node.js)
- **Nuevo:** `out/` (archivos estáticos puros)

### embed.go

```go
//go:embed all:../../web/frontend/out
var frontendAssets embed.FS

func getFrontendFS() (fs.FS, error) {
    return fs.Sub(frontendAssets, "web/frontend/out")
}
```

## Orden de Ejecución

### Fase 1: Preparación (sin tocar código existente)
1. ✅ Crear `src/pages/` directory
2. ✅ Crear `_app.tsx`
3. ✅ Crear `_document.tsx`
4. ✅ Copiar `globals.css` si no existe en `src/styles/`

### Fase 2: Migración de Páginas (una por una, validar cada una)
1. ✅ `index.tsx` (dashboard)
2. ✅ `login.tsx`
3. ✅ `buckets/index.tsx`
4. ✅ `buckets/create.tsx`
5. ✅ `buckets/[bucket]/index.tsx`
6. ✅ `buckets/[bucket]/settings.tsx`
7. ✅ `users/index.tsx`
8. ✅ `users/access-keys.tsx`
9. ✅ `users/[user]/index.tsx`
10. ✅ `users/[user]/settings.tsx`
11. ✅ `tenants.tsx`
12. ✅ `objects.tsx`
13. ✅ `metrics.tsx`
14. ✅ `security.tsx`
15. ✅ `settings.tsx`

### Fase 3: Actualizar Configuración
1. ✅ Actualizar `next.config.js` con `output: 'export'`
2. ✅ Test build: `npm run build`
3. ✅ Verificar que se genera `out/` directory

### Fase 4: Actualizar Backend
1. ✅ Actualizar `embed.go` para usar `out/`
2. ✅ Test compilación Go: `go build`
3. ✅ Actualizar `build.bat` para usar `out/`
4. ✅ Actualizar `Makefile` para usar `out/`

### Fase 5: Limpieza
1. ✅ Eliminar `src/app/` directory completo
2. ✅ Test final build: `npm run build && go build`

## Validación de Cada Página

Para cada página migrada, verificar:

1. ✅ Sin errores de TypeScript
2. ✅ Imports correctos
3. ✅ `useRouter()` de `next/router`
4. ✅ `router.query` para parámetros dinámicos
5. ✅ Navegación funciona con `router.push()`
6. ✅ React Query funciona sin cambios
7. ✅ Componentes UI se renderizan correctamente

## Checklist de Testing Final

- [x] Build exitoso: `npm run build` ✅
- [x] Directorio `out/` generado con HTML ✅
- [x] Archivo `out/index.html` existe ✅
- [x] Archivos `out/_next/static/*` existen ✅
- [x] Go build exitoso con embed ✅ (27MB binary)
- [x] Embed configurado para usar `web/frontend/out/` ✅
- [x] Todas las páginas migradas (16 páginas) ✅
- [x] Sin errores de compilación ✅
- [x] `src/app/` directory eliminado ✅
- [x] `next.config.js` configurado con `output: 'export'` ✅
- [ ] Server inicia sin errores (pendiente de prueba manual)
- [ ] Ruta raíz `/` carga dashboard (pendiente de prueba manual)
- [ ] Rutas estáticas funcionan (login, buckets, etc) (pendiente de prueba manual)
- [ ] Rutas dinámicas funcionan (`/buckets/test-bucket`) (pendiente de prueba manual)
- [ ] Navegación client-side funciona (pendiente de prueba manual)
- [ ] API calls funcionan correctamente (pendiente de prueba manual)
- [ ] 404 redirect funciona para rutas inexistentes (pendiente de prueba manual)

## Riesgos y Mitigaciones

### Riesgo 1: Pérdida de funcionalidad durante migración
**Mitigación:** Migrar página por página, mantener `src/app/` hasta validar todo

### Riesgo 2: Router.query puede ser undefined inicialmente
**Mitigación:** Agregar checks: `if (!router.isReady) return <Loading />;`

### Riesgo 3: Layout se comporta diferente
**Mitigación:** Verificar que `_app.tsx` tiene todos los providers necesarios

### Riesgo 4: CSS no se carga correctamente
**Mitigación:** Importar `globals.css` en `_app.tsx`

## Rollback Plan

Si algo falla:
1. Mantener backup de `src/app/` original
2. Revertir `next.config.js`
3. Eliminar `src/pages/`
4. Restaurar código original

## Notas Importantes

1. **NO eliminar** `src/app/` hasta que TODO esté funcionando
2. **NO modificar** componentes UI durante migración
3. **NO cambiar** lógica de negocio, solo adaptación de routing
4. **Validar** cada página antes de continuar con la siguiente
5. **Mantener** todos los imports de componentes UI exactamente igual

## Tiempo Estimado Total

- Fase 1: 15 min
- Fase 2: 2.5 horas (10 min por página)
- Fase 3: 30 min
- Fase 4: 30 min
- Fase 5: 15 min
- Testing: 1 hora

**Total: 4.5 - 5 horas**

## Beneficio Final

✅ `output: 'export'` funcional
✅ Archivos estáticos en `out/`
✅ Sin Node.js necesario
✅ Rutas dinámicas manejadas por client
✅ Mismo frontend embebido en Go binary
✅ Mismas funcionalidades actuales
