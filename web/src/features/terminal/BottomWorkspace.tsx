import { usePullRequestCatalog } from '../github/usePullRequestCatalog'
import { PullRequestTabs } from '../github/PullRequestTabs'
import { ProviderBadge } from '../../components/ui/ProviderBadge'
import { memo, useEffect, useMemo, useRef, useState } from 'react'
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
import { flattenFiles, useFiles, useLocalDiff } from '../../api/files'
import { loadPullRequest } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'
import { useProjectWorktrees, useProjectAgents, useProjectProcesses } from '../../stores/projectSelectors'
import { panelPreferences } from '../../stores/panelPreferences'
import { StatusDot } from '../branches/BranchesIsland'
import { useOpenRuntimeEntries, type RuntimeEntry } from './openRuntimeEntries'
import type { Agent, EditorPreference, Process, PullRequest, RepoFile, Worktree } from '../../types'
import { AgentTerminal } from './AgentTerminal'
import { ProcessTerminal } from './ProcessTerminal'

function SortableRuntimeTile({ runtime }: { runtime: RuntimeEntry }) {
  const [terminalActions, setTerminalActions] = useState<HTMLDivElement | null>(null)
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
        className="runtime-heading flex h-[26px] shrink-0 cursor-grab items-center gap-2 px-2 active:cursor-grabbing"
        title="Drag runtime card"
      >
        <GripVertical size={9} className="shrink-0 text-muted-2" />
        {runtime.type === 'agent' ? (
          <ProviderBadge provider={runtime.agent.provider} size={18} />
        ) : (
          <span className="grid h-[18px] w-[18px] shrink-0 place-items-center rounded-[5px] bg-panel-4 text-muted"><TerminalSquare size={10} /></span>
        )}
        <div className="flex min-w-0 flex-1 items-baseline gap-2">
          <span className="shrink-0 truncate text-[12px] font-semibold text-text">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</span>
          <span className="min-w-0 truncate font-mono text-[10px] text-muted-2">
            {runtime.type === 'agent'
              ? runtime.agent.profileName ?? (runtime.agent.model + ' · ' + runtime.agent.reasoningEffort + (runtime.agent.fastMode ? ' · Fast' : ''))
              : runtime.process.command}
          </span>
        </div>
        {runtime.type === 'agent' && <div ref={setTerminalActions} className="flex shrink-0 items-center gap-2" onPointerDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()} />}
        <StatusDot status={runtime.type === 'agent' ? runtime.agent.state : runtime.process.status} />
        <button
          type="button"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation()
            closeRuntime(runtime.id)
          }}
          className="bonsai-focus icon-btn-20 shrink-0 hover:text-text"
          title="Close runtime card"
          aria-label="Close runtime card"
        >
          <X size={9} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {runtime.type === 'agent' ? (
          <AgentTerminal agent={runtime.agent} actionsHost={terminalActions} />
        ) : (
          <ProcessTerminal process={runtime.process} />
        )}
      </div>
    </section>
  )
}

