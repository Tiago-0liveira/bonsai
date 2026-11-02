import { beforeEach, expect, it, vi } from 'vitest'
import { loadAgentSettings, saveAgentSettings, type AgentSettings } from './settings'
import { localFetch } from './localClient'

vi.mock('./localClient', () => ({ localFetch: vi.fn() }))
const fetchMock = vi.mocked(localFetch)
const settings: AgentSettings = { revision: 7, keep_running: false, active_sessions: 1, notifications: { enabled: false, needs_input: true, turn_complete: true } }
const reply = (status: number, body: unknown) => ({ ok: status < 400, status, headers: new Headers({ 'content-type': 'application/json' }), json: async () => body }) as Response

beforeEach(() => fetchMock.mockReset())

it('reads the agent settings including the notification flags', async () => {
  fetchMock.mockResolvedValue(reply(200, settings))
  await expect(loadAgentSettings()).resolves.toEqual(settings)
  expect(fetchMock.mock.calls[0][0]).toBe('/api/settings/agents')
})

it.each([
  [{ keep_running: true }, { keep_running: true, revision: 7 }],
  [{ notifications: { turn_complete: false } }, { notifications: { turn_complete: false }, revision: 7 }],
  [{ keep_running: true, notifications: { enabled: true } }, { keep_running: true, notifications: { enabled: true }, revision: 7 }],
])('posts only the changed fields %j with the read revision', async (change, body) => {
  fetchMock.mockResolvedValue(reply(200, settings))
  await saveAgentSettings(settings, change)
  const [path, init] = fetchMock.mock.calls[0]
  expect(path).toBe('/api/settings/agents')
  expect(init?.method).toBe('POST')
  expect(JSON.parse(String(init?.body))).toEqual(body)
})

it('surfaces the server message of a stale revision (409)', async () => {
  fetchMock.mockResolvedValue(reply(409, { error: { code: 'settings_update_failed', message: 'web settings changed; reload and try again' } }))
  await expect(saveAgentSettings(settings, { keep_running: true })).rejects.toThrow('web settings changed; reload and try again')
})
