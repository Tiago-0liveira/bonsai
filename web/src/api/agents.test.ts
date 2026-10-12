import { describe, expect, it, vi } from 'vitest'
import { isLiveAgent, mapAgent, providerIdFor, startAgent, type AgentSummary } from './agents'
import { localFetch } from './localClient'

vi.mock('./localClient', () => ({ localFetch: vi.fn() }))

const summary = (provider: AgentSummary['provider']): AgentSummary => ({ id: 's1', project_id: 'p', worktree_id: 'w', account_id: 'a', provider, profile_name: 'Work', name: '', state: 'running', created_at: 'now' })

describe('agent provider mapping', () => {
  it('keeps the raw id for a provider it has no label for', () => {
    expect(mapAgent(summary('future' as never))).toMatchObject({ provider: 'future', providerId: 'future' })
  })

  it.each([['antigravity', 'Antigravity'], ['claude', 'Claude'], ['codex', 'Codex']] as const)('labels %s sessions as %s', (id, label) => {
    expect(mapAgent(summary(id))).toMatchObject({ provider: label, providerId: id, profileName: 'Work', name: 'Work', terminalId: 's1' })
  })

  it('keeps the profile and exit code of a session', () => {
    expect(mapAgent({ ...summary('codex'), account_id: 'acct-1', state: 'exited', exit_code: 3 })).toMatchObject({ accountId: 'acct-1', exitCode: 3, lifecycleState: 'exited' })
  })

  it('treats every backend provider as live and mock agents as not', () => {
    expect(isLiveAgent({ providerId: 'claude' })).toBe(true)
    expect(isLiveAgent({ providerId: 'antigravity' })).toBe(true)
    expect(isLiveAgent({ providerId: 'codex' })).toBe(true)
    expect(isLiveAgent({})).toBe(false)
    expect(isLiveAgent(undefined)).toBe(false)
    expect(providerIdFor('Claude')).toBe('claude')
    expect(providerIdFor('Codex')).toBe('codex')
    expect(providerIdFor('Gemini')).toBeUndefined()
    expect(isLiveAgent({ providerId: 'constructor' as never })).toBe(false)
    expect(isLiveAgent({ providerId: 'toString' as never })).toBe(false)
  })
})

describe('startAgent', () => {
  it('flattens launch options into the body without overriding the fixed fields', async () => {
    vi.mocked(localFetch).mockResolvedValue({ ok: true, json: async () => summary('claude') } as Response)
    await startAgent('p', { worktree_id: 'w', account_id: 'a', model: 'opus', options: { sandbox: 'read-only', approval: 'never', full_access: false, account_id: 'evil' }, cols: 80, rows: 24 }, 'key')
    const [path, init] = vi.mocked(localFetch).mock.calls[0]
    expect(path).toBe('/api/projects/p/agents')
    expect(JSON.parse(String(init?.body))).toEqual({ worktree_id: 'w', account_id: 'a', model: 'opus', sandbox: 'read-only', approval: 'never', full_access: false, cols: 80, rows: 24 })
    expect(init?.headers).toMatchObject({ 'Idempotency-Key': 'key' })
  })
})
