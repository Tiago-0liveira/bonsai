import { BellRing } from 'lucide-react'
import { agentDisplayStatus, type AgentStatusSource, type AgentStatusTone } from '../../lib/agentStatus'
import type { ProviderActivity } from '../../api/agents'

const SOLID: Record<AgentStatusTone, string> = { accent: 'bg-accent-solid', warn: 'bg-warn-solid', danger: 'bg-danger-solid', muted: 'bg-muted-2' }
const RING: Record<AgentStatusTone, string> = { accent: 'border-accent-solid', warn: 'border-warn-solid', danger: 'border-danger-solid', muted: 'border-muted-2' }
const CHIP: Record<AgentStatusTone, string> = {
  accent: 'bg-accent/12 text-accent',
  warn: 'bg-warn-solid/[.16] text-warn',
  danger: 'bg-danger-solid/[.2] text-danger',
  muted: 'bg-panel-4 text-muted',
}
const PULSE = ' animate-pulse motion-reduce:animate-none'

interface Props {
  agent: AgentStatusSource
  providerActivity?: ProviderActivity
  className?: string
}

/** The agent's status as a dot. An inferred status is a hollow ring, so it never relies on color alone. */
export function AgentStatusDot({ agent, providerActivity, className = '' }: Props) {
  const status = agentDisplayStatus(agent, providerActivity)
  const look = status.inferred ? 'border border-dashed bg-transparent ' + RING[status.tone] : SOLID[status.tone]
  return <span role="img" aria-label={status.description} title={status.description} data-agent-status={status.key} data-inferred={status.inferred || undefined}
    className={'inline-block h-1.5 w-1.5 shrink-0 rounded-full ' + look + (status.pulse ? PULSE : '') + (className ? ' ' + className : '')} />
}

/** The agent's status as text, with an icon while the agent needs input. */
export function AgentStatusBadge({ agent, providerActivity, className = '' }: Props) {
  const status = agentDisplayStatus(agent, providerActivity)
  return <span role="img" aria-label={status.description} title={status.description} data-agent-status={status.key} data-inferred={status.inferred || undefined}
    className={'inline-flex shrink-0 items-center gap-1 rounded-[5px] px-1.5 font-mono text-[9.5px] ' + CHIP[status.tone] + (status.inferred ? ' border border-dashed border-current opacity-80' : '') + (status.key === 'waiting' ? ' font-semibold' : '') + (className ? ' ' + className : '')}>
    {status.key === 'waiting' && <BellRing size={9} aria-hidden="true" className="motion-safe:animate-pulse" />}
    {status.label}
  </span>
}

/** "n need input": the attention marker of worktree rows and canvas nodes. Renders nothing for zero. */
export function NeedsInputChip({ count, className = '' }: { count: number; className?: string }) {
  if (count <= 0) return null
  const text = `${count} ${count === 1 ? 'agent needs' : 'agents need'} input`
  return <span role="img" aria-label={text} title={text} data-needs-input={count}
    className={'inline-flex shrink-0 items-center gap-1 rounded-[5px] bg-warn-solid/[.2] px-1.5 font-mono text-[9.5px] font-semibold text-warn' + (className ? ' ' + className : '')}>
    <BellRing size={9} aria-hidden="true" />{count}
  </span>
}
