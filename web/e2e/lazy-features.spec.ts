import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend } from './mockGit'
import './presentationFixtures'

type Chunk = { file: string; name?: string; isDynamicEntry?: boolean; imports?: string[]; css?: string[] }
const manifest = (): Record<string, Chunk> => JSON.parse(readFileSync('dist-e2e/.vite/manifest.json', 'utf8'))
const facade = (feature: string) => `src/features/${feature}.tsx`
function manifestKey(feature: string, graph = manifest()) {
  const source = feature === 'app/ApplicationRoot' ? 'src/app/ApplicationRoot.tsx' : facade(feature)
  if (graph[source]) return source
  // Vite can emit a shared dynamic chunk without a facade key.
  const name = feature.split('/').at(-1)
  const key = Object.keys(graph).find(key => graph[key].name === name && graph[key].isDynamicEntry)
  if (!key) throw new Error(`No dynamic chunk for ${source}`)
  return key
}
const chunk = (feature: string) => { const graph = manifest(); return graph[manifestKey(feature, graph)] }
const localOrigin = 'http://127.0.0.1:7011'

async function openLocal(page: Page, route = '') {
  await mockGitBackend(page, false, false, false, localOrigin)
  await page.goto(`${localOrigin}/app${route}`)
}

// Collect the facade's full static graph, including shared CSS preloads.
function staticAssets(feature: string) {
  const graph = manifest(), files = new Set<string>(), visited = new Set<string>()
  const visit = (key: string) => {
    if (visited.has(key)) return
    visited.add(key)
    const entry = graph[key]
    files.add(entry.file)
    for (const css of entry.css ?? []) files.add(css)
    for (const dependency of entry.imports ?? []) visit(dependency)
  }
  visit(manifestKey(feature, graph))
  return files
}

test('branches paint while canvas is pending; unused features stay unfetched', async ({ page }) => {
  const requests = new Set<string>()
  page.on('request', request => requests.add(new URL(request.url()).pathname.slice(1)))
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route(`**/${chunk('workspace/WorkspacePage').file}`, async route => { await pending; await route.continue() })
  try {
    await openLocal(page)
    await expect(page.getByText('Loading workspace…')).toBeVisible()
    await expect(page.locator('.desktop-rail').getByText('fix/daemon-lifecycle', { exact: true }).first()).toBeVisible()
    expect(await page.locator('.react-flow').count()).toBe(0)
    for (const feature of ['files/FilesPage', 'files/FilesPanel', 'github/PullRequestsRoute', 'github/PullRequestDetail', 'settings/SettingsPage', 'logs/LogsPage', 'terminal/AgentTerminal', 'terminal/ProcessTerminal', 'command-palette/CommandPalette', 'workspace/CreateWorktreeDialog', 'workspace/DeleteWorktreeDialog', 'workspace/StartAgentDialog', 'workspace/StartProcessDialog', 'workspace/EnvEditor', 'agents/AgentsRoute']) {
      expect(requests.has(chunk(feature).file), feature).toBe(false)
    }
    // Session launch details load with the dialog, the Agents page or a live session's inspector card.
    expect(requests.has(manifest()['src/api/launchDetails.ts'].file), 'api/launchDetails').toBe(false)
  } finally { release() }
  await expect(page.locator('.react-flow')).toBeVisible()
})

