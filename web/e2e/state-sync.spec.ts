import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('renders local worktrees before provider enrichment and updates PR/CI later', async ({ page }) => {
  await mockGitBackend(page, false, true)
  await openConnectedApp(page)

  const daemon = page.locator('.react-flow__node-worktree').filter({ hasText: 'fix/daemon-lifecycle' })
  await expect(daemon).toBeVisible()
  await expect(daemon.getByRole('img', { name: 'CI unknown', exact: true })).toBeVisible()
  await expect(daemon.getByText('#23', { exact: true })).toHaveCount(0)

  await expect(daemon.getByText('#23', { exact: true })).toBeVisible({ timeout: 5_000 })
  await expect(daemon.getByRole('img', { name: /^CI failed/ })).toBeVisible()
})
