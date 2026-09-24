import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The backend serves the built bundle from ../web/dist (STATIC_DIR).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/ws': {
        target: 'http://localhost:8080',
        ws: true,
      },
      '/api': 'http://localhost:8080',
    },
  },
})
