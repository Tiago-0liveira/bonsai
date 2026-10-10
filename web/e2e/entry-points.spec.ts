import { expect, test, type Page } from '@playwright/test'
import { DEFAULT_API_ORIGIN, mockGitBackend } from './mockGit'

// The same journey through both ways of opening Bonsai:
// - hosted-style: the page comes from another HTTPS origin and reaches the
//   local API cross-origin on 127.0.0.1:7001 (app.bonsai.dev today);
// - local-served: the page comes from the Go API itself (`bonsai web`), here
//   the TestEmbeddedUIBrowserFixture server on 127.0.0.1:7011, and every API
//   call is same-origin.
const entries = [
  { name: 'hosted-style origin', url: 'https://127.0.0.1:4173/app', api: DEFAULT_API_ORIGIN, local: false },
  { name: 'local-served origin', url: 'http://127.0.0.1:7011/app', api: 'http://127.0.0.1:7011', local: true },
]

async function spyOnPermissionQueries(page: Page) {
  await page.addInitScript(() => {
    const calls: string[] = []
    Object.defineProperty(window, '__permissionQueries', { value: calls })
    const permissions = navigator.permissions
    if (!permissions?.query) return
    const query = permissions.query.bind(permissions)
    permissions.query = ((descriptor: { name: string }) => {
      calls.push(descriptor.name)
      return query(descriptor as PermissionDescriptor)
    }) as typeof permissions.query
  })
}

for (const entry of entries) {
  test(`${entry.name}: connect, load project, open PR detail, live WS event`, async ({ page }) => {
    // Any blocked API or WebSocket connection would show up as a connect-src
    // violation. (A library's inline <style> trips style-src-elem on every
    // entry, hosted included; that is not what this test is about.)
    const connectViolations: string[] = []
    page.on('console', message => {
      if (/Refused to connect|connect-src/i.test(message.text())) connectViolations.push(message.text())
    })
    await spyOnPermissionQueries(page)
    // Provider data arrives over the event WebSocket one second after
    // bootstrap: the live update this test waits for.
    await mockGitBackend(page, false, true, false, entry.api)

    const response = await page.goto(entry.url)
    expect(response?.ok()).toBe(true)
    if (entry.local) {
      // Served by Go: its own CSP, runtime config, and no click to connect.
      expect(response?.headers()['content-security-policy']).toContain("connect-src 'self';")
      await expect(page.locator('meta[name="bonsai-entry"]')).toHaveAttribute('content', 'local')
      await expect(page.locator('meta[name="bonsai-local-api-origin"]')).toHaveAttribute('content', entry.api)
    } else {
      await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
    }
    await page.locator('.react-flow').waitFor({ state: 'visible' })

    // Project loaded from the WebSocket bootstrap, before provider data.
    const daemon = page.locator('.react-flow__node-worktree').filter({ hasText: 'fix/daemon-lifecycle' })
    await expect(daemon).toBeVisible()
    // Live WebSocket update: the PR badge appears without a reload.
    await expect(daemon.getByText('#23', { exact: true })).toBeVisible({ timeout: 5_000 })

    // PR detail.
    await page.getByRole('link', { name: 'GitHub', exact: true }).click()
    await page.getByRole('option', { name: /#23 fix\(daemon\): stabilize lifecycle cleanup/ }).click()
    await expect(page.getByText('Make daemon shutdown cleanup deterministic.').first()).toBeVisible()

    const queries = await page.evaluate(() => (window as unknown as { __permissionQueries: string[] }).__permissionQueries)
    const relayStatus = page.getByText(/GitHub realtime|Connect GitHub/)
    if (entry.local) {
      // Same origin: no Local Network Access probe, so no browser prompt.
      expect(queries.filter(name => /network/.test(name))).toEqual([])
      await expect(relayStatus).toHaveCount(0)
    } else {
      expect(queries).toContain('loopback-network')
    }
    expect(connectViolations).toEqual([])
  })
}

test('local-served origin: refreshes deep links and serves immutable assets', async ({ page, request }) => {
  await mockGitBackend(page, false, false, false, 'http://127.0.0.1:7011')
  const response = await page.goto('http://127.0.0.1:7011/app/github')
  expect(response?.status()).toBe(200)
  await page.locator('.react-flow, [role="listbox"]').first().waitFor({ state: 'visible' })
  await expect(page).toHaveURL(/\/app\/github/)

  const script = await page.locator('script[type="module"]').first().getAttribute('src')
  expect(script).toMatch(/^\/assets\//)
  const asset = await request.get(`http://127.0.0.1:7011${script}`, { headers: { 'Accept-Encoding': 'gzip' } })
  expect(asset.status()).toBe(200)
  expect(asset.headers()['cache-control']).toBe('public, max-age=31536000, immutable')
  expect(asset.headers()['content-encoding']).toBe('gzip')

  const root = await request.get('http://127.0.0.1:7011/', { maxRedirects: 0 })
  expect(root.status()).toBe(302)
  expect(root.headers()['location']).toBe('/app/')

  // A foreign origin cannot open an API session against the local-served API.
  const foreign = await request.post('http://127.0.0.1:7011/api/session', { headers: { Origin: 'https://evil.example' } })
  expect(foreign.status()).toBe(403)
  const hosted = await request.post('http://127.0.0.1:7011/api/session', { headers: { Origin: 'https://app.bonsai.dev' } })
  expect(hosted.status()).toBe(403)
  const own = await request.post('http://127.0.0.1:7011/api/session', { headers: { Origin: 'http://localhost:7011' } })
  expect(own.status()).toBe(201)
})

test('local-served origin: real API, no mocks, every request allowed', async ({ page }) => {
  // Same-origin GETs carry no Origin header; the API must still accept them.
  const failures: string[] = []
  page.on('response', response => { if (response.status() >= 400) failures.push(`${response.status()} ${response.url()}`) })
  await spyOnPermissionQueries(page)
  for (const origin of ['http://127.0.0.1:7011', 'http://localhost:7011']) {
    await page.goto(`${origin}/app`)
    // The fixture has no project roots, so the connected app asks for them.
    await expect(page.getByRole('dialog', { name: 'Choose project folders' })).toBeVisible()
    const queries = await page.evaluate(() => (window as unknown as { __permissionQueries: string[] }).__permissionQueries)
    expect(queries.filter(name => /network/.test(name))).toEqual([])
  }
  expect(failures.filter(failure => !failure.endsWith('/favicon.ico'))).toEqual([])
})
