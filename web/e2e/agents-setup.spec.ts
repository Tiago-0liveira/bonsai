import { expect, test, type Page } from '@playwright/test'
import { DEFAULT_API_ORIGIN, mockGitBackend, type AgentsTabFixture } from './mockGit'

const backend = (page: Page, fixture: Partial<AgentsTabFixture> = {}) => mockGitBackend(page, false, false, false, DEFAULT_API_ORIGIN, { enabled: true, ...fixture })

function trackPageErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.stack ?? error.message))
  return errors
}

const profiles = (page: Page) => page.getByRole('complementary', { name: 'Providers and profiles' })
const addDialog = (page: Page) => page.getByRole('dialog', { name: 'Add profile' })
const terminalText = (page: Page) => page.getByTestId('setup-terminal').locator('.xterm-rows')

async function openAdd(page: Page, provider: RegExp, name: string) {
  await page.goto('/app/agents')
  await page.getByRole('region', { name: 'Fleet summary' }).getByRole('button', { name: 'Add profile' }).click()
  const dialog = addDialog(page)
  await dialog.getByRole('list', { name: 'Providers' }).getByRole('button', { name: provider }).click()
  await dialog.getByRole('textbox', { name: 'Profile name' }).fill(name)
  return dialog
}

test.beforeEach(async ({ page }) => { await page.setViewportSize({ width: 1600, height: 960 }) })

test('create a Codex profile with a device code, then start an agent with it', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents')
  await page.getByRole('region', { name: 'Fleet summary' }).getByRole('button', { name: 'Add profile' }).click()
  const dialog = addDialog(page)
  const cards = dialog.getByRole('list', { name: 'Providers' })
  await expect(cards.getByRole('button', { name: /Antigravity/ })).toBeDisabled()
  await expect(cards.getByRole('button', { name: /Antigravity/ })).toContainText('agy is not installed')
  await expect(cards.getByRole('button', { name: /Codex/ })).toContainText('2 profiles')
  await cards.getByRole('button', { name: /Codex/ }).click()

  await expect(dialog.getByRole('radio', { name: /Device code/ })).toBeChecked()
  await expect(dialog.getByText('Recommended')).toBeVisible()
  await expect(dialog.getByRole('checkbox', { name: /Copy settings from ~\/.codex/ })).toBeChecked()
  await dialog.getByRole('textbox', { name: 'Profile name' }).fill('Personal')
  await expect(dialog.getByRole('alert')).toContainText('already has this name')
  await dialog.getByRole('textbox', { name: 'Profile name' }).fill('laptop')
  await dialog.getByRole('button', { name: 'Sign in' }).click()

  const signin = page.getByRole('dialog', { name: 'Add profile' })
  await expect(terminalText(page)).toContainText('WXYZ-2345')
  await expect(terminalText(page)).toContainText('https://auth.example.com/codex/device')
  await expect(signin.getByText('laptop is ready.')).toBeVisible()
  await expect(signin).toContainText('laptop@example.com')
  await expect(signin).toContainText('Plus')
  const start = controls.setupRequests.find(item => item.path === '/api/agents/setup')!
  expect(start.body).toEqual({ provider: 'codex', name: 'laptop', auth_mode: 'device', seed: true })
  expect(start.key).toBeTruthy()

  await expect(profiles(page).getByRole('button', { name: /^laptop/ })).toBeVisible()
  await expect(page).toHaveURL(/profile=codex-laptop/)
  await signin.getByRole('button', { name: 'Start agent with this profile' }).click()
  const startDialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(startDialog.getByLabel('Profile')).toContainText('laptop')
  expect(errors).toEqual([])
})

test('create a Codex API-key profile: only the start request carries the key', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  const secret = 'sk-e2e-0123456789abcdef'
  const dialog = await openAdd(page, /Codex/, 'ci')
  await dialog.getByRole('radio', { name: /API key/ }).check()
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByRole('alert')).toContainText('Paste the OpenAI API key.')
  await dialog.getByLabel('OpenAI API key', { exact: true }).fill(secret)
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByText('ci is ready.')).toBeVisible()
  await expect(dialog).toContainText('API key')

  const [start, ...later] = controls.setupRequests
  expect(start.body).toEqual({ provider: 'codex', name: 'ci', auth_mode: 'api-key', seed: true, secret })
  expect(later.length).toBeGreaterThan(0)
  for (const request of later) expect(JSON.stringify(request), request.path).not.toContain(secret)
  // Nothing the page keeps or shows has the key.
  expect(await page.content()).not.toContain(secret)
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage }))).not.toContain(secret)
  expect(errors).toEqual([])
})

