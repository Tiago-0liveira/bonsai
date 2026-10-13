import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('starts three Codex sessions on two profiles, each launched with its profile\'s defaults', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await mockGitBackend(page)
  await openConnectedApp(page)

  const start = async (name: string, profile: 'Personal' | 'Team') => {
    await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await expect(dialog.getByRole('button', { name: /Codex/ })).toBeEnabled()
    await dialog.getByRole('button', { name: /Codex/ }).click()
    // Two profiles, so none is preselected.
    await expect(dialog.getByText('Choose a profile to continue.')).toBeVisible()
    await dialog.getByRole('option', { name: new RegExp(`^${profile}`) }).click()
    await expect(dialog.getByLabel('Profile')).toContainText(profile === 'Personal' ? 'me@example.com' : 'team@example.com')
    // Model, permissions and effort are set on the profile's Defaults tab, not per launch.
    await expect(dialog.getByTestId('launch-defaults')).toBeVisible()
    await expect(dialog.getByLabel('Agent model')).toHaveCount(0)
    await expect(dialog.getByRole('slider', { name: 'Effort' })).toHaveCount(0)
    await dialog.getByLabel('Agent name').fill(name)
    const request = page.waitForRequest(item => item.method() === 'POST' && item.url().endsWith('/api/projects/bonsai/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    const body = (await request).postDataJSON()
    await expect(dialog).toHaveCount(0)
    return body
  }

  const first = await start('First', 'Personal')
  expect(first).toMatchObject({ account_id: 'codex-personal', name: 'First', worktree_id: 'wt-daemon' })
  const second = await start('Second', 'Personal')
  expect(second).toMatchObject({ account_id: 'codex-personal', name: 'Second' })
  const third = await start('Third', 'Team')
  expect(third).toMatchObject({ account_id: 'codex-team', name: 'Third' })
  for (const body of [first, second, third]) {
    for (const key of ['sandbox', 'approval', 'effort', 'permissions']) expect(body).not.toHaveProperty(key)
    expect(body.model ?? '').toBe('')
  }

  for (const id of ['codex-session-1', 'codex-session-2', 'codex-session-3']) {
    await expect(page.getByTestId(`rf__node-${id}`)).toBeVisible()
    await expect(page.getByTestId(`rf__node-${id}`).locator('[data-provider-logo="codex"]')).toBeVisible()
  }
  await expect(page.locator('.react-flow__node-agent')).toHaveCount(3)
  await expect(page.locator('.runtime-tile')).toHaveCount(3)
  await expect(page.locator('.runtime-heading [data-provider-logo="codex"]')).toHaveCount(3)
  await expect(page.locator('.xterm').first()).toBeVisible()
  for (const id of ['codex-session-1', 'codex-session-3']) {
    await page.getByTestId(`rf__node-${id}`).click()
    await expect(page.locator('.inspector-hero [data-provider-logo="codex"]')).toBeVisible()
  }
  expect(errors).toEqual([])
})

const openDialog = async (page: Page) => {
  await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
  return page.getByRole('dialog', { name: 'Start agent' })
}

test('explains the profile setup and opens its Defaults tab from the dialog', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  const dialog = await openDialog(page)
  await dialog.getByRole('button', { name: /Codex/ }).click()
  await dialog.getByRole('option', { name: /^Team/ }).click()
  await expect(dialog.getByLabel('Profile')).toContainText('team@example.com')
  await expect(dialog.getByTestId('profile-details')).toContainText('Same login as profile "Personal"')

  await dialog.getByRole('button', { name: 'Edit defaults' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL(/\/app\/agents\?.*profile=codex-team/)
  await expect(page).toHaveURL(/tab=defaults/)
  await expect(page.getByRole('form', { name: 'Profile defaults' })).toBeVisible()
})
