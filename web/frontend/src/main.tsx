import React, { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './i18n' // Initialize i18n
import App from './App.tsx'

/** Catches crashes above ProtectedRoute (providers, router, primera pintura). Sin esto, un error deja #root en blanco. */
class RootErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { err: Error | null }
> {
  state = { err: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { err: error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('RootErrorBoundary:', error, info)
  }

  render() {
    if (this.state.err) {
      return (
        <div
          style={{
            minHeight: '100vh',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 24,
            fontFamily: 'system-ui, sans-serif',
            background: '#f9fafb',
            color: '#111',
          }}
        >
          <div style={{ maxWidth: 480 }}>
            <h1 style={{ fontSize: '1.25rem', marginBottom: 8 }}>La consola no pudo iniciar</h1>
            <p style={{ fontSize: '0.875rem', color: '#4b5563', marginBottom: 16 }}>
              {this.state.err.message}
            </p>
            <p style={{ fontSize: '0.8125rem', color: '#6b7280', marginBottom: 16 }}>
              Abre las herramientas de desarrollo (F12) → pestaña Consola y copia el error completo. Prueba recargar sin
              caché (Ctrl+Shift+R) o borrar datos del sitio para esta URL.
            </p>
            <button
              type="button"
              onClick={() => window.location.reload()}
              style={{
                padding: '8px 16px',
                background: '#2563eb',
                color: '#fff',
                border: 'none',
                borderRadius: 8,
                cursor: 'pointer',
              }}
            >
              Recargar
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </StrictMode>,
)
