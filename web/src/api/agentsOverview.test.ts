import { describe, expect, it, vi } from 'vitest'
import { accountAddCommand, agentAccountsIfChanged, agentUsage, byHeadroom, headroomLabel, isActiveSession, refreshAgentUsage } from './agentsOverview'
import type { AgentUsage } from './agents'
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

describe('headroom', () => {
  it('labels the headline pair by window, else by label', () => {
    expect(headroomLabel({ primary: { id: 'five_hour', window: '5h', remaining_fraction: 0.724 }, secondary: { id: 'weekly', window: 'weekly', remaining_fraction: 0.4 } })).toBe('5h 72% · week 40%')
    expect(headroomLabel({ primary: { id: 'five_hour', label: 'Session (5h)', remaining_fraction: 1.2 }, secondary: { id: 'weekly' } })).toBe('Session (5h) 100%')
    expect(headroomLabel({ secondary: { id: 'window_1440m', remaining_fraction: 0 } })).toBe('window_1440m 0%')
    expect(headroomLabel(undefined)).toBe('')
  })

  it('orders profiles by score, highest first, keeping unknown ones in order after them', () => {
    const entry = (id: string, score?: number): AgentUsage => ({ account_id: id, provider: 'codex', headline: score === undefined ? undefined : { score } })
    const usage = { b: entry('b', 0.3), d: entry('d', 0.8), e: entry('e'), f: entry('f', 0.3) }
    const ordered = byHeadroom(['a', 'b', 'c', 'd', 'e', 'f'].map(id => ({ id })), usage)
    expect(ordered.map(item => item.id)).toEqual(['d', 'b', 'f', 'a', 'c', 'e'])
    expect(byHeadroom([{ id: 'x' }, { id: 'y' }], {}).map(item => item.id)).toEqual(['x', 'y'])
  })
})
