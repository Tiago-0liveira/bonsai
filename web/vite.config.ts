import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const securityHeaders = {
  'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://api.bonsai.dev http://127.0.0.1:7001 ws://127.0.0.1:7001; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://api.bonsai.dev",
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Permissions-Policy': 'loopback-network=(self), local-network=(self), local-network-access=(self)',
}

export default defineConfig({
  plugins: [react()],
  server: { host: '127.0.0.1' },
  preview: { headers: securityHeaders },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
