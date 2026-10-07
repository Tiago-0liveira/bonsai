import { expect, test, type Page } from '@playwright/test'
import { openConnectedApp } from './mockGit'
import './presentationFixtures'

test.skip(!['linux', 'darwin'].includes(process.platform), 'Native daemon fixture')
test.setTimeout(90000)

async function launch(page: Page, command: string, policy: string, retries = 0, args = '', branch?: string) {
  await page.keyboard.press('Control+k')
  await page.getByRole('option', { name: 'Start process', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start process' })
  if (branch || !(await dialog.getByLabel('Process command').isVisible())) {
    await dialog.getByLabel('Process worktree and branch').click()
    if (branch) await page.getByRole('option', { name: new RegExp(`^${branch}\\b`) }).click()
    else await page.getByRole('option').first().click()
  }
  await dialog.getByLabel('Process command').click()
  await page.getByRole('option', { name: new RegExp(`^${command}\\b`) }).click()
  await dialog.getByLabel('Restart policy').click()
  await page.getByRole('option', { name: policy, exact: true }).click()
  if (policy !== 'Never') await dialog.getByLabel('Maximum retries').fill(String(retries))
  if (args) await dialog.getByLabel('arguments', { exact: true }).fill(args)
  const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/projects/repo/processes'))
  await dialog.getByRole('button', { name: 'Start process', exact: true }).click()
  const summary = await (await response).json()
  await expect(dialog).toHaveCount(0)
  await expect(page.locator(`[data-process-node-id="${summary.id}"]`)).toHaveCount(1)
  const terminal = page.locator(`[data-process-id="${summary.id}"]`)
  await expect(terminal.locator('.xterm')).toBeVisible()
  return { terminal, summary }
}

test('real daemon: launch opens logs, retries and restart retain history, duplicate commands stay isolated', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 1100 })
  await openConnectedApp(page)
  const { terminal: failed, summary } = await launch(page, 'fail', 'On failure', 1, 'BROWSER_ARG\nargument with spaces')
  await page.getByTitle('Maximize workspace', { exact: true }).click()
  await expect(failed).toContainText('failed · connected · exit 2', { timeout: 15000 })
  await expect(failed.locator('.xterm-screen')).toContainText('FINAL_WITHOUT_NEWLINE')
  await expect(failed).toContainText('retries 1/1')
  const downloadEvent = page.waitForEvent('download')
  await failed.getByRole('button', { name: 'Download retained logs' }).click()
  const download = await downloadEvent
  const stream = await download.createReadStream()
  const chunks: Buffer[] = []
  for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
  const logs = Buffer.concat(chunks).toString('utf8')
  expect(logs.match(/INITIAL_OUTPUT/g)).toHaveLength(2)
  expect(logs).toContain('ARGV:["BROWSER_ARG","argument with spaces"]')
  expect(logs).toContain('éFINAL_WITHOUT_NEWLINE')
  expect(logs).toContain('retries exhausted')
  await failed.getByRole('button', { name: 'Restart process', exact: true }).click()
  await expect(failed).toContainText('attempt 4', { timeout: 15000 })
  await expect(failed).toContainText('failed · connected · exit 2')
  await expect(page.locator(`[data-process-node-id="${summary.id}"]`)).toHaveCount(1)
  const first = await launch(page, 'stay', 'Never', 0, 'ONLY_A')
  const second = await launch(page, 'stay', 'Never', 0, 'ONLY_B')
  await expect(first.terminal).toContainText('running · connected')
  await expect(second.terminal).toContainText('running · connected')
  await expect(first.terminal.locator('.xterm-screen')).toContainText('ONLY_A')
  await expect(second.terminal.locator('.xterm-screen')).toContainText('ONLY_B')
  await expect(first.terminal.locator('.xterm-screen')).not.toContainText('ONLY_B')
  // Closing the view detaches without stopping the running command.
  await second.terminal.locator('xpath=ancestor::section[1]').getByRole('button', { name: 'Close runtime card', exact: true }).click()
  await expect(page.locator(`[data-process-node-id="${second.summary.id}"]`)).toHaveCount(1)
  await expect.poll(() => page.evaluate(id => window.__bonsaiTestStore.getState().processes.find(process => process.id === id)?.lifecycleStatus, second.summary.id)).toBe('running')
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  await page.getByRole('option', { name: /stay.*ONLY_B/ }).click()
  const reopened = page.locator(`[data-process-id="${second.summary.id}"]`)
  await expect(reopened).toContainText('running · connected')
  await expect(reopened.locator('.xterm-screen')).toContainText('ONLY_B')
  await first.terminal.getByRole('button', { name: 'Stop process', exact: true }).click()
  await reopened.getByRole('button', { name: 'Stop process', exact: true }).click()
  await expect(first.terminal).toContainText('stopped')
  await expect(reopened).toContainText('stopped')
  let ticks = 0
  page.on('websocket', socket => socket.on('framereceived', event => {
    try {
      const frame = JSON.parse(String(event.payload))
      if (frame.type === 'output' && Buffer.from(frame.data, 'base64').toString().includes('SCROLL_TICK')) ticks++
    } catch { /* unrelated streams */ }
  }))
  const scrolled = await launch(page, 'scroll', 'Never')
  const viewport = scrolled.terminal.locator('.xterm-viewport')
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
  await viewport.evaluate(element => { element.scrollTop = 0 })
  const before = ticks
  await expect.poll(() => ticks).toBeGreaterThan(before)
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBe(0)
  await scrolled.terminal.getByRole('button', { name: 'Stop process', exact: true }).click()
  const backoff = await launch(page, 'fail', 'On failure', 3, 'CANCEL_RETRY')
  await expect(backoff.terminal).toContainText('backoff', { timeout: 5000 })
  await backoff.terminal.getByRole('button', { name: 'Stop process', exact: true }).click()
  await expect(backoff.terminal).toContainText('stopped')
  await expect(backoff.terminal).toContainText('attempt 1')
  await page.reload()
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect(page.locator(`[data-process-node-id="${second.summary.id}"]`)).toHaveCount(1)
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  await page.getByRole('option', { name: /stay.*ONLY_B/ }).click()
  await expect(page.locator(`[data-process-id="${second.summary.id}"]`)).toContainText('stopped · connected')
  await expect(page.locator(`[data-process-id="${second.summary.id}"] .xterm-screen`)).toContainText('ONLY_B')
})

