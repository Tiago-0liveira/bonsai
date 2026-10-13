import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import { waitForViewportToSettle } from './viewport'

test.beforeEach(async ({ page }) => {
  await mockGitBackend(page)
})

const box = async (page: Page, selector: string) => (await page.locator(selector).first().boundingBox())!

test('canvas fills the shell behind the islands and fits inside the old pane frame', async ({ page }) => {
  await openConnectedApp(page)
  await expect(page.locator('.react-flow')).toBeVisible()
  const flow = await box(page, '.react-flow')
  const viewport = page.viewportSize()!
  expect(flow.x).toBe(0)
  expect(flow.y).toBe(0)
  expect(flow.width).toBe(viewport.width)
  expect(flow.height).toBe(viewport.height)

  await page.getByRole('button', { name: 'Fit', exact: true }).click()
  await waitForViewportToSettle(page)
  const frame = await box(page, '[data-canvas-frame]')
  const rects = await page.locator('.react-flow__node').evaluateAll(nodes => nodes.map(node => node.getBoundingClientRect().toJSON() as DOMRect))
  for (const rect of rects) {
    expect(rect.left).toBeGreaterThanOrEqual(frame.x - 1)
    expect(rect.top).toBeGreaterThanOrEqual(frame.y - 1)
    expect(rect.right).toBeLessThanOrEqual(frame.x + frame.width + 1)
    expect(rect.bottom).toBeLessThanOrEqual(frame.y + frame.height + 1)
  }
})

test('selecting a branch centres its node in the frame, and other pages leave the canvas inert', async ({ page }) => {
  await openConnectedApp(page)
  await expect(page.locator('.react-flow')).toBeVisible()
  await waitForViewportToSettle(page)
  await page.locator('.desktop-rail [data-worktree-row="wt-web"] button').nth(1).click()
  await waitForViewportToSettle(page)
  const frame = await box(page, '[data-canvas-frame]')
  const node = await box(page, '[data-testid="rf__node-wt-web"]')
  expect(Math.abs(node.x + node.width / 2 - (frame.x + frame.width / 2))).toBeLessThan(4)
  expect(Math.abs(node.y + node.height / 2 - (frame.y + frame.height / 2))).toBeLessThan(4)

  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.locator('[data-canvas-layer]')).toHaveAttribute('inert', '')
  await expect(page.locator('.react-flow')).toBeVisible()
  await page.getByRole('link', { name: 'Canvas' }).click()
  await expect(page.locator('[data-canvas-layer]')).not.toHaveAttribute('inert', '')
})
