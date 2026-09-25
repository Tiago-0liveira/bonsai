import { useEffect, useMemo, useState } from 'react'
import * as Tabs from '@radix-ui/react-tabs'
import {
  Bot,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  CircleDot,
  ExternalLink,
  FileCode2,
  Files,
  GitBranch,
  GitPullRequest,
  Maximize2,
  Minus,
  PanelRight,
  Search,
  TerminalSquare,
  X,
  XCircle,
} from 'lucide-react'
import { flattenFiles, repoFiles } from '../../mock/files'
import { processes } from '../../mock/processes'
import { pullRequests } from '../../mock/pullRequests'
import { useBonsaiStore } from '../../stores/bonsai'
import type { Agent, PullRequest, Worktree } from '../../types'
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
  if (visited.has(worktree.id)) return null
  const nextVisited = new Set(visited)
  nextVisited.add(worktree.id)
  const children = allWorktrees.filter(
    (item) => item.id !== worktree.id && item.mergeTargetBranch === worktree.branch,
  )
  const branchAgents = agents.filter((agent) => agent.worktreeId === worktree.id)

  return (
    <div>
      <button
        type="button"
        onClick={() => onWorktree(worktree)}
        style={{ paddingLeft: 8 + depth * 14 }}
        className={
          'bonsai-focus flex h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[10px] ' +
          (selectedId === worktree.id
            ? 'bg-[rgb(var(--purple)/.11)] text-[rgb(var(--text))]'
            : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
        }
      >
        <GitBranch size={11} className="shrink-0" />
        <span className="min-w-0 flex-1 truncate font-mono">{worktree.branch}</span>
        <StatusDot status={worktree.status} />
      </button>
      {branchAgents.map((agent) => (
        <button
          type="button"
          key={agent.id}
          onClick={() => onAgent(agent)}
          style={{ paddingLeft: 23 + depth * 14 }}
          className="bonsai-focus flex h-6 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[9px] text-[rgb(var(--muted-2))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
        >
          <Bot size={10} />
          <span className="min-w-0 flex-1 truncate">{agent.name}</span>
          <StatusDot status={agent.state} />
        </button>
      ))}
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
    </div>
  )
}

function BranchSidebar() {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const setDockWorktreeId = useBonsaiStore((state) => state.setDockWorktreeId)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const defaultWorktree = projectWorktrees.find((item) => item.branch === project?.defaultBranch)
  const roots = projectWorktrees.filter(
    (item) => item.branch !== project?.defaultBranch && item.mergeTargetBranch === project?.defaultBranch,
  )

  const selectWorktree = (worktree: Worktree) => {
    setDockWorktreeId(worktree.id)
    setSelection({ type: 'worktree', id: worktree.id })
  }

  const selectAgent = (agent: Agent) => {
    setDockWorktreeId(agent.worktreeId)
    setSelection({ type: 'agent', id: agent.id })
    openTerminal(agent.id)
  }

  return (
    <aside className="flex w-[214px] shrink-0 flex-col border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="flex h-9 shrink-0 items-center border-b border-[rgb(var(--border))] px-2.5">
        <GitBranch size={12} className="mr-1.5 text-[rgb(var(--muted))]" />
        <span className="text-[10px] font-semibold">Branches</span>
        <span className="ml-auto text-[9px] text-[rgb(var(--muted-2))]">{projectWorktrees.length}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {defaultWorktree && (
          <button
            type="button"
            onClick={() => selectWorktree(defaultWorktree)}
            className={
              'bonsai-focus mb-1 flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left text-[10px] ' +
              (dockWorktreeId === defaultWorktree.id
                ? 'bg-[rgb(var(--green)/.08)] text-[rgb(var(--text))]'
                : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
            }
          >
            <GitBranch size={11} className="text-[rgb(var(--green))]" />
            <span className="min-w-0 flex-1 truncate font-mono">{defaultWorktree.branch}</span>
            <span className="rounded bg-[rgb(var(--green)/.10)] px-1 py-0.5 text-[7px] text-[rgb(var(--green))]">default</span>
          </button>
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
            onWorktree={selectWorktree}
            onAgent={selectAgent}
          />
        ))}
      </div>
      <div className="border-t border-[rgb(var(--border))] px-2.5 py-2 text-[8px] text-[rgb(var(--muted-2))]">
        Branch hierarchy follows merge targets.
      </div>
    </aside>
  )
}

