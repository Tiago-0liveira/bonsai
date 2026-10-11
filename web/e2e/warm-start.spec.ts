import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test'
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { staticFiles, resolveChunk, selectors } from '../scripts/check-bundle-budget.mjs'
import './presentationFixtures'

// Runs against TestWarmStartBrowserFixture (127.0.0.1:7013): the real API
// wiring with three generated projects. "alpha" is the active project.
const FIXTURE = 'http://127.0.0.1:7013'
const WARMUPS = Number(process.env.BONSAI_P7_WARMUPS ?? 3)
const RUNS = Number(process.env.BONSAI_P7_RUNS ?? 5)
const LARGE = process.env.BONSAI_P7_SIZE === 'large'
const SIZE = LARGE ? { worktrees: 100, prs: 500 } : { worktrees: 25, prs: 50 }

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

async function fixture(path: string, params: Record<string, string | number> = {}) {
  const query = new URLSearchParams(Object.entries(params).map(([key, value]) => [key, String(value)]))
  const response = await fetch(`${FIXTURE}/__fixture/${path}?${query}`, { method: 'POST' })
  expect(response.ok, `fixture ${path}`).toBe(true)
  return response.json() as Promise<{ epoch: string; active: string }>
}

type Sample = { readyToPaint: number | null; navigationToReady: number | null; requestedJs?: string[]; error?: string; trace?: string }

