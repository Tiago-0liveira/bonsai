import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import './presentationFixtures'

test('records main workspace computations and Profiler commits independently of network/storage', async ({ page }, info) => {
  let requests = 0
  page.on('request', request => { if (request.url().includes('/api/')) requests++ })
  await page.addInitScript(() => {
    window.__bonsaiMetrics = { graph: 0, labels: 0, commits: [] }
    const set = Storage.prototype.setItem
    Object.defineProperty(window, '__workspaceWrites', { value: { count: 0 } })
    Storage.prototype.setItem = function(key, value) {
      if (key === 'bonsai-web-workspace-v6') (window as unknown as { __workspaceWrites: { count: number } }).__workspaceWrites.count++
      return set.call(this, key, value)
    }
  })
  await mockGitBackend(page)
  await openConnectedApp(page)
  await expect(page.locator('.react-flow')).toBeVisible()
  await page.waitForTimeout(800) // Finish initialization, Fit and preference debounce.
  const read = () => page.evaluate(() => ({
    graph: window.__bonsaiMetrics.graph, labels: window.__bonsaiMetrics.labels,
    commits: window.__bonsaiMetrics.commits.length,
    durationMs: window.__bonsaiMetrics.commits.reduce((sum, commit) => sum + commit.duration, 0),
    writes: (window as unknown as { __workspaceWrites: { count: number } }).__workspaceWrites.count,
  }))
  const results: Record<string, unknown> = {}
  const measure = async (name: string, update: () => Promise<unknown>) => {
    const before = await read()
    const beforeRequests = requests
    await update()
    await page.waitForTimeout(250)
    const after = await read()
    const delta = Object.fromEntries(Object.keys(before).map(key => [key, after[key as keyof typeof after] - before[key as keyof typeof before]]))
    results[name] = { ...delta, requests: requests - beforeRequests }
    return delta
  }
  const processOnly = await measure('process-only', () => page.evaluate(() => {
    const store = window.__bonsaiTestStore
    store.setState({ processes: store.getState().processes.map(process => ({ ...process, pid: (process.pid ?? 0) + 1 })) })
  }))
  expect(processOnly).toMatchObject({ graph: 0, labels: 0, commits: 0, writes: 0 })
  const inactive = await measure('inactive-project', () => page.evaluate(() => {
    const store = window.__bonsaiTestStore
    store.setState({ projects: store.getState().projects.map(project => project.id === 'sprout-lab' ? { ...project, health: 'warning' } : project) })
  }))
  expect(inactive).toMatchObject({ graph: 0, labels: 0, commits: 0, writes: 0 })
  const selection = await measure('selection-only', () => page.evaluate(() => window.__bonsaiTestStore.getState().setSelection({ type: 'worktree', id: 'wt-daemon' })))
  expect(selection.graph).toBe(0)
  expect(selection.labels).toBe(0)
  expect(selection.commits).toBeGreaterThan(0)
  const status = await measure('worktree-status', () => page.evaluate(() => {
    const store = window.__bonsaiTestStore
    store.setState({ worktrees: store.getState().worktrees.map(tree => tree.id === 'wt-daemon' ? { ...tree, ciStatus: 'passed', ciFailed: 0 } : tree) })
  }))
  expect(status.graph).toBeGreaterThan(0)
  expect(status.writes).toBe(0)
  await info.attach('workspace-profile.json', { body: JSON.stringify(results, null, 2), contentType: 'application/json' })
  console.log('Workspace profile:', JSON.stringify(results))
})
