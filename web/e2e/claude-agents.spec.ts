import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('starts two Claude sessions on the same profile, each launched with the profile\'s defaults', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await mockGitBackend(page)
  await openConnectedApp(page)

  const start = async (name: string) => {
    await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await dialog.getByRole('button', { name: /Claude/ }).click()
    await expect(dialog.getByLabel('Profile')).toContainText('Work')
    await expect(dialog.getByTestId('profile-details')).toContainText('auth: login · dev@example.com')
    await expect(dialog.getByTestId('profile-details')).toContainText('Token expires in 12 days')
    // Model, permissions and effort are the profile's; the dialog only shows them.
    await expect(dialog.getByTestId('launch-defaults')).toBeVisible()
    await expect(dialog.getByLabel('Agent model')).toHaveCount(0)
    await expect(dialog.getByRole('slider', { name: 'Effort' })).toHaveCount(0)
    await dialog.getByLabel('Agent name').fill(name)
    const launch = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/api/projects/bonsai/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    const body = (await launch).postDataJSON()
    expect(body).toMatchObject({ account_id: 'claude-profile', name, worktree_id: 'wt-daemon' })
    for (const key of ['permission_mode', 'effort']) expect(body).not.toHaveProperty(key)
    expect(body.model ?? '').toBe('')
    await expect(dialog).toHaveCount(0)
  }

  await start('First')
  await expect(page.getByTestId('rf__node-claude-session-1')).toBeVisible()
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await start('Second')
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
