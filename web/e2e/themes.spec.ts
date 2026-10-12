import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp, openFullStackApp } from './mockGit'
import { builtInThemes } from '../src/theme/themes'
import { terminalTheme } from '../src/theme/terminalColors'
import './presentationFixtures'

const themeKey = 'bonsai-theme-v1'
const root = (page: Page) => page.locator('html')
const channels = (hex: string) => [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16)).join(', ')
const rgb = (hex: string) => `rgb(${channels(hex)})`
const rgba = (hex: string, alpha: number) => `rgba(${channels(hex)}, ${alpha})`

async function picker(page: Page) {
  await page.keyboard.press('Control+k')
  await page.getByRole('option', { name: 'Change theme…', exact: true }).click()
  await expect(page.getByPlaceholder('Search themes…')).toBeFocused()
}
async function choose(page: Page, name: string) {
  await picker(page)
  await page.getByRole('option', { name: new RegExp(`^${name}( Current)?$`) }).click()
  await expect(page.getByRole('dialog', { name: 'Command menu' })).toHaveCount(0)
}

test('Appearance cards persist before first React content and synchronize same-origin tabs', async ({ page, context }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  const appearance = page.getByRole('region', { name: 'Appearance' })
  await expect(appearance.getByRole('radio')).toHaveCount(3)
  const light = appearance.getByRole('radio', { name: 'Modern Light' })
  await light.locator('..').hover()
  await light.focus()
  await expect(root(page)).toHaveAttribute('data-theme', 'bonsai')
  await appearance.getByText('Modern Light', { exact: true }).click()
  await expect(light).toBeChecked()
  await expect(root(page)).toHaveAttribute('data-theme', 'modern-light')
  await expect(page.getByLabel('When opening this project')).toHaveCSS('color-scheme', 'light')
  expect(await page.evaluate(key => JSON.parse(localStorage.getItem(key)!).activeId, themeKey)).toBe('modern-light')

  await page.addInitScript(() => {
    const observer = new MutationObserver(() => {
      if (document.getElementById('root')?.firstElementChild) {
        Object.defineProperty(window, '__firstContentTheme', { value: document.documentElement.dataset.theme })
        observer.disconnect()
      }
    })
    observer.observe(document, { subtree: true, childList: true })
  })
  await page.reload()
  await expect(light).toBeChecked()
  expect(await page.evaluate(() => (window as unknown as { __firstContentTheme: string }).__firstContentTheme)).toBe('modern-light')

  const other = await context.newPage()
  await mockGitBackend(other)
  await openConnectedApp(other)
  await expect(root(other)).toHaveAttribute('data-theme', 'modern-light')
  await appearance.getByText('Modern Dark', { exact: true }).click()
  await expect(root(other)).toHaveAttribute('data-theme', 'modern-dark')
  await choose(other, 'Bonsai')
  await expect(root(page)).toHaveAttribute('data-theme', 'bonsai')
  await other.close()
})

