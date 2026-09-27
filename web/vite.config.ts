import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const securityHeaders = {
  'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://api.bonsai.dev http://127.0.0.1:7001 ws://127.0.0.1:7001; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://api.bonsai.dev",
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Permissions-Policy': 'local-network-access=(self)',
}

const httpsKey = process.env.BONSAI_E2E_HTTPS_KEY
const httpsCert = process.env.BONSAI_E2E_HTTPS_CERT
const previewHttps = httpsKey && httpsCert
  ? { key: readFileSync(httpsKey), cert: readFileSync(httpsCert) }
  : undefined

export default defineConfig({
  plugins: [react()],
  server: { host: '127.0.0.1' },
  preview: { headers: securityHeaders, https: previewHttps },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
