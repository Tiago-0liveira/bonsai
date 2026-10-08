import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test.beforeEach(async ({ page }) => {
  await mockGitBackend(page)
})

test('inspector leads from branch blockers to the matching review and profile launch controls', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await openConnectedApp(page)
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('button', { name: /fix\/daemon-lifecycle/ }).click()
  await expect(inspector.getByText('Resolve merge conflicts', { exact: true })).toBeVisible()
  await expect(inspector.getByText('1 commit behind main', { exact: true })).toBeVisible()

  // The PR card in the inspector opens the GitHub tab on that pull request.
  await inspector.getByRole('button', { name: /#23 fix\(daemon\)/ }).click()
  await expect(page).toHaveURL(/\/github$/)
  await expect(page.getByRole('heading', { name: /#23/ })).toBeVisible()
  await expect(page.getByText('go test ./...', { exact: true }).first()).toBeVisible()
  await page.getByRole('link', { name: 'Canvas', exact: true }).click()

  await expect(inspector.getByText('No agent sessions.', { exact: true })).toBeVisible()
  await inspector.getByRole('button', { name: 'Start agent', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Antigravity')
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Start agent', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Close start agent' }).click()
  await expect(page.locator('.react-flow__node-agent')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('branch settings remain editable and project overview stays scoped', async ({ page }) => {
  await openConnectedApp(page)
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('button', { name: /fix\/daemon-lifecycle/ }).click()
  await inspector.getByText('Branch settings', { exact: true }).click()
  await inspector.getByRole('textbox', { name: 'Tag name' }).fill('needs-review')
  await inspector.getByRole('button', { name: 'Save tag' }).click()
  await page.reload()
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await inspector.getByText('Branch settings', { exact: true }).click()
  await expect(inspector.getByRole('textbox', { name: 'Tag name' })).toHaveValue('needs-review')
  await expect(inspector.getByRole('button', { name: 'Merge target', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Project', exact: true }).click()
  await page.getByRole('option', { name: 'sprout-lab', exact: true }).click()
  await expect(inspector.getByRole('heading', { name: 'sprout-lab', exact: true })).toBeVisible()
  await expect(inspector.getByText('No branch blockers reported.')).toBeVisible()
  await expect(inspector.getByText('fix/daemon-lifecycle', { exact: true })).toHaveCount(0)
})
