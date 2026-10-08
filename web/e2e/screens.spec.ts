import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'

// Review artifacts, not pixel diffs: run this spec and compare the PNGs under
// web/test-results/ with the design mockups.
const viewport = { width: 1600, height: 960 }

test.beforeEach(async ({ page }) => {
  await page.setViewportSize(viewport)
  await mockGitBackend(page)
})

function trackPageErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.stack ?? error.message))
  return errors
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  await page.waitForTimeout(600)
  await page.screenshot({ path: testInfo.outputPath(`${name}.png`) })
}

test('canvas', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await capture(page, testInfo, 'canvas')
  expect(errors).toEqual([])
})

test('canvas-runtime-open', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('button', { name: 'feat/web-workspace', exact: true }).click()
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  await page.locator('[data-bonsai-select-menu="Open runtime"] [role="option"]').first().click()
  await expect(page.locator('.runtime-tile')).toHaveCount(1)
  await capture(page, testInfo, 'canvas-runtime-open')
  expect(errors).toEqual([])
})

test('canvas-dock-collapsed', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.evaluate(() => window.__bonsaiTestStore.getState().setDockState('collapsed'))
  await capture(page, testInfo, 'canvas-dock-collapsed')
  expect(errors).toEqual([])
})

test('inspector-worktree', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.locator('.react-flow__node-worktree').first().click()
  await capture(page, testInfo, 'inspector-worktree')
  expect(errors).toEqual([])
})

test('github', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'GitHub', exact: true }).click()
  await capture(page, testInfo, 'github')
  expect(errors).toEqual([])
})

test('table', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Table', exact: true }).click()
  await capture(page, testInfo, 'table')
  expect(errors).toEqual([])
})

test('logs', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Logs', exact: true }).click()
  await capture(page, testInfo, 'logs')
  expect(errors).toEqual([])
})

test('settings', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  await capture(page, testInfo, 'settings')
  expect(errors).toEqual([])
})

test('dialog-create-worktree', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.getByRole('button', { name: 'New worktree' }).click()
  await capture(page, testInfo, 'dialog-create-worktree')
  expect(errors).toEqual([])
})

test('command-palette', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  await openConnectedApp(page)
  await page.evaluate(() => window.__bonsaiTestStore.getState().setPaletteOpen(true))
  await capture(page, testInfo, 'command-palette')
  expect(errors).toEqual([])
})

test('connect-gate', async ({ page }, testInfo) => {
  const errors = trackPageErrors(page)
  // Deliberately no openConnectedApp: this is the first screen before connecting.
  await page.goto('/app')
  await expect(page.getByRole('button', { name: 'Connect to local Bonsai' })).toBeVisible()
  await capture(page, testInfo, 'connect-gate')
  expect(errors).toEqual([])
})
