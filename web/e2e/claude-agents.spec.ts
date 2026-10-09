import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('starts two Claude sessions on the same profile with their launch options', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await mockGitBackend(page)
  await openConnectedApp(page)

  const start = async (name: string, mode: string) => {
    await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await dialog.getByRole('button', { name: /Claude/ }).click()
    await expect(dialog.getByLabel('Profile')).toContainText('Work')
    await expect(dialog.getByTestId('profile-details')).toContainText('auth: login · dev@example.com')
    await expect(dialog.getByTestId('profile-details')).toContainText('Token expires in 12 days')
    await expect(dialog.getByRole('switch', { name: 'Antigravity full access' })).toHaveCount(0)
    await dialog.getByLabel('Agent name').fill(name)
    await dialog.getByLabel('Agent model').click()
    await expect(page.getByRole('option')).toHaveCount(3)
    await page.getByRole('option', { name: /Claude Sonnet 5.5/ }).click()
    await expect(dialog.getByLabel('Agent model')).toHaveValue('claude-sonnet-5-5')
    await dialog.getByLabel('Permission mode').click()
    await page.getByRole('option', { name: mode, exact: true }).click()
    await dialog.getByLabel('Effort').click()
    await page.getByRole('option', { name: 'high', exact: true }).click()
    const launch = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/api/projects/bonsai/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    expect((await launch).postDataJSON()).toMatchObject({ account_id: 'claude-profile', name, model: 'claude-sonnet-5-5', permission_mode: mode === 'Plan' ? 'plan' : 'acceptEdits', effort: 'high', worktree_id: 'wt-daemon' })
    await expect(dialog).toHaveCount(0)
  }

  await start('First', 'Plan')
  await expect(page.getByTestId('rf__node-claude-session-1')).toBeVisible()
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await start('Second', 'Accept edits')
  await expect(page.getByTestId('rf__node-claude-session-1')).toBeVisible()
  await expect(page.getByTestId('rf__node-claude-session-2')).toBeVisible()
  await expect(page.locator('.react-flow__node-agent')).toHaveCount(2)
  await expect(page.locator('.runtime-tile')).toHaveCount(2)
  await expect(page.locator('.xterm').first()).toBeVisible()
  expect(errors).toEqual([])
})

test('remembers the last used provider for the next launch', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(dialog.getByRole('button', { name: /Antigravity/ })).toHaveAttribute('aria-pressed', 'true')
  await dialog.getByRole('button', { name: /Claude/ }).click()
  await dialog.getByRole('button', { name: 'Close start agent' }).click()
  await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Start agent' }).getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
})
