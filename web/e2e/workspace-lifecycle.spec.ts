import { expect, test } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import './presentationFixtures'

test.beforeEach(async ({ page }) => { await mockGitBackend(page) })

test('canvas identity, viewport and dock drafts survive resize, maximize and collapse', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await openConnectedApp(page)
  const canvas = page.locator('.react-flow')
  await expect(canvas).toBeVisible()
  await page.waitForTimeout(400) // Initial Fit animation.
  const canvasElement = await canvas.elementHandle()
  const viewport = page.locator('.react-flow__viewport')
  const canvasBox = (await canvas.boundingBox())!
  await page.mouse.move(canvasBox.x + canvasBox.width / 2, canvasBox.y + canvasBox.height / 2)
  const initialViewport = await viewport.getAttribute('style')
  await page.mouse.wheel(0, -150)
  await expect.poll(() => viewport.getAttribute('style')).not.toBe(initialViewport)
  await page.waitForTimeout(200) // Wheel zoom animation.
  const savedViewport = await viewport.getAttribute('style')
  const dock = page.locator('[data-panel-id="bottom-workspace"]')
  const height = () => page.evaluate(() => window.__bonsaiTestStore.getState().dockHeight)
  const beforeResize = await height()
  const handle = page.locator('[data-panel-resize-handle-id="workspace-resize"]')
  const handleBox = (await handle.boundingBox())!
  await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2)
  await page.mouse.down()
  await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y - 50, { steps: 5 })
  expect(await height()).toBe(beforeResize)
  await page.mouse.up()
  await expect.poll(height).toBeGreaterThan(beforeResize)
  const normalHeight = await height()
  await page.getByTitle('Maximize workspace').click()
  await expect(dock).toHaveAttribute('data-panel-size', '68.0')
  await expect.poll(height).toBe(normalHeight)
  await page.getByTitle('Minimize workspace').click()
  await expect(dock).toHaveAttribute('data-panel-size', '0.0')
  await expect(dock.locator('[inert]')).toHaveAttribute('aria-hidden', 'true')
  await page.getByRole('button', { name: 'Open workspace' }).click()
  await expect(dock).toHaveAttribute('data-panel-size', normalHeight.toFixed(1))
  await page.getByTitle('Maximize workspace').click()
  await page.getByTitle('Maximize workspace').click()
  await expect(dock).toHaveAttribute('data-panel-size', normalHeight.toFixed(1))
  expect(await canvasElement!.evaluate(element => element === document.querySelector('.react-flow'))).toBe(true)
  await expect(viewport).toHaveAttribute('style', savedViewport!)
  expect(errors).toEqual([])
})

test('a completed resize saves once and pending preferences survive immediate navigation and reload', async ({ page }) => {
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem
    Object.defineProperty(window, '__preferenceWrites', { value: [] })
    Storage.prototype.setItem = function(key, value) {
      if (key === 'bonsai-web-workspace-v6') (window as unknown as { __preferenceWrites: string[] }).__preferenceWrites.push(value)
      return original.call(this, key, value)
    }
  })
  await openConnectedApp(page)
  await page.waitForTimeout(800) // Initial Fit and deferred placement/viewport save.
  const writes = () => page.evaluate(() => (window as unknown as { __preferenceWrites: string[] }).__preferenceWrites.length)
  const baseline = await writes()
  const handle = page.locator('[data-panel-resize-handle-id="workspace-resize"]')
  const rect = (await handle.boundingBox())!
  await page.mouse.move(rect.x + rect.width / 2, rect.y + rect.height / 2)
  await page.mouse.down()
  await page.mouse.move(rect.x + rect.width / 2, rect.y - 55, { steps: 8 })
  expect(await writes()).toBe(baseline)
  await page.mouse.up()
  const finalHeight = await page.evaluate(() => window.__bonsaiTestStore.getState().dockHeight)
  // Router navigation flushes the completed height even before debounce expires.
  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  await expect.poll(writes).toBe(baseline + 1)
  await page.reload()
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect(page.locator('[data-panel-id="bottom-workspace"]')).toHaveAttribute('data-panel-size', finalHeight.toFixed(1))
})

test('main route search and review drafts survive dock toggles', async ({ page }) => {
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  const main = page.getByRole('main')
  const search = main.getByPlaceholder('Search pull requests')
  const review = main.getByPlaceholder('Leave a comment or review…')
  await review.fill('Unsaved review draft')
  await search.fill('daemon')
  const searchElement = await search.elementHandle()
  for (let i = 0; i < 2; i++) {
    await page.getByTitle('Maximize workspace').click()
    await page.getByTitle('Minimize workspace').click()
    await expect(search).toHaveValue('daemon')
    await expect(review).toHaveValue('Unsaved review draft')
    await page.getByRole('button', { name: 'Open workspace' }).click()
  }
  expect(await searchElement!.evaluate(element => element.isConnected)).toBe(true)
})

test('collapsed startup restores normal height and removes portaled dock menus from focus', async ({ page }) => {
  await openConnectedApp(page)
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  const menu = page.locator('[data-bonsai-select-menu="Open runtime"]')
  await expect(menu).toBeVisible()
  await page.evaluate(() => {
    const store = window.__bonsaiTestStore.getState()
    store.setDockHeight(41)
    store.setDockState('collapsed')
  })
  await expect(menu).toHaveCount(0)
  await page.keyboard.press('Tab')
  expect(await page.evaluate(() => Boolean(document.activeElement?.closest('[inert]')))).toBe(false)
  await page.reload()
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect(page.locator('[data-panel-id="bottom-workspace"]')).toHaveAttribute('data-panel-size', '0.0')
  await page.getByRole('button', { name: 'Open workspace' }).click()
  await expect(page.locator('[data-panel-id="bottom-workspace"]')).toHaveAttribute('data-panel-size', '41.0')
})
