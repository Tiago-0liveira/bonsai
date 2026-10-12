import { expect, test } from '@playwright/test'
import { openFullStackApp as openConnectedApp, selectMenu } from './mockGit'

test.skip(!['linux', 'darwin'].includes(process.platform), 'Native Unix PTY fixture')

test('real API and PTY: profile launch, input, reload, stop and retained output', async ({ page }) => {
  await openConnectedApp(page)
  await page.keyboard.press('Control+k')
  await page.getByRole('option', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await expect(dialog.getByRole('button', { name: /Claude/ })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: /Codex/ })).toBeEnabled()
  await dialog.getByRole('button', { name: /Antigravity/ }).click()
  await expect(dialog.getByLabel('Profile')).toContainText('Fixture')
  await dialog.getByLabel('Worktree and branch').click()
  await selectMenu(page, 'Worktree and branch').getByRole('option').first().click()
  const startResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/projects/repo/agents'))
  await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
  const session = await (await startResponse).json()
  await expect(page.locator('.xterm')).toBeVisible()
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await page.locator('.xterm-helper-textarea').pressSequentially('browser-input')
  await page.locator('.xterm-helper-textarea').press('Enter')
  await expect(page.locator('.xterm-screen')).toContainText('REPLY:browser-input')
  await page.getByRole('button', { name: 'Close runtime card', exact: true }).click()
  await expect(page.locator('.xterm')).toHaveCount(0)
  await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
  await page.getByRole('option', { name: /Fixture/ }).click()
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await expect(page.locator('.xterm-screen')).toContainText('REPLY:browser-input')
  await page.setViewportSize({ width: 1250, height: 850 })
  await page.reload()

  await expect(page.getByTestId(`rf__node-${session.id}`)).toBeVisible()
  await page.getByTestId(`rf__node-${session.id}`).click()
  await expect(page.locator('.runtime-tile')).toContainText('running · connected')
  await expect(page.locator('.xterm-screen')).toContainText('REPLY:browser-input')
  await page.getByRole('button', { name: 'Stop agent', exact: true }).first().click()
  await expect(page.locator('.runtime-tile')).toContainText('exited')
})