test('nested picker handles both shortcuts, search, Back, Escape, focus trapping and restoration', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  const trigger = page.getByRole('button', { name: 'Open command menu' })
  await trigger.click()
  const commands = page.getByPlaceholder('Search commands and views…')
  await commands.fill('Modern Dark')
  await page.getByRole('option', { name: 'Change theme…' }).click()
  const themes = page.getByPlaceholder('Search themes…')
  await expect(page.getByRole('option', { name: 'Bonsai Current' })).toHaveAttribute('aria-selected', 'true')
  await themes.press('ArrowDown')
  await expect(root(page)).toHaveAttribute('data-theme', 'bonsai')
  await page.keyboard.press('Tab')
  expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(commands).toHaveValue('Modern Dark')
  await page.getByRole('option', { name: 'Change theme…' }).click()
  await themes.fill('light')
  await expect(page.getByRole('option')).toHaveCount(1)
  await themes.press('Backspace')
  await expect(themes).toBeVisible()
  await page.getByRole('button', { name: 'Back to commands' }).click()
  await expect(commands).toHaveValue('Modern Dark')
  await page.getByRole('option', { name: 'Change theme…' }).click()
  await themes.press('Backspace')
  await expect(commands).toHaveValue('Modern Dark')
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
  await page.keyboard.press('Meta+k')
  await expect(commands).toHaveValue('')
  await page.keyboard.press('Meta+k')
  await expect(commands).toHaveCount(0)
  await choose(page, 'Modern Dark')
  await picker(page)
  await expect(page.getByRole('option', { name: 'Modern Dark Current' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('option', { name: 'Modern Dark Current' }).click()
  await expect(root(page)).toHaveAttribute('data-theme', 'modern-dark')
})

for (const failure of [false, true]) {
  test(`command ${failure ? 'failure' : 'loading'} fallback dismisses with Escape`, async ({ page }) => {
    const graph = JSON.parse(readFileSync('dist-e2e/.vite/manifest.json', 'utf8'))
    const chunk = graph['src/features/command-palette/CommandPalette.tsx'].file
    await mockGitBackend(page)
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    await page.route(`**/${chunk}`, async route => {
      if (failure) await route.fulfill({ status: 404, contentType: 'text/javascript', body: '' })
      else { await pending; await route.continue() }
    })
    try {
      await openConnectedApp(page)
      await page.keyboard.press('Control+k')
      await expect(page.getByText(failure ? 'This screen could not load' : 'Loading commands…')).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
    } finally { release?.() }
  })
}

test('30 warmed switches preserve workspace state and avoid network, storage and graph work', async ({ page }, info) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.addInitScript(() => {
    window.__bonsaiMetrics = { graph: 0, labels: 0, commits: [] }
    const writes = { workspace: 0, theme: 0 }
    Object.defineProperty(window, '__themeWrites', { value: writes })
    const setItem = Storage.prototype.setItem
    Storage.prototype.setItem = function(key, value) {
      if (key === 'bonsai-web-workspace-v6') writes.workspace++
      if (key === 'bonsai-theme-v1') writes.theme++
      return setItem.call(this, key, value)
    }
  })
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(1000) // Complete initial Fit and preference debounce.
  const state = () => page.evaluate(() => {
    const s = window.__bonsaiTestStore.getState()
    return { project: s.activeProjectId, selection: s.selection, viewport: s.viewport, placements: s.nodePlacements, dockState: s.dockState, dockHeight: s.dockHeight }
  })
  const before = await state()
  const samples: number[] = []
  const applySamples: number[] = []
  const firstFrameSamples: number[] = []
  const followingFrameSamples: number[] = []
  let apiRequests = 0
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests++ })
  const metrics = () => page.evaluate(() => ({
    graph: window.__bonsaiMetrics.graph, labels: window.__bonsaiMetrics.labels,
    ...(window as unknown as { __themeWrites: { workspace: number; theme: number } }).__themeWrites,
  }))
  const baseline = await metrics()
  const cdp = process.env.BONSAI_THEME_TRACE === '1' ? await page.context().newCDPSession(page) : undefined
  await cdp?.send('Tracing.start', { categories: 'devtools.timeline,blink.user_timing', transferMode: 'ReturnAsStream' })
  for (let index = 0; index < 33; index++) {
    const theme = index % 2 === 0 ? 'Modern Light' : 'Modern Dark'
    await picker(page)
    // Measure from selection to a task after the first rendering opportunity.
    // Also retain the following-frame upper bound and trace markers for audit.
    // Excludes Playwright input transport and cold command chunk loading.
    const duration = await page.getByRole('option', { name: theme, exact: true }).evaluate((element, index) => new Promise<{ applyMs: number; firstFrameMs: number; paintedMs: number; followingFrameMs: number }>(resolve => {
      performance.mark(`bonsai:theme-switch-${index}`)
      const started = performance.now()
      ;(element as HTMLElement).click()
      const applyMs = performance.now() - started
      requestAnimationFrame(() => {
        const firstFrameMs = performance.now() - started
        // A task after the first rendering opportunity avoids counting a whole
        // extra frame of compositor scheduling as selection-to-paint latency.
        const painted = new Promise<number>(done => setTimeout(() => done(performance.now() - started), 0))
        requestAnimationFrame(async () => {
          const followingFrameMs = performance.now() - started
          resolve({ applyMs, firstFrameMs, paintedMs: await painted, followingFrameMs })
        })
      })
    }), index)
    await expect(root(page)).toHaveAttribute('data-theme', index % 2 === 0 ? 'modern-light' : 'modern-dark')
    if (index >= 3) {
      samples.push(duration.paintedMs)
      applySamples.push(duration.applyMs)
      firstFrameSamples.push(duration.firstFrameMs)
      followingFrameSamples.push(duration.followingFrameMs)
    }
  }
  if (cdp) {
    const complete = new Promise<string>(resolve => cdp.once('Tracing.tracingComplete', event => resolve(event.stream)))
    await cdp.send('Tracing.end')
    const handle = await complete
    let trace = ''
    for (;;) {
      const result = await cdp.send('IO.read', { handle })
      trace += result.base64Encoded ? Buffer.from(result.data, 'base64').toString() : result.data
      if (result.eof) break
    }
    await cdp.send('IO.close', { handle })
    mkdirSync(info.outputDir, { recursive: true })
    const path = info.outputPath('theme-switch-trace.json')
    writeFileSync(path, trace)
    await info.attach('theme-switch-trace.json', { contentType: 'application/json', path })
  }
  expect(await state()).toEqual(before)
  expect(apiRequests).toBe(0)
  const after = await metrics()
  expect(after.graph).toBe(baseline.graph)
  expect(after.labels).toBe(baseline.labels)
  expect(after.workspace).toBe(baseline.workspace)
  expect(after.theme - baseline.theme).toBe(33)
  const sorted = [...samples].sort((a, b) => a - b)
  const p95 = sorted[Math.ceil(sorted.length * .95) - 1]
  mkdirSync(info.outputDir, { recursive: true })
  const report = info.outputPath('theme-switch-performance.json')
  writeFileSync(report, JSON.stringify({ viewport: '1440x900', method: 'first-frame post-render task (paint proxy); following-frame upper bound retained', warmups: 3, samples, applySamples, firstFrameSamples, followingFrameSamples, p95, targetMs: 50, before: baseline, after, apiRequests }, null, 2))
  await info.attach('theme-switch-performance.json', { contentType: 'application/json', path: report })
  const applyP95 = [...applySamples].sort((a, b) => a - b)[Math.ceil(applySamples.length * .95) - 1]
  console.log(`Theme synchronous selection/apply p95: ${applyP95.toFixed(1)} ms`)
  console.log(`Theme selection-to-paint p95: ${p95.toFixed(1)} ms (30 warmed switches; target ≤50 ms)`)
})

