import { expect, test } from '@playwright/test'
import { DEFAULT_API_ORIGIN, mockGitBackend } from './mockGit'

test('application connects automatically and reload creates a fresh capability', async ({ page }) => {
  await mockGitBackend(page)
  let sessions = 0
  const external: string[] = []
  page.on('request', request => { if (!request.url().startsWith(DEFAULT_API_ORIGIN)) external.push(request.url()) })
  await page.route(`${DEFAULT_API_ORIGIN}/api/session`, route => {
    sessions++
    return route.fulfill({ status: 201, json: { token: `capability-${sessions}`, expires_at: new Date(Date.now() + 60_000).toISOString() } })
  })
  await page.goto('/app')
  await expect(page.locator('.react-flow')).toBeVisible()
  expect(sessions).toBe(1)
  await page.reload()
  await expect(page.locator('.react-flow')).toBeVisible()
  expect(sessions).toBe(2)
  await expect(page.getByRole('link', { name: /Connect GitHub|Sign in with GitHub/i })).toHaveCount(0)
  expect(external).toEqual([])
})

test('local entry sends restrictive security headers', async ({ page }) => {
  const response = await page.goto('/app')
  const headers = response!.headers()
  expect(headers['content-security-policy']).toContain("script-src 'self'")
  expect(headers['content-security-policy']).toContain("connect-src 'self';")
  expect(headers['referrer-policy']).toBe('no-referrer')
  expect(headers['x-content-type-options']).toBe('nosniff')
  expect(await page.locator('script[src^="http"]').count()).toBe(0)
})

test('/agents renders the Agents page and no canvas', async ({ page }) => {
  await mockGitBackend(page)
  await page.goto('/app/agents')
  await expect(page.getByRole('region', { name: 'Fleet summary' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Agents', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('.react-flow')).toHaveCount(0)
  // The old canvas focus route is gone: the canvas has its own nav item.
  await page.getByRole('link', { name: 'Canvas', exact: true }).click()
  await expect(page.locator('.react-flow')).toBeVisible()
})
