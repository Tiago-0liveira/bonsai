import { expect, test } from '@playwright/test'

test('renders the Bonsai workspace shell and worktree flow', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('bonsai', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Canvas', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'New worktree' })).toBeVisible()
  await expect(page.getByText('.env', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: 'New worktree' }).click()
  await expect(page.getByText('Existing branch', { exact: true })).toBeVisible()
  await expect(page.getByText('Origin branch', { exact: true })).toBeVisible()
  await expect(page.getByText('New branch', { exact: true })).toBeVisible()
  await expect(page.getByText('Worktree tag', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Close', exact: true }).click()
})
