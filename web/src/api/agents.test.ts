import { describe, expect, it, vi } from 'vitest'
import { isLiveAgent, mapAgent, providerIdFor, startAgent, type AgentSummary, type LaunchField } from './agents'
import { cachedAgentProviders, launchDetails } from './launchDetails'
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

  it('keeps the launch model and options of a session, and leaves them empty when none were recorded', () => {
    expect(mapAgent({ ...summary('codex'), model: 'gpt-6.1-sol', options: { effort: 'high', sandbox: 'read-only', approval: 'on-request' } }))
      .toMatchObject({ model: 'gpt-6.1-sol', reasoningEffort: 'high', launchOptions: { effort: 'high', sandbox: 'read-only', approval: 'on-request' } })
    const plain = mapAgent(summary('claude'))
    expect(plain).toMatchObject({ model: '', reasoningEffort: '' })
    expect(plain.launchOptions).toBeUndefined()
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

// Mirror what the Go providers declare.
const CODEX_FIELDS: LaunchField[] = [
  { id: 'permissions', label: 'Permissions', kind: 'select', default: 'auto', options: [
    { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
    { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
    { value: 'full-access', label: 'Full access', tone: 'danger', set: { sandbox: 'danger-full-access', approval: 'never' } },
    { value: 'bypass', label: 'Bypass', tone: 'danger', set: { sandbox: 'bypass' } },
  ] },
  { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: ['', 'low', 'high'].map(value => ({ value, label: value || 'Profile default' })) },
]
const CLAUDE_FIELDS: LaunchField[] = [
  { id: 'permission_mode', label: 'Permission mode', kind: 'select', options: [{ value: '', label: 'Profile default' }, { value: 'acceptEdits', label: 'Accept edits' }, { value: 'bypassPermissions', label: 'Bypass permissions', tone: 'danger' }] },
  { id: 'effort', label: 'Effort', kind: 'select', options: ['', 'high'].map(value => ({ value, label: value || 'Profile default' })) },
]
const ANTIGRAVITY_FIELDS: LaunchField[] = [{ id: 'full_access', label: 'Full access', kind: 'toggle', options: [{ value: 'false', label: 'Ask permissions' }, { value: 'true', label: 'Full access', tone: 'danger' }] }]

describe('launchDetails', () => {
  it('labels a preset by the option whose fields all match, and a bare value with its field', () => {
    expect(launchDetails(CODEX_FIELDS, 'gpt-6.1-sol', { effort: 'high', sandbox: 'read-only', approval: 'on-request' })).toEqual(['gpt-6.1-sol', 'Read only', 'high effort'])
    expect(launchDetails(CODEX_FIELDS, '', { sandbox: 'bypass' })).toEqual(['Bypass'])
    // A per-model effort the field does not list still reads as an effort.
    expect(launchDetails(CODEX_FIELDS, '', { effort: 'ultra' })).toEqual(['ultra effort'])
  })

  it('labels selects and toggles of other providers', () => {
    expect(launchDetails(CLAUDE_FIELDS, 'opus', { permission_mode: 'bypassPermissions', effort: 'high' })).toEqual(['opus', 'Bypass permissions', 'high effort'])
    expect(launchDetails(ANTIGRAVITY_FIELDS, '', { full_access: 'true' })).toEqual(['Full access'])
    expect(launchDetails(ANTIGRAVITY_FIELDS, 'gemini', { full_access: 'false' })).toEqual(['gemini', 'Ask permissions'])
  })

  it('shows values no field declares as sent, and nothing for a launch without choices', () => {
    expect(launchDetails([], 'opus', { effort: 'high' })).toEqual(['opus', 'high'])
    expect(launchDetails(CODEX_FIELDS, '', { sandbox: 'custom', approval: 'never' })).toEqual(['custom', 'never'])
    expect(launchDetails(CODEX_FIELDS)).toEqual([])
  })
})

describe('cachedAgentProviders', () => {
  it('fetches the provider list once per page and again after a failure', async () => {
    vi.mocked(localFetch).mockReset()
    vi.mocked(localFetch).mockRejectedValueOnce(new Error('offline'))
    await expect(cachedAgentProviders()).rejects.toThrow('offline')
    vi.mocked(localFetch).mockResolvedValue({ ok: true, json: async () => [{ id: 'codex', label: 'Codex', available: true }] } as Response)
    await expect(cachedAgentProviders()).resolves.toEqual([{ id: 'codex', label: 'Codex', available: true }])
    await cachedAgentProviders()
    expect(vi.mocked(localFetch).mock.calls.map(([path]) => path)).toEqual(['/api/agents/providers', '/api/agents/providers'])
  })
})
