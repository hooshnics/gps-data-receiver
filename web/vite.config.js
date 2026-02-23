import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  root: '.',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/health': { target: 'http://localhost:8080', changeOrigin: true },
      '/ready': { target: 'http://localhost:8080', changeOrigin: true },
      '/monitoring': { target: 'http://localhost:8080', changeOrigin: true },
      '/socket.io': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true,
        secure: false,
        configure: (proxy) => {
          proxy.on('proxyReqWs', (proxyReq, req, socket) => {
            proxyReq.setHeader('Origin', 'http://localhost:8080')
          })
        },
      },
    },
  },
})
