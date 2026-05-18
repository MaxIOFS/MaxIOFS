import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: './',
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    minify: 'oxc',
    chunkSizeWarningLimit: 1024,
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          // One chunk per dynamic locale — all JSON files for a language land
          // in the same output file so the browser makes exactly one request
          // per language switch regardless of how many namespaces exist.
          const localeMatch = id.match(/\/locales\/([a-z]{2,3})\//);
          if (localeMatch) {
            const lang = localeMatch[1];
            if (lang !== 'en') return `lang-${lang}`;
          }

          if (id.includes('react-router-dom')) return 'router';
          if (id.includes('@tanstack/react-query')) return 'query';
          if (id.includes('recharts')) return 'charts';
          if (id.includes('lucide-react')) return 'icons';
          if (id.includes('axios') || id.includes('date-fns') || id.includes('clsx') ||
              id.includes('class-variance-authority') || id.includes('tailwind-merge')) return 'utils';
        },
      },
    },
  },
  server: {
    port: 5173,
    watch: {
      ignored: ['**/test-results/**', '**/playwright-report/**'],
    },
    proxy: {
      // Proxy API requests to the Go backend console server
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        secure: false,
      },
    },
  },
})
