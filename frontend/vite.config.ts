import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const apiTarget = process.env.VITE_API_TARGET ?? 'http://127.0.0.1:3000'
const dockerDev = process.env.VITE_API_TARGET !== undefined

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    // Dentro do Compose, /api vai direto ao serviço Go e o polling detecta edições feitas no host Windows.
    // changeOrigin: false explícito: o Host precisa continuar igual ao Origin do navegador, senão a checagem de mesma
    // origem (CSRF) recusa as escritas. A forma curta ('/api': 'http://...') liga changeOrigin no Vite.
    proxy: { '/api': { target: apiTarget, changeOrigin: false } },
    watch: dockerDev ? { usePolling: true, interval: 1000 } : undefined,
  },
  test: { include: ['src/**/*.test.ts'] },
})