test('real daemon: empty panels and saved subsets restore across projects, worktrees and reconnect', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 1100 })
  await openConnectedApp(page)
  const launchA = await launch(page, 'stay', 'Never', 0, 'RESTORE_A', 'main')
  const launchB = await launch(page, 'stay', 'Never', 0, 'RESTORE_B', 'main')
  const launchC = await launch(page, 'stay', 'Never', 0, 'RESTORE_C', 'main')
  const launchD = await launch(page, 'stay', 'Never', 0, 'RESTORE_D', 'feat/restoration')
  const launchE = await launch(page, 'stay', 'Never', 0, 'RESTORE_E', 'feat/restoration')
  const launches = [launchA, launchB, launchC, launchD, launchE]
  const ids = launches.map(launch => launch.summary.id as string)
  const cards = () => page.locator('.runtime-tile [data-process-id]').evaluateAll(elements => elements.map(element => element.getAttribute('data-process-id')))
  const branch = async (name: string) => page.getByRole('button', { name, exact: true }).click()
  const project = async (name: string) => {
    await page.getByRole('button', { name: 'Project', exact: true }).click()
    await page.getByRole('option', { name, exact: true }).click()
  }
  const closeAll = async () => {
    while (await page.getByRole('button', { name: 'Close runtime card', exact: true }).count()) {
      await page.getByRole('button', { name: 'Close runtime card', exact: true }).first().click()
    }
  }
  const reconnect = async () => {
    await page.reload()
    await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
    await expect(page.locator('.react-flow')).toBeVisible()
    for (const id of ids) await expect(page.locator(`[data-process-node-id="${id}"]`)).toHaveCount(1)
  }
  await closeAll()
  await branch('main'); await closeAll()
  await project('Restoration fixture'); await project('PTY fixture')
  await reconnect()
  await expect(page.locator('.runtime-tile')).toHaveCount(0)
  await branch('feat/restoration')
  await expect(page.locator('.runtime-tile')).toHaveCount(0)
  expect(await page.evaluate(ids => ids.every(id => window.__bonsaiTestStore.getState().processes.find(process => process.id === id)?.lifecycleStatus === 'running'), ids)).toBe(true)

  const reopen = async (marker: string, id: string) => {
    await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
    await page.getByRole('option', { name: new RegExp(marker) }).click()
    await expect(page.locator(`[data-process-id="${id}"] .xterm-screen`)).toContainText(marker)
  }
  await reopen('RESTORE_D', ids[3])
  await branch('main')
  await reopen('RESTORE_C', ids[2]); await reopen('RESTORE_A', ids[0])
  // Exercise the sortable cards through their drag handles and persist C after A.
  const c = page.locator(`[data-process-id="${ids[2]}"]`).locator('xpath=ancestor::section[1]').locator('.runtime-heading')
  const a = page.locator(`[data-process-id="${ids[0]}"]`).locator('xpath=ancestor::section[1]').locator('.runtime-heading')
  const cBox = (await c.boundingBox())!, aBox = (await a.boundingBox())!
  await page.mouse.move(cBox.x + 10, cBox.y + cBox.height / 2); await page.mouse.down()
  await page.mouse.move(aBox.x + aBox.width / 2, aBox.y + aBox.height / 2, { steps: 12 }); await page.mouse.up()
  await expect.poll(cards).toEqual([ids[0], ids[2]])
  await a.click()
  await project('Restoration fixture'); await project('PTY fixture')
  await expect.poll(cards).toEqual([ids[0], ids[2]])
  await reconnect()
  await expect.poll(cards).toEqual([ids[0], ids[2]])
  await expect(page.locator('.runtime-tile-active [data-process-id]')).toHaveAttribute('data-process-id', ids[0])
  await branch('feat/restoration')
  await expect.poll(cards).toEqual([ids[3]])
  await expect(page.locator(`[data-process-id="${ids[3]}"] .xterm-screen`)).toContainText('RESTORE_D')
  await reconnect()
  await expect.poll(cards).toEqual([ids[3]])
  // Cleanup uses explicit stops only on this isolated fixture's new processes.
  await launchD.terminal.getByRole('button', { name: 'Stop process', exact: true }).click()
  await page.evaluate(ids => {
    for (const id of ids) window.__bonsaiTestStore.getState().openRuntime(id)
  }, ids.slice(0, 3))
  for (const id of ids.slice(0, 3)) await page.locator(`[data-process-id="${id}"]`).getByRole('button', { name: 'Stop process', exact: true }).click()
  await branch('feat/restoration'); await reopen('RESTORE_E', ids[4])
  await launchE.terminal.getByRole('button', { name: 'Stop process', exact: true }).click()
})