function RuntimeWorkspace() {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const setDockWorktreeId = useBonsaiStore((state) => state.setDockWorktreeId)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const setActiveTerminalId = useBonsaiStore((state) => state.setActiveTerminalId)
  const rightPanels = useBonsaiStore((state) => state.rightPanels)
  const toggleRightPanel = useBonsaiStore((state) => state.toggleRightPanel)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)

  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const fallbackWorktree = projectWorktrees.find((item) => item.branch !== project?.defaultBranch) ?? projectWorktrees[0]
  const worktree = projectWorktrees.find((item) => item.id === dockWorktreeId) ?? fallbackWorktree
  const worktreeAgents = agents.filter((item) => item.worktreeId === worktree?.id)
  const worktreeProcesses = processes.filter((item) => item.worktreeId === worktree?.id)
  const runtimes = [
    ...worktreeAgents.map((agent) => ({ id: agent.id, type: 'agent' as const, label: agent.name, status: agent.state })),
    ...worktreeProcesses.map((process) => ({ id: process.id, type: 'process' as const, label: process.name, status: process.status })),
  ]
  const [activeRuntimeId, setActiveRuntimeId] = useState(runtimes[0]?.id ?? '')

  useEffect(() => {
    if (worktree && worktree.id !== dockWorktreeId) setDockWorktreeId(worktree.id)
  }, [dockWorktreeId, setDockWorktreeId, worktree])

  useEffect(() => {
    const next = runtimes.find((item) => item.status === 'running' || item.status === 'healthy') ?? runtimes[0]
    setActiveRuntimeId(next?.id ?? '')
  }, [worktree?.id])

  const activeAgent = worktreeAgents.find((item) => item.id === activeRuntimeId)
  const activeProcess = worktreeProcesses.find((item) => item.id === activeRuntimeId)

  useEffect(() => {
    if (activeAgent) setActiveTerminalId(activeAgent.terminalId)
  }, [activeAgent, setActiveTerminalId])

  const selectRuntime = (runtime: (typeof runtimes)[number]) => {
    setActiveRuntimeId(runtime.id)
    const agent = worktreeAgents.find((item) => item.id === runtime.id)
    if (agent) openTerminal(agent.id)
  }

  return (
    <section className="flex min-w-[300px] flex-1 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-9 shrink-0 items-center border-b border-[rgb(var(--border))] px-2">
        <div className="mr-2 min-w-0">
          <div className="max-w-44 truncate font-mono text-[9px] text-[rgb(var(--muted-2))]">{worktree?.branch ?? 'No worktree'}</div>
        </div>
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {runtimes.map((runtime) => (
            <button
              type="button"
              key={runtime.id}
              onClick={() => selectRuntime(runtime)}
              className={
                'bonsai-focus flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-[9px] ' +
                (activeRuntimeId === runtime.id
                  ? 'bg-[rgb(var(--panel-2))] text-[rgb(var(--text))]'
                  : 'text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))]')
              }
            >
              {runtime.type === 'agent' ? <Bot size={10} /> : <TerminalSquare size={10} />}
              <StatusDot status={runtime.status} />
              {runtime.label}
            </button>
          ))}
          {!runtimes.length && <span className="px-2 text-[9px] text-[rgb(var(--muted-2))]">No agents or processes on this worktree.</span>}
        </div>
        <div className="ml-2 flex shrink-0 items-center gap-1">
          <button
            type="button"
            onClick={() => toggleRightPanel('files')}
            title="Toggle Files / Git Diff"
            className={
              'bonsai-focus flex h-6 items-center gap-1 rounded px-1.5 text-[9px] ' +
              (rightPanels.files ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
            }
          >
            <Files size={11} /> Files
          </button>
          <button
            type="button"
            onClick={() => toggleRightPanel('prs')}
            title="Toggle pull requests"
            className={
              'bonsai-focus flex h-6 items-center gap-1 rounded px-1.5 text-[9px] ' +
              (rightPanels.prs ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
            }
          >
            <GitPullRequest size={11} /> PRs
          </button>
          <span className="mx-0.5 h-4 w-px bg-[rgb(var(--border))]" />
          <button
            onClick={() => setDockState('collapsed')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Close bottom workspace"
          >
            <Minus size={12} />
          </button>
          <button
            onClick={() => setDockState(dockState === 'maximized' ? 'normal' : 'maximized')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Maximize bottom workspace"
          >
            <Maximize2 size={12} />
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1">
        {activeAgent ? (
          <FakeTerminal />
        ) : activeProcess ? (
          <div className="h-full overflow-auto bg-[#0c0e11] p-3 font-mono text-[10px] leading-5 text-[rgb(var(--muted))]">
            <div className="text-[rgb(var(--green))]">$ {activeProcess.command}</div>
            <div>[bonsai] process: {activeProcess.name}</div>
            <div>[bonsai] status: {activeProcess.status}</div>
            {activeProcess.port && <div>[bonsai] listening on http://localhost:{activeProcess.port}</div>}
            <div className="mt-2 text-[rgb(var(--muted-2))]">Process output is simulated in this frontend prototype.</div>
          </div>
        ) : (
          <div className="grid h-full place-items-center text-[10px] text-[rgb(var(--muted-2))]">
            Select a worktree with an agent or process.
          </div>
        )}
      </div>
    </section>
  )
}

function FilesDiffPanel() {
  const setRightPanel = useBonsaiStore((state) => state.setRightPanel)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const selectedFilePath = useBonsaiStore((state) => state.selectedFilePath)
  const setSelectedFilePath = useBonsaiStore((state) => state.setSelectedFilePath)
  const worktree = worktrees.find((item) => item.id === dockWorktreeId)
  const pr = pullRequests.find((item) => item.number === worktree?.prNumber)
  const files = flattenFiles(repoFiles).filter((item) => item.type === 'file')
  const selectedFile = files.find((item) => item.path === selectedFilePath) ?? files[0]

  return (
    <aside className="flex w-[330px] min-w-[270px] max-w-[36vw] shrink-0 flex-col border-l border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="flex h-9 shrink-0 items-center border-b border-[rgb(var(--border))] px-2.5">
        <FileCode2 size={12} className="mr-1.5 text-[rgb(var(--muted))]" />
        <span className="min-w-0 flex-1 truncate text-[10px] font-semibold">Files / Git Diff</span>
        <span className="mr-2 max-w-28 truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{worktree?.branch}</span>
        <button
          onClick={() => setRightPanel('files', false)}
          className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
          title="Close Files / Git Diff"
        >
          <X size={11} />
        </button>
      </div>

      <Tabs.Root defaultValue="files" className="flex min-h-0 flex-1 flex-col">
        <Tabs.List className="flex h-8 shrink-0 border-b border-[rgb(var(--border))] px-2">
          <Tabs.Trigger value="files" className="relative px-2 text-[9px] text-[rgb(var(--muted))] data-[state=active]:text-[rgb(var(--text))] data-[state=active]:after:absolute data-[state=active]:after:bottom-0 data-[state=active]:after:left-2 data-[state=active]:after:right-2 data-[state=active]:after:h-px data-[state=active]:after:bg-[rgb(var(--purple))]">Files</Tabs.Trigger>
          <Tabs.Trigger value="diff" className="relative px-2 text-[9px] text-[rgb(var(--muted))] data-[state=active]:text-[rgb(var(--text))] data-[state=active]:after:absolute data-[state=active]:after:bottom-0 data-[state=active]:after:left-2 data-[state=active]:after:right-2 data-[state=active]:after:h-px data-[state=active]:after:bg-[rgb(var(--purple))]">Git Diff</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="files" className="grid min-h-0 flex-1 grid-rows-[auto_1fr] outline-none">
          <div className="max-h-28 overflow-auto border-b border-[rgb(var(--border))] p-1.5">
            {files.map((file) => (
              <button
                type="button"
                key={file.id}
                onClick={() => setSelectedFilePath(file.path)}
                className={
                  'flex w-full items-center gap-1.5 rounded px-2 py-1 text-left font-mono text-[9px] ' +
                  (selectedFile?.id === file.id ? 'bg-[rgb(var(--purple)/.10)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
                }
              >
                <FileCode2 size={9} />
                <span className="truncate">{file.path}</span>
              </button>
            ))}
          </div>
          <pre className="min-h-0 overflow-auto whitespace-pre-wrap p-3 font-mono text-[9px] leading-4 text-[rgb(var(--muted))]">
            {selectedFile?.content ?? 'No file selected.'}
          </pre>
        </Tabs.Content>
        <Tabs.Content value="diff" className="min-h-0 flex-1 overflow-auto p-2.5 outline-none">
          {pr?.files.map((file) => (
            <div key={file.path} className="mb-2 overflow-hidden rounded-md border border-[rgb(var(--border))]">
              <div className="flex items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1.5 font-mono text-[8px]">
                <span className="min-w-0 flex-1 truncate">{file.path}</span>
                <span className="text-[rgb(var(--green))]">+{file.additions}</span>
                <span className="ml-1 text-[rgb(var(--red))]">-{file.deletions}</span>
              </div>
              <pre className="overflow-auto p-2 font-mono text-[8px] leading-4 text-[rgb(var(--muted))]">{file.diff.join('\n')}</pre>
            </div>
          )) ?? (
            <div className="rounded-md border border-dashed border-[rgb(var(--border))] p-4 text-center text-[9px] text-[rgb(var(--muted-2))]">
              No PR diff linked to this worktree.
            </div>
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

function PullRequestDetails({ pr }: { pr: PullRequest }) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const project = projects.find((item) => item.id === activeProjectId)
  const [checksOpen, setChecksOpen] = useState(true)
  const success = pr.checks.filter((check) => check.status === 'success').length

  return (
    <div className="border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.55)] p-2.5">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="text-[10px] font-medium">{pr.title}</div>
          <div className="mt-1 truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{pr.branch} → {pr.base}</div>
        </div>
        <button
          type="button"
          onClick={() => {
            if (project?.repository.includes('/')) {
              window.open('https://github.com/' + project.repository + '/pull/' + pr.number, '_blank', 'noopener,noreferrer')
            }
          }}
          title="Open pull request URL"
          className="bonsai-focus grid h-6 w-6 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
        >
          <ExternalLink size={10} />
        </button>
      </div>

      <button
        type="button"
        onClick={() => setChecksOpen((open) => !open)}
        className="mt-2 flex h-7 w-full items-center gap-1.5 rounded-md border border-[rgb(var(--border))] px-2 text-left text-[9px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
      >
        {checksOpen ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
        CI checks
        <span className="ml-auto">{success}/{pr.checks.length}</span>
      </button>
      {checksOpen && (
        <div className="mt-1.5 space-y-1">
          {pr.checks.map((check) => (
            <div key={check.name} className="flex items-center gap-1.5 rounded px-2 py-1.5 text-[8px] text-[rgb(var(--muted))]">
              {checkIcon(check.status)}
              <span className="min-w-0 flex-1 truncate">{check.name}</span>
              <span className="capitalize text-[rgb(var(--muted-2))]">{check.status}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function PullRequestsPanel() {
  const setRightPanel = useBonsaiStore((state) => state.setRightPanel)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<'all' | 'open' | 'draft' | 'closed'>('all')
  const [selectedId, setSelectedId] = useState<string | null>(pullRequests[0]?.id ?? null)
  const filtered = useMemo(
    () =>
      pullRequests.filter((pr) => {
        const matchesQuery =
          !query.trim() ||
          pr.title.toLowerCase().includes(query.toLowerCase()) ||
          pr.branch.toLowerCase().includes(query.toLowerCase()) ||
          String(pr.number).includes(query)
        const matchesFilter =
          filter === 'all' ||
          (filter === 'open' && pr.status === 'Open') ||
          (filter === 'draft' && pr.status === 'Draft') ||
          (filter === 'closed' && (pr.status === 'Closed' || pr.status === 'Merged'))
        return matchesQuery && matchesFilter
      }),
    [filter, query],
  )

  return (
    <aside className="flex w-[350px] min-w-[285px] max-w-[38vw] shrink-0 flex-col border-l border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="flex h-9 shrink-0 items-center border-b border-[rgb(var(--border))] px-2.5">
        <GitPullRequest size={12} className="mr-1.5 text-[rgb(var(--muted))]" />
        <span className="text-[10px] font-semibold">Pull Requests</span>
        <span className="ml-1.5 text-[8px] text-[rgb(var(--muted-2))]">repository</span>
        <button
          onClick={() => setRightPanel('prs', false)}
          className="bonsai-focus ml-auto grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
          title="Close pull requests"
        >
          <X size={11} />
        </button>
      </div>
      <div className="flex shrink-0 gap-1.5 border-b border-[rgb(var(--border))] p-2">
        <label className="flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
          <Search size={10} className="text-[rgb(var(--muted-2))]" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search PRs"
            className="min-w-0 flex-1 bg-transparent text-[9px] outline-none placeholder:text-[rgb(var(--muted-2))]"
          />
        </label>
        <select
          value={filter}
          onChange={(event) => setFilter(event.target.value as typeof filter)}
          className="bonsai-focus h-7 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-1.5 text-[8px] outline-none"
        >
          <option value="all">All</option>
          <option value="open">Open</option>
          <option value="draft">Draft</option>
          <option value="closed">Closed</option>
        </select>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {filtered.map((pr) => {
          const expanded = selectedId === pr.id
          const success = pr.checks.filter((check) => check.status === 'success').length
          return (
            <div key={pr.id} className="border-b border-[rgb(var(--border))]">
              <button
                type="button"
                onClick={() => setSelectedId(expanded ? null : pr.id)}
                className="flex w-full items-start gap-2 px-2.5 py-2.5 text-left hover:bg-[rgb(var(--panel-2))]"
              >
                <GitPullRequest size={11} className={pr.status === 'Open' ? 'mt-0.5 text-[rgb(var(--green))]' : 'mt-0.5 text-[rgb(var(--muted-2))]'} />
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
        {!filtered.length && (
          <div className="p-5 text-center text-[9px] text-[rgb(var(--muted-2))]">No pull requests match this filter.</div>
        )}
      </div>
    </aside>
  )
}

export function BottomWorkspace() {
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const rightPanels = useBonsaiStore((state) => state.rightPanels)
  const toggleRightPanel = useBonsaiStore((state) => state.toggleRightPanel)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const selectedWorktree = worktrees.find((item) => item.id === dockWorktreeId)

  if (dockState === 'collapsed') {
    return (
      <div className="flex h-full items-center border-t border-[rgb(var(--border))] bg-[rgb(var(--panel))] px-2">
        <GitBranch size={11} className="mr-1.5 text-[rgb(var(--muted))]" />
        <span className="max-w-48 truncate font-mono text-[9px] text-[rgb(var(--muted))]">{selectedWorktree?.branch ?? 'Workspace'}</span>
        <div className="ml-auto flex items-center gap-1">
          <button
            onClick={() => toggleRightPanel('files')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Toggle Files / Git Diff"
          >
            <Files size={11} />
          </button>
          <button
            onClick={() => toggleRightPanel('prs')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Toggle pull requests"
          >
            <PanelRight size={11} />
          </button>
          <button
            onClick={() => setDockState('normal')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Open bottom workspace"
          >
            <ChevronUp size={12} />
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 bg-[rgb(var(--panel))]">
      <BranchSidebar />
      <RuntimeWorkspace />
      {rightPanels.files && <FilesDiffPanel />}
      {rightPanels.prs && <PullRequestsPanel />}
    </div>
  )
}
