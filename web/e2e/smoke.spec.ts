import { expect, test } from '@playwright/test'

test('renders the Bonsai workspace shell', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('bonsai', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Workspace', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Inspector', { exact: true }).first()).toBeVisible()
})
