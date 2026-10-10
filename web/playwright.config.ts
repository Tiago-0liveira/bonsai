import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  use: {
    baseURL: 'https://127.0.0.1:4173',
    ignoreHTTPSErrors: true,
    trace: 'on-first-retry',
  },
  webServer: [{
    command: 'pnpm build --mode e2e && node e2e/https-preview.mjs',
    url: 'https://127.0.0.1:4173',
    ignoreHTTPSErrors: true,
    reuseExistingServer: !process.env.CI,
  }, {
    // The local-served entry: the real Go API handler serving web/dist-e2e on
    // its own origin, with its own headers and runtime config. Started after
    // the build above (web servers start in order).
    command: 'cd .. && BONSAI_UI_BROWSER_FIXTURE=1 go test ./internal/server/localapi -run "^TestEmbeddedUIBrowserFixture$" -count=1 -timeout=30m',
    url: 'http://127.0.0.1:7011/health',
    reuseExistingServer: false,
    timeout: 180_000,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10000 },
  }, ...(['linux', 'darwin'].includes(process.platform) ? [{
    command: 'cd .. && BONSAI_AGENT_BROWSER_FIXTURE=1 go test ./internal/server/localapi -run "^TestAgentBrowserFixture$" -count=1 -timeout=10m',
    url: 'http://127.0.0.1:7001/health',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10000 },
  }] : [])],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
