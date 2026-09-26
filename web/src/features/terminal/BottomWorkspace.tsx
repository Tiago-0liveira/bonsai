import { useEffect, useMemo, useRef, useState } from 'react'
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  rectSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import * as Tabs from '@radix-ui/react-tabs'
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels'
import {
  Archive,
  Bot,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CircleDot,
  ExternalLink,
  FileCode2,
  Files,
  Folder,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequest,
  GripVertical,
  ListTree,
  Maximize2,
  Minus,
  Search,
  TerminalSquare,
  X,
  XCircle,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { flattenFiles, repoFiles } from '../../mock/files'
import { processes } from '../../mock/processes'
import { useBonsaiStore } from '../../stores/bonsai'
import type { Agent, EditorPreference, Process, PullRequest, RepoFile, Worktree } from '../../types'
import { FakeTerminal } from './FakeTerminal'

function StatusDot({ status }: { status: 'healthy' | 'warning' | 'error' | 'idle' | 'running' | 'finished' }) {
  const className =
    status === 'healthy' || status === 'running'
      ? 'bg-[rgb(var(--green))]'
      : status === 'warning'
        ? 'bg-[rgb(var(--orange))]'
        : status === 'error'
          ? 'bg-[rgb(var(--red))]'
          : 'bg-[rgb(var(--muted-2))]'
  return <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + className} />
}

function ProviderMark({ provider }: { provider: Agent['provider'] }) {
  const label = provider === 'Codex' ? 'O' : provider === 'Claude' ? 'A' : 'G'
  return (
    <span className="grid h-6 w-6 shrink-0 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[9px] font-semibold text-[rgb(var(--muted))]">
      {label}
    </span>
  )
}

function agentPresentation(agent: Agent) {
  return agent.presentation ?? (agent.archived ? 'archived' : 'canvas')
}

