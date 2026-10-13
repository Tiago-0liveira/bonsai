import { expect, test, type Page } from '@playwright/test'
import { chooseEffort } from './mockGit'

test.skip(!['linux', 'darwin'].includes(process.platform), 'Native Unix PTY fixture')

// The real API over the production agent runtime (TestAgentsTabBrowserFixture):
// no profiles at start, fake codex/claude on PATH, a temp HOME.
const ORIGIN = 'http://127.0.0.1:7012'
const API_KEY = 'sk-fullstack-0123456789abcdef'

const profiles = (page: Page) => page.getByRole('complementary', { name: 'Providers and profiles' })
const addDialog = (page: Page) => page.getByRole('dialog', { name: 'Add profile' })
const sessions = (page: Page) => page.getByRole('list', { name: 'Sessions' })

function trackPageErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.stack ?? error.message))
  return errors
}

async function addCodexProfile(page: Page, name: string, mode: RegExp) {
  await page.getByRole('region', { name: 'Fleet summary' }).getByRole('button', { name: 'Add profile' }).click()
  const dialog = addDialog(page)
  await dialog.getByRole('list', { name: 'Providers' }).getByRole('button', { name: /Codex/ }).click()
  await dialog.getByRole('textbox', { name: 'Profile name' }).fill(name)
  await dialog.getByRole('radio', { name: mode }).check()
  return dialog
}

