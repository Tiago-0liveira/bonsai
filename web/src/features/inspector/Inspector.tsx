import { processActive } from '../../stores/processProjection'
import { openGitHub } from '../../api/git'
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import * as ScrollArea from '@radix-ui/react-scroll-area'
import * as Tabs from '@radix-ui/react-tabs'
import {
  Archive, ArrowDown, ArrowRight, ArrowUp, Bot, CheckCircle2, ChevronRight,
  CircleDot, Clock3, FileDiff, GitBranch, GitPullRequest, History, Layers3,
  Play, RotateCcw, Save, SlidersHorizontal, Square, TerminalSquare, Undo2,
  AlertCircle, XCircle, type LucideIcon,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import { useProjectWorktrees, useProjectAgents, useProjectProcesses, useProjectPullRequests } from '../../stores/projectSelectors'
import { FilesDiffPanel } from '../files/FilesDiffPanel'
import { ProcessActions } from '../terminal/ProcessActions'
import type { Agent, PullRequest, SyncFreshnessState, Worktree } from '../../types'

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
  Open: { label: '● OPEN', className: 'bg-accent/[.12] text-accent' },
  Draft: { label: 'DRAFT', className: 'bg-panel-3 text-muted' },
  Merged: { label: 'MERGED', className: 'bg-ok/[.12] text-ok' },
  Closed: { label: 'CLOSED', className: 'bg-panel-3 text-muted' },
}

function PullRequestCard({ pr, onInspect }: { pr: PullRequest; onInspect: () => void }) {
  const chip = PR_CHIP[pr.status]
  const conflicts = pr.mergeable === false
  return <button type="button" onClick={onInspect} className="bonsai-focus block w-full rounded-xl border border-border bg-panel-2 p-3 text-left transition-colors hover:border-border-strong">
    <span className="flex items-start gap-2">
      <span className="min-w-0 flex-1 text-[13px] leading-5 text-text"><span className="font-mono text-accent">#{pr.number}</span> {pr.title}</span>
      <span className={'chip shrink-0 ' + chip.className}>{chip.label}</span>
    </span>
    <span className="mt-1.5 flex items-center gap-2 font-mono text-[10px] text-muted-2">
      <span className="min-w-0 flex-1 truncate">{pr.branch} → {pr.base}</span>
      <span className={conflicts ? 'text-danger' : 'text-ok'}>{conflicts ? 'conflicts' : 'no conflicts'}</span>
    </span>
    <span className="mt-1 block text-[10px] text-muted-2">{pr.files.length} files · {pr.commits.length} commits · View review</span>
  </button>
}