function BranchTreeItem({
  worktree,
  allWorktrees,
  agents,
  depth,
  selectedId,
  visited,
  onWorktree,
  onAgent,
}: {
  worktree: Worktree
  allWorktrees: Worktree[]
  agents: Agent[]
  depth: number
  selectedId: string
  visited: Set<string>
  onWorktree: (worktree: Worktree) => void
  onAgent: (agent: Agent) => void
}) {
  const collapsedBranchIds = useBonsaiStore((state) => state.collapsedBranchIds)
  const toggleBranchCollapsed = useBonsaiStore((state) => state.toggleBranchCollapsed)
  if (visited.has(worktree.id)) return null
  const nextVisited = new Set(visited)
  nextVisited.add(worktree.id)
  const children = allWorktrees.filter((item) => item.id !== worktree.id && item.mergeTargetBranch === worktree.branch)
  const branchAgents = agents.filter((agent) => agent.worktreeId === worktree.id && agentPresentation(agent) !== 'archived')
  const canvasAgents = branchAgents.filter((agent) => agentPresentation(agent) === 'canvas')
  const historyAgents = branchAgents.filter((agent) => agentPresentation(agent) === 'history')
  const collapsed = collapsedBranchIds.includes(worktree.id)
  const expandable = canvasAgents.length > 0 || historyAgents.length > 0 || children.length > 0
  const indent = depth * 10

  return (
    <div>
      <div className="grid h-7 grid-cols-[18px_minmax(0,1fr)_28px] items-center" style={{ paddingLeft: indent }}>
        <button
          type="button"
          onClick={() => expandable && toggleBranchCollapsed(worktree.id)}
          className="grid h-6 w-[18px] place-items-center rounded text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))]"
          title={collapsed ? 'Expand branch' : 'Collapse branch'}
        >
          {expandable ? (collapsed ? <ChevronRight size={9} /> : <ChevronDown size={9} />) : <span className="h-1 w-1 rounded-full bg-[rgb(var(--muted-2))]" />}
        </button>
        <button
          type="button"
          onClick={() => onWorktree(worktree)}
          className={
            'bonsai-focus flex h-7 min-w-0 items-center gap-1.5 rounded-md px-1.5 text-left text-[9px] ' +
            (selectedId === worktree.id ? 'bg-[rgb(var(--purple)/.11)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
          }
        >
          <GitBranch size={10} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate font-mono">{worktree.branch}</span>
          {canvasAgents.length > 0 && <span className="text-[7px] text-[rgb(var(--muted-2))]">{canvasAgents.length}</span>}
        </button>
        <span className="grid place-items-center"><StatusDot status={worktree.status} /></span>
      </div>

      {!collapsed && (
        <>
          {canvasAgents.map((agent) => (
            <button
              type="button"
              key={agent.id}
              onClick={() => onAgent(agent)}
              style={{ paddingLeft: 24 + indent }}
              className="bonsai-focus flex h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[8px] text-[rgb(var(--muted-2))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
            >
              <span className="grid h-5 w-5 shrink-0 place-items-center rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[7px] font-semibold">
                {agent.provider === 'Codex' ? 'O' : agent.provider === 'Claude' ? 'A' : 'G'}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate">{agent.name}</span>
                <span className="block truncate text-[6.5px] text-[rgb(var(--muted-2))]">{agent.model} · {agent.reasoningEffort}</span>
              </span>
              <StatusDot status={agent.state} />
            </button>
          ))}
          {historyAgents.length > 0 && (
            <div style={{ paddingLeft: 28 + indent }} className="flex h-6 items-center gap-1.5 pr-2 text-[7.5px] text-[rgb(var(--muted-2))]">
              <Archive size={8} /> <span>History</span><span>· {historyAgents.length}</span>
            </div>
          )}
          {children.map((child) => (
            <BranchTreeItem
              key={child.id}
              worktree={child}
              allWorktrees={allWorktrees}
              agents={agents}
              depth={depth + 1}
              selectedId={selectedId}
              visited={nextVisited}
              onWorktree={onWorktree}
              onAgent={onAgent}
            />
          ))}
        </>
      )}
    </div>
  )
}

function BranchSidebar() {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const defaultWorktree = projectWorktrees.find((item) => item.branch === project?.defaultBranch)
  const roots = projectWorktrees.filter((item) => item.branch !== project?.defaultBranch && item.mergeTargetBranch === project?.defaultBranch)

  return (
    <aside className="dock-pane flex h-full min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-3">
        <GitBranch size={13} className="text-[rgb(var(--purple))]" />
        <span className="dock-title">Branches</span>
        <span className="dock-count ml-auto">{projectWorktrees.length}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-1">
        {defaultWorktree && (
          <div className="grid h-7 grid-cols-[18px_minmax(0,1fr)_auto] items-center">
            <span />
            <button
              type="button"
              onClick={() => setSelection({ type: 'worktree', id: defaultWorktree.id })}
              className={
                'bonsai-focus flex h-7 min-w-0 items-center gap-1.5 rounded-md px-1.5 text-left text-[9px] ' +
                (dockWorktreeId === defaultWorktree.id ? 'bg-[rgb(var(--green)/.08)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
              }
            >
              <GitBranch size={10} className="shrink-0 text-[rgb(var(--green))]" />
              <span className="min-w-0 flex-1 truncate font-mono">{defaultWorktree.branch}</span>
            </button>
            <span className="ml-1 rounded bg-[rgb(var(--green)/.10)] px-1 py-0.5 text-[6.5px] text-[rgb(var(--green))]">default</span>
          </div>
        )}
        {roots.map((worktree) => (
          <BranchTreeItem
            key={worktree.id}
            worktree={worktree}
            allWorktrees={projectWorktrees}
            agents={agents}
            depth={0}
            selectedId={dockWorktreeId}
            visited={new Set()}
            onWorktree={(item) => setSelection({ type: 'worktree', id: item.id })}
            onAgent={(agent) => setSelection({ type: 'agent', id: agent.id })}
          />
        ))}
      </div>
    </aside>
  )
}

type RuntimeEntry =
  | { id: string; type: 'agent'; agent: Agent }
  | { id: string; type: 'process'; process: Process }

function SortableRuntimeTile({ runtime }: { runtime: RuntimeEntry }) {
  const active = useBonsaiStore((state) => state.dockRuntimeId === runtime.id)
  const closeRuntime = useBonsaiStore((state) => state.closeRuntime)
  const setDockRuntimeId = useBonsaiStore((state) => state.setDockRuntimeId)
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: runtime.id })
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.55 : 1 }

  return (
    <section
      ref={setNodeRef}
      style={style}
      onClick={() => setDockRuntimeId(runtime.id)}
      className={"runtime-tile flex h-full min-h-0 min-w-0 flex-col overflow-hidden " + (active ? "runtime-tile-active" : "")}
    >
      <div
        {...attributes}
        {...listeners}
        className="runtime-heading flex h-10 shrink-0 cursor-grab items-center gap-2 px-2.5 active:cursor-grabbing"
        title="Drag terminal"
      >
        <GripVertical size={9} className="shrink-0 text-[rgb(var(--muted-2))]" />
        {runtime.type === 'agent' ? (
          <span className="grid h-5 w-5 shrink-0 place-items-center rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[7px] font-semibold">
            {runtime.agent.provider === 'Codex' ? 'O' : runtime.agent.provider === 'Claude' ? 'A' : 'G'}
          </span>
        ) : (
          <span className="grid h-5 w-5 place-items-center rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))]"><TerminalSquare size={9} /></span>
        )}
        <div className="min-w-0 flex-1">
          <div className="truncate text-[11px] font-medium">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</div>
          <div className="truncate text-[9px] text-[rgb(var(--muted))]">
            {runtime.type === 'agent'
              ? runtime.agent.model + ' · ' + runtime.agent.reasoningEffort + (runtime.agent.fastMode ? ' · Fast' : '')
              : runtime.process.command}
          </div>
        </div>
        <StatusDot status={runtime.type === 'agent' ? runtime.agent.state : runtime.process.status} />
        <button
          type="button"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation()
            closeRuntime(runtime.id)
          }}
          className="grid h-5 w-5 place-items-center rounded text-[rgb(var(--muted-2))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          title="Close terminal"
        >
          <X size={9} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {runtime.type === 'agent' ? (
          <FakeTerminal terminalId={runtime.agent.terminalId} />
        ) : (
          <div className="h-full min-h-0 overflow-auto p-2.5 font-mono text-[8px] leading-4 text-[rgb(var(--muted))]">
            <div className="text-[rgb(var(--green))]">$ {runtime.process.command}</div>
            <div>[bonsai] status: {runtime.process.status}</div>
            {runtime.process.port && <div>[bonsai] listening on http://localhost:{runtime.process.port}</div>}
          </div>
        )}
      </div>
    </section>
  )
}

