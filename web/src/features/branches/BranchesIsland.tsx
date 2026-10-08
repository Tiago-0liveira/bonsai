import { memo, useMemo } from 'react'
import { Archive, ChevronDown, ChevronRight, CircleCheck, CircleX, Clock3, GitBranch, LoaderCircle, Plus } from 'lucide-react'
import { ProviderBadge } from '../../components/ui/ProviderBadge'
import { useBonsaiStore } from '../../stores/bonsai'
import { agentById, branchTreeSelector, worktreeById, type BranchTreeIndex } from '../terminal/branchTree'
import type { CiStatus } from '../../types'

export function StatusDot({ status }: { status: 'healthy' | 'warning' | 'error' | 'idle' | 'running' | 'finished' }) {
  const className =
    status === 'healthy' || status === 'running'
      ? 'bg-accent-solid'
      : status === 'warning'
        ? 'bg-warn-solid'
        : status === 'error'
          ? 'bg-danger'
          : 'bg-muted-2'
  return <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + className} />
}

function CiIcon({ status }: { status: CiStatus }) {
  if (status === 'passed') return <CircleCheck size={12} className="text-ok" />
  if (status === 'running') return <LoaderCircle size={12} className="animate-spin text-accent" />
  if (status === 'failed') return <CircleX size={12} className="text-danger" />
  return <Clock3 size={12} className="text-muted-2" />
}

const EMPTY_VISITED = new Set<string>()
const EMPTY_IDS: string[] = []