test('real API and PTY: two concurrent sessions of one Claude profile, reload and stop one', async ({ page }) => {
  await openConnectedApp(page)
  const start = async (name: string) => {
    await page.keyboard.press('Control+k')
    await page.getByRole('option', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await dialog.getByRole('button', { name: /Claude/ }).click()
    await expect(dialog.getByLabel('Profile')).toContainText('Work')
    await dialog.getByLabel('Worktree and branch').click()
    await selectMenu(page, 'Worktree and branch').getByRole('option').first().click()
    await dialog.getByLabel('Agent name').fill(name)
    await dialog.getByLabel('Agent model').click()
    const suggestions = page.getByRole('listbox', { name: 'Agent model suggestions' })
    await expect(suggestions.getByRole('option', { name: /^Opus/ })).toBeVisible()
    await expect(suggestions.getByRole('option', { name: /^Fable/ })).toBeVisible()
    await suggestions.getByRole('option', { name: /^Sonnet/ }).click()
    await expect(dialog.getByLabel('Agent model')).toHaveValue('sonnet')
    await dialog.getByRole('radio', { name: 'Plan', exact: true }).click()
    const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/projects/repo/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    const session = await (await response).json()
    expect(session).toMatchObject({ provider: 'claude', profile_name: 'Work' })
    return session.id as string
  }
  const tile = (name: string) => page.locator('.runtime-tile').filter({ hasText: name })
  const type = async (name: string, text: string) => {
    const input = tile(name).locator('.xterm-helper-textarea')
    await input.pressSequentially(text)
    await input.press('Enter')
  }

  const first = await start('alpha')
  await expect(tile('alpha').locator('.xterm-screen')).toContainText('READY')
  const second = await start('beta')
  expect(second).not.toBe(first)
  await expect(tile('beta').locator('.xterm-screen')).toContainText('READY')
  await type('alpha', 'to-alpha')
  await type('beta', 'to-beta')
  await expect(tile('alpha').locator('.xterm-screen')).toContainText('REPLY:to-alpha')
  await expect(tile('beta').locator('.xterm-screen')).toContainText('REPLY:to-beta')
  await expect(tile('alpha').locator('.xterm-screen')).not.toContainText('REPLY:to-beta')
  await expect(tile('beta').locator('.xterm-screen')).not.toContainText('REPLY:to-alpha')

  await page.setViewportSize({ width: 1250, height: 850 })
  await page.reload()

  await expect(page.getByTestId(`rf__node-${first}`)).toBeVisible()
  await expect(page.getByTestId(`rf__node-${second}`)).toBeVisible()
  await page.getByTestId(`rf__node-${first}`).click()
  await expect(tile('alpha').locator('.xterm-screen')).toContainText('REPLY:to-alpha')

  await tile('alpha').getByRole('button', { name: 'Stop agent', exact: true }).click()
  await expect(tile('alpha')).toContainText('exited')
  await page.getByTestId(`rf__node-${second}`).click()
  await expect(tile('beta')).toContainText('running · connected')
  await type('beta', 'still-alive')
  await expect(tile('beta').locator('.xterm-screen')).toContainText('REPLY:still-alive')
})

test('real API and PTY: two Codex profiles with two sessions each beside a Claude session', async ({ page }) => {
  // Five tiles share the dock; a tall window keeps each terminal readable.
  await page.setViewportSize({ width: 1800, height: 1700 })
  await openConnectedApp(page)
  const start = async (provider: 'Codex' | 'Claude', profile: string, name: string, permissions?: string) => {
    await page.keyboard.press('Control+k')
    await page.getByRole('option', { name: 'Start agent', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Start agent' })
    await dialog.getByRole('button', { name: new RegExp(provider) }).click()
    if (provider === 'Codex') {
      // Two profiles, so the choice is explicit; the details carry the fake login.
      await dialog.getByRole('option', { name: new RegExp(`^${profile}`) }).click()
      await expect(dialog.getByTestId('profile-details')).toContainText(profile === 'Personal' ? 'me@example.com' : 'team@example.com')
      await expect(dialog.getByTestId('profile-details')).toContainText('logged in')
      await dialog.getByLabel('Agent model').click()
      const suggestions = page.getByRole('listbox', { name: 'Agent model suggestions' })
      await expect(suggestions.getByRole('option', { name: /GPT-6.1-Sol/ })).toBeVisible()
      await suggestions.getByRole('option', { name: /GPT-6-Luna/ }).click()
      await expect(dialog.getByLabel('Agent model')).toHaveValue('gpt-6-luna')
      if (permissions) {
        await dialog.getByRole('radio', { name: permissions, exact: true }).click()
      }
    } else {
      await expect(dialog.getByLabel('Profile')).toContainText(profile)
    }
    await dialog.getByLabel('Worktree and branch').click()
    await selectMenu(page, 'Worktree and branch').getByRole('option').first().click()
    await dialog.getByLabel('Agent name').fill(name)
    const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/projects/repo/agents'))
    await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
    const session = await (await response).json()
    expect(session).toMatchObject({ provider: provider.toLowerCase(), profile_name: profile })
    return session.id as string
  }
  const tile = (name: string) => page.locator('.runtime-tile').filter({ hasText: name })
  const type = async (name: string, text: string) => {
    const input = tile(name).locator('.xterm-helper-textarea')
    await input.pressSequentially(text)
    await input.press('Enter')
  }

  const sessions = {
    'cx-personal-1': await start('Codex', 'Personal', 'cx-personal-1'),
    'cx-personal-2': await start('Codex', 'Personal', 'cx-personal-2', 'Read only'),
    'cx-team-1': await start('Codex', 'Team', 'cx-team-1'),
    'cx-team-2': await start('Codex', 'Team', 'cx-team-2', 'Full access'),
    'claude-1': await start('Claude', 'Work', 'claude-1'),
  }
  const names = Object.keys(sessions)
  expect(new Set(Object.values(sessions)).size).toBe(names.length)
  for (const name of names) {
    await expect(tile(name).locator('.xterm-screen')).toContainText('READY')
    await expect(tile(name)).toContainText('running · connected')
  }
  for (const name of names) await type(name, `to-${name}`)
  for (const name of names) {
    await expect(tile(name).locator('.xterm-screen')).toContainText(`REPLY:to-${name}`)
    for (const other of names.filter(item => item !== name)) await expect(tile(name).locator('.xterm-screen')).not.toContainText(`REPLY:to-${other}`)
  }
  for (const name of names.filter(item => item.startsWith('cx-'))) {
    await expect(tile(name).locator('[data-provider-logo="codex"]')).toBeVisible()
  }

  await page.reload()
  for (const id of Object.values(sessions)) await expect(page.getByTestId(`rf__node-${id}`)).toBeVisible()
  for (const [name, id] of Object.entries(sessions)) {
    await page.getByTestId(`rf__node-${id}`).click()
    await expect(tile(name)).toContainText('running · connected')
    await expect(tile(name).locator('.xterm-screen')).toContainText(`REPLY:to-${name}`)
  }

  await tile('cx-personal-1').getByRole('button', { name: 'Stop agent', exact: true }).click()
  await expect(tile('cx-personal-1')).toContainText('exited')
  for (const name of names.filter(item => item !== 'cx-personal-1')) {
    await page.getByTestId(`rf__node-${sessions[name as keyof typeof sessions]}`).click()
    await expect(tile(name)).toContainText('running · connected')
    await type(name, 'still-alive')
    await expect(tile(name).locator('.xterm-screen')).toContainText('REPLY:still-alive')
  }
})
