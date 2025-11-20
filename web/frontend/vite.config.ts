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
    sourcemap: false,
    minify: 'esbuild',
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom'],
          router: ['react-router-dom'],
          query: ['@tanstack/react-query'],
          charts: ['recharts'],
          icons: ['lucide-react'],
          alerts: ['sweetalert2'],
          utils: ['axios', 'date-fns', 'clsx', 'class-variance-authority', 'tailwind-merge'],
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
