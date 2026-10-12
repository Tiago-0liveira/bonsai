import { expect, test, type Page } from '@playwright/test'
import { chooseEffort, DEFAULT_API_ORIGIN, mockGitBackend } from './mockGit'

const backend = (page: Page, removeWarnings?: string[]) => mockGitBackend(page, false, false, false, DEFAULT_API_ORIGIN, { enabled: true, removeWarnings })

function trackPageErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.stack ?? error.message))
  return errors
}

const profiles = (page: Page) => page.getByRole('complementary', { name: 'Providers and profiles' })

test.beforeEach(async ({ page }) => { await page.setViewportSize({ width: 1600, height: 960 }) })

test('defaults: edit, save only the changed fields, reset, and guard an unsaved switch', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents?profile=codex-personal&tab=defaults')
  const form = page.getByRole('form', { name: 'Profile defaults' })
  await expect(form.getByRole('combobox', { name: 'Default model' })).toHaveValue('gpt-6.1-sol')
  const effort = form.getByRole('slider', { name: 'Effort' })
  const stops = () => effort.locator('[data-stop]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-stop')))
  await expect(form.getByRole('radio', { name: 'Auto', exact: true })).toBeChecked()
  await expect(effort).toHaveAttribute('aria-valuetext', 'Provider default')
  await expect(page.getByText(/Every new launch of this profile starts with these choices/)).toBeVisible()
  await expect(form.getByRole('button', { name: 'Save defaults' })).toBeDisabled()

  // Reset puts the saved values back.
  await form.getByRole('radio', { name: 'Full access', exact: true }).click()
  await expect(form.getByRole('note')).toContainText('Codex can change any file')
  await expect(form.getByText('Unsaved changes')).toBeVisible()
  await form.getByRole('button', { name: 'Reset' }).click()
  await expect(form.getByRole('radio', { name: 'Auto', exact: true })).toBeChecked()

  // The effort list follows the chosen model.
  await form.getByRole('combobox', { name: 'Default model' }).fill('gpt-6-luna')
  await expect.poll(stops).toEqual(['low', 'medium', 'high', 'xhigh', 'max'])
  await chooseEffort(form, 'medium')
  await form.getByRole('button', { name: 'Save defaults' }).click()
  await expect(form.getByText('Defaults saved')).toBeVisible()
  expect(controls.profileRequests.at(-1)).toEqual({ method: 'PATCH', id: 'codex-personal', body: { defaults: { model: 'gpt-6-luna', effort: 'medium' } } })

  // Back to the provider default sends null; the overview shows what was saved.
  await effort.locator('xpath=../..').getByRole('button', { name: /Provider default/ }).click()
  await form.getByRole('button', { name: 'Save defaults' }).click()
  await expect.poll(() => controls.profileRequests.at(-1)?.body).toEqual({ defaults: { effort: null } })

  // Unsaved edits ask before a profile or tab switch.
  await form.getByRole('combobox', { name: 'Default model' }).fill('gpt-6.1-sol')
  await profiles(page).getByRole('button', { name: /^Team/ }).click()
  const guard = page.getByRole('dialog', { name: 'Discard unsaved defaults' })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing' }).click()
  await expect(page).toHaveURL(/profile=codex-personal/)
  await expect(form.getByRole('combobox', { name: 'Default model' })).toHaveValue('gpt-6.1-sol')
  await page.getByRole('tab', { name: 'Overview' }).click()
  await page.getByRole('dialog', { name: 'Discard unsaved defaults' }).getByRole('button', { name: 'Discard changes' }).click()
  await expect(page).toHaveURL(/tab=overview/)
  await expect(page.getByRole('region', { name: 'Profile defaults' })).toContainText('gpt-6-luna')
  expect(errors).toEqual([])
})

test('rename in the header: Enter saves, Escape cancels, a taken name shows the API message', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page)
  await page.goto('/app/agents?profile=codex-personal')
  const heading = page.getByRole('region', { name: 'Profile details' }).getByRole('heading', { level: 2 })

  await page.getByRole('button', { name: 'Rename profile' }).click()
  await page.getByRole('textbox', { name: 'Profile name' }).fill('Discarded')
  await page.keyboard.press('Escape')
  await expect(heading).toHaveText('Personal')
  expect(controls.profileRequests).toEqual([])

  await page.getByRole('button', { name: 'Rename profile' }).click()
  const input = page.getByRole('textbox', { name: 'Profile name' })
  await input.fill('has space')
  await expect(page.getByRole('alert')).toContainText('Names start with a letter or digit')
  await input.fill('Team')
  await expect(page.getByRole('alert')).toContainText('already has this name')
  await input.fill('Main')
  await input.press('Enter')
  await expect(heading).toHaveText('Main')
  expect(controls.profileRequests).toEqual([{ method: 'PATCH', id: 'codex-personal', body: { name: 'Main' } }])
  await expect(profiles(page).getByRole('button', { name: /^Main/ })).toBeVisible()
  await expect(profiles(page).getByRole('button', { name: /^Personal/ })).toHaveCount(0)
  expect(errors).toEqual([])
})

test('remove: 409 lists the running sessions, then stops them and removes the profile', async ({ page }) => {
  const errors = trackPageErrors(page)
  const controls = await backend(page, ['Could not delete the stored token'])
  await page.goto('/app/agents?profile=codex-personal')
  await page.getByRole('button', { name: 'Remove profile…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Remove profile' })
  await expect(dialog).toContainText('Remove Personal?')
  await expect(dialog).toContainText('conversation history, session data and settings')
  await dialog.getByRole('button', { name: 'Remove profile', exact: true }).click()

  const running = dialog.getByRole('list', { name: 'Running sessions' })
  await expect(running.getByRole('listitem')).toHaveText(['Daemon cleanup', 'Lab experiment'])
  expect(controls.profileRequests.map(item => item.method)).toEqual(['DELETE'])
  await dialog.getByRole('button', { name: 'Stop 2 sessions and remove' }).click()

  const done = page.getByRole('dialog', { name: 'Profile removed' })
  await expect(done).toContainText('Could not delete the stored token')
  await done.getByRole('button', { name: 'Close' }).click()
  expect(controls.profileRequests.map(item => item.method)).toEqual(['DELETE', 'DELETE'])
  await expect(profiles(page).getByRole('button', { name: /^Personal/ })).toHaveCount(0)
  // The selection moves to another profile of the provider and the URL follows.
  await expect(page).toHaveURL(/profile=codex-team/)
  await expect(page.getByRole('heading', { level: 2, name: 'Team' })).toBeVisible()
  expect(errors).toEqual([])
})

test('remove without running sessions closes at once', async ({ page }) => {
  const controls = await backend(page)
  await page.goto('/app/agents?profile=claude-token')
  await page.getByRole('button', { name: 'Remove profile…' }).click()
  await page.getByRole('dialog', { name: 'Remove profile' }).getByRole('button', { name: 'Remove profile', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(controls.profileRequests).toEqual([{ method: 'DELETE', id: 'claude-token', body: null }])
  await expect(profiles(page).getByRole('button', { name: /^CI token/ })).toHaveCount(0)
})