export function RuntimeWorkspace() {
  const hostRef = useRef<HTMLElement | null>(null)
  const [wideHeader, setWideHeader] = useState(false)
  const dockRuntimeId = useBonsaiStore((state) => state.dockRuntimeId)
  const openRuntimeIds = useBonsaiStore((state) => state.openRuntimeIds)
  const openRuntime = useBonsaiStore((state) => state.openRuntime)
  const focusRuntime = useBonsaiStore(state => state.focusRuntime)
  const reorderOpenRuntime = useBonsaiStore((state) => state.reorderOpenRuntime)
  const rightPanels = useBonsaiStore((state) => state.rightPanels)
  const toggleRightPanel = useBonsaiStore((state) => state.toggleRightPanel)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }))
  const { worktree, available, openEntries } = useOpenRuntimeEntries()

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => setWideHeader(entry.contentRect.width >= 690))
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  const onDragEnd = (event: DragEndEvent) => {
    const active = String(event.active.id)
    const over = event.over?.id ? String(event.over.id) : ''
    if (over) reorderOpenRuntime(active, over)
  }

  const runtimeOptions = available.map((runtime) => ({
    value: runtime.id,
    label: runtime.type === 'agent' ? runtime.agent.name : runtime.process.name,
    description: runtime.type === 'agent' ? runtime.agent.provider + ' · ' + (runtime.agent.profileName ?? runtime.agent.model) : runtime.process.command,
    meta: openRuntimeIds.includes(runtime.id) ? 'open' : undefined,
  }))

  return (
    <section ref={hostRef} className="island dock-pane flex h-full min-h-0 min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-3">
        <TerminalSquare size={13} className="shrink-0 text-ok" />
        <span className="dock-title">Terminals</span>
        <span className="island-count">{openRuntimeIds.length}</span>
        {wideHeader ? (
          <>
            <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
              {openEntries.map((runtime) => (
                <button
                  key={runtime.id}
                  type="button"
                  onClick={() => focusRuntime(runtime.id)}
                  className={
                    'bonsai-focus flex h-6 min-w-0 max-w-[170px] items-center gap-1.5 rounded-md px-2 text-left text-[12px] ' +
                    (dockRuntimeId === runtime.id ? 'bg-panel-3 text-text' : 'text-muted hover:bg-panel-2 hover:text-text')
                  }
                >
                  {runtime.type === 'agent' ? (
                    <ProviderBadge provider={runtime.agent.provider} size={16} />
                  ) : <TerminalSquare size={12} className="shrink-0" />}
                  <span className="min-w-0 truncate">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</span>
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
          <button type="button" onClick={() => toggleRightPanel('prs')} title="Toggle pull requests" className={'bonsai-focus btn-ghost text-[11px] ' + (rightPanels.prs ? '!bg-accent/[.12] !text-text' : '')}>
            <GitPullRequest size={11} /> PRs
          </button>
          <span className="mx-0.5 h-4 w-px bg-border" />
          <button onClick={() => setDockState('collapsed')} className="bonsai-focus btn-ghost h-6 w-6 justify-center px-0" title="Minimize workspace"><Minus size={11} /></button>
          <button onClick={() => setDockState(dockState === 'maximized' ? 'normal' : 'maximized')} className="bonsai-focus btn-ghost h-6 w-6 justify-center px-0" title="Maximize workspace"><Maximize2 size={11} /></button>
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
            Open an agent terminal or managed process above. Generic shells are unavailable.
          </div>
        )}
      </div>
    </section>
  )
}

function checkIcon(status: 'success' | 'running' | 'failed') {
  if (status === 'success') return <CheckCircle2 size={11} className="text-[rgb(var(--ok))]" />
  if (status === 'failed') return <XCircle size={11} className="text-[rgb(var(--danger))]" />
  return <CircleDot size={11} className="text-[rgb(var(--warn))]" />
}

function PullRequestOperations({ pr }: { pr: PullRequest }) {
  const setStatus = useBonsaiStore((state) => state.setPullRequestStatus)
  return (
    <div className="flex flex-wrap gap-1.5">
      {pr.status === 'Open' && (
        <>
          <button disabled={!pr.mergeable} onClick={() => setStatus(pr.id, 'Merged')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--accent)/.35)] bg-[rgb(var(--accent)/.08)] px-2 text-[8px] text-[rgb(var(--accent))] disabled:opacity-35"><GitMerge size={9} /> Merge</button>
          <button onClick={() => setStatus(pr.id, 'Closed')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--danger)/.3)] px-2 text-[8px] text-[rgb(var(--danger))]"><X size={9} /> Close</button>
        </>
      )}
      {pr.status === 'Draft' && <button onClick={() => setStatus(pr.id, 'Open')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--accent)/.3)] px-2 text-[8px] text-[rgb(var(--accent))]"><GitPullRequest size={9} /> Open PR</button>}
      {pr.status === 'Closed' && <button onClick={() => setStatus(pr.id, 'Open')} className="flex h-6 items-center gap-1 rounded border border-[rgb(var(--accent)/.3)] px-2 text-[8px] text-[rgb(var(--accent))]"><GitPullRequest size={9} /> Reopen</button>}
    </div>
  )
}