for (const theme of builtInThemes) {
  test(`${theme.name} representative screens under production CSP`, async ({ page }, info) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await mockGitBackend(page)
    await page.route('**/api/settings/editors', route => route.fulfill({ json: { environment: { id: 'local', label: 'this computer' }, revision: 1, editors: [{ id: 'vscode', name: 'Visual Studio Code', command: 'code', folders: true }], preferred: 'vscode', saved_preferred: 'vscode' } }))
    await openConnectedApp(page)
    await choose(page, theme.name)
    await expect(page.locator('.app-surface')).toHaveCSS('background-color', rgb(theme.colors.bg))
    await expect(root(page)).not.toHaveAttribute('data-theme-switching', '')
    await expect(page.getByRole('button', { name: 'Agent', exact: true })).toHaveCSS('background-color', rgb(theme.colors['accent-solid']))
    await page.screenshot({ path: info.outputPath(`${theme.id}-workspace.png`) })
    await page.getByRole('link', { name: 'Settings', exact: true }).click()
    await expect(page.getByRole('radio', { name: theme.name })).toBeChecked()
    await page.screenshot({ path: info.outputPath(`${theme.id}-settings.png`) })
    await picker(page)
    const current = page.getByRole('option', { name: `${theme.name} Current` })
    await expect(current).toHaveAttribute('aria-selected', 'true')
    await expect(current).toHaveCSS('background-color', rgba(theme.colors.accent, .12))
    await page.screenshot({ path: info.outputPath(`${theme.id}-picker.png`) })
    await page.keyboard.press('Control+k')
    await page.getByRole('link', { name: 'GitHub', exact: true }).click()
    await page.getByRole('option', { name: /^#23 / }).click()
    await expect(page.getByText('Make daemon shutdown cleanup deterministic.').first()).toBeVisible()
    await page.screenshot({ path: info.outputPath(`${theme.id}-prs.png`) })
    expect(errors).toEqual([])
  })
}

test('real PTY stays connected and retains output while ANSI colors follow all themes', async ({ page }, info) => {
  test.setTimeout(60000)
  test.skip(!['linux', 'darwin'].includes(process.platform), 'Native Unix PTY fixture')
  await page.setViewportSize({ width: 1440, height: 900 })
  await openFullStackApp(page)
  await page.keyboard.press('Control+k')
  await page.getByRole('option', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await dialog.getByRole('button', { name: /Antigravity/ }).click()
  await dialog.getByLabel('Worktree and branch').click()
  await page.getByRole('option').first().click()
  await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
  const terminal = page.locator('.xterm')
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await expect(terminal).toContainText('READY')
  await terminal.evaluate(element => { (element as HTMLElement).dataset.themeProbe = 'retained' })
  const input = page.locator('.xterm-helper-textarea')
  await input.pressSequentially('theme-retained-output')
  await input.press('Enter')
  await expect(terminal).toContainText('REPLY:theme-retained-output')
  let sockets = 0
  page.on('websocket', () => sockets++)
  for (const theme of builtInThemes) {
    await choose(page, theme.name)
    await expect(terminal).toHaveAttribute('data-theme-probe', 'retained')
    await expect(terminal).toContainText('REPLY:theme-retained-output')
    await expect(page.locator('.runtime-tile')).toContainText('running · connected')
    const ansi = page.locator('.xterm-fg-2').filter({ hasText: 'READY' }).first()
    await expect(ansi).toHaveCSS('color', rgb(terminalTheme(theme).green))
    await expect(page.locator('.xterm-screen')).toHaveCSS('background-color', rgb(theme.colors.well))
    await page.screenshot({ path: info.outputPath(`${theme.id}-terminal.png`) })
  }
  expect(sockets).toBe(0)
  await input.pressSequentially('after-theme-switch')
  await input.press('Enter')
  await expect(terminal).toContainText('REPLY:after-theme-switch')
  await page.getByRole('button', { name: 'Stop agent', exact: true }).first().click()
  await expect(page.locator('.runtime-tile')).toContainText('exited')
})