// `trail` carries the guide columns of every ancestor ('│  ' when that ancestor still has
// siblings below, blank otherwise). It is a string so memoized rows keep a stable prop.
const BranchTreeItem = memo(function BranchTreeItem({ worktreeId, index, depth, visited, isLast, trail }: {
  worktreeId: string; index: BranchTreeIndex; depth: number; visited: Set<string>; isLast: boolean; trail: string
}) {
  const worktree = useBonsaiStore(state => worktreeById(state.worktrees, worktreeId))
  const selected = useBonsaiStore(state => state.dockWorktreeId === worktreeId)
  const collapsed = useBonsaiStore(state => state.collapsedBranchIds.includes(worktreeId))
  const toggleBranchCollapsed = useBonsaiStore(state => state.toggleBranchCollapsed)
  const setSelection = useBonsaiStore(state => state.setSelection)
  const nextVisited = useMemo(() => new Set([...visited, worktreeId]), [visited, worktreeId])
  const children = index.children[worktreeId] ?? EMPTY_IDS
  const canvasAgents = index.canvasAgents[worktreeId] ?? EMPTY_IDS
  const historyAgents = index.historyAgents[worktreeId] ?? EMPTY_IDS
  if (!worktree || visited.has(worktreeId)) return null
  const expandable = canvasAgents.length > 0 || historyAgents.length > 0 || children.length > 0
  const childTrail = trail + (isLast ? '   ' : '│  ')

  return (
    <div>
      <div className="grid h-[22px] grid-cols-[minmax(0,1fr)_auto] items-center">
        <div className={'flex h-[22px] min-w-0 items-center rounded-md pr-1.5 font-mono text-[11.5px] ' + (selected ? 'bg-accent/[.09] text-text' : 'text-muted hover:bg-panel-2')}>
          <button
            type="button"
            onClick={() => expandable && toggleBranchCollapsed(worktree.id)}
            className="grid h-[22px] w-[18px] shrink-0 place-items-center text-muted-2 hover:text-text"
            title={collapsed ? 'Expand branch' : 'Collapse branch'}
          >
            {expandable ? (collapsed ? <ChevronRight size={9} /> : <ChevronDown size={9} />) : <span className="h-1 w-1 rounded-full bg-muted-2" />}
          </button>
          <button
            type="button"
            onClick={() => setSelection({ type: 'worktree', id: worktree.id })}
            className="bonsai-focus flex h-[22px] min-w-0 flex-1 items-center text-left"
          >
            <span aria-hidden className="shrink-0 whitespace-pre text-muted-2/60">{trail + (isLast ? '└─ ' : '├─ ')}</span>
            <span className="min-w-0 flex-1 truncate">{worktree.branch}</span>
            {canvasAgents.length > 0 && <span className="ml-1 text-[9.5px] text-muted-2">{canvasAgents.length}</span>}
          </button>
        </div>
        <span className="flex items-center gap-1.5 pl-1.5 pr-1.5">
          <CiIcon status={worktree.ciStatus} />
          {worktree.prNumber ? <span className="font-mono text-[10px] text-muted-2">#{worktree.prNumber}</span> : null}
        </span>
      </div>

      {!collapsed && (
        <>
          {canvasAgents.map(id => <BranchAgentRow key={id} id={id} trail={childTrail} />)}
          {historyAgents.length > 0 && (
            <div style={{ paddingLeft: 18 }} className="flex h-[22px] items-center gap-1.5 pr-2 font-mono text-[10px] text-muted-2">
              <span aria-hidden className="whitespace-pre text-muted-2/60">{childTrail}</span>
              <Archive size={9} /> <span>History</span><span>· {historyAgents.length}</span>
            </div>
          )}
          {children.map((id, position) => <BranchTreeItem key={id} worktreeId={id} index={index} depth={depth + 1} visited={nextVisited} isLast={position === children.length - 1} trail={childTrail} />)}
        </>
      )}
    </div>
  )
})

const BranchAgentRow = memo(function BranchAgentRow({ id, trail }: { id: string; trail: string }) {
  const agent = useBonsaiStore(state => agentById(state.agents, id))
  const setSelection = useBonsaiStore(state => state.setSelection)
  if (!agent) return null
  return <button type="button" onClick={() => setSelection({ type: 'agent', id })} style={{ paddingLeft: 18 }} className="bonsai-focus flex h-[22px] w-full items-center gap-1.5 rounded-md pr-2 text-left font-mono text-[10.5px] text-muted-2 hover:bg-panel-2 hover:text-text">
    <span aria-hidden className="whitespace-pre text-muted-2/60">{trail}</span>
    <ProviderBadge provider={agent.provider} size={16} />
    <span className="min-w-0 flex-1 truncate">{agent.name}<span className="ml-1.5 text-[9.5px] text-muted-2">{agent.profileName ?? `${agent.model} · ${agent.reasoningEffort}`}</span></span>
    <StatusDot status={agent.state} />
  </button>
})

export function BranchesIsland() {
  const activeProjectId = useBonsaiStore(state => state.activeProjectId)
  const index = useBonsaiStore(useMemo(() => branchTreeSelector(activeProjectId), [activeProjectId]))
  const defaultWorktree = useBonsaiStore(state => worktreeById(state.worktrees, index.defaultId ?? ''))
  const defaultSelected = useBonsaiStore(state => state.dockWorktreeId === index.defaultId)
  const setSelection = useBonsaiStore(state => state.setSelection)
  const setWorktreeDialogOpen = useBonsaiStore(state => state.setWorktreeDialogOpen)
  const agentCount = useMemo(() => Object.values(index.canvasAgents).reduce((total, ids) => total + ids.length, 0), [index])

  return (
    <aside aria-label="Branches" className="island flex h-[min(296px,40vh)] shrink-0 flex-col overflow-hidden">
      <div className="island-title shrink-0 gap-2">
        <GitBranch size={13} className="text-ok" />
        <span>Branches</span>
        <span className="island-count">{index.count}</span>
        <button type="button" aria-label="Add worktree" title="Add worktree" onClick={() => setWorktreeDialogOpen(true)} className="bonsai-focus icon-btn-20 ml-auto hover:text-text">
          <Plus size={12} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {defaultWorktree && (
          <div className={'flex h-[22px] w-full min-w-0 items-center gap-2 rounded-md pr-2 font-mono text-[11.5px] ' + (defaultSelected ? 'bg-accent/[.09] text-text' : 'hover:bg-panel-2')}>
            <button
              type="button"
              onClick={() => setSelection({ type: 'worktree', id: defaultWorktree.id })}
              className="bonsai-focus flex h-[22px] min-w-0 flex-1 items-center gap-2 px-2 text-left"
            >
              <span className="h-[7px] w-[7px] shrink-0 rounded-full bg-accent-solid" />
              <span className="min-w-0 flex-1 truncate text-accent">{defaultWorktree.branch}</span>
            </button>
            <span className="chip bg-accent/[.14] text-accent">default</span>
          </div>
        )}
        {index.roots.map((id, position) => <BranchTreeItem key={id} worktreeId={id} index={index} depth={0} visited={EMPTY_VISITED} isLast={position === index.roots.length - 1} trail="" />)}
      </div>
      <div className="flex h-[26px] shrink-0 items-center border-t border-border-subtle px-3 font-mono text-[10px] text-muted-2">
        <span>{index.count} worktrees · {agentCount} agents</span>
        <button type="button" onClick={() => setWorktreeDialogOpen(true)} className="bonsai-focus ml-auto text-accent hover:underline">+ new branch</button>
      </div>
    </aside>
  )
}
