import { expect, test, type Page } from '@playwright/test'
import { mockGitBackend, openConnectedApp } from './mockGit'
import './presentationFixtures'

async function editorBackend(page: Page, installed = true) {
  let preferred = ''
  let revision = 0
  const opened: { url: string; body: unknown }[] = []
  await mockGitBackend(page)
  await page.route('**/api/settings/editors', async route => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      expect(body.environment_id).toBe('wsl:host:Ubuntu')
      expect(body.revision).toBe(revision)
      preferred = body.editor
      revision++
    }
    await route.fulfill({ json: {
      environment: { id: 'wsl:host:Ubuntu', label: 'WSL (Ubuntu)' }, revision,
      editors: installed ? [{ id: 'zed', name: 'Zed', command: '/usr/bin/zed', folders: true }] : [],
      preferred, saved_preferred: preferred,
    } })
  })
  await page.route('**/api/worktrees/*/files', route => route.fulfill({ json: [{ path: 'file with spaces.ts', status: '' }] }))
  await page.route('**/api/worktrees/*/open-editor', async route => {
    opened.push({ url: route.request().url(), body: route.request().postDataJSON() })
    await route.fulfill({ status: 202, json: { editor: 'Zed', environment: 'WSL (Ubuntu)' } })
  })
  return opened
}

test('chooses a native editor with the dock collapsed, opens the pending file, and persists the environment setting', async ({ page }) => {
  const opened = await editorBackend(page)
  await openConnectedApp(page)
  await page.getByTestId('rf__node-wt-web').click()
  await page.evaluate(() => window.__bonsaiTestStore.getState().setDockState('collapsed'))
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('tab', { name: 'Files', exact: true }).click()
  await inspector.getByRole('button', { name: 'file with spaces.ts', exact: true }).click()
  const chooser = page.getByRole('dialog', { name: 'Open files with…' })
  await expect(chooser).toBeVisible()
  await expect(chooser).toContainText('WSL (Ubuntu)')
  await expect(chooser.getByRole('button', { name: 'Cursor' })).toHaveCount(0)
  await chooser.getByRole('button', { name: 'Zed', exact: true }).click()
  await expect(chooser).toBeHidden()
  expect(opened).toEqual([{ url: expect.stringContaining('/api/worktrees/wt-web/open-editor'), body: { environment_id: 'wsl:host:Ubuntu', path: 'file with spaces.ts' } }])

  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  const section = page.getByRole('region', { name: 'Environment editor' })
  await expect(section.getByRole('combobox', { name: 'Default editor' })).toHaveValue('zed')
  await page.reload()
  await expect(section.getByRole('combobox', { name: 'Default editor' })).toHaveValue('zed')
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('bonsai-web-workspace-v6')!).state)).not.toHaveProperty('editorPreference')
})

test('shows an empty native environment without offering system editors', async ({ page }) => {
  await editorBackend(page, false)
  await openConnectedApp(page)
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  const section = page.getByRole('region', { name: 'Environment editor' })
  await expect(section).toContainText('No supported editors were found')
  await expect(section.getByRole('combobox')).toBeDisabled()
  await expect(section.getByRole('option')).toHaveCount(1)
  await expect(section).toContainText('through their WSL launchers')
})

test('real discovery only offers WSL-aware wrappers for Windows editors in WSL', async ({ request }) => {
  const origin = 'http://127.0.0.1:7011'
  const session = await request.post(origin + '/api/session', { headers: { Origin: origin } })
  expect(session.ok()).toBe(true)
  const { token } = await session.json()
  const response = await request.get(origin + '/api/settings/editors', { headers: { Origin: origin, 'X-Bonsai-Session': token } })
  expect(response.ok()).toBe(true)
  const settings = await response.json()
  expect(settings.environment.id).toBeTruthy()
  if (settings.environment.id.startsWith('wsl:')) {
    for (const editor of settings.editors) {
      expect(editor.command).not.toMatch(/\.exe$/i)
      // A Windows drive command must be a distinct "-windows" editor, launched
      // through its WSL wrapper and never confused with a native one.
      if (/^\/mnt\/[a-z]\//.test(editor.command)) expect(editor.id).toMatch(/-windows$/)
    }
  }
})