// One measured page load in a fresh browser context with empty canonical
// state. The clock starts when the authenticated socket reports `ready` and
// stops two animation frames after the active project's branch rows and PR
// badges are all in the DOM.
async function measureLoad(browser: Browser, expected: { rows: number; prs: number }, testInfo: TestInfo, traceName?: string): Promise<Sample> {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  if (traceName) await context.tracing.start({ screenshots: true, snapshots: true })
  try {
    const page = await context.newPage()
    const requests = new Set<string>()
    page.on('request', request => {
      const path = new URL(request.url()).pathname.slice(1)
      if (path.startsWith('assets/') && path.endsWith('.js')) requests.add(path)
    })
    await page.addInitScript(({ rows, prs }) => {
      const metrics = window as unknown as { __p7DomAt?: number; __p7PaintAt?: number }
      // The production build has no `bonsai:ws-ready` mark; the socket's
      // ready frame stands in for it there.
      const NativeSocket = window.WebSocket
      window.WebSocket = class extends NativeSocket {
        constructor(...args: ConstructorParameters<typeof WebSocket>) {
          super(...args)
          this.addEventListener('message', event => {
            if (typeof event.data === 'string' && event.data.startsWith('{"type":"ready"') && !performance.getEntriesByName('bonsai:ws-ready-frame').length) performance.mark('bonsai:ws-ready-frame')
          })
        }
      }
      const complete = () => {
        const list = document.querySelector('aside[aria-label="Branches"]')
        return !!list && list.querySelectorAll('[data-worktree-row]').length === rows && list.querySelectorAll('[data-pr-badge]').length === prs
          && list.querySelectorAll('[data-ci-badge]').length === rows - 1
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
    await page.waitForFunction(() => (window as unknown as { __p7PaintAt?: number }).__p7PaintAt !== undefined, null, { timeout: 20_000 })
    const sample = await page.evaluate(() => {
      const ready = (performance.getEntriesByName('bonsai:ws-ready')[0] ?? performance.getEntriesByName('bonsai:ws-ready-frame')[0])?.startTime
      const paint = (window as unknown as { __p7PaintAt: number }).__p7PaintAt
      return { readyToPaint: ready === undefined ? null : paint - ready, navigationToReady: ready ?? null }
    })
    // Paint can precede the canvas import. Verify the complete default-route
    // request graph afterwards without extending the paint clock.
    await expect(page.locator('.react-flow')).toBeVisible()
    const directory = process.env.BONSAI_WARM_START_UI === 'dist' ? 'dist' : 'dist-e2e'
    const graph = JSON.parse(readFileSync(`${directory}/.vite/manifest.json`, 'utf8'))
    const roots = ['entry', 'application', 'workspace'].map(id => resolveChunk(graph, selectors[id], id))
    expect([...requests].sort(), 'browser requests match first-screen static graph').toEqual(staticFiles(graph, roots))
    return { ...sample, requestedJs: [...requests].sort(), ...(traceName ? { trace: `${traceName}.zip` } : {}) }
  } catch (error) {
    return { readyToPaint: null, navigationToReady: null, error: String(error) }
  } finally {
    if (traceName) {
      const path = testInfo.outputPath(`${traceName}.zip`)
      await context.tracing.stop({ path })
      await testInfo.attach(traceName, { path, contentType: 'application/zip' })
    }
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
    navigationToReadyP95: ready.length ? round(percentile(ready, 95)) : null,
    navigationToReadyMax: ready.length ? round(Math.max(...ready)) : null,
  }
}

async function report(testInfo: TestInfo, name: string, samples: Sample[], chromiumVersion: string) {
  const build = process.env.BONSAI_WARM_START_UI === 'dist' ? 'production (embedded UI)' : 'e2e (React profiling)'
  const summary = { ...summarize(samples), size: SIZE, build, samples,
    sourceRevision: execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(),
    node: process.version, playwright: JSON.parse(readFileSync('node_modules/@playwright/test/package.json', 'utf8')).version,
    chromium: chromiumVersion, viewport: { width: 1440, height: 900 }, warmups: WARMUPS,
  }
  mkdirSync('bundle-reports/measurements', { recursive: true })
  writeFileSync(`bundle-reports/measurements/${build.startsWith('production') ? 'production' : 'profiling'}-${name}.json`, JSON.stringify(summary, null, 2) + '\n')
  await testInfo.attach(name, { body: JSON.stringify(summary, null, 2), contentType: 'application/json' })
  console.log(`[warm-start] ${name} ${JSON.stringify({ ...summary, samples: undefined })}`)
  return summary
}

const expected = { rows: SIZE.worktrees, prs: Math.min(SIZE.worktrees - 1, SIZE.prs) }

test.beforeEach(async () => {
  await fixture('reset', SIZE)
})

test('cold start: ready to branch list and PR badges painted', async ({ browser }, testInfo) => {
  test.setTimeout(15 * 60_000)
  const samples: Sample[] = []
  for (let run = 0; run < WARMUPS + RUNS; run++) {
    // A restarted API with empty memory and live providers.
    await fixture('restart', { cache: 'missing' })
    const sample = await measureLoad(browser, expected, testInfo, run === WARMUPS + RUNS - 1 ? `cold-${SIZE.worktrees}-${SIZE.prs}` : undefined)
    if (run >= WARMUPS) samples.push(sample)
  }
  const summary = await report(testInfo, `cold-${SIZE.worktrees}wt-${SIZE.prs}pr`, samples, browser.version())
  expect(summary.failures).toBe(0)
})

test('warm start: restored branch list and PR badges paint while providers are pending', async ({ browser }, testInfo) => {
  test.setTimeout(15 * 60_000)
  // Populate the cache through real live commits, then restart from it.
  await fixture('settle')
  const samples: Sample[] = []
  for (let run = 0; run < WARMUPS + RUNS; run++) {
    // Every revalidation stays blocked until the measurement ends.
    await fixture('restart', { components: 'local,catalog,checks' })
    const sample = await measureLoad(browser, expected, testInfo, run === WARMUPS + RUNS - 1 ? `warm-${SIZE.worktrees}-${SIZE.prs}` : undefined)
    if (run >= WARMUPS) samples.push(sample)
  }
  await fixture('release', { components: 'local,catalog,checks' })
  const summary = await report(testInfo, `warm-${SIZE.worktrees}wt-${SIZE.prs}pr`, samples, browser.version())
  expect(summary.failures).toBe(0)
  // The ≤ 300 ms gate is local (BONSAI_P7_GATE=1); CI only reports.
  if (process.env.BONSAI_P7_GATE === '1') expect(summary.p95).toBeLessThanOrEqual(300)
})

const branches = (page: Page) => page.getByRole('complementary', { name: 'Branches' })
const prBadges = (page: Page) => branches(page).locator('[data-pr-badge]')
const staleBadges = (page: Page, kind: 'pr' | 'ci') => branches(page).locator(`[data-${kind}-badge][data-stale]`)

async function openWarm(page: Page) {
  await page.goto(`${FIXTURE}/app`)
  await expect(branches(page).locator('[data-worktree-row]')).toHaveCount(expected.rows)
}

test('restored data is labeled until each component revalidates on its own', async ({ page }) => {
  await fixture('settle')
  await fixture('restart', { components: 'local,catalog,checks' })
  await openWarm(page)
  await expect(prBadges(page)).toHaveCount(expected.prs)
  await expect(branches(page).locator('[data-stale-label]')).toHaveText('saved')
  await expect(staleBadges(page, 'pr')).toHaveCount(expected.prs)
  await expect(staleBadges(page, 'ci')).toHaveCount(expected.rows - 1)
  await expect(page.getByRole('complementary', { name: 'Inspector' }).getByText('saved', { exact: true })).toBeVisible()
  await expect(page.locator('.react-flow__node-worktree').getByRole('img', { name: /\(stale\)$/ }).first()).toBeVisible()

  // Local Git first: the Inspector goes live, provider labels stay.
  await fixture('release', { components: 'local' })
  await expect(page.getByRole('complementary', { name: 'Inspector' }).getByText('live', { exact: true })).toBeVisible()
  await expect(staleBadges(page, 'pr')).toHaveCount(expected.prs)

  // The catalog: PR links are confirmed while checks are still pending.
  await fixture('release', { components: 'catalog' })
  await expect(staleBadges(page, 'pr')).toHaveCount(0)
  await expect(prBadges(page)).toHaveCount(expected.prs)
  await expect(staleBadges(page, 'ci')).toHaveCount(expected.rows - 1)
  await expect(branches(page).locator('[data-stale-label]')).toHaveCount(0)

  await fixture('release', { components: 'checks' })
  await expect(staleBadges(page, 'ci')).toHaveCount(0)
  await expect(page.locator('.react-flow__node-worktree').getByRole('img', { name: /\(stale\)$/ })).toHaveCount(0)
})

for (const damage of ['missing', 'corrupt', 'oversized', 'unsupported']) {
  test(`a ${damage} cache starts cold, then recovers`, async ({ page }) => {
    await fixture('settle')
    await fixture('restart', { components: 'catalog,checks', cache: damage })
    await openWarm(page)
    // Cold: local Git loads, nothing is restored, nothing is labeled saved.
    await expect(branches(page).locator('[data-stale-label]')).toHaveCount(0)
    await expect(prBadges(page)).toHaveCount(0)
    await fixture('release', { components: 'catalog,checks' })
    await expect(prBadges(page)).toHaveCount(expected.prs)
    // Live data replaced the bad document: the next start is warm again.
    await fixture('settle')
    await fixture('restart', { components: 'local,catalog,checks' })
    await openWarm(page)
    await expect(branches(page).locator('[data-stale-label]')).toHaveText('saved')
    await expect(prBadges(page)).toHaveCount(expected.prs)
  })
}

test('a removed project stays absent after a restart', async ({ page }) => {
  await fixture('settle')
  await fixture('remove', { project: 'gamma' })
  await fixture('restart', { components: 'local,catalog,checks' })
  await openWarm(page)
  const projects = await page.evaluate(() => window.__bonsaiTestStore.getState().projects.map(project => project.id))
  expect(projects).toEqual(['alpha', 'beta'])
})

test('an API restart keeps the page intact and ignores delayed events of the old process', async ({ page }) => {
  // Proxy the event socket so a message of the old process can be replayed
  // after the restart, as a late delivery would.
  let oldUpdate: string | undefined
  const sockets: { send: (message: string) => void }[] = []
  await page.routeWebSocket(`${FIXTURE.replace('http', 'ws')}/events`, socket => {
    const server = socket.connectToServer()
    sockets.push(socket)
    server.onMessage(message => {
      const text = String(message)
      if (!oldUpdate && text.includes('"project_snapshot"') && text.includes('"alpha"')) oldUpdate = text.replace('"project_snapshot"', '"project_update"').replace(/"sequence":\d+/g, '"sequence":999999')
      socket.send(message)
    })
  })
  await fixture('settle')
  await fixture('restart')
  await openWarm(page)
  await expect(prBadges(page)).toHaveCount(expected.prs)
  const epochBefore = (await fixture('release')).epoch
  await fixture('restart', { components: 'local,catalog,checks' })
  await expect(branches(page).locator('[data-stale-label]')).toBeVisible({ timeout: 15_000 })
  // The old capability died with the old process; the page gets a new one
  // on its own and bootstraps from the restored state.
  await expect(branches(page).locator('[data-stale-label]')).toHaveText('saved', { timeout: 15_000 })
  expect(oldUpdate).toContain(epochBefore)
  sockets.at(-1)!.send(oldUpdate!)
  await page.waitForTimeout(300)
  await expect(branches(page).locator('[data-worktree-row]')).toHaveCount(expected.rows)
  await expect(staleBadges(page, 'pr')).toHaveCount(expected.prs)
  await fixture('release', { components: 'local,catalog,checks' })
  await expect(staleBadges(page, 'ci')).toHaveCount(0)
})

test('connects, paints warm and navigates with all browser storage blocked', async ({ page }) => {
  await page.addInitScript(() => {
    for (const name of ['localStorage', 'sessionStorage', 'indexedDB']) {
      Object.defineProperty(window, name, { configurable: true, get() { throw new DOMException('blocked', 'SecurityError') } })
    }
  })
  await fixture('settle')
  await fixture('restart', { components: 'local,catalog,checks' })
  await openWarm(page)
  await expect(branches(page).locator('[data-stale-label]')).toHaveText('saved')
  // The fixture configures no project roots, so the API asks for them.
  await page.getByRole('dialog', { name: 'Choose project folders' }).getByRole('button', { name: 'Later' }).click()
  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved from the last session' })).toBeVisible()
  await expect(page.getByRole('listbox', { name: 'Pull requests' }).getByRole('option')).toHaveCount(SIZE.prs)
  await fixture('release', { components: 'local,catalog,checks' })
  await expect(page.getByRole('status').filter({ hasText: 'Saved from the last session' })).toHaveCount(0)
})