function RuntimeWorkspace() {
  const hostRef = useRef<HTMLElement | null>(null)
  const [wideHeader, setWideHeader] = useState(false)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const dockRuntimeId = useBonsaiStore((state) => state.dockRuntimeId)
  const openRuntimeIds = useBonsaiStore((state) => state.openRuntimeIds)
  const openRuntime = useBonsaiStore((state) => state.openRuntime)
  const reorderOpenRuntime = useBonsaiStore((state) => state.reorderOpenRuntime)
  const rightPanels = useBonsaiStore((state) => state.rightPanels)
  const toggleRightPanel = useBonsaiStore((state) => state.toggleRightPanel)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }))

  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const fallbackWorktree = projectWorktrees.find((item) => item.branch !== project?.defaultBranch) ?? projectWorktrees[0]
  const worktree = projectWorktrees.find((item) => item.id === dockWorktreeId) ?? fallbackWorktree
  const worktreeAgents = agents.filter((item) => item.worktreeId === worktree?.id && agentPresentation(item) === 'canvas')
  const worktreeProcesses = processes.filter((item) => item.worktreeId === worktree?.id)
  const available: RuntimeEntry[] = [
    ...worktreeAgents.map((agent) => ({ id: agent.id, type: 'agent' as const, agent })),
    ...worktreeProcesses.map((process) => ({ id: process.id, type: 'process' as const, process })),
  ]
  const availableMap = new Map(available.map((runtime) => [runtime.id, runtime]))
  const openEntries = openRuntimeIds.map((id) => availableMap.get(id)).filter((item): item is RuntimeEntry => Boolean(item))

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => setWideHeader(entry.contentRect.width >= 690))
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (!worktree || !available.length) return
    if (dockRuntimeId && availableMap.has(dockRuntimeId)) {
      if (!openRuntimeIds.includes(dockRuntimeId)) openRuntime(dockRuntimeId)
      return
    }
    if (!openEntries.length) {
      const preferred = available.find((runtime) =>
        runtime.type === 'agent' ? runtime.agent.state === 'running' : runtime.process.status === 'healthy',
      ) ?? available[0]
      openRuntime(preferred.id)
    }
  }, [worktree?.id, dockRuntimeId, openRuntimeIds.join('|')])

  const onDragEnd = (event: DragEndEvent) => {
    const active = String(event.active.id)
    const over = event.over?.id ? String(event.over.id) : ''
    if (over) reorderOpenRuntime(active, over)
  }

  const runtimeOptions = available.map((runtime) => ({
    value: runtime.id,
    label: runtime.type === 'agent' ? runtime.agent.name : runtime.process.name,
    description: runtime.type === 'agent' ? runtime.agent.provider + ' · ' + runtime.agent.model : runtime.process.command,
    meta: openRuntimeIds.includes(runtime.id) ? 'open' : undefined,
  }))

  return (
    <section ref={hostRef} className="dock-pane flex h-full min-h-0 min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-2.5">
        {wideHeader ? (
          <>
            <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
              {openEntries.map((runtime) => (
                <button
                  key={runtime.id}
                  type="button"
                  onClick={() => openRuntime(runtime.id)}
                  className={
                    'flex h-7 min-w-0 max-w-[170px] items-center gap-1.5 rounded-md px-2 text-left ' +
                    (dockRuntimeId === runtime.id ? 'bg-[rgb(var(--panel-2))] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
                  }
                >
                  {runtime.type === 'agent' ? (
                    <span className="grid h-4 w-4 shrink-0 place-items-center rounded border border-[rgb(var(--border))] text-[6px] font-semibold">
                      {runtime.agent.provider === 'Codex' ? 'O' : runtime.agent.provider === 'Claude' ? 'A' : 'G'}
                    </span>
                  ) : <TerminalSquare size={9} />}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[10px] font-medium">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</span>
                    {runtime.type === 'agent' && <span className="block truncate text-[8px] text-[rgb(var(--muted))]">{runtime.agent.provider} · {runtime.agent.model}</span>}
                  </span>
                </button>
              ))}
            </div>
            <div className="w-[142px] shrink-0">
              <BonsaiSelect ariaLabel="Open runtime" compact searchable value="" onChange={openRuntime} placeholder="+ Open" options={runtimeOptions} />
            </div>
          </>
        ) : (
          <div className="min-w-0 flex-1">
            <BonsaiSelect ariaLabel="Open runtime" compact searchable value={dockRuntimeId} onChange={openRuntime} placeholder="Open agent / process…" options={runtimeOptions} />
          </div>
        )}

        <div className="ml-auto flex shrink-0 items-center gap-1">
          <button type="button" onClick={() => toggleRightPanel('files')} title="Toggle Files / Git Diff" className={'bonsai-focus flex h-6 items-center gap-1 rounded px-1.5 text-[8px] ' + (rightPanels.files ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')}>
            <Files size={10} /> Files
          </button>
          <button type="button" onClick={() => toggleRightPanel('prs')} title="Toggle pull requests" className={'bonsai-focus flex h-6 items-center gap-1 rounded px-1.5 text-[8px] ' + (rightPanels.prs ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')}>
            <GitPullRequest size={10} /> PRs
          </button>
          <span className="mx-0.5 h-4 w-px bg-[rgb(var(--border))]" />
          <button onClick={() => setDockState('collapsed')} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title="Minimize workspace"><Minus size={11} /></button>
          <button onClick={() => setDockState(dockState === 'maximized' ? 'normal' : 'maximized')} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title="Maximize workspace"><Maximize2 size={11} /></button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden p-2">
        {openEntries.length ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={openEntries.map((item) => item.id)} strategy={rectSortingStrategy}>
              <div className="grid h-full min-h-0 auto-rows-fr grid-cols-[repeat(auto-fit,minmax(min(210px,100%),1fr))] gap-2">
                {openEntries.map((runtime) => <SortableRuntimeTile key={runtime.id} runtime={runtime} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="grid h-full place-items-center rounded-md border border-dashed border-[rgb(var(--border))] text-[9px] text-[rgb(var(--muted-2))]">
            Open an agent or process terminal.
          </div>
        )}
      </div>
    </section>
  )
}

function GitStatus({ status }: { status?: RepoFile['gitStatus'] }) {
  if (!status || status === 'committed') return null
  const label = status === 'modified' ? 'M' : status === 'untracked' ? 'U' : status === 'added' ? 'A' : 'D'
  const tone = status === 'deleted' ? 'text-[rgb(var(--red))]' : status === 'untracked' ? 'text-[rgb(var(--green))]' : 'text-[rgb(var(--orange))]'
  return <span className={'ml-auto shrink-0 font-mono text-[8px] ' + tone}>{label}</span>
}

function countChanged(nodes: RepoFile[]): number {
  return nodes.reduce((total, node) => {
    if (node.type === 'file') return total + (node.gitStatus && node.gitStatus !== 'committed' ? 1 : 0)
    return total + countChanged(node.children ?? [])
  }, 0)
}

function filterTree(nodes: RepoFile[], mode: 'changed' | 'committed'): RepoFile[] {
  return nodes.flatMap((node) => {
    if (node.type === 'file') {
      const changed = Boolean(node.gitStatus && node.gitStatus !== 'committed')
      return (mode === 'changed' ? changed : !changed) ? [node] : []
    }
    const children = filterTree(node.children ?? [], mode)
    return children.length ? [{ ...node, children }] : []
  })
}

function FileTreeRows({ nodes, depth = 0 }: { nodes: RepoFile[]; depth?: number }) {
  const requestOpenFile = useBonsaiStore((state) => state.requestOpenFile)
  const [openFolders, setOpenFolders] = useState<string[]>(['src', 'features', 'workspace'])
  return (
    <>
      {nodes.map((node) => {
        if (node.type === 'folder') {
          const open = openFolders.includes(node.id)
          return (
            <div key={node.id}>
              <button
                type="button"
                onClick={() => setOpenFolders((items) => open ? items.filter((id) => id !== node.id) : [...items, node.id])}
                style={{ paddingLeft: 6 + depth * 12 }}
                className="flex h-6 w-full items-center gap-1.5 rounded pr-2 text-left text-[9px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
              >
                {open ? <ChevronDown size={9} /> : <ChevronRight size={9} />}
                <Folder size={10} />
                <span className="min-w-0 flex-1 truncate">{node.name}</span>
                {countChanged(node.children ?? []) > 0 && <span className="text-[7px] text-[rgb(var(--orange))]">{countChanged(node.children ?? [])}</span>}
              </button>
              {open && node.children && <FileTreeRows nodes={node.children} depth={depth + 1} />}
            </div>
          )
        }
        return (
          <button
            type="button"
            key={node.id}
            onClick={() => requestOpenFile(node.path)}
            style={{ paddingLeft: 20 + depth * 12 }}
            className="flex h-6 w-full items-center gap-1.5 rounded pr-2 text-left font-mono text-[8px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <FileCode2 size={9} />
            <span className="min-w-0 flex-1 truncate">{node.name}</span>
            <GitStatus status={node.gitStatus} />
          </button>
        )
      })}
    </>
  )
}

function FilesDiffPanel() {
  const setRightPanel = useBonsaiStore((state) => state.setRightPanel)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const requestOpenFile = useBonsaiStore((state) => state.requestOpenFile)
  const [view, setView] = useState<'flat' | 'tree'>('tree')
  const worktree = worktrees.find((item) => item.id === dockWorktreeId)
  const pr = pullRequests.find((item) => item.number === worktree?.prNumber)
  const files = flattenFiles(repoFiles).filter((item) => item.type === 'file')
  const changed = files.filter((item) => item.gitStatus && item.gitStatus !== 'committed')
  const committed = files.filter((item) => !item.gitStatus || item.gitStatus === 'committed')
  const changedTree = filterTree(repoFiles, 'changed')
  const committedTree = filterTree(repoFiles, 'committed')

  return (
    <aside className="dock-pane flex h-full min-w-0 flex-col">
      <Tabs.Root defaultValue="files" className="flex min-h-0 flex-1 flex-col">
        <div className="dock-heading flex shrink-0 items-center px-2">
          <Tabs.List className="dock-tabs flex h-full items-center gap-1">
            <Tabs.Trigger value="files" className="bonsai-focus dock-tab">Files</Tabs.Trigger>
            <Tabs.Trigger value="diff" className="bonsai-focus dock-tab">Git Diff</Tabs.Trigger>
          </Tabs.List>
          <div className="ml-auto flex items-center gap-1">
            <button onClick={() => setView(view === 'tree' ? 'flat' : 'tree')} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title={view === 'tree' ? 'Flat file list' : 'File tree'}>
              {view === 'tree' ? <Files size={11} /> : <ListTree size={11} />}
            </button>
            <button onClick={() => setRightPanel('files', false)} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title="Close Files / Git Diff"><X size={11} /></button>
          </div>
        </div>

        <Tabs.Content value="files" className="min-h-0 flex-1 overflow-auto p-1.5 outline-none">
          {view === 'tree' ? (
            <>
              <div className="px-2 pb-1 pt-1 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Changed</div>
              {changedTree.length ? <FileTreeRows nodes={changedTree} /> : <div className="px-2 py-2 text-[8px] text-[rgb(var(--muted-2))]">No changed files</div>}
              <div className="mt-2 border-t border-[rgb(var(--border))] px-2 pb-1 pt-2 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Repository</div>
              <FileTreeRows nodes={committedTree} />
            </>
          ) : (
            <>
              <div className="px-2 pb-1 pt-1 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Changed</div>
              {changed.map((file) => (
                <button key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-6 w-full items-center gap-1.5 rounded px-2 text-left font-mono text-[8px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
                  <FileCode2 size={9} /><span className="min-w-0 flex-1 truncate">{file.path}</span><GitStatus status={file.gitStatus} />
                </button>
              ))}
              <div className="mt-2 border-t border-[rgb(var(--border))] px-2 pb-1 pt-2 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Repository</div>
              {committed.map((file) => (
                <button key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-6 w-full items-center gap-1.5 rounded px-2 text-left font-mono text-[8px] text-[rgb(var(--muted-2))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
                  <FileCode2 size={9} /><span className="truncate">{file.path}</span>
                </button>
              ))}
            </>
          )}
        </Tabs.Content>

        <Tabs.Content value="diff" className="min-h-0 flex-1 overflow-auto p-2.5 outline-none">
          {pr?.files.length ? pr.files.map((file) => (
            <div key={file.path} className="mb-2 overflow-hidden rounded-md border border-[rgb(var(--border))]">
              <div className="flex items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1.5 font-mono text-[8px]">
                <span className="min-w-0 flex-1 truncate">{file.path}</span>
                <span className="text-[rgb(var(--green))]">+{file.additions}</span>
                <span className="ml-1 text-[rgb(var(--red))]">-{file.deletions}</span>
              </div>
              <pre className="overflow-auto p-2 font-mono text-[8px] leading-4 text-[rgb(var(--muted))]">{file.diff.join('\n')}</pre>
            </div>
          )) : (
            <div className="rounded-md border border-dashed border-[rgb(var(--border))] p-4 text-center text-[9px] text-[rgb(var(--muted-2))]">No PR diff linked to this worktree.</div>
          )}
        </Tabs.Content>
      </Tabs.Root>
    </aside>
  )
}

function checkIcon(status: 'success' | 'running' | 'failed') {
  if (status === 'success') return <CheckCircle2 size={11} className="text-[rgb(var(--green))]" />
  if (status === 'failed') return <XCircle size={11} className="text-[rgb(var(--red))]" />
  return <CircleDot size={11} className="text-[rgb(var(--orange))]" />
}

function PullRequestOperations({ pr }: { pr: PullRequest }) {
  const setStatus = useBonsaiStore((state) => state.setPullRequestStatus)
  return (
    <div className="flex flex-wrap gap-1.5">
      {pr.status === 'Open' && (
        <>
          <button disabled={!pr.mergeable} onClick={() => setStatus(pr.id, 'Merged')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--purple)/.35)] bg-[rgb(var(--purple)/.08)] px-2 text-[8px] text-[rgb(var(--purple))] disabled:opacity-35"><GitMerge size={9} /> Merge</button>
          <button onClick={() => setStatus(pr.id, 'Closed')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--red)/.3)] px-2 text-[8px] text-[rgb(var(--red))]"><X size={9} /> Close</button>
        </>
      )}
      {pr.status === 'Draft' && <button onClick={() => setStatus(pr.id, 'Open')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--green)/.3)] px-2 text-[8px] text-[rgb(var(--green))]"><GitPullRequest size={9} /> Open PR</button>}
      {pr.status === 'Closed' && <button onClick={() => setStatus(pr.id, 'Open')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--green)/.3)] px-2 text-[8px] text-[rgb(var(--green))]"><GitPullRequest size={9} /> Reopen</button>}
    </div>
  )
}

function PullRequestDetails({ pr }: { pr: PullRequest }) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const project = projects.find((item) => item.id === activeProjectId)
  const [checksOpen, setChecksOpen] = useState(true)
  const success = pr.checks.filter((check) => check.status === 'success').length
  const latest = pr.commits.at(-1)

  return (
    <div className="bg-[rgb(var(--bg)/.48)] px-2.5 pb-2.5">
      <div className="flex items-center gap-2 pt-2">
        <div className="flex min-w-0 flex-1 items-center gap-1.5 text-[8px] text-[rgb(var(--muted-2))]">
          <GitCommitHorizontal size={9} />
          <span>{pr.commits.length} commit{pr.commits.length === 1 ? '' : 's'}</span>
          <span>·</span>
          <span className="truncate font-mono">{latest?.sha}</span>
          <span>·</span>
          <span>{latest?.time ?? pr.updatedAt}</span>
        </div>
        <button
          onClick={() => project?.repository.includes('/') && window.open('https://github.com/' + project.repository + '/pull/' + pr.number, '_blank', 'noopener,noreferrer')}
          className="bonsai-focus grid h-6 w-6 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
          title="Open pull request URL"
        >
          <ExternalLink size={10} />
        </button>
      </div>

      {latest && <div className="mt-1 truncate text-[8px] text-[rgb(var(--muted))]">{latest.message}</div>}

      <button onClick={() => setChecksOpen((open) => !open)} className="mt-2 flex h-7 w-full items-center gap-1.5 rounded-md border border-[rgb(var(--border))] px-2 text-left text-[9px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">
        {checksOpen ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
        CI checks
        <span className="ml-auto">{success}/{pr.checks.length}</span>
      </button>
      {checksOpen && (
        <div className="mt-1">
          {pr.checks.map((check) => (
            <div key={check.name} className="flex items-center gap-1.5 rounded px-2 py-1.5 text-[8px] text-[rgb(var(--muted))]">
              {checkIcon(check.status)}
              <span className="min-w-0 flex-1 truncate">{check.name}</span>
              <span className="capitalize text-[rgb(var(--muted-2))]">{check.status}</span>
            </div>
          ))}
        </div>
      )}
      <div className="mt-2"><PullRequestOperations pr={pr} /></div>
    </div>
  )
}

function PullRequestsPanel() {
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const setRightPanel = useBonsaiStore((state) => state.setRightPanel)
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState('recent')
  const selectedId = useBonsaiStore((state) => state.inspectedPullRequestId)
  const focusNonce = useBonsaiStore((state) => state.pullRequestFocusNonce)
  const setSelectedId = useBonsaiStore((state) => state.setInspectedPullRequestId)
  const selectedRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setQuery('')
  }, [focusNonce])

  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest' })
  }, [selectedId, query, focusNonce])

  const filtered = useMemo(() => {
    const items = pullRequests.filter((pr) => {
      const needle = query.trim().toLowerCase()
      return !needle || (pr.title + ' ' + pr.branch + ' ' + pr.number).toLowerCase().includes(needle)
    })
    if (sort === 'number') return [...items].sort((a, b) => b.number - a.number)
    if (sort === 'checks') return [...items].sort((a, b) => a.checks.filter((check) => check.status === 'failed').length - b.checks.filter((check) => check.status === 'failed').length)
    return items
  }, [pullRequests, query, sort])

  return (
    <aside className="dock-pane flex h-full min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-3">
        <GitPullRequest size={13} className="shrink-0 text-[rgb(var(--green))]" />
        <span className="dock-title min-w-0 truncate">Pull requests</span>
        <span className="dock-count">{pullRequests.length}</span>
        <button onClick={() => setRightPanel('prs', false)} className="bonsai-focus ml-auto grid h-6 w-6 shrink-0 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-3))]" title="Close pull requests"><X size={12} /></button>
      </div>
      <div className="flex shrink-0 items-center gap-1.5 border-b border-[rgb(var(--border)/.5)] p-2">
        <label className="flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
          <Search size={9} className="text-[rgb(var(--muted-2))]" />
          <input aria-label="Search pull requests" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search PRs" className="min-w-0 flex-1 bg-transparent text-[8px] outline-none placeholder:text-[rgb(var(--muted-2))]" />
        </label>
        <div className="w-[92px] shrink-0">
          <BonsaiSelect ariaLabel="Sort pull requests" compact value={sort} onChange={setSort} options={[
            { value: 'recent', label: 'Recent' },
            { value: 'number', label: 'Number' },
            { value: 'checks', label: 'Checks' },
          ]} />
        </div>

      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {filtered.map((pr) => {
          const expanded = selectedId === pr.id
          const success = pr.checks.filter((check) => check.status === 'success').length
          return (
            <div key={pr.id} ref={expanded ? selectedRef : undefined} className="border-b border-[rgb(var(--border)/.55)]">
              <button type="button" onClick={() => setSelectedId(expanded ? null : pr.id)} className="flex w-full items-start gap-2 px-2.5 py-2.5 text-left hover:bg-[rgb(var(--panel-2))]">
                <GitPullRequest size={11} className={pr.status === 'Open' ? 'mt-0.5 text-[rgb(var(--green))]' : pr.status === 'Draft' ? 'mt-0.5 text-[rgb(var(--purple))]' : 'mt-0.5 text-[rgb(var(--muted-2))]'} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[9px] font-medium">#{pr.number} {pr.title}</span>
                  <span className="mt-1 block truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{pr.branch} → {pr.base}</span>
                </span>
                <span className="shrink-0 text-[8px] text-[rgb(var(--muted-2))]">{success}/{pr.checks.length}</span>
                {expanded ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              </button>
              {expanded && <PullRequestDetails pr={pr} />}
            </div>
          )
        })}
      </div>
    </aside>
  )
}

