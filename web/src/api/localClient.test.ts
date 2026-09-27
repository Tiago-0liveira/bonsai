import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  __resetLocalClientForTests,
  connectLocalBonsai,
  getLocalConnectionSnapshot,
  localFetch,
} from './localClient'

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('local Bonsai client', () => {
  beforeEach(() => {
    __resetLocalClientForTests()
    vi.restoreAllMocks()
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  })

  it('creates an in-memory capability only after an explicit connect', async () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem')
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ version: 'test', api_version: 1 }))
      .mockResolvedValueOnce(jsonResponse({ token: 'secret-session', expires_at: new Date(Date.now() + 60_000).toISOString() }, 201))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))

    expect(fetchMock).not.toHaveBeenCalled()
    await connectLocalBonsai()
    expect(getLocalConnectionSnapshot().status).toBe('connected')
    await localFetch('/api/projects')
    const privileged = fetchMock.mock.calls.at(-1)
    expect(new Headers(privileged?.[1]?.headers).get('X-Bonsai-Session')).toBe('secret-session')
    expect(setItem.mock.calls.some(([, value]) => String(value).includes('secret-session'))).toBe(false)
  })

  it('surfaces an incompatible local API protocol explicitly', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ version: 'old', api_version: 0 }))
    await connectLocalBonsai()
    expect(getLocalConnectionSnapshot()).toMatchObject({ status: 'version-incompatible', apiVersion: 0 })
  })

  it('does not create a capability when the health probe fails', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('failed to fetch'))
    await connectLocalBonsai()
    expect(getLocalConnectionSnapshot().status).toBe('bonsai-not-running')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
