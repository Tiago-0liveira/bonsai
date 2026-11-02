import type { ProviderActivity } from '../api/agents'
import type { Agent, AgentActivity } from '../types'

export type AgentStatusKey = 'starting' | 'stopping' | 'exited' | 'failed' | 'running' | 'working' | 'stalled' | 'waiting' | 'idle'
export type AgentStatusTone = 'accent' | 'warn' | 'danger' | 'muted'

export interface AgentDisplayStatus {
  key: AgentStatusKey
  /** Short text; mirrors `Display` in internal/core/agentactivity/display.go so the TUI and the web agree. */
  label: string
  /** Label plus the tool or reason, for tooltips and screen readers. */
  description: string
  tone: AgentStatusTone
  pulse: boolean
  /** Bonsai guessed this from the terminal; the provider did not report it. */
  inferred: boolean
}

/** The fields the status reads: a mapped agent, or a `status` frame of its terminal socket. */
export type AgentStatusSource = Pick<Agent, 'state' | 'lifecycleState' | 'activity'>

const WAITING_REASON: Partial<Record<NonNullable<AgentActivity['reason']>, string>> = {
  permission: 'Needs permission',
  question: 'Needs an answer',
  plan: 'Needs plan approval',
}

const LIFECYCLE: Record<'starting' | 'stopping' | 'exited' | 'failed', { label: string; tone: AgentStatusTone; pulse: boolean }> = {
  starting: { label: 'Starting', tone: 'muted', pulse: true },
  stopping: { label: 'Stopping', tone: 'muted', pulse: true },
  exited: { label: 'Exited', tone: 'muted', pulse: false },
  failed: { label: 'Failed', tone: 'danger', pulse: false },
}

function build(key: AgentStatusKey, label: string, tone: AgentStatusTone, pulse: boolean, inferred = false, description = label): AgentDisplayStatus {
  return { key, label, description: inferred ? description + ' (inferred)' : description, tone, pulse, inferred }
}

/**
 * The one status every surface shows. The lifecycle (starting, stopping, exited, failed) wins over
 * activity: a dead process is never "Idle". Unknown or missing activity is plain "Running", never an error,
 * because hooks may be disabled or unsupported. This is display only: whether an agent is alive stays
 * `agent.state === 'running'`, whatever the status says.
 *
 * `providerActivity` (from GET /api/agents/providers) marks a state as inferred when the provider cannot
 * report it, even for an older holder that did not set the flag.
 */
export function agentDisplayStatus(agent: AgentStatusSource, providerActivity?: ProviderActivity): AgentDisplayStatus {
  // Mock agents carry no lifecycle: their coarse state stands in for it.
  const lifecycle = agent.lifecycleState ?? (agent.state === 'finished' ? 'exited' : 'running')
  if (lifecycle !== 'running') {
    const { label, tone, pulse } = LIFECYCLE[lifecycle]
    return build(lifecycle, label, tone, pulse)
  }
  const activity = agent.activity
  if (!activity) {
    return agent.state === 'idle' ? build('idle', 'Idle', 'muted', false) : build('running', 'Running', 'accent', false)
  }
  const inferred = Boolean(activity.inferred) || Boolean(providerActivity && !providerActivity.native.includes(activity.state) && providerActivity.inferred.includes(activity.state))
  switch (activity.state) {
    case 'working':
      return activity.stalled
        ? build('stalled', 'Stalled', 'warn', false, inferred, 'Stalled: no progress for a while')
        : build('working', 'Working', 'accent', true, inferred, activity.detail ? `Working: ${activity.detail}` : 'Working')
    case 'waiting': {
      const label = activity.reason ? `Needs input (${activity.reason})` : 'Needs input'
      const what = (activity.reason && WAITING_REASON[activity.reason]) || 'Needs input'
      return build('waiting', label, 'warn', true, inferred, activity.detail ? `${what}: ${activity.detail}` : what)
    }
    case 'idle':
      return build('idle', 'Idle', 'muted', false, inferred)
    default:
      return build('running', 'Running', 'accent', false)
  }
}

/** Agents that are alive and blocked on the user; optionally only those of one worktree. */
export function countNeedsInput(agents: readonly Agent[], worktreeId?: string): number {
  let count = 0
  for (const agent of agents) {
    if (worktreeId !== undefined && agent.worktreeId !== worktreeId) continue
    if (agent.lifecycleState === 'running' && agent.activity?.state === 'waiting') count++
  }
  return count
}
