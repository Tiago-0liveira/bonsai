import { WORKSPACE_STORAGE_VERSION } from '../src/stores/workspacePersistence'
import { expect, test } from '@playwright/test'
import { mockGitBackend } from './mockGit'
import './presentationFixtures'

const key = 'bonsai-web-workspace-v6'
const marker = 'SYNTHETIC_ENV_MARKER_FOR_MIGRATION'

test('legacy values are scrubbed before connection and stay removed across reloads', async ({ page }) => {
  await mockGitBackend(page)
  await page.addInitScript(({ key, marker }) => {
    if (localStorage.getItem(key) !== null) return
    localStorage.setItem('unrelated-app', 'leave-me-alone')
    localStorage.setItem(key, JSON.stringify({ version: 0, state: {
      activeProjectId: 'bonsai', activeWorkspaceId: 'personal', selection: { type: 'agent', id: 'worker' },
      agents: [{ id: 'worker', worktreeId: 'wt-web', state: 'running', prompt: marker }],
      dockRuntimeId: 'worker', openRuntimeIds: ['worker'], terminalOutput: { shell: [marker] },
      envVariables: { bonsai: [{ id: 'secret', key: 'SECRET', value: marker, secret: true }, { id: 'plain', key: 'PLAIN', value: marker, secret: false }] },
      dockHeight: 42, editorPreference: 'cursor',
      boardItems: [{ id: 'custom', title: 'User-authored draft', kind: 'Task', status: 'feat', assignee: '', priority: 'High' }],
      nodePlacements: { worker: { x: 0, y: 0, mode: 'manual' }, 'wt-web': { x: 120, y: 240, mode: 'manual' } },
    } }))
  }, { key, marker })

  for (let reload = 0; reload < 2; reload++) {
    await page.goto('/app')
    await expect(page.getByRole('button', { name: 'Connect to local Bonsai' })).toBeVisible()
    const before = await page.evaluate(key => ({
      stored: localStorage.getItem(key),
      unrelated: localStorage.getItem('unrelated-app'),
      state: window.__bonsaiTestStore.getState(),
    }), key)
    expect(before.stored).not.toContain(marker)
    expect(JSON.stringify(before.state)).not.toContain(marker)
    expect(before.state.envVariables).toEqual({})
    expect(before.state.agents).toEqual([])
    expect(before.state.terminalOutput).toEqual({})
    expect(before.state.terminalSessions).toEqual([])
    expect(before.state.openRuntimeIds).toEqual([])
    expect(before.state.selection).toEqual({ type: 'worktree', id: 'wt-web' })
    expect(before.state.boardItems[0].title).toBe('User-authored draft')
    expect(before.state.editorPreference).toBe('cursor')
    expect(before.state.nodePlacements['wt-web']).toEqual({ x: 120, y: 240, mode: 'manual' })
    expect(before.state.nodePlacements.worker).toBeUndefined()
    expect(before.unrelated).toBe('leave-me-alone')
    expect(JSON.parse(before.stored!).version).toBe(WORKSPACE_STORAGE_VERSION)
    await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
    await expect(page.locator('.react-flow')).toBeVisible()
    await expect(page.locator('.react-flow__node-agent')).toHaveCount(0)
    await page.locator('.react-flow__node-env').click()
    const env = page.getByRole('complementary', { name: 'Environment variables' })
    await expect(env).toContainText('Environment editing is unavailable')
    await expect(env.getByRole('button', { name: 'Add variable' })).toBeDisabled()
    await expect(env.locator('input')).toHaveCount(0)
    await expect(page.getByText(marker)).toHaveCount(0)
    expect(await page.evaluate(key => localStorage.getItem(key), key)).not.toContain(marker)
  }
})

test('malformed storage shows an actionable error and safe defaults', async ({ page }) => {
  await page.addInitScript(({ key, marker }) => localStorage.setItem(key, '{invalid ' + marker), { key, marker })
  await page.goto('/app')
  const alert = page.getByRole('alert')
  await expect(alert).toContainText('Saved workspace data was invalid')
  await expect(page.getByRole('button', { name: 'Connect to local Bonsai' })).toBeVisible()
  expect(await page.evaluate(key => localStorage.getItem(key), key)).not.toContain(marker)
  await alert.getByRole('button', { name: 'Retry saving preferences' }).click()
  await expect(alert).toHaveCount(0)
})

test('unavailable storage keeps the app usable and recovery saves only allowed data', async ({ page }) => {
  await mockGitBackend(page)
  await page.addInitScript(() => {
    const original = Storage.prototype.getItem
    let reads = 0
    Storage.prototype.getItem = function (key) {
      if (key === 'bonsai-web-workspace-v6' && reads++ === 0) throw new Error('storage denied')
      return original.call(this, key)
    }
    const write = Storage.prototype.setItem
    let writes = 0
    Storage.prototype.setItem = function (key, value) {
      if (key === 'bonsai-web-workspace-v6' && writes++ === 0) throw new Error('storage denied')
      return write.call(this, key, value)
    }
  })
  await page.goto('/app')
  const alert = page.getByRole('alert')
  await expect(alert).toContainText('Browser storage is unavailable')
  await alert.getByRole('button', { name: 'Retry saving preferences' }).click()
  await expect(alert).toContainText('Browser storage could not be saved')
  await alert.getByRole('button', { name: 'Retry saving preferences' }).click()
  await expect(alert).toHaveCount(0)
  const saved = await page.evaluate(key => JSON.parse(localStorage.getItem(key)!), key)
  expect(saved.state).not.toHaveProperty('envVariables')
  expect(saved.state).not.toHaveProperty('agents')
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await expect(page.locator('.react-flow')).toBeVisible()
})
