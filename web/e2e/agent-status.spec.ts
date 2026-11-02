import { expect, test } from '@playwright/test'
import { DEFAULT_API_ORIGIN, mockGitBackend, openConnectedApp } from './mockGit'

const since = '2099-01-01T00:00:00Z'

test('a project snapshot carrying activity shows what the agent needs, in the node, the row and the tab title', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.stack ?? error.message))
  await page.setViewportSize({ width: 1600, height: 960 })
  const backend = await mockGitBackend(page, false, false, false, DEFAULT_API_ORIGIN, { enabled: true })
  await openConnectedApp(page)

  const agent = page.getByTestId('rf__node-codex-session-1')
  const row = page.locator('[data-worktree-row="wt-daemon"]')
  await expect(agent.getByRole('img', { name: 'Running' })).toBeVisible()
  const baseTitle = await page.title()

  backend.setAgentActivity('codex-session-1', { state: 'waiting', reason: 'permission', detail: 'Bash', since, source: 'hook' })
  await expect(agent).toContainText('Needs permission: Bash')
  await expect(agent.getByRole('img', { name: 'Needs permission: Bash' })).toBeVisible()
  await expect(page.getByTestId('rf__node-wt-daemon').getByRole('img', { name: '1 agent needs input' })).toBeVisible()
  await expect(row.getByRole('img', { name: '1 agent needs input' })).toBeVisible()
  await expect.poll(() => page.title()).toBe(`(1) ${baseTitle}`)

  const stop = page.getByRole('complementary', { name: 'Inspector' }).getByRole('button', { name: 'Stop agent' })
  // Waiting is still alive: the agent keeps its terminal and stop controls.
  await agent.click()
  await expect(stop).toBeEnabled()

  backend.setAgentActivity('codex-session-1', { state: 'idle', reason: 'turn_complete', since, source: 'hook' })
  await expect(agent.getByRole('img', { name: 'Idle' })).toBeVisible()
  await expect(row.getByRole('img', { name: /needs input/ })).toHaveCount(0)
  await expect.poll(() => page.title()).toBe(baseTitle)
  await expect(stop).toBeEnabled()
  expect(errors).toEqual([])
})
