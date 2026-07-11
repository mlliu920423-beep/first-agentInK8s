import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/healthz': 'http://localhost:8080',
    },
  },
  build: {
    // Produce plain relative-path assets so the Go embed serves them from /.
    outDir: 'dist',
    emptyOutDir: true,
  },
})
