import { processActive } from '../../stores/processProjection'
import { openGitHub } from '../../api/git'
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import * as ScrollArea from '@radix-ui/react-scroll-area'
import * as Tabs from '@radix-ui/react-tabs'
import { useRouter } from '@tanstack/react-router'
import {
  Archive, Bot, Check, CheckCircle2, ChevronRight, CircleDot, Clock3, Copy,
  ExternalLink, FileDiff, GitBranch, GitPullRequest, History, Layers3, LoaderCircle,
  Play, Plus, RotateCcw, Save, SlidersHorizontal, Square, TerminalSquare, Trash2, Undo2,
  AlertCircle, XCircle, type LucideIcon,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import { useProjectWorktrees, useProjectAgents, useProjectProcesses, useProjectPullRequests } from '../../stores/projectSelectors'
import { FilesPanel } from '../files/FilesPanel'
import { ProviderBadge } from '../../components/ui/ProviderBadge'
import { ProcessActions } from '../terminal/ProcessActions'
import type { Agent, AgentProvider, Process, PullRequest, SyncFreshnessState, Worktree } from '../../types'

const presentation = (agent: Agent) => agent.presentation ?? (agent.archived ? 'archived' : 'canvas')

function branchIssues(worktree: Worktree, pr?: PullRequest) {
  const issues: string[] = []
  if (worktree.gitState?.includes('conflict') || pr?.mergeable === false) issues.push('Resolve merge conflicts')
  const failed = pr ? pr.checks.filter((check) => check.status === 'failed').length : worktree.ciFailed
  if (failed || worktree.ciStatus === 'failed') issues.push(failed ? `${failed} failing check${failed === 1 ? '' : 's'}` : 'Checks failed')
  if (worktree.gitStatusError) issues.push('Git status unavailable: ' + worktree.gitStatusError)
  if (worktree.divergenceAvailable && worktree.behind) issues.push(`${worktree.behind} commit${worktree.behind === 1 ? '' : 's'} behind ${worktree.upstream || worktree.mergeTargetBranch}`)
  return issues
}

const FRESHNESS: Record<SyncFreshnessState, { dot: string; text: string; label: string }> = {
  ready: { dot: 'bg-accent-solid', text: 'text-muted-2', label: 'live' },
  stale: { dot: 'bg-warn-solid', text: 'text-muted-2', label: 'stale' },
  loading: { dot: 'bg-muted-2', text: 'text-muted-2', label: 'syncing' },
  error: { dot: 'bg-danger', text: 'text-muted-2', label: 'offline' },
  unavailable: { dot: 'bg-danger', text: 'text-muted-2', label: 'offline' },
}

function FreshnessLabel({ state }: { state?: SyncFreshnessState }) {
  if (!state) return null
  const view = FRESHNESS[state]
  return <span className={'ml-auto flex items-center gap-1.5 font-mono text-[10px] normal-case tracking-normal ' + view.text}><span className={'h-1.5 w-1.5 rounded-full ' + view.dot} />{view.label}</span>
}

const tabTrigger = 'bonsai-focus h-full uppercase text-muted transition-colors hover:text-text data-[state=active]:text-text data-[state=active]:shadow-[inset_0_-2px_0_0_rgb(var(--accent-solid))]'

function Section({ title, meta, children }: { title: string; meta?: ReactNode; children: ReactNode }) {
  return <section className="inspector-section">
    <div className="mb-2.5 flex items-center justify-between gap-2"><h3 className="font-mono text-[9.5px] font-medium uppercase tracking-[.08em] text-muted-2">{title}</h3>{meta}</div>
    {children}
  </section>
}

function QuickButton({ icon: Icon, label, onClick, danger = false, primary = false, unavailable = false }: { icon: LucideIcon; label: string; onClick: () => void; danger?: boolean; primary?: boolean; unavailable?: boolean }) {
  return <button type="button" onClick={onClick} disabled={unavailable} title={unavailable ? 'Unavailable in the connected app' : undefined} className={'disabled:opacity-40 bonsai-focus inspector-action ' + (danger ? 'btn-danger-tint' : primary ? 'btn-primary' : 'btn-bordered')}>
    <Icon size={13} className="shrink-0" />{label}
  </button>
}

function Metric({ value, label, icon: Icon, tone = '' }: { value: number | string; label: string; icon: LucideIcon; tone?: string }) {
  return <div className="min-w-0 rounded-[10px] border border-border bg-panel-2 px-2.5 py-2">
    <div className={'flex items-center gap-1.5 font-mono text-[13px] font-semibold tabular-nums ' + (tone || 'text-text')}><Icon size={12} className="opacity-70" />{value}</div>
    <div className="mt-1 font-mono text-[9.5px] uppercase tracking-[.06em] text-muted-2">{label}</div>
  </div>
}

const PR_CHIP: Record<PullRequest['status'], { label: string; className: string }> = {
  Open: { label: '● OPEN', className: 'text-accent' },
  Draft: { label: 'DRAFT', className: 'text-muted' },
  Merged: { label: '● MERGED', className: 'text-ok' },
  Closed: { label: 'CLOSED', className: 'text-muted-2' },
}

const card = 'rounded-xl border border-border bg-panel-2'
const cardLabel = 'font-mono text-[10px] font-medium uppercase tracking-[.08em] text-muted-2'

function StatTile({ label, children }: { label: string; children: ReactNode }) {
  return <div className="min-w-0 rounded-xl border border-border bg-panel-2 px-3 py-2.5">
    <div className={cardLabel}>{label}</div>
    <div className="mt-1 flex items-baseline gap-2 font-mono text-[15px] font-semibold tabular-nums">{children}</div>
  </div>
}

function IconButton({ icon: Icon, label, onClick }: { icon: LucideIcon; label: string; onClick: () => void }) {
  return <button type="button" aria-label={label} title={label} onClick={onClick} className="bonsai-focus btn-ghost h-7 w-7 justify-center px-0"><Icon size={14} /></button>
}

function PullRequestCard({ pr, onOpen }: { pr?: PullRequest; onOpen: () => void }) {
  if (!pr) return <div className={card + ' p-3.5'}><div className={cardLabel}>Pull request</div><p className="mt-2 text-[12px] text-muted-2">No pull request linked to this branch.</p></div>
  const chip = PR_CHIP[pr.status]
  const conflicts = pr.mergeable === false
  return <button type="button" onClick={onOpen} title="Open in GitHub tab" className={'bonsai-focus block w-full p-3.5 text-left transition-colors hover:border-border-strong ' + card}>
    <span className="flex items-center justify-between gap-2"><span className={cardLabel}>Pull request</span><span className={'font-mono text-[10px] font-medium tracking-[.08em] ' + chip.className}>{chip.label}</span></span>
    <span className="mt-3 flex items-start gap-2.5">
      <GitPullRequest size={16} className="mt-0.5 shrink-0 text-accent" />
      <span className="min-w-0 flex-1 text-[13.5px] font-semibold leading-5 text-text"><span className="font-mono text-accent">#{pr.number}</span> {pr.title}</span>
    </span>
    <span className="mt-1.5 block truncate pl-[26px] font-mono text-[11px] text-muted-2">{pr.branch} → {pr.base}</span>
    <span className="mt-3 flex items-center gap-2 border-t border-border-subtle pt-3 font-mono text-[11px] text-muted-2">
      <span>{pr.files.length} {pr.files.length === 1 ? 'file' : 'files'} · {pr.commits.length} {pr.commits.length === 1 ? 'commit' : 'commits'}</span>
      <span className={'ml-auto flex items-center gap-1.5 ' + (conflicts ? 'text-danger' : 'text-ok')}>{conflicts ? <XCircle size={13} /> : <CheckCircle2 size={13} />}{conflicts ? 'conflicts' : 'no conflicts'}</span>
    </span>
  </button>
}

function CiCard({ checks }: { checks: PullRequest['checks'] }) {
  if (!checks.length) return null
  const success = checks.filter(check => check.status === 'success').length
  const state = checks.some(check => check.status === 'failed') ? 'failing' : success === checks.length ? 'passing' : 'running'
  const tone = { failing: 'text-danger', passing: 'text-ok', running: 'text-accent' }[state]
  const segment = { success: 'bg-ok', running: 'bg-accent-solid', failed: 'bg-danger' }
  return <div className={card + ' p-3.5'}>
    <div className="flex items-center justify-between">
      <span className={cardLabel}>CI/CD</span>
      <span className={'font-mono text-[10px] font-medium uppercase tracking-[.08em] ' + tone}>{state} · {success} of {checks.length}</span>
    </div>
    <div className="mt-2.5 flex gap-1.5" aria-hidden>{checks.map((check, index) => <span key={check.id || `${check.name}:${index}`} className={'h-1 flex-1 rounded-full ' + segment[check.status]} />)}</div>
    <div className="mt-3.5 space-y-2.5">{checks.map((check, index) => <div key={check.id || `${check.name}:${index}`} className="flex items-center gap-2.5 text-[13px]">
      {check.status === 'failed' ? <XCircle size={15} className="shrink-0 text-danger" /> : check.status === 'success' ? <CheckCircle2 size={15} className="shrink-0 text-ok" /> : <LoaderCircle size={15} className="shrink-0 animate-spin text-accent motion-reduce:animate-none" />}
      <span className="min-w-0 flex-1 break-words font-mono text-text">{check.name}</span>
      <span className={'font-mono text-[11px] ' + (check.status === 'running' ? 'text-accent' : check.status === 'failed' ? 'text-danger' : 'text-muted-2')}>{check.status === 'success' ? 'passed' : check.status === 'failed' ? 'failed' : 'running'}</span>
    </div>)}</div>
  </div>
}

function ProcessDot({ process }: { process: Pick<Process, 'lifecycleStatus'> }) {
  const status = process.lifecycleStatus
  if (status === 'running') return <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent-solid" />
  if (status === 'failed' || status === 'lost') return <XCircle size={13} aria-hidden className="shrink-0 text-warn" />
  if (status === 'starting' || status === 'backoff' || status === 'stopping') return <LoaderCircle size={13} aria-hidden className="shrink-0 animate-spin text-accent motion-reduce:animate-none" />
  return <CheckCircle2 size={13} aria-hidden className="shrink-0 text-muted-2" />
}

function RuntimeCard({ processes, agents, onProcess, onAgent, onStartProcess, onStartAgent }: {
  processes: Process[]; agents: Agent[]
  onProcess: (id: string) => void; onAgent: (id: string) => void
  onStartProcess: () => void; onStartAgent: () => void
}) {
  const head = (label: string, count: number, action: string, onAdd: () => void) => <div className="mb-2 flex items-center gap-1.5">
    <span className={cardLabel}>{label}</span>
    <span className="ml-auto font-mono text-[10px] text-muted-2">{count}</span>
    <button type="button" aria-label={action} title={action} onClick={onAdd} className="bonsai-focus btn-ghost h-5 w-5 justify-center px-0"><Plus size={12} /></button>
  </div>
  const row = 'bonsai-focus flex w-full items-center gap-2.5 rounded-md py-1.5 text-left transition-colors hover:bg-panel-3'
  return <div className={card + ' grid grid-cols-2 divide-x divide-border-subtle'}>
    <div className="min-w-0 p-3.5">
      {head('Processes', processes.length, 'Start process', onStartProcess)}
      {processes.length ? processes.map((item) => <button type="button" key={item.id} onClick={() => onProcess(item.id)} className={row}>
        <ProcessDot process={item} />
        <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-text">{item.name}</span>
        <span className="shrink-0 font-mono text-[10.5px] text-muted-2">{item.port !== undefined && item.lifecycleStatus === 'running' ? ':' + item.port : item.lifecycleStatus}</span>
      </button>) : <p className="py-1.5 text-[12px] text-muted-2">No processes.</p>}
    </div>
    <div className="min-w-0 p-3.5">
      {head('Agents', agents.length, 'Start agent', onStartAgent)}
      {agents.length ? agents.map((item) => <button type="button" key={item.id} onClick={() => onAgent(item.id)} className={row}>
        <ProviderBadge provider={item.provider as AgentProvider} size={20} />
        <span className="min-w-0 flex-1 truncate text-[12.5px] text-text">{item.name}</span>
        {item.state === 'running'
          ? <span aria-label="running" className="h-2 w-2 shrink-0 rounded-full bg-accent-solid shadow-[0_0_0_3px_rgb(var(--accent-solid)/.2)]" />
          : item.state === 'idle'
            ? <span aria-label="waiting" className="h-2 w-2 shrink-0 rounded-full bg-warn-solid shadow-[0_0_0_3px_rgb(var(--warn-solid)/.25)]" />
            : <Check size={13} aria-label="finished" className="shrink-0 text-muted-2" />}
      </button>) : <p className="py-1.5 text-[12px] text-muted-2">No agent sessions.</p>}
    </div>
  </div>
}

function AgentRow({ agent, onClick }: { agent: Agent; onClick: () => void }) {
  return <button onClick={onClick} className="bonsai-focus inspector-link">
    <span className={'inspector-avatar ' + (agent.state === 'running' ? 'text-[rgb(var(--accent))]' : 'text-[rgb(var(--muted))]')}><Bot size={14} /></span>
    <span className="min-w-0 flex-1"><span className="block truncate text-[11px] font-medium">{agent.name}</span><span className="mt-0.5 block truncate text-[10px] text-[rgb(var(--muted))]">{agent.task}</span></span>
    <span className="shrink-0 text-right text-[9px] text-[rgb(var(--muted))]"><span className="block capitalize">{agent.lifecycleState ?? agent.state}</span><span className="mt-0.5 block text-[rgb(var(--muted-2))]">{agent.runtime}</span></span>
  </button>
}

export function Inspector() {
  const selection = useBonsaiStore((state) => state.selection)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useProjectWorktrees(activeProjectId)
  const tags = useBonsaiStore((state) => state.worktreeTags)
  const agents = useProjectAgents(activeProjectId)
  const processes = useProjectProcesses(activeProjectId)
  const inventory = useBonsaiStore(state => state.processes)
  const pullRequests = useProjectPullRequests(activeProjectId)
  const terminalLines = useBonsaiStore(state => {
    const selected = state.selection.type === 'agent' ? state.agents.find(agent => agent.id === state.selection.id) : undefined
    return selected ? state.terminalOutput[selected.terminalId] : undefined
  })
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const moveAgentToHistory = useBonsaiStore((state) => state.moveAgentToHistory)
  const restoreAgentFromHistory = useBonsaiStore((state) => state.restoreAgentFromHistory)
  const archiveAgent = useBonsaiStore((state) => state.archiveAgent)
  const restoreAgent = useBonsaiStore((state) => state.restoreAgent)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const openStartAgentDialog = useBonsaiStore((state) => state.openStartAgentDialog)
  const openStartProcessDialog = useBonsaiStore((state) => state.openStartProcessDialog)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const setWorktreeTag = useBonsaiStore((state) => state.setWorktreeTag)
  const setDeleteWorktreeId = useBonsaiStore(state => state.setDeleteWorktreeId)
  const worktreeGroups = useBonsaiStore(state => state.worktreeGroups[activeProjectId])
  const toggleAutomaticGroup = useBonsaiStore(state => state.toggleAutomaticGroup)
  const setWorktreeStackPreference = useBonsaiStore((state) => state.setWorktreeStackPreference)
  const setWorktreeMergeTarget = useBonsaiStore((state) => state.setWorktreeMergeTarget)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const inspectPullRequest = useBonsaiStore((state) => state.inspectPullRequest)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const router = useRouter({ warn: false })
  const [tagDraft, setTagDraft] = useState('')

  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined
  const process = selection.type === 'process' ? inventory.find(item => item.id === selection.id) : undefined
  const worktree = worktrees.find((item) => item.id === (selection.type === 'worktree' ? selection.id : agent?.worktreeId ?? process?.worktreeId))
  const project = useBonsaiStore(state => state.projects.find(item => item.id === (worktree?.projectId ?? activeProjectId)))
  const projectWorktrees = worktrees
  const projectAgents = useMemo(() => agents.filter(item => presentation(item) === 'canvas'), [agents])
  const branchAgents = projectAgents.filter((item) => item.worktreeId === worktree?.id)
  const branchProcesses = processes.filter((item) => item.worktreeId === worktree?.id)
  const freshness = useBonsaiStore(state => state.syncFreshness[activeProjectId]?.local?.state)
  const pr = pullRequests.find((item) => item.id === `${worktree?.projectId}:${worktree?.prNumber}`)
  const attention = useMemo(() => projectWorktrees.map((item) => ({ worktree: item, issues: branchIssues(item, pullRequests.find((request) => request.id === `${item.projectId}:${item.prNumber}`)) })).filter((item) => item.issues.length).sort((a, b) => b.issues.length - a.issues.length), [projectWorktrees, pullRequests])
  const issues = worktree ? branchIssues(worktree, pr) : []
  const output = agent ? (terminalLines ?? []).filter((line) => line.trim()).slice(-5) : []
  const automaticGroup = worktree ? (worktreeGroups ?? []).find(group => group.worktree_ids.includes(worktree.id)) : undefined
  const groupCollapsed = useBonsaiStore(state => automaticGroup
    ? !state.expandedAutomaticGroups.includes(automaticGroup.id)
    : Boolean(worktree && project && state.collapsedTagGroups.includes(project.id + ':' + worktree.tag)))

  useEffect(() => { setTagDraft(worktree?.tag ?? '') }, [worktree?.id, worktree?.tag])
  if (!project) return null

  const submitTag = (event: FormEvent) => { event.preventDefault(); if (worktree) setWorktreeTag(worktree.id, tagDraft) }
  const groupCount = automaticGroup?.worktree_ids.length ?? (worktree ? projectWorktrees.filter(item => item.tag === worktree.tag).length : 0)
  const mergeTargets = projectWorktrees.filter((item) => item.id !== worktree?.id).map((item) => item.branch)

  return (
    <aside aria-label="Inspector" className="island [overflow-wrap:anywhere] flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <Tabs.Root defaultValue="inspector" className="flex min-h-0 flex-1 flex-col">
        <div className="island-title shrink-0 gap-[22px]">
          <Tabs.List className="flex h-full items-center gap-[22px]">
            <Tabs.Trigger value="inspector" className={tabTrigger}>Inspector</Tabs.Trigger>
            <Tabs.Trigger value="files" className={tabTrigger}>Files</Tabs.Trigger>
          </Tabs.List>
          <FreshnessLabel state={freshness} />
        </div>
        <Tabs.Content value="inspector" className="flex min-h-0 flex-1 flex-col outline-none data-[state=inactive]:hidden">
      <ScrollArea.Root className="min-h-0 flex-1 overflow-hidden">
        <ScrollArea.Viewport className="inspector-viewport h-full w-full">
          <div className="space-y-4 p-4">
            {selection.type === 'project' && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--muted))]"><GitBranch size={12} /> Workspace overview</div>
                <h2 className="text-lg font-semibold tracking-tight">{project.name}</h2>
                <p className="mt-1 break-all text-[11px] text-[rgb(var(--muted))]">{project.repository}</p>
                <div className="mt-4 grid grid-cols-2 gap-2"><QuickButton icon={GitBranch} label="Worktree" onClick={() => setWorktreeDialogOpen(true)} /><QuickButton icon={Bot} label="Agent" primary onClick={() => openStartAgentDialog()} /><QuickButton icon={Play} label="Start process" onClick={() => openStartProcessDialog('', project.id)} /></div>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <Metric value={projectWorktrees.length} label="Branches" icon={GitBranch} />
                <Metric value={projectAgents.filter((item) => item.state === 'running').length} label="Running" icon={Bot} tone="text-accent" />
                <Metric value={projectWorktrees.reduce((total, item) => total + item.dirtyFiles, 0)} label="Changed files" icon={FileDiff} tone="text-warn" />
              </div>
              <Section title="Needs attention" meta={<span className="inspector-count">{attention.length}</span>}>
                {attention.length ? <div className="space-y-2">{attention.map(({ worktree: item, issues: reasons }) => <button key={item.id} onClick={() => setSelection({ type: 'worktree', id: item.id })} className="bonsai-focus inspector-attention">
                  <AlertCircle size={14} className="mt-0.5 shrink-0 text-[rgb(var(--warn))]" /><span className="min-w-0 flex-1"><span className="block truncate font-mono text-[10px] text-warn">{item.branch}</span><span className="mt-1 block text-[10px] leading-4 text-[rgb(var(--muted))]">{reasons.join(' · ')}</span></span><ChevronRight size={12} className="mt-0.5 shrink-0 text-[rgb(var(--muted))]" />
                </button>)}</div> : <p className="flex items-center gap-2 text-[11px] text-[rgb(var(--muted))]"><CheckCircle2 size={14} className="text-[rgb(var(--ok))]" />No branch blockers reported.</p>}
              </Section>
              <Section title="Agents" meta={<span className="inspector-count">{projectAgents.length}</span>}>
                {projectAgents.length ? projectAgents.map((item) => <AgentRow key={item.id} agent={item} onClick={() => setSelection({ type: 'agent', id: item.id })} />) : <p className="inspector-empty">No agent sessions.</p>}
              </Section>
            </>}

            {worktree && !agent && !process && <>
              <div className="flex items-start gap-3">
                <span className="grid h-12 w-12 shrink-0 place-items-center rounded-xl border border-accent-solid/40 bg-accent-solid/10 text-accent"><GitBranch size={22} /></span>
                <div className="min-w-0 flex-1 pt-0.5">
                  <div className="flex items-center gap-2">
                    <h2 className="min-w-0 truncate font-mono text-[16px] font-bold leading-6 text-text" title={worktree.branch}>{worktree.branch}</h2>
                    {worktree.tag && <span className="chip shrink-0 border border-accent/30 bg-accent-solid/14 text-accent">{worktree.tag}</span>}
                  </div>
                  <p className="mt-0.5 truncate font-mono text-[11px] text-muted-2" title={worktree.path}>from {worktree.mergeTargetBranch}{worktree.path ? ' · ' + worktree.path : ''}</p>
                </div>
                <div className="flex shrink-0 items-center gap-0.5 pt-0.5">
                  <IconButton icon={Copy} label="Copy branch name" onClick={() => { void navigator.clipboard?.writeText(worktree.branch); setNotice('Copied ' + worktree.branch) }} />
                  <IconButton icon={ExternalLink} label="Open on GitHub" onClick={() => openGitHub(pr ? 'pull/' + pr.number : 'tree/' + worktree.branch, worktree.projectId)} />
                </div>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <StatTile label="Sync">
                  {worktree.divergenceAvailable
                    ? <><span className="text-accent">↑{worktree.ahead}</span><span className={worktree.behind ? 'text-warn' : 'text-muted-2'}>↓{worktree.behind}</span></>
                    : <span className="text-muted-2">—</span>}
                </StatTile>
                <StatTile label="Changes">
                  {pr?.files.length
                    ? <><span className="text-accent">+{pr.files.reduce((total, file) => total + file.additions, 0)}</span><span className="text-danger">−{pr.files.reduce((total, file) => total + file.deletions, 0)}</span></>
                    : <span className="text-muted-2">—</span>}
                </StatTile>
                <StatTile label="Uncommitted">
                  <span className={worktree.dirtyFiles ? 'text-warn' : 'text-muted-2'}>{worktree.dirtyFiles} {worktree.dirtyFiles === 1 ? 'file' : 'files'}</span>
                </StatTile>
              </div>
              {issues.length > 0 && <div className="inspector-alert"><div className="mb-2 flex items-center gap-2 text-[11px] font-medium text-warn"><AlertCircle size={13} />Before merging</div>{issues.map((issue) => <p key={issue} className="mt-1 text-[11px] leading-5 text-muted">{issue}</p>)}</div>}
              {pr && <CiCard checks={pr.checks} />}
              <PullRequestCard pr={pr} onOpen={() => { if (pr) { inspectPullRequest(pr.id); void router?.navigate({ to: '/github' }) } }} />
              <RuntimeCard
                processes={branchProcesses}
                agents={branchAgents}
                onProcess={(id) => setSelection({ type: 'process', id })}
                onAgent={(id) => setSelection({ type: 'agent', id })}
                onStartProcess={() => openStartProcessDialog(worktree.id)}
                onStartAgent={() => openStartAgentDialog(worktree.id)}
              />
              <details key={worktree.id} className="inspector-settings"><summary className="bonsai-focus flex cursor-pointer items-center gap-2 text-[11px] font-medium"><SlidersHorizontal size={13} className="text-[rgb(var(--muted))]" />Branch settings<ChevronRight size={12} className="inspector-disclosure ml-auto" /></summary>
                <section className="mt-4">
                  <div className="mb-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Merge target</div>
                  <BonsaiSelect
                    ariaLabel="Merge target"
                    searchable
                    value={worktree.mergeTargetBranch}
                    onChange={(value) => setWorktreeMergeTarget(worktree.id, value)}
                    options={[project.defaultBranch, ...mergeTargets.filter((branch) => branch !== project.defaultBranch)].map((branch) => ({ value: branch, label: branch, description: branch === project.defaultBranch ? 'default branch' : 'worktree branch' }))}
                  />
                </section>

                <section className="mt-4">
                  <div className="mb-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Stacking</div>
                  <BonsaiSelect
                    ariaLabel="Stacking preference"
                    value={worktree.stackPreference ?? 'auto'}
                    onChange={(value) => setWorktreeStackPreference(worktree.id, value as 'auto' | 'never')}
                    options={[
                      { value: 'auto', label: 'Automatic', description: 'Use the automatic connection group, or group by tag.' },
                      { value: 'never', label: 'Always keep separate', description: 'Never include this worktree in a collapsed stack.' },
                    ]}
                  />
                  {groupCount > 1 && (
                    <button
                      onClick={() => automaticGroup ? toggleAutomaticGroup(automaticGroup.id) : toggleTagGroup(project.id, worktree.tag)}
                      className="bonsai-focus mt-2 flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[10px] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
                    >
                      <Layers3 size={11} /> {groupCollapsed ? 'Expand' : 'Collapse'} {automaticGroup ? 'Local / unlinked' : worktree.tag} group
                    </button>
                  )}
                </section>

                {inventory.some(p => p.worktreeId === worktree.id && processActive(p) && !processes.some(visible => visible.id === p.id)) && <Section title="Earlier active executions" meta={inventory.filter(p => p.worktreeId === worktree.id && processActive(p) && !processes.some(visible => visible.id === p.id)).length}>
                  {inventory.filter(p => p.worktreeId === worktree.id && processActive(p) && !processes.some(visible => visible.id === p.id)).map(p => <div key={p.id} className="mb-3">
                    <p className="mb-1 break-all text-[10px]">#{p.daemonId} · {p.command} · {p.lifecycleStatus}</p>
                    <ProcessActions process={p} />
                  </div>)}
                </Section>}

                <form onSubmit={submitTag} className="mt-4">
                  <div className="mb-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Tag name</div>
                  <div className="flex gap-1.5">
                    <input aria-label="Tag name" value={tagDraft} onChange={(event) => setTagDraft(event.target.value)} className="bonsai-focus h-8 min-w-0 flex-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[10px] outline-none" />
                    <button aria-label="Save tag" type="submit" className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[rgb(var(--muted))]"><Save size={11} /></button>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {tags.map((tag) => (
                      <button key={tag.id} type="button" onClick={() => { setTagDraft(tag.name); setWorktreeTag(worktree.id, tag.name) }} className="rounded border border-[rgb(var(--border))] px-1.5 py-0.5 text-[8px] text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))]">{tag.name}</button>
                    ))}
                  </div>
                </form>

              </details>
            </>}

            {process && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--muted))]"><TerminalSquare size={13} />Managed process<span className="ml-auto">{process.lifecycleStatus}</span></div>
                <h2 className="break-words text-[16px] font-semibold">{process.name}</h2>
                <p className="my-3 break-all font-mono text-[11px] text-[rgb(var(--muted))]">{process.command}</p>
                <ProcessActions process={process} />
              </div>
              {worktree ? <button onClick={() => setSelection({ type: 'worktree', id: worktree.id })} className="bonsai-focus inspector-link"><GitBranch size={13} />{worktree.branch}<ChevronRight size={12} className="ml-auto" /></button> : <p className="inspector-empty">Worktree association unavailable.</p>}
              {process.exitError && <p role="alert" className="break-words text-[11px] text-[rgb(var(--danger))]">{process.exitError}</p>}
              <Section title="Process details"><dl className="space-y-2 text-[10px]">
                <div className="flex justify-between"><dt>State</dt><dd>{process.lifecycleStatus}</dd></div>
                {process.exitCode !== undefined && <div className="flex justify-between"><dt>Exit code</dt><dd>{process.exitCode}</dd></div>}
                <div className="flex justify-between"><dt>Attempt</dt><dd>{process.attempt ?? 1}</dd></div>
                <div className="flex justify-between"><dt>Restart policy</dt><dd>{process.policy?.mode ?? 'no'}</dd></div>
                <div className="flex justify-between"><dt>Retries</dt><dd>{process.retryCount ?? 0}/{process.policy?.max_restarts ?? 0}</dd></div>
                {process.retryAt && <div className="flex justify-between"><dt>Next retry</dt><dd>{process.retryAt}</dd></div>}
                {process.startedAt && <div className="flex justify-between gap-2"><dt>Started</dt><dd>{process.startedAt}</dd></div>}
              </dl></Section>
            </>}
            {agent && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--muted))]"><Bot size={13} />{agent.provider}<span className={'ml-auto flex items-center gap-1.5 capitalize ' + (agent.state === 'running' ? 'text-[rgb(var(--accent))]' : '')}><span className="h-1.5 w-1.5 rounded-full bg-current" />{agent.lifecycleState ?? agent.state}</span></div>
                <h2 className="text-[16px] font-semibold tracking-tight">{agent.name}</h2>
                <p className="mt-2 text-[12px] leading-5 text-[rgb(var(--muted))]">{agent.task}</p>
                <div className="mt-3 flex items-center gap-1.5 text-[10px] text-[rgb(var(--muted))]"><Clock3 size={12} />{agent.providerId === 'antigravity' ? 'API session' : `${agent.runtime} runtime`}<span className="ml-auto capitalize">{presentation(agent)}</span></div>
                <div className="mt-4"><QuickButton icon={TerminalSquare} unavailable={agent.providerId !== 'antigravity'} label="Open terminal" primary onClick={() => openTerminal(agent.id)} /></div>
              </div>
              {worktree && <button onClick={() => setSelection({ type: 'worktree', id: worktree.id })} className="bonsai-focus inspector-link rounded-lg border border-[rgb(var(--border))]"><GitBranch size={13} className="shrink-0 text-[rgb(var(--accent))]" /><span className="min-w-0 flex-1"><span className="block truncate font-mono text-[10px]">{worktree.branch}</span><span className="mt-1 block text-[10px] text-[rgb(var(--muted))]">{worktree.dirtyFiles} uncommitted · {issues.length ? issues.length + ' branch issues' : 'View branch details'}</span></span><ChevronRight size={12} /></button>}
              {agent.providerId !== 'antigravity' && <><Section title="Recorded agent output" meta={<TerminalSquare size={12} className="text-[rgb(var(--muted))]" />}>
                {output.length ? <pre className="inspector-output">{output.join('\n')}</pre> : <p className="inspector-empty">No output captured for this session yet.</p>}
              </Section>
              <Section title="Instructions"><p className="whitespace-pre-wrap break-words text-[11px] leading-[1.8] text-[rgb(var(--muted))]">{agent.prompt || 'No instructions recorded.'}</p></Section></>}
              <Section title="Session">
                <dl className="space-y-2.5 text-[10px]">
                  <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">{agent.providerId === 'antigravity' ? 'Profile' : 'Model'}</dt><dd className="break-all text-right">{agent.profileName ?? agent.model}</dd></div>
                  {agent.providerId !== 'antigravity' && <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Reasoning</dt><dd>{agent.reasoningEffort}{agent.fastMode ? ' · Fast' : ''}</dd></div>}
                  <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Started</dt><dd>{agent.createdAt}</dd></div>
                  {agent.state === 'finished' && agent.finishedAt && <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Finished</dt><dd>{agent.finishedAt}</dd></div>}
                </dl>
              </Section>
              <div className="grid grid-cols-2 gap-2">
                {presentation(agent) === 'canvas' && (agent.state === 'running' ? <QuickButton icon={Square} unavailable={agent.providerId !== 'antigravity'} label="Stop agent" onClick={() => setAgentState(agent.id, 'finished')} /> : <QuickButton icon={RotateCcw} unavailable label="Restart" onClick={() => setAgentState(agent.id, 'running')} />)}
                {presentation(agent) === 'canvas' && agent.state === 'finished' && <QuickButton icon={History} label="Move to history" onClick={() => moveAgentToHistory(agent.id)} />}
                {presentation(agent) === 'history' && <QuickButton icon={Undo2} label="Restore to canvas" onClick={() => restoreAgentFromHistory(agent.id)} />}
                {presentation(agent) === 'archived' ? <QuickButton icon={Undo2} label="Restore" onClick={() => restoreAgent(agent.id)} /> : <QuickButton icon={Archive} label="Archive" danger onClick={() => archiveAgent(agent.id)} />}
              </div>
            </>}
            {!worktree && !agent && !process && selection.type !== 'project' && <p className="inspector-empty py-8 text-center">Select a project, worktree, agent, or process.</p>}
          </div>
        </ScrollArea.Viewport>
        <ScrollArea.Scrollbar orientation="vertical" className="w-1.5 p-[1px]"><ScrollArea.Thumb className="rounded bg-[rgb(var(--border-strong))]" /></ScrollArea.Scrollbar>
      </ScrollArea.Root>
          {worktree && !agent && !process && (
            <div className={'grid shrink-0 gap-2 px-4 pb-4 pt-1 ' + (worktree.main ? 'grid-cols-1' : 'grid-cols-[1.2fr_1fr]')}>
              <button type="button" aria-label="Launch" onClick={() => openStartAgentDialog(worktree.id)} className="bonsai-focus btn-primary flex h-11 items-center justify-center gap-2 text-[14px]"><Play size={15} fill="currentColor" />Launch</button>
              {!worktree.main && <button type="button" aria-label="Delete worktree" onClick={() => setDeleteWorktreeId(worktree.id)} className="bonsai-focus btn-danger-tint flex h-11 items-center justify-center gap-2 rounded-lg px-4 text-[14px] font-medium"><Trash2 size={15} />Delete</button>}
            </div>
          )}
        </Tabs.Content>
        <Tabs.Content value="files" className="flex min-h-0 flex-1 flex-col outline-none data-[state=inactive]:hidden">
          <FilesPanel />
        </Tabs.Content>
      </Tabs.Root>
    </aside>
  )
}
