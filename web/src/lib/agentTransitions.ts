import type { ActivityReason, ActivityState, Agent } from '../types'

export type AgentAlertKind = 'needs_input' | 'turn_complete'
export interface AgentAlert {
  agentId: string
  worktreeId: string
  name: string
  kind: AgentAlertKind
  /** Start of the state that caused the alert; with the agent and kind it identifies one alert. */
  since: string
}
/** Last observed activity of each live agent. */
export type SeenActivity = ReadonlyMap<string, ActivityState>

/** Only a running session reports a meaningful activity; anything else counts as `unknown`. */
function currentState(agent: Agent): ActivityState {
  return agent.lifecycleState === 'running' && agent.activity ? agent.activity.state : 'unknown'
}

function alertKind(before: ActivityState, now: ActivityState | undefined, reason: ActivityReason | undefined): AgentAlertKind | undefined {
  if (now === 'waiting') return 'needs_input'
  if (now !== 'idle') return undefined
  if (before === 'working') return 'turn_complete'
  return before === 'waiting' && (reason === 'turn_complete' || reason === 'error') ? 'turn_complete' : undefined
}

export function seenActivity(agents: readonly Agent[]): SeenActivity {
  return new Map(agents.map(agent => [agent.id, currentState(agent)]))
}

/**
 * Compares a snapshot with what was seen before and returns the alerts to raise plus the new baseline.
 * Only observed transitions alert: with no baseline (bootstrap) or for an agent first seen now, the current
 * state is just recorded. Waiting alerts from any other state; Idle alerts only after Working (a turn ended),
 * so an agent that starts idle stays quiet. Snapshots are debounced, so a turn that ends right after a prompt is
 * answered shows up as Waiting -> Idle: that alerts too, but only when the idle reason says the turn ended
 * (`ready` and `interrupted` are not completions).
 */
export function detectAgentAlerts(previous: SeenActivity | undefined, agents: readonly Agent[]): { alerts: AgentAlert[]; seen: SeenActivity } {
  const seen = seenActivity(agents)
  const alerts: AgentAlert[] = []
  if (!previous) return { alerts, seen }
  for (const agent of agents) {
    const before = previous.get(agent.id)
    const now = seen.get(agent.id)
    if (before === undefined || before === now) continue
    const kind = alertKind(before, now, agent.activity?.reason)
    if (kind) alerts.push({ agentId: agent.id, worktreeId: agent.worktreeId, name: agent.name, kind, since: agent.activity?.since ?? '' })
  }
  return { alerts, seen }
}
