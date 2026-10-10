import { afterEach, describe, expect, it, vi } from 'vitest'
import { runtimeEntry, runtimeMeta } from './runtimeConfig'

function setMeta(name: string, content: string) {
  const meta = document.createElement('meta')
  meta.name = name
  meta.content = content
  document.head.append(meta)
}

afterEach(() => {
  document.head.querySelectorAll('meta').forEach(meta => meta.remove())
  vi.resetModules()
  vi.restoreAllMocks()
})

describe('runtime config meta tags', () => {
  it('treats absent tags and deploy placeholders as unset, but keeps empty values', () => {
    expect(runtimeMeta('bonsai-relay-origin')).toBeUndefined()
    setMeta('bonsai-relay-origin', '__BONSAI_RELAY_ORIGIN__')
    expect(runtimeMeta('bonsai-relay-origin')).toBeUndefined()
    document.head.querySelector('meta')?.remove()
    setMeta('bonsai-relay-origin', '')
    expect(runtimeMeta('bonsai-relay-origin')).toBe('')
  })

  it('reports the local entry only when the local API marks the page', () => {
    expect(runtimeEntry()).toBe('hosted')
    setMeta('bonsai-entry', 'local')
    expect(runtimeEntry()).toBe('local')
  })
})

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('page served by the local API', () => {
  it('connects same-origin without a Local Network Access probe', async () => {
    setMeta('bonsai-local-api-origin', window.location.origin)
    setMeta('bonsai-entry', 'local')
    const client = await import('./localClient')
    expect(client.LOCAL_API_HTTP).toBe(window.location.origin)
    expect(client.LOCAL_API_SAME_ORIGIN).toBe(true)
    expect(client.LOCAL_ENTRY).toBe(true)

    const query = vi.fn().mockResolvedValue({ state: 'prompt' })
    const original = navigator.permissions
    Object.defineProperty(navigator, 'permissions', { configurable: true, value: { query } })
    const statuses: string[] = []
    client.subscribeLocalConnection(() => statuses.push(client.getLocalConnectionSnapshot().status))
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ version: 'test', api_version: client.LOCAL_API_PROTOCOL_VERSION }))
      .mockResolvedValueOnce(jsonResponse({ token: 'session', expires_at: new Date(Date.now() + 60_000).toISOString() }, 201))
      .mockResolvedValueOnce(jsonResponse([]))
    try {
      await client.connectLocalBonsai()
      await client.localFetch('/api/projects')
    } finally {
      Object.defineProperty(navigator, 'permissions', { configurable: true, value: original })
    }

    expect(client.getLocalConnectionSnapshot().status).toBe('connected')
    expect(query).not.toHaveBeenCalled()
    expect(statuses).not.toContain('requesting-permission')
    for (const [url, init] of fetchMock.mock.calls) {
      expect(String(url).startsWith(window.location.origin + '/')).toBe(true)
      expect((init as RequestInit & { targetAddressSpace?: string }).targetAddressSpace).toBeUndefined()
    }
  })

  it('names bonsai web when the API stops answering', async () => {
    setMeta('bonsai-local-api-origin', window.location.origin)
    const client = await import('./localClient')
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('failed to fetch'))
    await client.connectLocalBonsai()
    expect(client.getLocalConnectionSnapshot()).toMatchObject({ status: 'bonsai-not-running' })
    expect(client.getLocalConnectionSnapshot().message).toContain('bonsai web')
    expect(client.getLocalConnectionSnapshot().message).not.toContain('Local Network Access')
  })

  it('keeps the cross-origin loopback path for the hosted entry', async () => {
    const client = await import('./localClient')
    expect(client.LOCAL_API_HTTP).toBe('http://127.0.0.1:7001')
    expect(client.LOCAL_API_SAME_ORIGIN).toBe(false)
    expect(client.LOCAL_ENTRY).toBe(false)
  })
})
