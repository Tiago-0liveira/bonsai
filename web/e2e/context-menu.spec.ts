import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import { waitForViewportToSettle } from './viewport'

test.beforeEach(async ({ page }) => {
  await mockGitBackend(page)
})

// Radix positions the first frame of a context menu on the previous click
// point ((0, 0) on first open). That frame must never be visible.
test('context menus only appear at the click point', async ({ page }) => {
  await openConnectedApp(page)
  await waitForViewportToSettle(page)
  await page.evaluate(() => {
    const frames: { x: number; y: number }[] = []
    Object.assign(window, { __menuFrames: frames })
    const record = () => {
      document.querySelectorAll('[data-radix-popper-content-wrapper]').forEach(wrapper => {
        const content = wrapper.firstElementChild
        if (content && getComputedStyle(content).opacity !== '0') {
          const rect = wrapper.getBoundingClientRect()
          frames.push({ x: Math.round(rect.left), y: Math.round(rect.top) })
        }
      })
      requestAnimationFrame(record)
    }
    record()
  })
  // Click points are fractions of the node box: the initial fit's zoom varies
  // with load, and a fixed offset can land past the node on an edge.
  for (const [id, fx, fy] of [['wt-web', 0.3, 0.4], ['wt-daemon', 0.2, 0.4], ['wt-web', 0.9, 0.6]] as const) {
    await page.evaluate(() => { (window as unknown as { __menuFrames: unknown[] }).__menuFrames.length = 0 })
    const node = page.getByTestId('rf__node-' + id)
    const box = (await node.boundingBox())!
    const x = Math.round(box.width * fx)
    const y = Math.round(box.height * fy)
    await node.click({ button: 'right', position: { x, y } })
    await expect(page.getByRole('menuitem', { name: 'Open inspector', exact: true })).toBeVisible()
    await page.waitForTimeout(150)
    const frames = await page.evaluate(() => (window as unknown as { __menuFrames: { x: number; y: number }[] }).__menuFrames)
    expect(frames.length).toBeGreaterThan(0)
    // Every visible frame sits at the click point (shifted only to stay on screen).
    for (const frame of frames) {
      expect(Math.abs(frame.x - (box.x + x))).toBeLessThan(220)
      expect(Math.abs(frame.y - (box.y + y))).toBeLessThan(320)
      expect(frame).toEqual(frames[frames.length - 1])
    }
    await page.keyboard.press('Escape')
  }
})