for (const failure of ['missing JS', 'missing CSS', 'offline import']) {
  test(`${failure}: local screen recovers and retains branches without reloading`, async ({ page }) => {
    const assets = staticAssets('workspace/WorkspacePage')
    const asset = failure === 'missing CSS' ? [...assets].find(file => file.endsWith('.css'))! : chunk('workspace/WorkspacePage').file
    expect(asset).toBeTruthy()
    let documentRequests = 0
    page.on('request', request => { if (request.isNavigationRequest() && request.frame() === page.mainFrame()) documentRequests++ })
    await page.route(`**/${asset}`, route => failure === 'offline import'
      ? route.abort('internetdisconnected')
      : route.fulfill({ status: 404, contentType: asset.endsWith('.css') ? 'text/css' : 'text/javascript', body: '' }))
    await openLocal(page)
    await expect(page.getByText('This screen could not load')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Reload app' })).toBeVisible()
    await expect(page.locator('.desktop-rail').getByText('fix/daemon-lifecycle', { exact: true }).first()).toBeVisible()
    await page.waitForTimeout(500)
    expect(documentRequests).toBe(1)
  })
}

test('initial surface failure has eager recovery and an explicit reload action', async ({ page }) => {
  const application = chunk('app/ApplicationRoot').file
  let documentRequests = 0
  page.on('request', request => { if (request.isNavigationRequest() && request.frame() === page.mainFrame()) documentRequests++ })
  await page.route(`**/${application}`, route => route.fulfill({ status: 404, contentType: 'text/javascript', body: '' }))
  await page.goto(`${localOrigin}/app`)
  await expect(page.getByRole('button', { name: 'Reload app' })).toBeVisible()
  expect(documentRequests).toBe(1)
  await page.getByRole('button', { name: 'Reload app' }).click()
  await expect.poll(() => documentRequests).toBe(2)
  await expect(page.getByRole('button', { name: 'Reload app' })).toBeVisible()
})

test('PR list stays usable while markdown detail is pending', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route(`**/${chunk('github/PullRequestDetail').file}`, async route => { await pending; await route.continue() })
  try {
    await openLocal(page, '/github')
    await expect(page.getByText('Loading pull request…')).toBeVisible()
    const row = page.getByRole('option', { name: /#23 fix\(daemon\): stabilize lifecycle cleanup/ })
    await expect(row).toBeVisible()
    await row.click()
    await expect(page.getByText('Loading pull request…')).toBeVisible()
  } finally { release() }
  await expect(page.getByText('Make daemon shutdown cleanup deterministic.').first()).toBeVisible()
})

test('command shortcut loads on demand, focuses input and dismisses with Escape', async ({ page }) => {
  const requests = new Set<string>()
  page.on('request', request => requests.add(new URL(request.url()).pathname.slice(1)))
  await openLocal(page)
  await expect(page.locator('.react-flow')).toBeVisible()
  expect(requests.has(chunk('command-palette/CommandPalette').file)).toBe(false)
  await page.keyboard.press('Control+k')
  await expect(page.getByPlaceholder('Search commands and views…')).toBeFocused()
  expect(requests.has(chunk('command-palette/CommandPalette').file)).toBe(true)
  await page.keyboard.press('Escape')
  await expect(page.getByPlaceholder('Search commands and views…')).toHaveCount(0)
})

for (const [route, feature] of [['files', 'files/FilesPage'], ['settings', 'settings/SettingsPage'], ['logs', 'logs/LogsPage'], ['agents', 'agents/AgentsRoute']] as const) {
  test(`${route} loads on demand under the local production CSP without fetching canvas`, async ({ page }) => {
    const requests = new Set<string>(), scriptViolations: string[] = []
    page.on('request', request => requests.add(new URL(request.url()).pathname.slice(1)))
    page.on('console', message => { if (/Refused to load.*(?:script|stylesheet)/i.test(message.text())) scriptViolations.push(message.text()) })
    await openLocal(page, `/${route}`)
    await expect(page.locator('main .island').first()).toBeVisible()
    await expect.poll(() => requests.has(chunk(feature).file)).toBe(true)
    expect(requests.has(chunk('workspace/WorkspacePage').file)).toBe(false)
    expect(scriptViolations).toEqual([])
  })
}

test('a restored terminal in the collapsed dock waits for visibility and stays mounted across navigation', async ({ page }) => {
  const requests = new Set<string>()
  page.on('request', request => requests.add(new URL(request.url()).pathname.slice(1)))
  await openLocal(page)
  await expect(page.locator('.react-flow')).toBeVisible()
  await page.evaluate(() => {
    const state = window.__bonsaiTestStore.getState()
    state.setDockState('collapsed')
    state.openRuntime('bonsai:1')
    // Opening a runtime expands the dock; collapse in the same update turn.
    window.__bonsaiTestStore.getState().setDockState('collapsed')
  })
  await expect(page.getByRole('button', { name: 'Open workspace' })).toBeVisible()
  expect(requests.has(chunk('terminal/ProcessTerminal').file)).toBe(false)
  await page.getByRole('button', { name: 'Open workspace' }).click()
  const terminal = page.locator('[data-process-id="bonsai:1"]')
  await expect(terminal).toBeVisible()
  const element = await terminal.elementHandle()
  await page.getByTitle('Minimize workspace').click()
  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  await expect(page.getByRole('option', { name: /#23 fix\(daemon\): stabilize lifecycle cleanup/ })).toBeVisible()
  await page.getByRole('button', { name: 'Open workspace' }).click()
  expect(await element!.evaluate(node => node === document.querySelector('[data-process-id="bonsai:1"]'))).toBe(true)
})
