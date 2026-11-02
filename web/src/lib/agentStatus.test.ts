import { describe, expect, it } from 'vitest'
import type { Agent, AgentActivity } from '../types'
import { agentDisplayStatus, countNeedsInput, type AgentStatusSource } from './agentStatus'

const activity = (patch: Partial<AgentActivity> = {}): AgentActivity => ({ state: 'working', since: '2099-01-01T00:00:00Z', source: 'hook', ...patch })
const agent = (lifecycleState: Agent['lifecycleState'], act?: AgentActivity): AgentStatusSource => ({ state: lifecycleState === 'exited' || lifecycleState === 'failed' ? 'finished' : 'running', lifecycleState, activity: act })

describe('agentDisplayStatus', () => {
  it.each([
    ['starting', 'Starting', 'muted', true],
    ['stopping', 'Stopping', 'muted', true],
    ['exited', 'Exited', 'muted', false],
    ['failed', 'Failed', 'danger', false],
  ] as const)('lets the %s lifecycle win over any activity', (lifecycle, label, tone, pulse) => {
    for (const act of [undefined, activity({ state: 'idle' }), activity({ state: 'waiting', reason: 'permission' }), activity({ stalled: true })]) {
      expect(agentDisplayStatus(agent(lifecycle, act))).toMatchObject({ key: lifecycle, label, tone, pulse, inferred: false })
    }
  })

  it.each([
    ['no activity (old holder)', undefined, 'running', 'Running'],
    ['unknown', activity({ state: 'unknown', source: 'lifecycle' }), 'running', 'Running'],
    ['working', activity(), 'working', 'Working'],
    ['stalled', activity({ stalled: true }), 'stalled', 'Stalled'],
    ['idle', activity({ state: 'idle', reason: 'turn_complete' }), 'idle', 'Idle'],
    ['waiting without a reason', activity({ state: 'waiting' }), 'waiting', 'Needs input'],
    ['waiting for permission', activity({ state: 'waiting', reason: 'permission' }), 'waiting', 'Needs input (permission)'],
    ['waiting for a plan', activity({ state: 'waiting', reason: 'plan' }), 'waiting', 'Needs input (plan)'],
  ] as const)('shows %s of a running session as %s', (_name, act, key, label) => {
    expect(agentDisplayStatus(agent('running', act))).toMatchObject({ key, label })
  })

  it('treats a running session whose activity is not reported as plain Running, never an error', () => {
    const status = agentDisplayStatus(agent('running'))
    expect(status).toMatchObject({ key: 'running', tone: 'accent', pulse: false, inferred: false })
  })

  it('does not call an idle session stopped: it is still running underneath', () => {
    const idle = { state: 'running' as const, lifecycleState: 'running' as const, activity: activity({ state: 'idle' }) }
    expect(agentDisplayStatus(idle).key).toBe('idle')
    expect(idle.state).toBe('running')
  })

  it('pulses working and waiting, and flags waiting as the warning tone', () => {
    expect(agentDisplayStatus(agent('running', activity()))).toMatchObject({ tone: 'accent', pulse: true })
    expect(agentDisplayStatus(agent('running', activity({ state: 'waiting' })))).toMatchObject({ tone: 'warn', pulse: true })
    expect(agentDisplayStatus(agent('running', activity({ stalled: true })))).toMatchObject({ tone: 'warn', pulse: false })
  })

  it('describes what a waiting session needs, with the tool name when there is one', () => {
    expect(agentDisplayStatus(agent('running', activity({ state: 'waiting', reason: 'permission', detail: 'Bash' }))).description).toBe('Needs permission: Bash')
    expect(agentDisplayStatus(agent('running', activity({ state: 'waiting', reason: 'question' }))).description).toBe('Needs an answer')
    expect(agentDisplayStatus(agent('running', activity({ state: 'waiting', reason: 'plan' }))).description).toBe('Needs plan approval')
    expect(agentDisplayStatus(agent('running', activity({ state: 'waiting', reason: 'elicitation' }))).description).toBe('Needs input')
    expect(agentDisplayStatus(agent('running', activity({ detail: 'Edit' }))).description).toBe('Working: Edit')
  })

  it('flags an inferred status in its description, never by tone alone', () => {
    const status = agentDisplayStatus(agent('running', activity({ state: 'waiting', reason: 'permission', inferred: true })))
    expect(status).toMatchObject({ key: 'waiting', label: 'Needs input (permission)', inferred: true, description: 'Needs permission (inferred)' })
  })

  it('marks a state inferred when the provider cannot report it natively', () => {
    const waiting = agent('running', activity({ state: 'waiting' }))
    expect(agentDisplayStatus(waiting, { native: ['working', 'idle'], inferred: ['waiting'] }).inferred).toBe(true)
    expect(agentDisplayStatus(waiting, { native: ['working', 'waiting', 'idle'], inferred: [] }).inferred).toBe(false)
    expect(agentDisplayStatus(waiting).inferred).toBe(false)
  })

  it('falls back to the coarse state of an agent that has no lifecycle (simulated agents)', () => {
    expect(agentDisplayStatus({ state: 'finished' }).key).toBe('exited')
    expect(agentDisplayStatus({ state: 'running' }).key).toBe('running')
    expect(agentDisplayStatus({ state: 'idle' }).key).toBe('idle')
  })
})

describe('countNeedsInput', () => {
  const live = (id: string, worktreeId: string, lifecycleState: Agent['lifecycleState'], act?: AgentActivity) => ({ id, worktreeId, state: 'running', lifecycleState, activity: act }) as Agent
  const agents = [
    live('a', 'w1', 'running', activity({ state: 'waiting' })),
    live('b', 'w1', 'running', activity()),
    live('c', 'w2', 'running', activity({ state: 'waiting' })),
    live('d', 'w2', 'exited', activity({ state: 'waiting' })),
    live('e', 'w2', 'running'),
  ]

  it('counts running sessions that wait, optionally per worktree', () => {
    expect(countNeedsInput(agents)).toBe(2)
    expect(countNeedsInput(agents, 'w1')).toBe(1)
    expect(countNeedsInput(agents, 'none')).toBe(0)
  })
})
