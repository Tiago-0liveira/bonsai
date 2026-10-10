import { expect, test, type Browser, type TestInfo } from '@playwright/test'

// Runs against TestWarmStartBrowserFixture (127.0.0.1:7013): the real API
// wiring with three generated projects. "alpha" is the active project.
const FIXTURE = 'http://127.0.0.1:7013'
const WARMUPS = Number(process.env.BONSAI_P7_WARMUPS ?? 3)
const RUNS = Number(process.env.BONSAI_P7_RUNS ?? 5)
const LARGE = process.env.BONSAI_P7_SIZE === 'large'
const SIZE = LARGE ? { worktrees: 100, prs: 500 } : { worktrees: 25, prs: 50 }

test.describe.configure({ mode: 'serial' })
test.setTimeout(10 * 60_000)

async function fixture(path: string, params: Record<string, string | number> = {}) {
  const query = new URLSearchParams(Object.entries(params).map(([key, value]) => [key, String(value)]))
  const response = await fetch(`${FIXTURE}/__fixture/${path}?${query}`, { method: 'POST' })
  expect(response.ok, `fixture ${path}`).toBe(true)
  return response.json() as Promise<{ epoch: string; active: string }>
}

type Sample = { readyToPaint: number | null; navigationToReady: number | null }

// One measured page load in a fresh browser context with empty canonical
// state. The clock starts when the authenticated socket reports `ready` and
// stops two animation frames after the active project's branch rows and PR
// badges are all in the DOM.
async function measureLoad(browser: Browser, expected: { rows: number; prs: number }): Promise<Sample> {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  try {
    const page = await context.newPage()
    await page.addInitScript(({ rows, prs }) => {
      const metrics = window as unknown as { __p7DomAt?: number; __p7PaintAt?: number }
      const complete = () => {
        const list = document.querySelector('aside[aria-label="Branches"]')
        return !!list && list.querySelectorAll('[data-worktree-row]').length >= rows && list.querySelectorAll('[data-pr-badge]').length >= prs
      }
      const observer = new MutationObserver(() => {
        if (metrics.__p7DomAt !== undefined || !complete()) return
        metrics.__p7DomAt = performance.now()
        observer.disconnect()
        requestAnimationFrame(() => requestAnimationFrame(() => { metrics.__p7PaintAt = performance.now() }))
      })
      observer.observe(document, { childList: true, subtree: true })
    }, expected)
    await page.goto(`${FIXTURE}/app`)
    try {
      await page.waitForFunction(() => (window as unknown as { __p7PaintAt?: number }).__p7PaintAt !== undefined, null, { timeout: 20_000 })
    } catch {
      return { readyToPaint: null, navigationToReady: null }
    }
    return await page.evaluate(() => {
      const ready = performance.getEntriesByName('bonsai:ws-ready')[0]?.startTime
      const paint = (window as unknown as { __p7PaintAt: number }).__p7PaintAt
      return { readyToPaint: ready === undefined ? null : paint - ready, navigationToReady: ready ?? null }
    })
  } finally {
    await context.close()
  }
}

function percentile(values: number[], p: number) {
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]
}

function summarize(samples: Sample[]) {
  // A missing paint counts as a failure, ranked above every real sample.
  const values = samples.map(sample => sample.readyToPaint ?? Number.POSITIVE_INFINITY)
  const ready = samples.map(sample => sample.navigationToReady).filter((value): value is number => value !== null)
  const round = (value: number) => Math.round(value * 10) / 10
  return {
    runs: samples.length,
    failures: samples.filter(sample => sample.readyToPaint === null).length,
    p50: round(percentile(values, 50)),
    p95: round(percentile(values, 95)),
    max: round(Math.max(...values)),
    navigationToReadyP50: ready.length ? round(percentile(ready, 50)) : null,
  }
}

async function report(testInfo: TestInfo, name: string, samples: Sample[]) {
  const summary = { ...summarize(samples), size: SIZE, build: 'e2e (React profiling)', samples }
  await testInfo.attach(name, { body: JSON.stringify(summary, null, 2), contentType: 'application/json' })
  console.log(`[warm-start] ${name} ${JSON.stringify({ ...summary, samples: undefined })}`)
  return summary
}

const expected = { rows: SIZE.worktrees, prs: Math.min(SIZE.worktrees - 1, SIZE.prs) }

test.beforeEach(async () => {
  await fixture('reset', SIZE)
})

test('cold start: ready to branch list and PR badges painted', async ({ browser }, testInfo) => {
  const samples: Sample[] = []
  for (let run = 0; run < WARMUPS + RUNS; run++) {
    // A restarted API with empty memory and live providers.
    await fixture('restart')
    const sample = await measureLoad(browser, expected)
    if (run >= WARMUPS) samples.push(sample)
  }
  const summary = await report(testInfo, `cold-${SIZE.worktrees}wt-${SIZE.prs}pr`, samples)
  expect(summary.failures).toBe(0)
})

test('warm start: restored branch list and PR badges paint while providers are pending', async ({ browser }, testInfo) => {
  // Populate the cache through real live commits, then restart from it.
  await fixture('settle')
  const samples: Sample[] = []
  for (let run = 0; run < WARMUPS + RUNS; run++) {
    // Every revalidation stays blocked until the measurement ends.
    await fixture('restart', { components: 'local,catalog,checks' })
    const sample = await measureLoad(browser, expected)
    if (run >= WARMUPS) samples.push(sample)
  }
  await fixture('release', { components: 'local,catalog,checks' })
  const summary = await report(testInfo, `warm-${SIZE.worktrees}wt-${SIZE.prs}pr`, samples)
  expect(summary.failures).toBe(0)
  // The ≤ 300 ms gate is local (BONSAI_P7_GATE=1); CI only reports.
  if (process.env.BONSAI_P7_GATE === '1') expect(summary.p95).toBeLessThanOrEqual(300)
})
