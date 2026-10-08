import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('renders the Bonsai workspace and core dialogs without page errors', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.stack ?? error.message))


  await mockGitBackend(page)
  await openConnectedApp(page)

  await expect(page.getByText('bonsai', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Canvas', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'New worktree' })).toBeVisible()
  await expect(page.getByText(/visible nodes/)).toHaveCount(0)

  await page.getByRole('button', { name: 'New worktree' }).click()
  await expect(page.getByText('Existing branch', { exact: true })).toBeVisible()
  await expect(page.getByText('Remote branch', { exact: true })).toBeVisible()
  await expect(page.getByText('New branch', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Merge target' }).click()
  const mergeMenu = page.locator('[data-bonsai-select-menu="Merge target"]')
  await expect(mergeMenu).toBeVisible()
  const menuBox = await mergeMenu.boundingBox()
  const viewport = page.viewportSize()
  expect(menuBox).not.toBeNull()
  expect(viewport).not.toBeNull()
  if (menuBox && viewport) {
    expect(menuBox.x).toBeGreaterThanOrEqual(0)
    expect(menuBox.y).toBeGreaterThanOrEqual(0)
    expect(menuBox.x + menuBox.width).toBeLessThanOrEqual(viewport.width)
    expect(menuBox.y + menuBox.height).toBeLessThanOrEqual(viewport.height)
  }
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Close', exact: true }).click()

  await page.locator('.react-flow__node-project').click()
  await expect(page.getByRole('button', { name: 'Agent', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Agent', exact: true }).click()
  await expect(page.getByText('Start agent', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('dialog')).toContainText('Antigravity')
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Start agent', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Close start agent' }).click()

  await page.locator('.react-flow__node-worktree').first().click()
  await page.waitForTimeout(100)

  await page.getByTitle('Minimize workspace').click()
  await expect(page.getByRole('button', { name: 'Open workspace' })).toBeVisible()
  await page.getByRole('button', { name: 'Open workspace' }).click()
  await expect(page.getByTitle('Minimize workspace')).toBeVisible()

  expect(pageErrors).toEqual([])
})


test('expanding a stack keeps unrelated branches fixed', async ({ page }) => {
  await mockGitBackend(page, false, false, true)
  await openConnectedApp(page)

  const unrelated = page.locator('.react-flow__node-worktree').filter({ hasText: 'fix/daemon-lifecycle' })
  const stack = page.locator('.react-flow__node-stack').filter({ hasText: 'Local / unlinked' })
  await expect(unrelated).toBeVisible()
  await expect(stack).toBeVisible()
  await page.waitForTimeout(450)

  const before = await unrelated.boundingBox()
  expect(before).not.toBeNull()

  await stack.locator('button').filter({ hasText: 'Expand' }).click()
  await expect(page.locator('.react-flow__node-stack').filter({ hasText: 'Local / unlinked' })).toHaveCount(0)
  await expect(page.getByTestId('rf__node-wt-local-one')).toBeVisible()

  const after = await unrelated.boundingBox()
  expect(after).not.toBeNull()
  if (before && after) {
    expect(Math.abs(after.x - before.x)).toBeLessThan(1)
    expect(Math.abs(after.y - before.y)).toBeLessThan(1)
  }
})

test('connection PR catalog reaches the GitHub page and matching worktree', async ({ page }) => {
  await mockGitBackend(page)
  await openConnectedApp(page)

  await expect(page.locator('.dock-pane').getByText('Pull requests', { exact: true })).toHaveCount(0)
  await expect(page.getByTestId('rf__node-wt-daemon')).toContainText('#23')

  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  await expect(page.getByText('#23 fix(daemon): stabilize lifecycle cleanup').first()).toBeVisible()
  await page.getByRole('button', { name: 'Project', exact: true }).click()
  await page.getByRole('option', { name: 'sprout-lab', exact: true }).click()
  await expect(page.getByRole('main').getByText('No open pull requests.').first()).toBeVisible()
})
