import { expect, test } from '@playwright/test'
import { injectAgentPresentation } from './presentationFixtures'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('palette and context menus disable unsupported execution while Git remains usable', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.keyboard.press('Control+k')
  const palette = page.locator('[cmdk-root]')
  await expect(palette).toContainText('Agent execution and interactive shells are unavailable')
  for (const label of ['Start agent', 'Stop agent', 'Open terminal', 'Run pnpm dev', 'Run pnpm test', 'Run cargo test', 'Run make test']) {
    const command = palette.getByRole('option', { name: label + ' unavailable', exact: true })
    await expect(command).toHaveAttribute('aria-disabled', 'true')
    await command.click({ force: true })
    await expect(palette).toBeVisible()
  }
  await palette.locator('input').fill('Run pnpm test')
  await page.keyboard.press('Enter')
  await expect(palette).toBeVisible()
  await expect(page.locator('.react-flow__node-agent')).toHaveCount(0)
  await palette.locator('input').fill('')
  await palette.getByRole('option', { name: 'Fit canvas', exact: true }).click()
  await expect(palette).toHaveCount(0)

  await page.getByTestId('rf__node-bonsai').click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: 'Start agent unavailable' })).toBeDisabled()
  await page.keyboard.press('Escape')
  const worktree = page.getByTestId('rf__node-wt-daemon')
  await worktree.click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: 'Start agent unavailable' })).toBeDisabled()
  await expect(page.getByRole('menuitem', { name: 'Delete worktree', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await worktree.click()

  for (const [label, action] of [['Pull', 'pull'], ['Push', 'push']]) {
    await page.keyboard.press('Control+k')
    const request = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/api/worktrees/wt-daemon/' + action))
    await palette.getByRole('option', { name: label, exact: true }).click()
    await request
    await expect(palette).toHaveCount(0)
  }
  expect(errors).toEqual([])
})

test('managed process cards describe status without implying a shell or listening URL', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.locator('.react-flow__node-stack').filter({ hasText: 'feat' }).getByRole('button', { name: /Expand/ }).click()
  await page.getByTestId('rf__node-wt-web').click()
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  await page.getByRole('option', { name: /Vite/ }).click()
  const card = page.locator('.runtime-tile').filter({ hasText: 'Vite' })
  await expect(card).toContainText('Process status')
  await expect(card).toContainText('Command: pnpm dev')
  await expect(card).toContainText('Status: running')
  await expect(card).toContainText('Configured port: 5173')
  await expect(card).not.toContainText('http://localhost')
  await expect(card.locator('.xterm')).toHaveCount(0)
  await page.getByRole('link', { name: 'Logs', exact: true }).click()
  await expect(page.getByRole('main')).toContainText('Activity logs are unavailable')
  await expect(page.getByText('UI builder is running')).toHaveCount(0)
})


test('explicit agent presentation fixtures cannot restart, stop, or open an interactive terminal', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  await injectAgentPresentation(page)
  const agent = page.getByTestId('rf__node-agent-daemon')
  await agent.click({ button: 'right' })
  for (const label of ['Open terminal', 'Start', 'Restart', 'Stop']) {
    await expect(page.getByRole('menuitem', { name: label + ' unavailable', exact: true })).toBeDisabled()
  }
  await page.keyboard.press('Escape')
  await agent.click()
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByRole('button', { name: 'Stop agent', exact: true })).toBeDisabled()
  await expect(inspector.getByRole('button', { name: 'Open terminal', exact: true })).toBeDisabled()
  await expect(inspector.getByRole('button', { name: 'Archive', exact: true })).toBeEnabled()
})