async function startFromTab(page: Page, profile: string, name: string, permissions?: string) {
  await page.getByRole('tab', { name: 'Overview' }).click()
  await page.getByRole('button', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(dialog.getByLabel('Profile')).toContainText(profile)
  // The dialog opens on the profile's saved model.
  await expect(dialog.getByLabel('Agent model')).toHaveValue('gpt-6-luna')
  await dialog.getByLabel('Worktree and branch').click()
  await page.locator('[data-bonsai-select-menu="Worktree and branch"]').getByRole('option').first().click()
  await dialog.getByLabel('Agent name').fill(name)
  await chooseEffort(dialog, 'high')
  if (permissions) await dialog.getByRole('radio', { name: permissions, exact: true }).click()
  const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/projects/repo/agents'))
  await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
  const session = await (await response).json()
  expect(session).toMatchObject({ provider: 'codex', profile_name: profile, model: 'gpt-6-luna' })
  await expect(dialog).toBeHidden()
  return session.id as string
}

test('real API: create Codex profiles in the browser, set defaults, run sessions, guarded remove', async ({ page }) => {
  test.setTimeout(120_000)
  const errors = trackPageErrors(page)
  await page.setViewportSize({ width: 1600, height: 960 })
  // Every request body after the one that carries the API key must not hold it.
  const bodies: { url: string, body: string }[] = []
  page.on('request', request => {
    if (request.url().startsWith(`${ORIGIN}/api/`)) bodies.push({ url: request.url(), body: request.postData() ?? '' })
  })
  const responses: Promise<string>[] = []
  page.on('response', response => {
    if (response.url().startsWith(`${ORIGIN}/api/`)) responses.push(response.text().catch(() => ''))
  })

  await page.goto(`${ORIGIN}/app/agents`)
  await expect(page.getByRole('heading', { name: 'No profiles yet' })).toBeVisible()
  const list = profiles(page)
  await expect(list.getByRole('region', { name: 'Antigravity' })).toContainText('Install agy and restart Bonsai')
  await expect(list.getByRole('region', { name: 'Codex' })).toContainText('v0.162.1')

  // 1. Device code: the job terminal shows the verification URL and code, then the profile is ready.
  let dialog = await addCodexProfile(page, 'laptop', /Device code/)
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  const terminal = page.getByTestId('setup-terminal').locator('.xterm-rows')
  await expect(terminal).toContainText('BONSAI-E2E1')
  await expect(terminal).toContainText('https://auth.openai.com/codex/device')
  // The fake login only finishes after reading a line, so reaching "ready"
  // proves what the browser types gets to the login process through the PTY.
  await expect(terminal).toContainText('Enter code:')
  await expect(dialog.getByText('laptop is ready.')).toHaveCount(0)
  const input = page.getByTestId('setup-terminal').locator('.xterm-helper-textarea')
  await input.pressSequentially('typed-in-browser')
  await input.press('Enter')
  await expect(dialog.getByText('laptop is ready.')).toBeVisible()
  await expect(dialog).toContainText('fullstack@example.com')
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(list.getByRole('button', { name: /^laptop/ })).toBeVisible()

  // 2. API key in the start body only: nothing later sends or returns it.
  dialog = await addCodexProfile(page, 'ci', /API key/)
  await dialog.getByLabel('OpenAI API key', { exact: true }).fill(API_KEY)
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByText('ci is ready.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(list.getByRole('button', { name: /^ci/ })).toBeVisible()
  const carrying = bodies.filter(item => item.body.includes(API_KEY))
  expect(carrying.map(item => new URL(item.url).pathname)).toEqual(['/api/agents/setup'])
  for (const text of await Promise.all(responses)) expect(text).not.toContain(API_KEY)
  expect(await page.content()).not.toContain(API_KEY)

  // 3. Usage of the signed-in profile comes from the fake app-server's phase0 fixtures.
  await list.getByRole('button', { name: /^laptop/ }).click()
  await page.getByRole('tab', { name: 'Usage' }).click()
  await expect(page.getByText('Plan: plus')).toBeVisible()
  const limits = page.getByRole('list', { name: 'Plan limits' })
  await expect(limits.getByRole('meter', { name: '5h remaining' })).toHaveAttribute('aria-valuenow', '72')
  await expect(limits.getByRole('meter', { name: 'Weekly remaining' })).toHaveAttribute('aria-valuenow', '96')
  await list.getByRole('button', { name: /^ci/ }).click()
  await expect(page.getByText('Usage is not available for this profile')).toBeVisible()

  // 4. Profile defaults are saved through the provider and shown on the overview.
  await list.getByRole('button', { name: /^laptop/ }).click()
  await page.getByRole('tab', { name: 'Defaults' }).click()
  const form = page.getByRole('form', { name: 'Profile defaults' })
  await form.getByRole('combobox', { name: 'Default model' }).fill('gpt-6-luna')
  await chooseEffort(form, 'medium')
  await form.getByRole('button', { name: 'Save defaults' }).click()
  await expect(form.getByText('Defaults saved')).toBeVisible()
  await page.getByRole('tab', { name: 'Overview' }).click()
  await expect(page.getByRole('region', { name: 'Profile defaults' })).toContainText('gpt-6-luna')

  // 5. Two sessions from the tab; the sessions list shows what each was launched with.
  const first = await startFromTab(page, 'laptop', 'fs-one')
  const second = await startFromTab(page, 'laptop', 'fs-two', 'Read only')
  expect(second).not.toBe(first)
  await page.goto(`${ORIGIN}/app/agents`)
  await profiles(page).getByRole('button', { name: /^laptop/ }).click()
  await page.getByRole('tab', { name: /Sessions/ }).click()
  await expect(sessions(page).getByRole('listitem')).toHaveCount(2)
  await expect(sessions(page).getByRole('listitem').filter({ hasText: 'fs-one' })).toContainText('gpt-6-luna · Auto · high effort')
  await expect(sessions(page).getByRole('listitem').filter({ hasText: 'fs-two' })).toContainText('gpt-6-luna · Read only · high effort')
  await expect(page.getByRole('region', { name: 'Fleet summary' })).toContainText('Running2')

  // 6. Remove while running: the API refuses and lists the sessions.
  await page.getByRole('tab', { name: 'Overview' }).click()
  await page.getByRole('button', { name: 'Remove profile…' }).click()
  const remove = page.getByRole('dialog', { name: 'Remove profile' })
  await remove.getByRole('button', { name: 'Remove profile', exact: true }).click()
  await expect(remove.getByRole('list', { name: 'Running sessions' }).getByRole('listitem')).toHaveText(['fs-one', 'fs-two'], { ignoreCase: false, useInnerText: true })
  await remove.getByRole('button', { name: 'Cancel' }).click()
  await expect(remove).toHaveCount(0)

  // 7. Stop both from the Sessions tab, then the removal goes through.
  await page.getByRole('tab', { name: /Sessions/ }).click()
  for (const name of ['fs-one', 'fs-two']) {
    await page.getByRole('button', { name: `Stop ${name}` }).click()
    await expect(sessions(page).getByRole('listitem').filter({ hasText: name })).toContainText('Exited')
  }
  await expect(page.getByRole('region', { name: 'Fleet summary' })).toContainText('Running0')
  await page.getByRole('tab', { name: 'Overview' }).click()
  await page.getByRole('button', { name: 'Remove profile…' }).click()
  await page.getByRole('dialog', { name: 'Remove profile' }).getByRole('button', { name: 'Remove profile', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(profiles(page).getByRole('button', { name: /^laptop/ })).toHaveCount(0)
  await expect(profiles(page).getByRole('button', { name: /^ci/ })).toBeVisible()
  expect(errors).toEqual([])
})
