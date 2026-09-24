import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dev server proxies WebSocket + REST to the Go backend; production builds
// are static files served (or copied into the backend image and served by
// nginx in docker compose).
export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/ws': { target: 'http://localhost:8080', ws: true },
      '/api': 'http://localhost:8080',
    },
  },
})
