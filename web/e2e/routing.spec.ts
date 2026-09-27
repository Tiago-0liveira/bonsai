import { expect, test } from '@playwright/test'

test('landing page is public and never probes localhost or relay realtime', async ({ page }) => {
  const privilegedRequests: string[] = []
  page.on('request', request => {
    const url = request.url()
    if (url.startsWith('http://127.0.0.1:7001') || url.startsWith('https://api.bonsai.dev')) privilegedRequests.push(url)
  })

  await page.goto('/')
  await expect(page.getByRole('heading', { name: /Work in parallel/i })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open Bonsai' }).first()).toHaveAttribute('href', '/app')
  await page.waitForTimeout(250)
  expect(privilegedRequests).toEqual([])
})

test('application waits for an explicit local connection and remains usable without relay auth', async ({ page }) => {
  let healthCalls = 0
  let sessionCalls = 0
  await page.route('http://127.0.0.1:7001/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/health') { healthCalls++; await route.fulfill({ json: { ok: true } }); return }
    if (url.pathname === '/version') { await route.fulfill({ json: { version: 'e2e', api_version: 1 } }); return }
    if (url.pathname === '/api/session') { sessionCalls++; await route.fulfill({ status: 201, json: { token: `session-${sessionCalls}`, expires_at: new Date(Date.now() + 60_000).toISOString() } }); return }
    if (url.pathname === '/api/projects') { await route.fulfill({ json: [] }); return }
    await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Not Found' } } })
  })
  await page.route('https://api.bonsai.dev/**', route => route.fulfill({ status: 401, body: 'authentication required' }))

  await page.goto('/app')
  await expect(page.getByRole('heading', { name: /Bonsai is not connected/i })).toBeVisible()
  expect(healthCalls).toBe(0)
  expect(sessionCalls).toBe(0)

  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect.poll(() => healthCalls).toBe(1)
  await expect.poll(() => sessionCalls).toBe(1)
  await expect(page.getByText('bonsai', { exact: true }).first()).toBeVisible()
})

test('reload requires a fresh local capability', async ({ page }) => {
  let sessions = 0
  await page.route('http://127.0.0.1:7001/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/health') return route.fulfill({ json: { ok: true } })
    if (path === '/version') return route.fulfill({ json: { version: 'e2e', api_version: 1 } })
    if (path === '/api/session') {
      sessions++
      return route.fulfill({ status: 201, json: { token: `capability-${sessions}`, expires_at: new Date(Date.now() + 60_000).toISOString() } })
    }
    if (path === '/api/projects') return route.fulfill({ json: [] })
    return route.fulfill({ status: 404, json: {} })
  })
  await page.route('https://api.bonsai.dev/**', route => route.fulfill({ status: 401, body: '' }))

  await page.goto('/app')
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect.poll(() => sessions).toBe(1)
  await page.reload()
  await expect(page.getByRole('button', { name: 'Connect to local Bonsai' })).toBeVisible()
  expect(sessions).toBe(1)
})

test('production preview sends restrictive security headers', async ({ page }) => {
  const response = await page.goto('/')
  expect(response).not.toBeNull()
  const headers = response!.headers()
  expect(headers['content-security-policy']).toContain("script-src 'self'")
  expect(headers['content-security-policy']).toContain("connect-src 'self' https://api.bonsai.dev http://127.0.0.1:7001 ws://127.0.0.1:7001")
  expect(headers['referrer-policy']).toBe('no-referrer')
  expect(headers['x-content-type-options']).toBe('nosniff')
  expect(await page.locator('script[src^="http"]').count()).toBe(0)
})
