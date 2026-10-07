import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import './presentationFixtures'

async function openProcessFixture(page: Page) {
  await page.setViewportSize({ width: 1500, height: 1000 })
  await mockGitBackend(page)
  await page.routeWebSocket(/ws:\/\/127\.0\.0\.1:7001\/api\/projects\/bonsai\/processes\/\d+\/terminal$/, socket => {
    socket.onMessage(message => {
      if (JSON.parse(String(message)).type !== 'attach') return
      const output = Buffer.from('RETAINED_PROCESS_OUTPUT\r\n')
      socket.send(JSON.stringify({ type: 'ready', version: 1, generation: 'process-node-fixture' }))
      socket.send(JSON.stringify({ type: 'output', offset: output.length, data: output.toString('base64') }))
    })
  })
  await openConnectedApp(page)
  return page.getByTestId('rf__node-bonsai:1')
}

test('process context menus inspect and navigate without opening output, then explicitly replay output', async ({ page }) => {
  const node = await openProcessFixture(page)
  await node.screenshot({ path: test.info().outputPath('process-node-running.png') })
  await node.click({ button: 'right' })
  await page.getByRole('menu', { name: 'Process actions' }).screenshot({ path: test.info().outputPath('process-node-menu.png') })
  for (const name of ['Open output', 'Open inspector', 'Open worktree', 'Stop process', 'Restart process']) {
    await expect(page.getByRole('menuitem', { name, exact: true })).toBeEnabled()
  }
  await page.getByRole('menuitem', { name: 'Open inspector', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Inspector' })).toContainText('Vite')
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().openRuntimeIds)).toEqual([])
  await node.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Open worktree', exact: true }).click()
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().selection)).toEqual({ type: 'worktree', id: 'wt-web' })
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().openRuntimeIds)).toEqual([])
  await node.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Open output', exact: true }).click()
  await expect(page.locator('[data-process-id="bonsai:1"] .xterm-screen')).toContainText('RETAINED_PROCESS_OUTPUT')
})

test('context menu and card controls share pending/errors and restart without opening a closed view or moving its node', async ({ page }) => {
  const node = await openProcessFixture(page)
  const placement = await page.evaluate(() => window.__bonsaiTestStore.getState().nodePlacements['bonsai:1'])
  let finishStop: () => void = () => {}
  const stopRequests: string[] = []
  await page.route('http://127.0.0.1:7001/api/projects/bonsai/processes/1', async route => {
    if (route.request().method() !== 'DELETE') { await route.fallback(); return }
    stopRequests.push(route.request().url())
    await new Promise<void>(resolve => { finishStop = resolve })
    await route.fulfill({ status: 503, json: { error: { message: 'Process control unavailable' } } })
  })
  await node.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Stop process', exact: true }).click()
  await expect(node.getByRole('status')).toHaveText('Stopping')
  await expect(node.getByRole('button', { name: 'Stop process', exact: true })).toBeDisabled()
  await expect(node.getByRole('button', { name: 'Restart process', exact: true })).toBeDisabled()
  await node.click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: 'Stopping process…', exact: true })).toBeDisabled()
  await expect(page.getByRole('menuitem', { name: 'Restart process', exact: true })).toBeDisabled()
  await expect.poll(() => stopRequests.length).toBe(1)
  finishStop()
  await page.keyboard.press('Escape')
  await expect(node.getByRole('alert')).toHaveText('Process control unavailable')
  await page.route('http://127.0.0.1:7001/api/projects/bonsai/processes/1/restart', async route => {
    await route.fulfill({ json: { id: 'bonsai:1', daemon_id: 1, project_id: 'bonsai', worktree_id: 'wt-web', label: 'Vite', command: 'pnpm dev', status: 'running', revision: 2 } })
  })
  await node.click({ button: 'right' })
  const restart = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/processes/1/restart'))
  await page.getByRole('menuitem', { name: 'Restart process', exact: true }).click()
  await restart
  await expect(node.getByRole('alert')).toHaveCount(0)
  await expect(node.getByRole('status')).toHaveText('Running')
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().openRuntimeIds)).toEqual([])
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().nodePlacements['bonsai:1'])).toEqual(placement)
  await expect(node).toHaveCount(1)
})

test('retained terminal states disable stop in the menu and card; unresolved associations disable worktree navigation', async ({ page }) => {
  const node = await openProcessFixture(page)
  for (const lifecycleStatus of ['failed', 'stopped', 'lost', 'done', 'stopping'] as const) {
    await page.evaluate(lifecycleStatus => {
      const store = window.__bonsaiTestStore
      store.setState({ processes: store.getState().processes.map(process => process.id === 'bonsai:1' ? {
        ...process, lifecycleStatus, exitCode: lifecycleStatus === 'failed' ? 2 : lifecycleStatus === 'done' ? 0 : undefined,
      } : process) })
    }, lifecycleStatus)
    if (lifecycleStatus === 'failed') await node.screenshot({ path: test.info().outputPath('process-node-failed.png') })
    await node.click({ button: 'right' })
    await expect(page.getByRole('menuitem', { name: 'Stop process', exact: true })).toBeDisabled()
    await expect(page.getByRole('menuitem', { name: 'Restart process', exact: true })).toBeEnabled()
    await page.keyboard.press('Escape')
    await expect(node.getByRole('button', { name: 'Stop process', exact: true })).toBeDisabled()
  }
  await page.evaluate(() => {
    const store = window.__bonsaiTestStore
    store.setState({ processes: store.getState().processes.map(process => process.id === 'bonsai:1' ? { ...process, worktreeId: '' } : process) })
  })
  await page.getByRole('button', { name: 'Fit', exact: true }).click()
  await node.click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: 'Open worktree', exact: true })).toBeDisabled()
  await expect(page.getByRole('menuitem', { name: 'Open output', exact: true })).toBeEnabled()
})
