import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  __resetLocalClientForTests,
  connectLocalBonsai,
  getLocalConnectionSnapshot,
  localFetch,
  openLocalEvents,
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
      .mockResolvedValueOnce(jsonResponse({ version: 'test', api_version: 3 }))
      .mockResolvedValueOnce(jsonResponse({ token: 'secret-session', expires_at: new Date(Date.now() + 60_000).toISOString() }, 201))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))

    expect(fetchMock).not.toHaveBeenCalled()
    await connectLocalBonsai()
    expect(getLocalConnectionSnapshot().status).toBe('connected')
    await localFetch('/api/projects')
    const privileged = fetchMock.mock.calls.at(-1)
    expect(new Headers(privileged?.[1]?.headers).get('X-Bonsai-Session')).toBe('secret-session')
    expect((privileged?.[1] as RequestInit & { targetAddressSpace?: string })?.targetAddressSpace).toBe('loopback')
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes('secret-session'))).toBe(true)
    expect(setItem.mock.calls.some(([, value]) => String(value).includes('secret-session'))).toBe(false)
  })

  it('surfaces local network permission denial without probing loopback', async () => {
    const originalPermissions = navigator.permissions
    const query = vi.fn().mockResolvedValue({ state: 'denied' })
    Object.defineProperty(navigator, 'permissions', { configurable: true, value: { query } })
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    try {
      await connectLocalBonsai()
      expect(getLocalConnectionSnapshot().status).toBe('permission-denied')
      expect(query).toHaveBeenCalledWith({ name: 'loopback-network' })
      expect(fetchMock).not.toHaveBeenCalled()
    } finally {
      Object.defineProperty(navigator, 'permissions', { configurable: true, value: originalPermissions })
    }
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


class FakeWebSocket {
  static latest: FakeWebSocket | undefined
  private listeners = new Map<string, Set<(event: Event | MessageEvent) => void>>()
  sent: string[] = []
  constructor(_url: string) { FakeWebSocket.latest = this }
  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    const fn = typeof listener === 'function' ? listener : (event: Event) => listener.handleEvent(event)
    const set = this.listeners.get(type) ?? new Set()
    set.add(fn as (event: Event | MessageEvent) => void)
    this.listeners.set(type, set)
  }
  removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    if (typeof listener === 'function') this.listeners.get(type)?.delete(listener as (event: Event | MessageEvent) => void)
  }
  send(value: string) { this.sent.push(value) }
  close() { this.emit('close', new CloseEvent('close')) }
  open() { this.emit('open', new Event('open')) }
  message(value: unknown) { this.emit('message', new MessageEvent('message', { data: JSON.stringify(value) })) }
  private emit(type: string, event: Event | MessageEvent) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener(event)
  }
}

describe('local event authentication', () => {
  it('does not resolve the event connection until authenticated ready supplies an epoch', async () => {
    vi.restoreAllMocks()
    __resetLocalClientForTests()
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ version: 'test', api_version: 3 }))
      .mockResolvedValueOnce(jsonResponse({ token: 'event-session', expires_at: new Date(Date.now() + 60_000).toISOString() }, 201))
    await connectLocalBonsai()

    const original = globalThis.WebSocket
    Object.defineProperty(globalThis, 'WebSocket', { configurable: true, value: FakeWebSocket })
    try {
      const events: unknown[] = []
      let resolved = false
      const pending = openLocalEvents(event => events.push(event)).then(value => {
        resolved = true
        return value
      })
      const socket = FakeWebSocket.latest
      expect(socket).toBeDefined()
      socket?.open()
      expect(socket?.sent).toEqual([JSON.stringify({ type: 'authenticate', token: 'event-session' })])
      await Promise.resolve()
      expect(resolved).toBe(false)

      socket?.message({ type: 'ready', epoch: 'backend-epoch' })
      const connection = await pending
      expect(connection.epoch).toBe('backend-epoch')
      expect(events).toContainEqual({ type: 'ready', epoch: 'backend-epoch' })
    } finally {
      Object.defineProperty(globalThis, 'WebSocket', { configurable: true, value: original })
      FakeWebSocket.latest = undefined
    }
  })
})
