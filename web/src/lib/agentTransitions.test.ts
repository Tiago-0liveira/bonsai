import { describe, expect, it } from 'vitest'
import type { ActivityReason, ActivityState, Agent } from '../types'
import { detectAgentAlerts, seenActivity, type SeenActivity } from './agentTransitions'

const agent = (id: string, state?: ActivityState, patch: Partial<Agent> = {}) => ({
  id, name: `session ${id}`, worktreeId: 'w1', state: 'running', lifecycleState: 'running',
  activity: state ? { state, since: '2099-01-01T00:00:00Z', source: 'hook' } : undefined, ...patch,
}) as Agent
const kinds = (previous: SeenActivity | undefined, agents: Agent[]) => detectAgentAlerts(previous, agents).alerts.map(alert => alert.kind)

describe('detectAgentAlerts', () => {
  it.each([
    ['unknown', 'waiting', ['needs_input']],
    ['working', 'waiting', ['needs_input']],
    ['idle', 'waiting', ['needs_input']],
    ['working', 'idle', ['turn_complete']],
    ['idle', 'working', []],
    ['waiting', 'working', []],
    ['unknown', 'idle', []],
    ['waiting', 'idle', []],
    ['working', 'working', []],
    ['waiting', 'waiting', []],
    ['idle', 'idle', []],
    ['working', 'unknown', []],
  ] as const)('%s -> %s raises %j', (before, now, expected) => {
    const previous = seenActivity([agent('a', before)])
    expect(kinds(previous, [agent('a', now)])).toEqual(expected)
  })

  describe('waiting -> idle coalesced by the debounced snapshot', () => {
    const idleWith = (reason?: ActivityReason) => agent('a', 'idle', { activity: { state: 'idle', reason, since: '2099-01-01T00:00:00Z', source: 'hook' } })
    it.each([
      ['turn_complete', ['turn_complete']],
      ['error', ['turn_complete']],
      ['ready', []],
      ['interrupted', []],
      [undefined, []],
    ] as const)('reason %s raises %j', (reason, expected) => {
      expect(kinds(seenActivity([agent('a', 'waiting')]), [idleWith(reason)])).toEqual(expected)
    })

    it('keeps the dedupe identity of the finishing state', () => {
      const { alerts } = detectAgentAlerts(seenActivity([agent('a', 'waiting')]), [idleWith('turn_complete')])
      expect(alerts).toEqual([{ agentId: 'a', worktreeId: 'w1', name: 'session a', kind: 'turn_complete', since: '2099-01-01T00:00:00Z' }])
    })
  })

  it('stays quiet on the first snapshot, whatever the states', () => {
    const agents = [agent('a', 'waiting'), agent('b', 'idle'), agent('c', 'working')]
    const { alerts, seen } = detectAgentAlerts(undefined, agents)
    expect(alerts).toEqual([])
    expect(seen.get('a')).toBe('waiting')
  })

  it('does not repeat for a repeated snapshot', () => {
    const first = detectAgentAlerts(seenActivity([agent('a', 'working')]), [agent('a', 'waiting')])
    expect(first.alerts).toHaveLength(1)
    expect(detectAgentAlerts(first.seen, [agent('a', 'waiting')]).alerts).toEqual([])
  })

  it('alerts again when the agent leaves and re-enters waiting', () => {
    let seen = seenActivity([agent('a', 'working')])
    const states: ActivityState[] = ['waiting', 'working', 'waiting']
    const raised = states.map(state => {
      const result = detectAgentAlerts(seen, [agent('a', state)])
      seen = result.seen
      return result.alerts.length
    })
    expect(raised).toEqual([1, 0, 1])
  })

  it('does not alert for an agent first seen after the baseline', () => {
    expect(kinds(seenActivity([agent('a', 'working')]), [agent('a', 'working'), agent('b', 'waiting')])).toEqual([])
  })

  it('treats a session that is not running as unknown', () => {
    const previous = seenActivity([agent('a', 'working')])
    expect(kinds(previous, [agent('a', 'idle', { lifecycleState: 'exited' })])).toEqual([])
    expect(kinds(seenActivity([agent('a', 'idle', { lifecycleState: 'stopping' })]), [agent('a', 'waiting')])).toEqual(['needs_input'])
  })

  it('carries only the name, worktree and state start of the alert', () => {
    const { alerts } = detectAgentAlerts(seenActivity([agent('a', 'working')]), [agent('a', 'waiting', { prompt: 'secret prompt', task: 'rm -rf' })])
    expect(alerts).toEqual([{ agentId: 'a', worktreeId: 'w1', name: 'session a', kind: 'needs_input', since: '2099-01-01T00:00:00Z' }])
  })
})
