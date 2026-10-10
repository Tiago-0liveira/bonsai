import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

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
  'Content-Security-Policy': `default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ${localApiOrigin} ${localWebSocketOrigin} ${relayOrigin}; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' ${relayOrigin}`,
}

// Vite dev injects the React Refresh preamble and CSS <style> elements for HMR.
// Keep these allowances confined to the loopback-only development server.
const developmentSecurityHeaders = {
  ...baseSecurityHeaders,
  'Content-Security-Policy': `default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; style-src-elem 'self' 'unsafe-inline'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ${devWebSocketOrigin} ${localApiOrigin} ${localWebSocketOrigin} ${relayOrigin}; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' ${relayOrigin}`,
}

export default defineConfig(({ mode }) => ({
  plugins: [react(), {
    name: 'e2e-fixtures-and-profiling',
    enforce: 'pre',
    // Browser presentation fixtures bypass persistence only in the test build.
    // The normal production build does not expose the store.
    transform(code, id) {
      if (mode === 'e2e' && id.endsWith('/src/stores/bonsai.ts')) {
        return code + '\nObject.defineProperty(window, "__bonsaiTestStore", { value: useBonsaiStore });\n'
      }
      if (mode !== 'e2e') return
      const count = (field: string) => `window.__bonsaiMetrics && window.__bonsaiMetrics.${field}++;`
      if (id.endsWith('/canvas/buildCanvasGraph.ts')) return code.replace('const { project,', count('graph') + '\n  const { project,')
      if (id.endsWith('/layout/prLabels.ts')) return code.replace('const rects = new Map', count('labels') + '\n  const rects = new Map')
      if (id.endsWith('/layout/AppShell.tsx')) return "import { Profiler } from 'react';\n" + code.replace('{children}</main>', '<Profiler id="main-workspace" onRender={(_id, phase, duration) => window.__bonsaiMetrics?.commits.push({phase, duration})}>{children}</Profiler></main>')
    },
  }],
  build: {
    // font-src 'self' blocks data: URIs, so small font subsets must stay files.
    assetsInlineLimit: (file: string) => file.endsWith('.woff2') ? false : undefined,
    // The e2e build carries test hooks. It gets its own directory so web/dist,
    // which release binaries embed, only ever holds the production build.
    outDir: mode === 'e2e' ? 'dist-e2e' : 'dist',
  },
  // The test build alone enables React's production Profiler callbacks.
  resolve: { alias: mode === 'e2e' ? [{ find: /^react-dom(?:\/client)?$/, replacement: fileURLToPath(new URL('./node_modules/react-dom/profiling.js', import.meta.url)) }] : [] },
  server: {
    host: '127.0.0.1',
    port: webPort,
    strictPort: true,
    headers: developmentSecurityHeaders,
  },
  preview: { headers: productionSecurityHeaders },
  test: {
    environment: 'jsdom',
    // Exercise real client panel lifecycle in jsdom; the package's Node export
    // intentionally strips panel registration/layout effects for SSR.
    alias: {
      'react-resizable-panels': fileURLToPath(new URL('./node_modules/react-resizable-panels/dist/react-resizable-panels.browser.development.esm.js', import.meta.url)),
    },
    globals: true,
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
      reporter: ['text-summary', 'html'],
      // Pure logic is held to full coverage; enforced by `pnpm test:coverage`.
      thresholds: {
        'src/lib/github/**': { statements: 100, branches: 100, functions: 100, lines: 100 },
      },
    },
  },
}))
