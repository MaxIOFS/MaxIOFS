import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          // Dependencias grandes que se cargan aparte
          react: ['react', 'react-dom'],
          router: ['react-router-dom'],
          query: ['@tanstack/react-query'],
          ui: ['sweetalert2', 'lucide-react', 'recharts'],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Proxy API requests to the Go backend console server
      '/api': {
        target: 'https://localhost:8081',
        changeOrigin: true,
        secure: false,
      },
    },
  },
})
