import { defineConfig, devices } from '@playwright/test'

const production = process.env.BONSAI_WARM_START_UI === 'dist'
const size = process.env.BONSAI_P7_SIZE === 'large' ? 'large' : 'small'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'warm-start.spec.ts',
  workers: 1,
  outputDir: `test-results/measurements/${production ? 'production' : 'profiling'}-${size}`,
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
  webServer: {
    command: `${production ? 'pnpm build' : 'pnpm build --mode e2e'} && cd .. && BONSAI_WARM_START_BROWSER_FIXTURE=1 go test ./internal/server/localapi -run '^TestWarmStartBrowserFixture$' -count=1 -timeout=60m`,
    url: 'http://127.0.0.1:7013/health',
    timeout: 180_000,
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
  },
})
