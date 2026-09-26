import { expect, test } from '@playwright/test'

test('renders the Bonsai workspace and core dialogs without page errors', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.stack ?? error.message))


  const repository = { id: 'bonsai', workspace_id: 'personal', full_name: 'owner/bonsai', default_branch: 'main' }
  const snapshot = { repository, online: true, sequence: 0, metadata: {}, local: { branches: [{ name: 'main', remote: false }, { name: 'feature', remote: false }], worktrees: [{ id: 'main', repository_id: 'bonsai', branch: 'main', main: true, local_head_sha: 'abc' }, { id: 'feature', repository_id: 'bonsai', branch: 'feature', main: false, local_head_sha: 'def' }] }, remote: { repository, branches: [], pull_requests: [] } }
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/events') { await route.fulfill({ status: 200, contentType: 'text/event-stream', body: ': ready\n\n' }); return }
    const data = url.pathname === '/api/projects' ? [repository] : url.pathname.endsWith('/git') ? snapshot : url.pathname.endsWith('/diff') ? { patch: '', files: [] } : []
    await route.fulfill({ json: data })
  })
  await page.goto('/')
  await expect(page.getByText('bonsai', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Canvas', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'New worktree' })).toBeVisible()
  await expect(page.getByText('.env', { exact: true }).first()).toBeVisible()
  await expect(page.getByText(/visible nodes/)).toHaveCount(0)

  await page.getByRole('button', { name: 'New worktree' }).click()
  await expect(page.getByText('Existing branch', { exact: true })).toBeVisible()
  await expect(page.getByText('Origin branch', { exact: true })).toBeVisible()
  await expect(page.getByText('New branch', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Merge target' }).click()
  const mergeMenu = page.locator('[data-bonsai-select-menu="Merge target"]')
  await expect(mergeMenu).toBeVisible()
  const menuBox = await mergeMenu.boundingBox()
  const viewport = page.viewportSize()
  expect(menuBox).not.toBeNull()
  expect(viewport).not.toBeNull()
  if (menuBox && viewport) {
    expect(menuBox.x).toBeGreaterThanOrEqual(0)
    expect(menuBox.y).toBeGreaterThanOrEqual(0)
    expect(menuBox.x + menuBox.width).toBeLessThanOrEqual(viewport.width)
    expect(menuBox.y + menuBox.height).toBeLessThanOrEqual(viewport.height)
  }
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Close', exact: true }).click()

  await page.locator('.react-flow__node-project').click()
  await expect(page.getByRole('button', { name: 'Agent', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Agent', exact: true }).click()
  await expect(page.getByText('Start agent', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Project-level launches require an explicit branch choice.')).toBeVisible()
  await page.getByRole('button', { name: 'Close start agent' }).click()

  await page.locator('.react-flow__node-worktree').first().click()
  await page.waitForTimeout(100)

  await page.getByTitle('Minimize workspace').click()
  await expect(page.getByRole('button', { name: 'Open workspace' })).toBeVisible()
  await page.getByRole('button', { name: 'Open workspace' }).click()
  await expect(page.getByTitle('Minimize workspace')).toBeVisible()

  expect(pageErrors).toEqual([])
})


test('expanding a stack keeps unrelated branches fixed', async ({ page }) => {
  await page.goto('/')

  const unrelated = page.locator('.react-flow__node-worktree').filter({ hasText: 'fix/daemon-lifecycle' })
  const stack = page.locator('.react-flow__node-stack').filter({ hasText: 'feat' })
  await expect(unrelated).toBeVisible()
  await expect(stack).toBeVisible()
  await page.waitForTimeout(450)

  const before = await unrelated.boundingBox()
  expect(before).not.toBeNull()

  await stack.locator('button').filter({ hasText: 'Expand' }).click()
  await expect(page.locator('.react-flow__node-stack').filter({ hasText: 'feat' })).toHaveCount(0)
  await expect(page.getByTestId('rf__node-wt-web')).toBeVisible()

  const after = await unrelated.boundingBox()
  expect(after).not.toBeNull()
  if (before && after) {
    expect(Math.abs(after.x - before.x)).toBeLessThan(1)
    expect(Math.abs(after.y - before.y)).toBeLessThan(1)
  }
})
