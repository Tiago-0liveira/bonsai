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
  }, ...(['linux', 'darwin'].includes(process.platform) ? [{
    command: 'cd .. && BONSAI_AGENT_BROWSER_FIXTURE=1 go test ./internal/server/localapi -run "^TestAgentBrowserFixture$" -count=1 -timeout=10m',
    url: 'http://127.0.0.1:7001/health',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10000 },
  }] : [])],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
