import { expect, test, type Page } from '@playwright/test'
import { bonsaiTheme } from '../src/theme/themes'
import { chooseEffort, DEFAULT_API_ORIGIN, mockGitBackend, openConnectedApp, selectMenu } from './mockGit'

const fixture = { enabled: true }
const backend = (page: Page) => mockGitBackend(page, false, false, false, DEFAULT_API_ORIGIN, fixture)

function trackPageErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.stack ?? error.message))
  return errors
}

test.beforeEach(async ({ page }) => { await page.setViewportSize({ width: 1600, height: 960 }) })

test('the nav opens the page: no canvas, providers, profiles and the fleet summary', async ({ page }) => {
  const errors = trackPageErrors(page)
  await backend(page)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Agents', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/agents$/)
  const summary = page.getByRole('region', { name: 'Fleet summary' })
  await expect(summary).toBeVisible()
  await expect(page.locator('.react-flow')).toHaveCount(0)

  const list = page.getByRole('complementary', { name: 'Providers and profiles' })
  await expect(list.getByRole('region', { name: 'Antigravity' })).toContainText('agy is not installed')
  await expect(list.getByRole('region', { name: 'Antigravity' })).toContainText('Install Antigravity')
  await expect(list.getByRole('region', { name: 'Codex' })).toContainText('v0.162.1')
  await expect(list.getByRole('list', { name: 'Codex profiles' }).getByRole('button')).toHaveCount(2)
  await expect(list.getByRole('region', { name: 'Codex' }).getByRole('button', { name: /Team.*Signed out/ })).toBeVisible()
  await expect(list.getByRole('region', { name: 'Claude' }).getByRole('button', { name: /CI token/ })).toBeVisible()
  // Claude, Codex, Antigravity, whatever order the API lists them in; each collapses.
  expect(await list.getByRole('region').evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label')))).toEqual(['Claude', 'Codex', 'Antigravity'])
  const claude = list.getByRole('button', { name: /^Claude/ })
  await claude.click()
  await expect(claude).toHaveAttribute('aria-expanded', 'false')
  await expect(list.getByRole('list', { name: 'Claude profiles' })).toHaveCount(0)
  await claude.click()
  await expect(list.getByRole('button', { name: /CI token/ })).toBeVisible()
  await expect(summary).toContainText('Profiles4')
  await expect(summary).toContainText('Signed in3/4')
  await expect(summary).toContainText('Running3')
  // Usage arrives after the first paint.
  await expect(summary).toContainText('Low headroom1')
  await expect(summary).toContainText('Lowest15%')
  expect(errors).toEqual([])
})

test('a deep link opens a profile on its tab and revalidates with the profile list ETag', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  const accountRequests: (string | undefined)[] = []
  page.on('request', request => {
    if (new URL(request.url()).pathname === '/api/agents/accounts') accountRequests.push(request.headers()['if-none-match'])
  })
  await page.goto('/app/agents?profile=codex-personal&tab=sessions')
  await expect(page.getByRole('heading', { level: 2, name: 'Personal' })).toBeVisible()
  await expect(page.getByRole('tab', { name: /Sessions/ })).toHaveAttribute('data-state', 'active')
  await expect(page.getByRole('list', { name: 'Sessions' }).getByRole('listitem')).toHaveCount(3)

  // A profile added elsewhere shows up after the API says the list changed.
  controls.addAccount({ id: 'codex-late', provider: 'codex', name: 'Late', auth_mode: 'chatgpt', auth_status: 'logged_in' })
  controls.publishAgentsChanged()
  await expect(page.getByRole('complementary', { name: 'Providers and profiles' }).getByRole('button', { name: /^Late/ })).toBeVisible()
  expect(accountRequests[0]).toBeUndefined()
  expect(accountRequests.at(-1)).toMatch(/^"accounts-1"$/)

  // Tab and profile choices live in the URL.
  await page.getByRole('tab', { name: 'Usage' }).click()
  await expect(page).toHaveURL(/profile=codex-personal&tab=usage/)
  await page.getByRole('complementary', { name: 'Providers and profiles' }).getByRole('button', { name: /^Team/ }).click()
  await expect(page).toHaveURL(/profile=codex-team&tab=usage/)
  expect(errors).toEqual([])
})