function EditorPreferenceDialog() {
  const open = useBonsaiStore((state) => state.editorPromptOpen)
  const path = useBonsaiStore((state) => state.pendingOpenFile)
  const setEditorPreference = useBonsaiStore((state) => state.setEditorPreference)
  const close = useBonsaiStore((state) => state.closeEditorPrompt)
  if (!open) return null

  const choices: Array<{ id: EditorPreference; label: string; description: string }> = [
    { id: 'vscode', label: 'Visual Studio Code', description: 'code --goto <file>' },
    { id: 'cursor', label: 'Cursor', description: 'cursor --goto <file>' },
    { id: 'zed', label: 'Zed', description: 'zed <file>' },
    { id: 'system', label: 'System default', description: 'Use the operating-system file handler' },
  ]

  return (
    <div className="fixed inset-0 z-[120] grid place-items-center bg-black/60 p-6 backdrop-blur-[2px]">
      <div className="w-full max-w-[430px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-2xl">
        <div className="flex h-11 items-center border-b border-[rgb(var(--border))] px-3">
          <FileCode2 size={13} className="mr-2 text-[rgb(var(--purple))]" />
          <div>
            <div className="text-[11px] font-semibold">Open files with…</div>
            <div className="max-w-[300px] truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{path}</div>
          </div>
          <button onClick={close} className="ml-auto grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"><X size={11} /></button>
        </div>
        <div className="space-y-1 p-2">
          {choices.map((choice) => (
            <button key={choice.id} onClick={() => setEditorPreference(choice.id)} className="flex w-full items-center gap-3 rounded-lg border border-transparent px-3 py-2.5 text-left hover:border-[rgb(var(--border))] hover:bg-[rgb(var(--panel-2))]">
              <span className="grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))]"><ExternalLink size={12} /></span>
              <span className="min-w-0">
                <span className="block text-[10px] font-medium">{choice.label}</span>
                <span className="mt-0.5 block text-[8px] text-[rgb(var(--muted-2))]">{choice.description}</span>
              </span>
            </button>
          ))}
        </div>
        <div className="border-t border-[rgb(var(--border))] px-3 py-2 text-[8px] text-[rgb(var(--muted-2))]">The browser prototype records this preference; the desktop integration will call the selected editor.</div>
      </div>
    </div>
  )
}

