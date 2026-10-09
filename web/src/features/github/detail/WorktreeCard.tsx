import { useMemo } from 'react'
import { Check, LayoutGrid, LoaderCircle } from 'lucide-react'
import type { Agent, Health, Process, Worktree } from '../../../types'
import { useBonsaiStore } from '../../../stores/bonsai'
import { Card } from './Card'

const DOT: Record<Health, string> = {
  healthy: 'bg-ok',
  warning: 'bg-warn',
  error: 'bg-danger',
  idle: 'bg-faint',
}

function AgentState({ state }: { state: Agent['state'] }) {
  if (state === 'running') return <LoaderCircle size={11} className="flex-none animate-spin text-accent" aria-label="running" />
  if (state === 'finished') return <Check size={11} className="flex-none text-ok" aria-label="finished" />
  return <span className="h-1.5 w-1.5 flex-none rounded-full bg-faint" role="img" aria-label="idle" />
}

/** What runs in the worktree that holds the PR's branch. */
export function WorktreeCard({ worktree, onShowOnCanvas }: { worktree: Worktree; onShowOnCanvas: (worktreeId: string) => void }) {
  const allProcesses = useBonsaiStore((state) => state.processes)
  const allAgents = useBonsaiStore((state) => state.agents)
  const processes = useMemo<Process[]>(() => allProcesses.filter((process) => process.worktreeId === worktree.id), [allProcesses, worktree.id])
  const agents = useMemo<Agent[]>(() => allAgents.filter((agent) => agent.worktreeId === worktree.id && !agent.archived), [allAgents, worktree.id])

  return (
    <Card title="Worktree" aside={<span className="font-mono">{worktree.branch}</span>}>
      <div className="flex flex-col gap-1 px-3 py-2">
        {processes.map((process) => (
          <div key={process.id} className="flex h-[22px] items-center gap-2 font-mono text-[11px] text-text">
            <span className={`h-1.5 w-1.5 flex-none rounded-full ${DOT[process.status]}`} role="img" aria-label={process.status} />
            <span className="min-w-0 flex-1 truncate" title={process.command}>{process.name}</span>
            {process.port ? <span className="flex-none text-muted-2">:{process.port}</span> : <span className="flex-none text-muted-2">{process.lifecycleStatus}</span>}
          </div>
        ))}
        {agents.map((agent) => (
          <div key={agent.id} className="flex h-[22px] items-center gap-2 text-[11.5px] text-text">
            <span className="grid h-4 w-4 flex-none place-items-center rounded bg-panel-3 font-mono text-[8px] text-muted" aria-hidden="true">{agent.name.slice(0, 2).toUpperCase()}</span>
            <span className="min-w-0 flex-1 truncate">{agent.name}</span>
            <span className="min-w-0 max-w-[45%] truncate font-mono text-[10px] text-muted-2" title={agent.task}>{agent.task}</span>
            <AgentState state={agent.state} />
          </div>
        ))}
        {!processes.length && !agents.length && <p className="text-[11.5px] text-muted-2">Nothing running here.</p>}
      </div>
      <div className="px-3 pb-2.5">
        <button
          type="button"
          onClick={() => onShowOnCanvas(worktree.id)}
          className="bonsai-focus btn-bordered flex h-[30px] w-full items-center justify-center gap-2 rounded-lg text-[12px] text-text"
        >
          <LayoutGrid size={12} aria-hidden="true" /> Show on canvas
        </button>
      </div>
    </Card>
  )
}