test('sessions of every project: open a terminal in another project, show it on canvas, stop one', async ({ page }) => {
  const errors = trackPageErrors(page)
  await backend(page)
  await page.goto('/app/agents?profile=codex-personal&tab=sessions')
  const sessions = page.getByRole('list', { name: 'Sessions' })
  await expect(sessions.getByRole('listitem')).toHaveCount(3)
  await expect(sessions.getByRole('listitem').nth(0)).toContainText('Daemon cleanup')
  await expect(sessions.getByRole('listitem').nth(1)).toContainText('sprout-lab')
  await expect(sessions.getByRole('listitem').nth(2)).toContainText('Exited')
  await expect(sessions.getByRole('listitem').nth(2)).toContainText('exit 0')

  await page.getByRole('button', { name: 'Open terminal for Lab experiment' }).click()
  await expect.poll(() => page.evaluate(() => window.__bonsaiTestStore.getState().activeProjectId)).toBe('sprout-lab')
  await expect(page.locator('.xterm').first()).toBeVisible()
  await expect(page).toHaveURL(/\/app\/agents/)

  const stop = page.waitForRequest(request => request.method() === 'DELETE' && request.url().endsWith('/api/projects/bonsai/agents/codex-session-1'))
  await page.getByRole('button', { name: 'Stop Daemon cleanup' }).click()
  await stop
  await expect(sessions.getByRole('listitem').filter({ hasText: 'Daemon cleanup' })).toContainText('Exited')
  await expect(page.getByRole('region', { name: 'Fleet summary' })).toContainText('Running2')

  await page.getByRole('button', { name: 'Show Lab experiment on canvas' }).click()
  await expect(page).toHaveURL(/\/app\/?$/)
  await expect(page.locator('.react-flow')).toBeVisible()
  expect(await page.evaluate(() => window.__bonsaiTestStore.getState().selection)).toEqual({ type: 'agent', id: 'codex-session-lab' })
  expect(errors).toEqual([])
})

test('usage: Codex 5h/weekly with an extra group, Claude per model, a token profile is not supported', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents?profile=codex-personal&tab=usage')
  await expect(page.getByText('Plan: plus')).toBeVisible()
  const limits = page.getByRole('list', { name: 'Plan limits' })
  await expect(limits.getByRole('meter', { name: '5 hour remaining' })).toHaveAttribute('aria-valuenow', '72')
  await expect(limits.getByRole('meter', { name: 'Weekly remaining' })).toHaveAttribute('aria-valuenow', '38')
  await expect(limits.getByText(/^resets in/).first()).toBeVisible()
  const spark = page.getByRole('region', { name: 'Codex Spark' })
  await expect(spark.getByRole('meter')).toHaveCount(2)

  const profiles = page.getByRole('complementary', { name: 'Providers and profiles' })
  await profiles.getByRole('button', { name: /^Work / }).click()
  await expect(page.getByRole('meter', { name: 'Opus (weekly) remaining' })).toHaveAttribute('aria-valuenow', '40')
  await expect(page.getByRole('meter', { name: 'Sonnet (weekly) remaining' })).toHaveAttribute('aria-valuenow', '80')
  await expect(page.getByText('Read from an undocumented Claude endpoint')).toBeVisible()
  // 15% is below the 20% threshold: tinted as low.
  await expect(page.getByRole('list', { name: 'Plan limits' }).getByRole('meter', { name: '5 hour remaining' }).locator('div')).toHaveClass(/bg-warn-solid/)

  await profiles.getByRole('button', { name: /^CI token/ }).click()
  await expect(page.getByText('Usage is not available for this profile')).toBeVisible()
  await expect(page.getByText('Setup tokens cannot read plan usage')).toBeVisible()
  await profiles.getByRole('button', { name: /^Team/ }).click()
  await expect(page.getByText('Not signed in').first()).toBeVisible()

  // Refresh asks for one profile, then for all of them; the API answers from its cache when throttled.
  await profiles.getByRole('button', { name: /^Personal/ }).click()
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('at most every 30 seconds')
  expect(controls.usageRequests.filter(item => item.method === 'POST')).toEqual([{ method: 'POST', account_id: 'codex-personal' }])
  await page.getByRole('button', { name: 'Refresh all' }).click()
  await expect.poll(() => controls.usageRequests.filter(item => item.method === 'POST').length).toBe(2)
  expect(errors).toEqual([])
})