test('Claude setup token: the terminal makes one, the prompt takes it', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  const dialog = await openAdd(page, /Claude/, 'ci-token')
  await dialog.getByRole('radio', { name: /Setup token/ }).check()
  await expect(dialog.getByText('Leave empty to generate one in the terminal.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(terminalText(page)).toContainText('Token created.')

  const prompt = dialog.getByRole('form', { name: 'Secret prompt' })
  await expect(dialog.getByRole('status', { name: 'Sign-in status' })).toContainText('Waiting for you to paste a secret')
  await prompt.getByLabel('Claude setup token', { exact: true }).fill('sk-ant-oat01-e2e-token')
  await prompt.getByRole('button', { name: 'Send' }).click()
  await expect(dialog.getByText('ci-token is ready.')).toBeVisible()
  await expect(dialog.getByRole('list', { name: 'Warnings' })).toContainText('Token profiles cannot read plan usage')

  expect(controls.setupRequests[0].body).toEqual({ provider: 'claude', name: 'ci-token', auth_mode: 'token', seed: true })
  const answer = controls.setupRequests.find(item => item.path.endsWith('/secret'))!
  expect(answer).toMatchObject({ method: 'POST', path: '/api/agents/setup/job-1/secret', body: { secret: 'sk-ant-oat01-e2e-token' } })
  expect(await page.content()).not.toContain('sk-ant-oat01-e2e-token')
  expect(errors).toEqual([])
})

test('a failed sign-in shows the error and can be tried again; a running one is canceled on close', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  const dialog = await openAdd(page, /Codex/, 'broken')
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByRole('alert')).toContainText('codex login exited with status 1')
  await expect(dialog.getByRole('status', { name: 'Sign-in status' })).toContainText('Sign-in failed')
  await dialog.getByRole('button', { name: 'Try again' }).click()
  await dialog.getByRole('textbox', { name: 'Profile name' }).fill('slow')
  await dialog.getByRole('radio', { name: /Browser sign-in/ }).check()
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByRole('status', { name: 'Sign-in status' })).toBeVisible()

  // Escape while it runs asks first; cancelling deletes the job and closes.
  await page.keyboard.press('Escape')
  const confirm = dialog.getByRole('alertdialog', { name: 'Cancel sign-in' })
  await expect(confirm).toContainText('the new profile is removed')
  await confirm.getByRole('button', { name: 'Cancel sign-in' }).click()
  await expect(page.getByRole('dialog', { name: 'Add profile' })).toHaveCount(0)
  expect(controls.setupRequests.some(item => item.method === 'DELETE' && item.path === '/api/agents/setup/job-2')).toBe(true)
  await expect(profiles(page).getByRole('button', { name: /^slow/ })).toHaveCount(0)
  expect(errors).toEqual([])
})

test('sign a signed-out profile in again from its warning, and reattach after a reload', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents?profile=claude-token')
  // Reload in the middle of a token sign-in: the page reattaches to the same job.
  await page.getByRole('button', { name: 'Sign in again…' }).click()
  const again = page.getByRole('dialog', { name: 'Sign in to CI token again' })
  await expect(again.getByRole('radio', { name: /Setup token/ })).toBeChecked()
  await again.getByRole('button', { name: 'Sign in' }).click()
  await expect(again.getByRole('form', { name: 'Secret prompt' })).toBeVisible()
  await page.reload()
  const reattached = page.getByRole('dialog', { name: 'Sign in to CI token again' })
  await expect(reattached.getByRole('form', { name: 'Secret prompt' })).toBeVisible()
  await reattached.getByLabel('Claude setup token', { exact: true }).fill('sk-ant-oat01-renewed')
  await reattached.getByRole('button', { name: 'Send' }).click()
  await expect(reattached.getByText('CI token is signed in again.')).toBeVisible()
  await reattached.getByRole('button', { name: 'Close' }).click()
  expect(controls.setupRequests[0]).toMatchObject({ method: 'POST', path: '/api/agents/accounts/claude-token/login', body: {} })

  await profiles(page).getByRole('button', { name: /^Team/ }).click()
  await page.getByRole('button', { name: 'Sign in now' }).click()
  const team = page.getByRole('dialog', { name: 'Sign in to Team again' })
  await expect(team.getByRole('radio', { name: /Device code/ })).toBeChecked()
  await team.getByRole('button', { name: 'Sign in' }).click()
  await expect(team.getByText('Team is signed in again.')).toBeVisible()
  await team.getByRole('button', { name: 'Close' }).click()
  await expect(page.getByRole('region', { name: 'Profile details' }).getByText('Signed in', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in now' })).toHaveCount(0)
  expect(errors).toEqual([])
})

test('without browser sign-in the dialog gives the CLI command', async ({ page }) => {
  await backend(page, { setupUnsupported: true })
  const dialog = await openAdd(page, /Codex/, 'laptop')
  await dialog.getByRole('button', { name: 'Sign in' }).click()
  await expect(dialog.getByText('bonsai agent account add codex laptop --auth device')).toBeVisible()
})
