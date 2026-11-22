import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    // `npm run dev` talks to a moipctl running on :8080
    proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } },
  },
})