function CiCard({ checks }: { checks: PullRequest['checks'] }) {
  if (!checks.length) return <p className="inspector-empty mt-2">No checks reported.</p>
  const success = checks.filter(check => check.status === 'success').length
  const state = checks.some(check => check.status === 'failed') ? 'failing' : success === checks.length ? 'passing' : 'running'
  const tone = { failing: 'text-danger', passing: 'text-ok', running: 'text-accent' }[state]
  const segment = { success: 'bg-ok', running: 'bg-accent-solid', failed: 'bg-danger' }
  return <div className="mt-2 rounded-xl border border-border bg-panel-2 p-3">
    <div className="flex items-center justify-between font-mono text-[9.5px] uppercase">
      <span className="text-muted-2">CI/CD</span>
      <span className={tone}>{state.toUpperCase()} · {success} OF {checks.length}</span>
    </div>
    <div className="mt-2 flex gap-1" aria-hidden>{checks.map((check, index) => <span key={check.id || `${check.name}:${index}`} className={'h-1 flex-1 rounded-full ' + segment[check.status]} />)}</div>
    <div className="mt-2.5 space-y-2">{checks.map((check, index) => <div key={check.id || `${check.name}:${index}`} className="flex items-center gap-2 text-[12px]">
      {check.status === 'failed' ? <XCircle size={12} className="shrink-0 text-danger" /> : check.status === 'success' ? <CheckCircle2 size={12} className="shrink-0 text-ok" /> : <CircleDot size={12} className="shrink-0 text-accent" />}
      <span className="min-w-0 flex-1 break-words text-text">{check.name}</span>
      <span className="font-mono text-[10px] text-muted-2">{check.status === 'success' ? 'Passed' : check.status === 'failed' ? 'Failed' : 'Running'}</span>
    </div>)}</div>
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
  const [tagDraft, setTagDraft] = useState('')

  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined
  const process = selection.type === 'process' ? inventory.find(item => item.id === selection.id) : undefined
  const worktree = worktrees.find((item) => item.id === (selection.type === 'worktree' ? selection.id : agent?.worktreeId ?? process?.worktreeId))
  const project = useBonsaiStore(state => state.projects.find(item => item.id === (worktree?.projectId ?? activeProjectId)))
  const projectWorktrees = worktrees
  const projectAgents = useMemo(() => agents.filter(item => presentation(item) === 'canvas'), [agents])
  const branchAgents = projectAgents.filter((item) => item.worktreeId === worktree?.id)
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
        <Tabs.Content value="inspector" className="flex min-h-0 flex-1 flex-col outline-none">
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
              <div className="inspector-hero">
                <div className="flex items-center gap-2.5">
                  <span className="grid h-9 w-9 shrink-0 place-items-center rounded-[9px] bg-accent/[.12] text-accent"><GitBranch size={17} /></span>
                  <div className="min-w-0 flex-1">
                    <h2 className="break-words font-mono text-[15px] font-semibold leading-5 text-text">{worktree.branch}</h2>
                    <div className="mt-1 flex items-center gap-1.5">
                      {worktree.tag && <span className="chip bg-accent/[.12] text-accent">{worktree.tag}</span>}
                      <span className="font-mono text-[10px] text-muted-2">{worktree.kind}</span>
                      <span className="ml-auto font-mono text-[10px] text-muted-2">{worktree.lastActivity}</span>
                    </div>
                  </div>
                </div>
                <div className="mt-2 flex items-center gap-1.5 font-mono text-[10px] text-muted-2"><ArrowRight size={11} /><span className="truncate">from {worktree.mergeTargetBranch}</span></div>
                <div className="mt-4 grid grid-cols-2 gap-2"><QuickButton icon={Bot} label="Start agent" primary onClick={() => openStartAgentDialog(worktree.id)} /><QuickButton icon={Play} label="Start process" onClick={() => openStartProcessDialog(worktree.id)} /></div>
              </div>
              {!worktree.main && <button type="button" onClick={() => setDeleteWorktreeId(worktree.id)} className="bonsai-focus btn-danger-tint h-[34px] rounded-lg px-3 text-[11px] font-medium">Delete worktree</button>}
              <div className="grid grid-cols-3 gap-2">
                <Metric value={worktree.dirtyFiles} label="Uncommitted" icon={FileDiff} tone={worktree.dirtyFiles ? 'text-warn' : ''} />
                <Metric value={worktree.divergenceAvailable ? worktree.ahead : '—'} label="Ahead" icon={ArrowUp} />
                <Metric value={worktree.divergenceAvailable ? worktree.behind : '—'} label="Behind" icon={ArrowDown} tone={worktree.divergenceAvailable && worktree.behind ? 'text-warn' : ''} />
              </div>
              {issues.length > 0 && <div className="inspector-alert"><div className="mb-2 flex items-center gap-2 text-[11px] font-medium text-warn"><AlertCircle size={13} />Before merging</div>{issues.map((issue) => <p key={issue} className="mt-1 text-[11px] leading-5 text-[rgb(var(--muted))]">{issue}</p>)}</div>}
              <Section title="Pull request">
                {pr ? <>
                  <PullRequestCard pr={pr} onInspect={() => inspectPullRequest(pr.id)} />
                  <div className="mt-2"><QuickButton icon={GitPullRequest} label="Open on GitHub" onClick={() => openGitHub('pull/' + pr.number, worktree.projectId)} /></div>
                  <CiCard checks={pr.checks} />
                </> : <p className="inspector-empty">{worktree.prNumber ? `PR #${worktree.prNumber} details are unavailable.` : 'No pull request linked to this branch.'}</p>}
              </Section>
              <Section title="Agents on this branch" meta={<span className="inspector-count">{branchAgents.length}</span>}>
                {branchAgents.length ? branchAgents.map((item) => <AgentRow key={item.id} agent={item} onClick={() => setSelection({ type: 'agent', id: item.id })} />) : <p className="inspector-empty">No agent sessions.</p>}
              </Section>
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
        </Tabs.Content>
        <Tabs.Content value="files" className="min-h-0 flex-1 outline-none">
          <FilesDiffPanel embedded />
        </Tabs.Content>
      </Tabs.Root>
    </aside>
  )
}
