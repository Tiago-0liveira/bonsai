import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../stores/bonsai', () => ({
  useBonsaiStore: { getState: () => ({ projects: [] }) },
}))

vi.mock('./git', () => ({
  projectForGitHubRepository: vi.fn((id: number) => id === 123 ? ['bonsai'] : []),
  requestProjectRefresh: vi.fn().mockResolvedValue(undefined),
  report: vi.fn(),
}))

import { requestProjectRefresh } from './git'
import { startRelayInvalidation } from './relayClient'

class FakeEventSource {
  static latest: FakeEventSource | undefined
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  private listeners = new Map<string, EventListenerOrEventListenerObject>()

  constructor(_url: string, _init?: EventSourceInit) {
    FakeEventSource.latest = this
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    this.listeners.set(type, listener)
  }

  close() {}

  message(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }
}

describe('relay invalidation client', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    FakeEventSource.latest = undefined
  })

  it('debounces repeated repository events into one local refresh', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 200 })))
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)

    const stop = startRelayInvalidation()
    await Promise.resolve()
    await Promise.resolve()

    expect(FakeEventSource.latest).toBeDefined()
    FakeEventSource.latest?.message({ repository_id: 123, event: 'pull_request', action: 'synchronize' })
    FakeEventSource.latest?.message({ repository_id: 123, event: 'pull_request', action: 'synchronize' })

    expect(requestProjectRefresh).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(250)
    expect(requestProjectRefresh).toHaveBeenCalledTimes(1)
    expect(requestProjectRefresh).toHaveBeenCalledWith('bonsai', 'provider')
    stop()
  })

  it('stays off when the page says there is no relay', async () => {
    const meta = document.createElement('meta')
    meta.name = 'bonsai-relay-origin'
    meta.content = ''
    document.head.append(meta)
    vi.resetModules()
    try {
      const relay = await import('./relayClient')
      const fetchMock = vi.fn()
      vi.stubGlobal('fetch', fetchMock)
      vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
      expect(relay.RELAY_ENABLED).toBe(false)
      const stop = relay.startRelayInvalidation()
      await Promise.resolve()
      expect(fetchMock).not.toHaveBeenCalled()
      expect(FakeEventSource.latest).toBeUndefined()
      expect(relay.getRelayConnectionSnapshot().status).toBe('disconnected')
      stop()
    } finally {
      meta.remove()
    }
  })
})
