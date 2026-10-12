import { describe, expect, it } from 'vitest'
import type { AgentAccount, AgentCapability, AgentUsage, LaunchField } from '../../api/agents'
import type { Agent, Project, Worktree } from '../../types'
import { activeSessionCounts, authState, formatAge, formatCountdown, groupExtraLimits, groupProfiles, limitName, profileDefaults, profileSessions, summarizeFleet, toPercent } from './agentsModel'
import { validateAgentsSearch } from './agentsSearch'

const NOW = Date.parse('2026-10-12T12:00:00Z')
const account = (id: string, name: string, provider: AgentAccount['provider'] = 'codex', rest: Partial<AgentAccount> = {}): AgentAccount => ({ id, name, provider, ...rest })
const capability = (id: string, label: string, available = true): AgentCapability => ({ id, label, available })

describe('groupProfiles', () => {
  it('lists Claude, Codex and Antigravity first, sorts profiles by name and adds providers only profiles know', () => {
    const groups = groupProfiles(
      [capability('antigravity', 'Antigravity', false), capability('codex', 'Codex'), capability('claude', 'Claude')],
      [account('c2', 'team'), account('c1', 'Personal'), account('x1', 'Solo', 'future' as never), account('k1', 'Work', 'claude')],
    )
    expect(groups.map(group => [group.id, group.label, group.accounts.map(item => item.name)])).toEqual([
      ['claude', 'Claude', ['Work']],
      ['codex', 'Codex', ['Personal', 'team']],
      ['antigravity', 'Antigravity', []],
      ['future', 'future', ['Solo']],
    ])
    expect(groups[2].capability?.available).toBe(false)
    expect(groups[3].capability).toBeUndefined()
  })

  it('puts other listed providers after the three, in API order', () => {
    const groups = groupProfiles([capability('zed', 'Zed'), capability('antigravity', 'Antigravity'), capability('aider', 'Aider'), capability('claude', 'Claude')], [])
    expect(groups.map(group => group.id)).toEqual(['claude', 'antigravity', 'zed', 'aider'])
  })

  it('breaks name ties by id so the order is stable', () => {
    const groups = groupProfiles([capability('codex', 'Codex')], [account('b', 'Same'), account('a', 'Same')])
    expect(groups[0].accounts.map(item => item.id)).toEqual(['a', 'b'])
  })
})

describe('authState', () => {
  it.each([
    ['logged_in', 'ok', 'Signed in'],
    ['token', 'ok', 'Token'],
    ['logged_out', 'bad', 'Signed out'],
    ['expired', 'bad', 'Token expired'],
    ['missing', 'bad', 'Token missing'],
    ['unknown', 'unknown', 'Status unknown'],
    ['revoked_by_admin', 'unknown', 'Revoked by admin'],
    [undefined, 'none', 'Not checked'],
  ] as const)('maps %s', (status, tone, label) => {
    expect(authState({ auth_status: status })).toEqual({ tone, label })
  })

  it('does not read inherited object keys as statuses', () => {
    expect(authState({ auth_status: 'constructor' }).tone).toBe('unknown')
  })
})

describe('sessions', () => {
  const projects = [{ id: 'one', name: 'First' }, { id: 'two', name: 'Second' }] as Project[]
  const worktrees = [{ id: 'w1', projectId: 'one', branch: 'main' }, { id: 'w2', projectId: 'two', branch: 'feat/x' }] as Worktree[]
  const agent = (id: string, rest: Partial<Agent>): Agent => ({ id, name: id, provider: 'Codex', providerId: 'codex', accountId: 'acct', model: '', reasoningEffort: '', workType: '', prompt: '', archived: false, presentation: 'canvas', state: 'running', task: 'running', runtime: '', terminalId: id, createdAt: '2026-10-12T10:00:00Z', lifecycleState: 'running', worktreeId: 'w1', projectId: 'one', ...rest })

  it('lists one profile across projects, running first, then newest', () => {
    const agents = [
      agent('old-done', { lifecycleState: 'exited', createdAt: '2026-10-12T08:00:00Z' }),
      agent('new-done', { lifecycleState: 'failed', createdAt: '2026-10-12T09:00:00Z', worktreeId: 'w2', projectId: 'two' }),
      agent('running', { createdAt: '2026-10-12T07:00:00Z', worktreeId: 'w2', projectId: 'two' }),
      agent('other-profile', { accountId: 'else' }),
      agent('simulated', { providerId: undefined }),
    ]
    const sessions = profileSessions(agents, 'acct', projects, worktrees)
    expect(sessions.map(item => item.agent.id)).toEqual(['running', 'new-done', 'old-done'])
    expect(sessions[0]).toMatchObject({ project: 'Second', branch: 'feat/x', active: true })
    expect(sessions[2]).toMatchObject({ project: 'First', branch: 'main', active: false })
  })

  it('falls back to the project id while the project is not loaded', () => {
    const [session] = profileSessions([agent('a', { projectId: 'gone', worktreeId: 'missing' })], 'acct', projects, worktrees)
    expect(session).toMatchObject({ project: 'gone', branch: '' })
  })

  it('counts only active live sessions per profile', () => {
    const counts = activeSessionCounts([
      agent('a', {}), agent('b', { lifecycleState: 'starting' }), agent('c', { lifecycleState: 'stopping' }),
      agent('d', { lifecycleState: 'exited' }), agent('e', { accountId: 'else' }), agent('f', { providerId: undefined }), agent('g', { accountId: undefined }),
    ])
    expect(Object.fromEntries(counts)).toEqual({ acct: 3, else: 1 })
  })
})

