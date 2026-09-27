import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const localApiOrigin = process.env.VITE_BONSAI_LOCAL_API_ORIGIN ?? 'http://127.0.0.1:7001'
const relayOrigin = process.env.VITE_BONSAI_RELAY_ORIGIN ?? 'https://api.bonsai.dev'
const localWebSocketOrigin = localApiOrigin.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:')
const webPort = Number(process.env.BONSAI_WEB_PORT ?? '5173')
const devWebSocketOrigin = `ws://127.0.0.1:${webPort}`

const baseSecurityHeaders = {
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Permissions-Policy': 'loopback-network=(self), local-network=(self), local-network-access=(self)',
}

const productionSecurityHeaders = {
  ...baseSecurityHeaders,
  'Content-Security-Policy': `default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ${localApiOrigin} ${localWebSocketOrigin} ${relayOrigin}; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://api.bonsai.dev`,
}

// Vite dev injects the React Refresh preamble and CSS <style> elements for HMR.
// Keep these allowances confined to the loopback-only development server.
const developmentSecurityHeaders = {
  ...baseSecurityHeaders,
  'Content-Security-Policy': `default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; style-src-elem 'self' 'unsafe-inline'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ${devWebSocketOrigin} ${localApiOrigin} ${localWebSocketOrigin} ${relayOrigin}; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://api.bonsai.dev`,
}

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: webPort,
    strictPort: true,
    headers: developmentSecurityHeaders,
  },
  preview: { headers: productionSecurityHeaders },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
