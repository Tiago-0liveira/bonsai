import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('inspector stays inside the viewport', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await mockGitBackend(page)
  await openConnectedApp(page)
  await page.locator('.react-flow__node-worktree').first().click()
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector).toBeVisible()
  await inspector.locator('h2').evaluate(el => { el.textContent = 'feat/' + 'authoritative-state-and-repository-selection'.repeat(8) })

  for (const width of [1110, 1280, 1536]) {
    await page.setViewportSize({ width, height: 720 })
    const box = await inspector.boundingBox()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(width)
    expect(box!.y + box!.height).toBeLessThanOrEqual(720)
    expect(await inspector.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  }
})
