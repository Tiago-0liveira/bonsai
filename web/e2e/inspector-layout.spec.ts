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
    const viewport = inspector.locator('[data-radix-scroll-area-viewport]')
    expect(await viewport.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  }
})

test('project cards fit long diagnostics and the last section remains reachable inside the rail', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await mockGitBackend(page)
  await openConnectedApp(page)
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByRole('heading', { name: 'Needs attention' })).toBeVisible()
  await inspector.locator('.inspector-attention').first().evaluate(el => {
    const text = el.querySelector('span > span:last-child')!
    text.textContent = 'Git status unavailable: chdir /tmp/bonsai-' + 'project-roots-and-discovery'.repeat(12) + ': no such file or directory'
  })
  await inspector.locator('h2').evaluate(el => { el.textContent = 'bonsai-' + 'long-project-name'.repeat(12) })

  for (const width of [1110, 1280, 1536]) {
    await page.setViewportSize({ width, height: 720 })
    const viewport = inspector.locator('[data-radix-scroll-area-viewport]')
    expect(await viewport.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    const box = (await viewport.boundingBox())!
    for (const card of await inspector.locator('.inspector-hero, .inspector-attention, .inspector-action, .inspector-count').all()) {
      const cardBox = (await card.boundingBox())!
      expect(cardBox.x).toBeGreaterThanOrEqual(box.x)
      expect(cardBox.x + cardBox.width).toBeLessThanOrEqual(box.x + box.width)
    }
    await viewport.evaluate(el => { el.scrollTop = el.scrollHeight })
    const lastSection = inspector.locator('.inspector-section').last()
    const lastBox = (await lastSection.boundingBox())!
    expect(lastBox.y + lastBox.height).toBeLessThanOrEqual(box.y + box.height)
    // The inspector lives in the left rail beside the dock, so it must end at the rail's 16px bottom inset.
    const inspectorBox = (await inspector.boundingBox())!
    expect(inspectorBox.y + inspectorBox.height).toBeLessThanOrEqual(720 - 16)
  }
  await page.getByTitle('Maximize workspace').click()
  const viewport = inspector.locator('[data-radix-scroll-area-viewport]')
  // Dock resizing is applied by a React effect after the click.
  await expect.poll(async () => {
    await viewport.evaluate(el => { el.scrollTop = el.scrollHeight })
    const box = (await viewport.boundingBox())!
    const lastBox = (await inspector.locator('.inspector-section').last().boundingBox())!
    return lastBox.y + lastBox.height - box.y - box.height
  }).toBeLessThanOrEqual(0)
  expect(await viewport.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})