function PullRequestDetails({ pr }: { pr: PullRequest }) {
  const project = useBonsaiStore(state => state.projects.find(item => item.id === state.activeProjectId))
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
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
          {pr.checks.map((check, index) => (
            <div key={check.id || `${check.name}:${index}`} className="flex items-center gap-1.5 rounded px-2 py-1.5 text-[8px] text-[rgb(var(--muted))]">
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
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const { rows: pullRequests, tab, setTab, message, retry } = usePullRequestCatalog(activeProjectId)
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
    if (selectedId) void loadPullRequest(selectedId)
  }, [selectedId])

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
    <aside className="island dock-pane flex h-full min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-3">
        <GitPullRequest size={13} className="shrink-0 text-[rgb(var(--accent))]" />
        <span className="dock-title min-w-0 truncate">Pull requests</span>
        <span className="island-count">{pullRequests.length}</span>
        <button onClick={() => setRightPanel('prs', false)} className="bonsai-focus ml-auto grid h-6 w-6 shrink-0 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-3))]" title="Close pull requests"><X size={12} /></button>
      </div>
      <PullRequestTabs value={tab} onChange={value => { setTab(value); if (value === 'closed') retry() }} />
      <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-[rgb(var(--border)/.5)] p-2">
        <label className="flex h-7 min-w-[96px] flex-1 items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
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
        {!filtered.length && <p className="p-3 text-[9px] text-[rgb(var(--muted-2))]">{message}</p>}
        {filtered.map((pr) => {
          const expanded = selectedId === pr.id
          const success = pr.checks.filter((check) => check.status === 'success').length
          return (
            <div key={pr.id} ref={expanded ? selectedRef : undefined} className="border-b border-[rgb(var(--border)/.55)]">
              <button type="button" onClick={() => setSelectedId(expanded ? null : pr.id)} className="flex w-full items-start gap-2 px-2.5 py-2.5 text-left hover:bg-[rgb(var(--panel-2))]">
                <GitPullRequest size={11} className={pr.status === 'Open' ? 'mt-0.5 text-[rgb(var(--accent))]' : pr.status === 'Draft' ? 'mt-0.5 text-[rgb(var(--muted))]' : 'mt-0.5 text-[rgb(var(--muted-2))]'} />
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
    <div className="fixed inset-0 z-[120] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <div className="w-full max-w-[430px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-2xl">
        <div className="flex h-11 items-center border-b border-[rgb(var(--border))] px-3">
          <FileCode2 size={13} className="mr-2 text-[rgb(var(--accent))]" />
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
        <div className="border-t border-[rgb(var(--border))] px-3 py-2 text-[8px] text-[rgb(var(--muted-2))]">This saves your editor preference. Opening an external editor is unavailable in the connected app.</div>
      </div>
    </div>
  )
}

function HorizontalResizeHandle() {
  return (
    <PanelResizeHandle className="dock-resize group relative w-3 shrink-0 cursor-col-resize">
      <div className="absolute left-1/2 top-1/2 h-9 w-px -translate-x-1/2 -translate-y-1/2 bg-[rgb(var(--border-strong))] opacity-0 transition-opacity group-hover:opacity-100" />
    </PanelResizeHandle>
  )
}

export function BottomWorkspace() {
  const rightPanels = useBonsaiStore((state) => state.rightPanels)
  const panelIds = ['runtime', ...(rightPanels.prs ? ['prs'] : [])]

  return (
    <>
      <PanelGroup autoSaveId="bonsai-bottom-panels-v1" storage={panelPreferences.storage} onLayout={layout => panelPreferences.remember(panelIds, layout)} direction="horizontal" className="bottom-workspace h-full min-h-0">
        <Panel id="runtime" order={1} defaultSize={rightPanels.prs ? 82 : 100} minSize={26}>
          <RuntimeWorkspace />
        </Panel>
        {rightPanels.prs && (
          <>
            <HorizontalResizeHandle />
            <Panel id="prs" order={2} defaultSize={18} minSize={14} maxSize={40}>
              <PullRequestsPanel />
            </Panel>
          </>
        )}
      </PanelGroup>
      <EditorPreferenceDialog />
    </>
  )
}