function HorizontalResizeHandle() {
  return (
    <PanelResizeHandle className="dock-resize group relative w-2 shrink-0 cursor-col-resize">
      <div className="absolute left-1/2 top-1/2 h-9 w-px -translate-x-1/2 -translate-y-1/2 bg-[rgb(var(--border-strong))] opacity-0 transition-opacity group-hover:opacity-100" />
    </PanelResizeHandle>
  )
}

export function BottomWorkspace() {
  const rightPanels = useBonsaiStore((state) => state.rightPanels)

  return (
    <>
      <PanelGroup autoSaveId="bonsai-bottom-panels-v1" direction="horizontal" className="bottom-workspace h-full min-h-0 p-2 pt-1">
        <Panel id="branches" order={1} defaultSize={14} minSize={9} maxSize={26}>
          <BranchSidebar />
        </Panel>
        <HorizontalResizeHandle />
        <Panel id="runtime" order={2} defaultSize={50} minSize={26}>
          <RuntimeWorkspace />
        </Panel>
        {rightPanels.files && (
          <>
            <HorizontalResizeHandle />
            <Panel id="files" order={3} defaultSize={18} minSize={13} maxSize={36}>
              <FilesDiffPanel />
            </Panel>
          </>
        )}
        {rightPanels.prs && (
          <>
            <HorizontalResizeHandle />
            <Panel id="prs" order={4} defaultSize={18} minSize={14} maxSize={40}>
              <PullRequestsPanel />
            </Panel>
          </>
        )}
      </PanelGroup>
      <EditorPreferenceDialog />
    </>
  )
}