test('start agent from a profile preselects it in the lazy dialog with its cached headroom', async ({ page }) => {
  const controls = await backend(page)
  await page.goto('/app/agents?profile=codex-personal')
  await page.getByRole('button', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(dialog.getByRole('button', { name: /Codex/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(dialog.getByLabel('Profile')).toContainText('Personal')
  await expect(dialog.getByLabel('Profile')).toContainText('5h 72% · week 38%')
  const options = dialog.getByRole('listbox', { name: 'Profile' }).getByRole('option')
  await expect(options).toHaveCount(2)
  await expect(options.nth(0)).toContainText('Personal')
  await expect(options.nth(1)).toContainText('Team')
  await expect(options.nth(1)).not.toContainText('%')
  expect(controls.usageRequests.filter(item => item.method === 'POST')).toEqual([])
})

test('a low profile warns in the dialog, and the launch details reach the sessions list and the inspector', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents?profile=claude-profile&tab=sessions')
  const sessions = page.getByRole('list', { name: 'Sessions' })
  await expect(sessions.getByRole('listitem').filter({ hasText: 'Release notes' })).toContainText('Plan')

  await page.getByRole('tab', { name: 'Overview' }).click()
  await page.getByRole('button', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(dialog.getByLabel('Profile')).toContainText('Work')
  await expect(dialog.getByRole('note')).toContainText('Low headroom: 5h 15% · week 55% left.')
  await dialog.getByLabel('Worktree and branch').click()
  await selectMenu(page, 'Worktree and branch').getByRole('option').first().click()
  await dialog.getByLabel('Agent model').fill('opus')
  await chooseEffort(dialog, 'max')
  await dialog.getByLabel('Agent name').fill('Low headroom run')
  await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(controls.usageRequests.filter(item => item.method === 'POST')).toEqual([])

  await page.goto('/app/agents?profile=claude-profile&tab=sessions')
  await expect(sessions.getByRole('listitem').filter({ hasText: 'Low headroom run' })).toContainText('opus · Default · max effort')
  await page.getByRole('button', { name: 'Show Release notes on canvas' }).click()
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByText('Launch', { exact: true })).toBeVisible()
  await expect(inspector).toContainText('Plan')
  expect(errors).toEqual([])
})

test('add profile opens the sign-in dialog on the providers', async ({ page }) => {
  await backend(page)
  await page.goto('/app/agents')
  await page.getByRole('button', { name: 'Add profile' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add profile' })
  await expect(dialog.getByRole('button', { name: /Claude/ })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: /Codex/ })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: /Antigravity/ })).toContainText('agy is not installed')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
})

test('the palette shows the agents of the active project on the canvas', async ({ page }) => {
  await backend(page)
  await page.goto('/app/agents')
  await expect(page.getByRole('region', { name: 'Fleet summary' })).toBeVisible()
  await page.keyboard.press('Control+k')
  await page.getByRole('option', { name: 'Show agents on canvas' }).click()
  await expect(page).toHaveURL(/\/app\/?$/)
  await expect(page.locator('.react-flow')).toBeVisible()
  // The one-shot request is consumed once the canvas has fitted its agents.
  await expect.poll(() => page.evaluate(() => window.__bonsaiTestStore.getState().canvasFitAgents)).toBe(false)
})

for (const mode of ['dark', 'light'] as const) {
  test(`${mode} theme screenshots and no horizontal overflow at a narrow width`, async ({ page }, testInfo) => {
    if (mode === 'light') {
      const theme = { ...bonsaiTheme, id: 'custom:paper', name: 'Paper', mode, colors: {
        ...bonsaiTheme.colors, bg: '#ffffff', well: '#f5f5f5', panel: '#ffffff',
        'panel-2': '#f5f5f5', 'panel-3': '#eeeeee', 'panel-4': '#e2e2e2',
        text: '#111111', muted: '#333333', 'muted-2': '#555555', 'accent-fg': '#000000',
      } }
      await page.addInitScript(theme => {
        localStorage.setItem('bonsai-theme-v1', JSON.stringify({ version: 1, activeId: theme.id, custom: [theme] }))
      }, theme)
    }
    await backend(page)
    for (const tab of ['overview', 'usage', 'sessions', 'defaults']) {
      await page.goto(`/app/agents?profile=codex-personal&tab=${tab}`)
      await expect(page.locator('html')).toHaveCSS('color-scheme', mode)
      await expect(page.getByRole('region', { name: 'Fleet summary' })).toContainText('Low headroom1')
      await page.waitForTimeout(300)
      await page.screenshot({ path: testInfo.outputPath(`agents-${tab}-${mode}.png`) })
    }
    await page.setViewportSize({ width: 420, height: 900 })
    await page.goto('/app/agents?profile=codex-personal&tab=usage')
    await expect(page.getByText('Plan: plus')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`agents-narrow-${mode}.png`) })
  })
}
