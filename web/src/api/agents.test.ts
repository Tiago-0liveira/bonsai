import { describe, expect, it } from 'vitest'
import { isLiveAgent, mapAgent, providerIdFor, type AgentSummary } from './agents'

const summary = (provider: AgentSummary['provider']): AgentSummary => ({ id: 's1', project_id: 'p', worktree_id: 'w', account_id: 'a', provider, profile_name: 'Work', name: '', state: 'running', created_at: 'now' })

describe('agent provider mapping', () => {
  it('keeps the raw id for a provider it has no label for', () => {
    expect(mapAgent(summary('future' as never))).toMatchObject({ provider: 'future', providerId: 'future' })
  })

  it.each([['antigravity', 'Antigravity'], ['claude', 'Claude']] as const)('labels %s sessions as %s', (id, label) => {
    expect(mapAgent(summary(id))).toMatchObject({ provider: label, providerId: id, profileName: 'Work', name: 'Work', terminalId: 's1' })
  })

  it('treats every backend provider as live and mock agents as not', () => {
    expect(isLiveAgent({ providerId: 'claude' })).toBe(true)
    expect(isLiveAgent({ providerId: 'antigravity' })).toBe(true)
    expect(isLiveAgent({})).toBe(false)
    expect(isLiveAgent(undefined)).toBe(false)
    expect(providerIdFor('Claude')).toBe('claude')
    expect(providerIdFor('Codex')).toBeUndefined()
    expect(isLiveAgent({ providerId: 'constructor' as never })).toBe(false)
    expect(isLiveAgent({ providerId: 'toString' as never })).toBe(false)
  })
})