describe('time formatting', () => {
  it('shows the reset countdown in minutes, hours and days', () => {
    const at = (ms: number) => new Date(NOW + ms).toISOString()
    expect(formatCountdown(at(30_000), NOW)).toBe('in <1m')
    expect(formatCountdown(at(45 * 60_000), NOW)).toBe('in 45m')
    expect(formatCountdown(at(135 * 60_000), NOW)).toBe('in 2h 15m')
    expect(formatCountdown(at((3 * 24 + 4) * 3_600_000 + 60_000), NOW)).toBe('in 3d 4h')
    expect(formatCountdown(at(-1000), NOW)).toBe('due')
    expect(formatCountdown(undefined, NOW)).toBe('')
    expect(formatCountdown('soon', NOW)).toBe('')
  })

  it('shows ages from just now to days', () => {
    const ago = (ms: number) => new Date(NOW - ms).toISOString()
    expect(formatAge(ago(3000), NOW)).toBe('just now')
    expect(formatAge(ago(42_000), NOW)).toBe('42s ago')
    expect(formatAge(ago(5 * 60_000), NOW)).toBe('5m ago')
    expect(formatAge(ago(3 * 3_600_000), NOW)).toBe('3h ago')
    expect(formatAge(ago(2 * 86_400_000), NOW)).toBe('2d ago')
    expect(formatAge('2h ago', NOW)).toBe('')
  })

  it('rounds and clamps percentages', () => {
    expect([toPercent(0.724), toPercent(1.4), toPercent(-0.2)]).toEqual([72, 100, 0])
  })
})

describe('summarizeFleet', () => {
  const usage = (id: string, score: number | undefined, low: boolean, reset?: string): AgentUsage => ({ account_id: id, provider: 'codex', headline: { score, low, next_reset: reset } })
  it('counts signed-in and low profiles and picks the lowest score and soonest future reset', () => {
    const accounts = [account('a', 'A', 'codex', { auth_status: 'logged_in' }), account('b', 'B', 'codex', { auth_status: 'logged_out' }), account('c', 'C', 'claude', { auth_status: 'token' }), account('d', 'D')]
    const summary = summarizeFleet(accounts, {
      a: usage('a', 0.72, false, new Date(NOW + 3_600_000).toISOString()),
      b: usage('b', 0.1, true, new Date(NOW - 60_000).toISOString()),
      c: usage('c', 0.4, false, new Date(NOW + 600_000).toISOString()),
    }, 2, NOW)
    expect(summary).toMatchObject({ profiles: 4, signedIn: 2, low: 1, running: 2, lowest: { accountId: 'b', score: 0.1 }, nextReset: { accountId: 'c' } })
  })

  it('has no lowest or reset without usage', () => {
    const summary = summarizeFleet([account('a', 'A')], {}, 0, NOW)
    expect(summary.lowest).toBeUndefined()
    expect(summary.nextReset).toBeUndefined()
  })
})

describe('limits', () => {
  it('names a limit by its label, else by its id as words', () => {
    expect(limitName({ id: 'five_hour', label: '5 hour' })).toBe('5 hour')
    expect(limitName({ id: 'seven_day_opus' })).toBe('Seven day opus')
  })

  it('groups extra limits in provider order with ungrouped limits first', () => {
    const groups = groupExtraLimits([
      { id: 'a', group: 'Codex Spark' }, { id: 'b' }, { id: 'c', group: 'Codex Spark' }, { id: 'd', group: 'Other' },
    ])
    expect(groups.map(item => [item.group, item.limits.map(limit => limit.id)])).toEqual([['', ['b']], ['Codex Spark', ['a', 'c']], ['Other', ['d']]])
    expect(groupExtraLimits(undefined)).toEqual([])
  })
})

describe('profileDefaults', () => {
  const fields: LaunchField[] = [
    { id: 'permissions', label: 'Permissions', kind: 'select', options: [
      { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
      { value: 'bypass', label: 'Bypass', tone: 'danger', set: { sandbox: 'bypass' } },
    ] },
    { id: 'effort', label: 'Effort', kind: 'select', options: [{ value: '', label: 'Profile default' }, { value: 'high', label: 'high' }] },
    { id: 'full_access', label: 'Full access', kind: 'toggle', options: [{ value: 'false', label: 'Ask permissions' }, { value: 'true', label: 'Full access', tone: 'danger' }] },
  ]
  it('labels the saved fields with the provider launch fields, resolving presets', () => {
    expect(profileDefaults(fields, account('a', 'A', 'codex', { model: 'gpt-5.5', sandbox: 'workspace-write', approval: 'on-request', effort: 'high', full_access: true })))
      .toEqual([{ label: 'Model', value: 'gpt-5.5' }, { label: 'Permissions', value: 'Auto' }, { label: 'Effort', value: 'high' }, { label: 'Full access', value: 'Full access' }])
  })

  it('shows nothing for a profile without saved defaults', () => {
    expect(profileDefaults(fields, account('a', 'A'))).toEqual([])
  })
})

describe('validateAgentsSearch', () => {
  it('keeps a profile and a known tab and drops everything else', () => {
    expect(validateAgentsSearch({ profile: 'codex-1', tab: 'usage' })).toEqual({ profile: 'codex-1', tab: 'usage' })
    expect(validateAgentsSearch({ profile: '', tab: 'bogus' })).toEqual({ profile: undefined, tab: undefined })
    expect(validateAgentsSearch({ profile: 7, tab: ['usage'] })).toEqual({ profile: undefined, tab: undefined })
  })
})
