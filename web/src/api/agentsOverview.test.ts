import { describe, expect, it, vi } from 'vitest'
import { accountAddCommand, agentAccountsIfChanged, agentUsage, isActiveSession, refreshAgentUsage } from './agentsOverview'
import { localFetch } from './localClient'

vi.mock('./localClient', () => ({ localFetch: vi.fn(), LOCAL_API_SAME_ORIGIN: true }))

describe('agents overview reads', () => {
  const json = (value: unknown, init: ResponseInit = {}) => new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' }, ...init })

  it('revalidates profiles with the previous ETag and reports no change on 304', async () => {
    vi.mocked(localFetch).mockResolvedValueOnce(json([{ id: 'a', name: 'A', provider: 'codex' }], { headers: { ETag: '"v1"' } }))
    const first = await agentAccountsIfChanged()
    expect(first).toEqual({ accounts: [{ id: 'a', name: 'A', provider: 'codex' }], etag: '"v1"' })
    expect(vi.mocked(localFetch).mock.calls.at(-1)?.[1]).toBeUndefined()
    vi.mocked(localFetch).mockResolvedValueOnce(new Response(null, { status: 304 }))
    await expect(agentAccountsIfChanged('"v1"')).resolves.toBeUndefined()
    expect(vi.mocked(localFetch).mock.calls.at(-1)).toEqual(['/api/agents/accounts', { headers: { 'If-None-Match': '"v1"' } }])
  })

  it('reads usage from the cached route and refreshes one profile with a JSON body', async () => {
    vi.mocked(localFetch).mockResolvedValueOnce(json([])).mockResolvedValueOnce(json([])).mockResolvedValueOnce(json([]))
    await agentUsage()
    await agentUsage('a/b')
    await refreshAgentUsage('acct 1')
    const calls = vi.mocked(localFetch).mock.calls.slice(-3)
    expect(calls[0]).toEqual(['/api/agents/usage', undefined])
    expect(calls[1][0]).toBe('/api/agents/usage?account_id=a%2Fb')
    expect(calls[2][0]).toBe('/api/agents/usage/refresh')
    expect(calls[2][1]).toMatchObject({ method: 'POST' })
    expect(JSON.parse(String(calls[2][1]?.body))).toEqual({ account_id: 'acct 1' })
  })

  it('reports API errors by message and old servers by an update hint', async () => {
    vi.mocked(localFetch).mockResolvedValueOnce(json({ error: { message: 'Cannot read profiles' } }, { status: 503 }))
    await expect(agentUsage()).rejects.toThrow('Cannot read profiles')
    vi.mocked(localFetch).mockResolvedValueOnce(json({}, { status: 404 }))
    await expect(agentUsage()).rejects.toThrow('Update Bonsai')
  })

  it('builds the add command with the provider hint', () => {
    expect(accountAddCommand('claude')).toBe('bonsai agent account add claude <name>')
    expect(accountAddCommand('codex')).toBe('bonsai agent account add codex <name> [--auth device]')
  })

  it('counts starting, running and stopping sessions as active', () => {
    expect(['starting', 'running', 'stopping', 'exited', 'failed', undefined].map(state => isActiveSession(state as never))).toEqual([true, true, true, false, false, false])
  })
})
