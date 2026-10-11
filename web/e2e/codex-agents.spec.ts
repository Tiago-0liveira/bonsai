import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

type Launch = { name: string; profile: 'Personal' | 'Team'; model?: RegExp; effort?: string; permissions?: string }

test('starts two Codex sessions on one profile and one on another, each with its own options', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await mockGitBackend(page)
  await openConnectedApp(page)

  const start = async (launch: Launch) => {
    await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await expect(dialog.getByRole('button', { name: /Codex/ })).toBeEnabled()
    await dialog.getByRole('button', { name: /Codex/ }).click()
    // Two profiles, so none is preselected.
    await expect(dialog.getByLabel('Profile')).toContainText('Choose a profile')
    await dialog.getByLabel('Profile').click()
    await page.getByRole('option', { name: new RegExp(`^${launch.profile}`) }).click()
    await expect(dialog.getByTestId('profile-details')).toContainText('logged in')
    await expect(dialog.getByLabel('Permissions')).toContainText('Auto')
    await expect(dialog.getByLabel('Agent model')).toHaveAttribute('placeholder', 'GPT-6.1-Sol')
    await dialog.getByLabel('Agent name').fill(launch.name)
    if (launch.model) {
      await dialog.getByLabel('Agent model').click()
      await page.getByRole('option', { name: launch.model }).click()
    }
    if (launch.effort) {
      await dialog.getByLabel('Effort').click()
      await page.getByRole('option', { name: launch.effort, exact: true }).click()
    }
    if (launch.permissions) {
      await dialog.getByLabel('Permissions').click()
      await page.getByRole('option', { name: launch.permissions, exact: true }).click()
    }
    const request = page.waitForRequest(item => item.method() === 'POST' && item.url().endsWith('/api/projects/bonsai/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    const body = (await request).postDataJSON()
    await expect(dialog).toHaveCount(0)
    return body
  }

  const first = await start({ name: 'First', profile: 'Personal', model: /GPT-6.1-Sol/, effort: 'ultra' })
  expect(first).toMatchObject({ account_id: 'codex-personal', name: 'First', model: 'gpt-6.1-sol', sandbox: 'workspace-write', approval: 'on-request', effort: 'ultra', worktree_id: 'wt-daemon' })
  expect(first).not.toHaveProperty('permissions')

  const second = await start({ name: 'Second', profile: 'Personal', permissions: 'Read only' })
  expect(second).toMatchObject({ account_id: 'codex-personal', name: 'Second', sandbox: 'read-only', approval: 'on-request' })
  expect(second).not.toHaveProperty('effort')

  const third = await start({ name: 'Third', profile: 'Team', permissions: 'Bypass' })
  expect(third).toMatchObject({ account_id: 'codex-team', name: 'Third', sandbox: 'bypass' })
  expect(third).not.toHaveProperty('approval')

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

test('drops an effort the chosen model does not offer and explains the profile setup', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)
  const dialog = await openDialog(page)
  await dialog.getByRole('button', { name: /Codex/ }).click()
  await dialog.getByLabel('Profile').click()
  await page.getByRole('option', { name: /^Team/ }).click()
  await expect(dialog.getByTestId('profile-details')).toContainText('auth: device · team@example.com · pro · logged in')
  await expect(dialog.getByTestId('profile-details')).toContainText('Same login as profile "Personal"')

  await dialog.getByLabel('Effort').click()
  await expect(page.getByRole('option')).toHaveText(['Profile default', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
  await page.getByRole('option', { name: 'ultra', exact: true }).click()
  await dialog.getByLabel('Agent model').click()
  await page.getByRole('option', { name: /GPT-6-Luna/ }).click()
  await expect(dialog.getByLabel('Effort')).toContainText('Profile default')
  await dialog.getByLabel('Effort').click()
  await expect(page.getByRole('option')).toHaveText(['Profile default', 'low', 'medium', 'high', 'xhigh', 'max'])
  await page.keyboard.press('Escape')

  await dialog.getByLabel('Permissions').click()
  await page.getByRole('option', { name: 'Full access', exact: true }).click()
  await expect(dialog.getByRole('note')).toContainText('without asking')
})
