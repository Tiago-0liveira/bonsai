import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import * as ScrollArea from '@radix-ui/react-scroll-area'
import {
  Archive, ArrowDown, ArrowRight, ArrowUp, Bot, CheckCircle2, ChevronRight,
  CircleDot, Clock3, FileDiff, GitBranch, GitPullRequest, History, Layers3,
  RotateCcw, Save, SlidersHorizontal, Square, TerminalSquare, Undo2,
  AlertCircle, XCircle, type LucideIcon,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import type { Agent, PullRequest, Worktree } from '../../types'

const presentation = (agent: Agent) => agent.presentation ?? (agent.archived ? 'archived' : 'canvas')

function branchIssues(worktree: Worktree, pr?: PullRequest) {
  const issues: string[] = []
  if (worktree.gitState?.includes('conflict') || pr?.mergeable === false) issues.push('Resolve merge conflicts')
  const failed = pr ? pr.checks.filter((check) => check.status === 'failed').length : worktree.ciFailed
  if (failed || worktree.ciStatus === 'failed') issues.push(failed ? `${failed} failing check${failed === 1 ? '' : 's'}` : 'Checks failed')
  if (worktree.behind) issues.push(`${worktree.behind} commit${worktree.behind === 1 ? '' : 's'} behind ${worktree.mergeTargetBranch}`)
  return issues
}

function Section({ title, meta, children }: { title: string; meta?: ReactNode; children: ReactNode }) {
  return <section className="inspector-section">
    <div className="mb-2.5 flex items-center justify-between gap-2"><h3 className="text-[11px] font-semibold text-[rgb(var(--text))]">{title}</h3>{meta}</div>
    {children}
  </section>
}

function QuickButton({ icon: Icon, label, onClick, danger = false, primary = false }: { icon: LucideIcon; label: string; onClick: () => void; danger?: boolean; primary?: boolean }) {
  return <button type="button" onClick={onClick} className={'bonsai-focus inspector-action ' + (danger ? 'inspector-action-danger' : primary ? 'inspector-action-primary' : '')}>
    <Icon size={13} className="shrink-0" />{label}
  </button>
}

function Metric({ value, label, icon: Icon, tone = '' }: { value: number | string; label: string; icon: LucideIcon; tone?: string }) {
  return <div className="min-w-0 rounded-lg border border-[rgb(var(--border)/.65)] bg-[rgb(var(--bg)/.5)] p-2.5">
    <div className={'flex items-center gap-1.5 text-[17px] font-semibold tabular-nums ' + tone}><Icon size={12} className="opacity-70" />{value}</div>
    <div className="mt-1 text-[10px] text-[rgb(var(--muted))]">{label}</div>
  </div>
}

function AgentRow({ agent, onClick }: { agent: Agent; onClick: () => void }) {
  return <button onClick={onClick} className="bonsai-focus inspector-link">
    <span className={'inspector-avatar ' + (agent.state === 'running' ? 'text-[rgb(var(--green))]' : 'text-[rgb(var(--muted))]')}><Bot size={14} /></span>
    <span className="min-w-0 flex-1"><span className="block truncate text-[11px] font-medium">{agent.name}</span><span className="mt-0.5 block truncate text-[10px] text-[rgb(var(--muted))]">{agent.task}</span></span>
    <span className="shrink-0 text-right text-[9px] text-[rgb(var(--muted))]"><span className="block capitalize">{agent.state}</span><span className="mt-0.5 block text-[rgb(var(--muted-2))]">{agent.runtime}</span></span>
  </button>
}

export function Inspector() {
  const selection = useBonsaiStore((state) => state.selection)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const tags = useBonsaiStore((state) => state.worktreeTags)
  const agents = useBonsaiStore((state) => state.agents)
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const terminalOutput = useBonsaiStore((state) => state.terminalOutput)
  const collapsedTagGroups = useBonsaiStore((state) => state.collapsedTagGroups)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const moveAgentToHistory = useBonsaiStore((state) => state.moveAgentToHistory)
  const restoreAgentFromHistory = useBonsaiStore((state) => state.restoreAgentFromHistory)
  const archiveAgent = useBonsaiStore((state) => state.archiveAgent)
  const restoreAgent = useBonsaiStore((state) => state.restoreAgent)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const openStartAgentDialog = useBonsaiStore((state) => state.openStartAgentDialog)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const setWorktreeTag = useBonsaiStore((state) => state.setWorktreeTag)
  const setWorktreeStackPreference = useBonsaiStore((state) => state.setWorktreeStackPreference)
  const setWorktreeMergeTarget = useBonsaiStore((state) => state.setWorktreeMergeTarget)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const inspectPullRequest = useBonsaiStore((state) => state.inspectPullRequest)
  const [tagDraft, setTagDraft] = useState('')

  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined
  const worktree = worktrees.find((item) => item.id === (selection.type === 'worktree' ? selection.id : agent?.worktreeId))
  const project = projects.find((item) => item.id === (worktree?.projectId ?? activeProjectId))
  const projectWorktrees = worktrees.filter((item) => item.projectId === project?.id)
  const projectWorktreeIds = new Set(projectWorktrees.map((item) => item.id))
  const projectAgents = agents.filter((item) => projectWorktreeIds.has(item.worktreeId) && presentation(item) === 'canvas')
  const branchAgents = projectAgents.filter((item) => item.worktreeId === worktree?.id)
  const pr = pullRequests.find((item) => item.number === worktree?.prNumber && item.branch === worktree?.branch)
  const attention = projectWorktrees.map((item) => ({ worktree: item, issues: branchIssues(item, pullRequests.find((request) => request.number === item.prNumber && request.branch === item.branch)) })).filter((item) => item.issues.length).sort((a, b) => b.issues.length - a.issues.length)
  const issues = worktree ? branchIssues(worktree, pr) : []
  const output = agent ? (terminalOutput[agent.terminalId] ?? []).filter((line) => line.trim()).slice(-5) : []

  useEffect(() => { setTagDraft(worktree?.tag ?? '') }, [worktree?.id, worktree?.tag])
  if (!project) return null

  const submitTag = (event: FormEvent) => { event.preventDefault(); if (worktree) setWorktreeTag(worktree.id, tagDraft) }
  const groupCount = worktree ? projectWorktrees.filter((item) => item.tag === worktree.tag).length : 0
  const groupCollapsed = worktree ? collapsedTagGroups.includes(project.id + ':' + worktree.tag) : false
  const mergeTargets = projectWorktrees.filter((item) => item.id !== worktree?.id).map((item) => item.branch)

  return (
    <aside aria-label="Inspector" className="desktop-inspector inspector-shell flex w-[310px] shrink-0 flex-col border-l border-[rgb(var(--border))]">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-[rgb(var(--border)/.7)] px-4">
        <SlidersHorizontal size={13} className="text-[rgb(var(--purple))]" /><span className="text-[12px] font-semibold">Inspector</span>
        <span className="ml-auto rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] px-2 py-0.5 text-[9px] capitalize text-[rgb(var(--muted))]">{selection.type}</span>
      </div>
      <ScrollArea.Root className="min-h-0 flex-1 overflow-hidden">
        <ScrollArea.Viewport className="h-full w-full">
          <div className="space-y-4 p-4">
            {selection.type === 'project' && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--muted))]"><GitBranch size={12} /> Workspace overview</div>
                <h2 className="text-lg font-semibold tracking-tight">{project.name}</h2>
                <p className="mt-1 break-all text-[11px] text-[rgb(var(--muted))]">{project.repository}</p>
                <div className="mt-4 grid grid-cols-2 gap-2"><QuickButton icon={GitBranch} label="Worktree" onClick={() => setWorktreeDialogOpen(true)} /><QuickButton icon={Bot} label="Agent" primary onClick={() => openStartAgentDialog()} /></div>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <Metric value={projectWorktrees.length} label="Branches" icon={GitBranch} />
                <Metric value={projectAgents.filter((item) => item.state === 'running').length} label="Running" icon={Bot} tone="text-[rgb(var(--green))]" />
                <Metric value={projectWorktrees.reduce((total, item) => total + item.dirtyFiles, 0)} label="Changed files" icon={FileDiff} tone="text-[rgb(var(--orange))]" />
              </div>
              <Section title="Needs attention" meta={<span className="inspector-count">{attention.length}</span>}>
                {attention.length ? <div className="space-y-2">{attention.map(({ worktree: item, issues: reasons }) => <button key={item.id} onClick={() => setSelection({ type: 'worktree', id: item.id })} className="bonsai-focus inspector-attention">
                  <AlertCircle size={14} className="mt-0.5 shrink-0 text-[rgb(var(--orange))]" /><span className="min-w-0 flex-1"><span className="block truncate font-mono text-[10px] text-[rgb(var(--text))]">{item.branch}</span><span className="mt-1 block text-[10px] leading-4 text-[rgb(var(--muted))]">{reasons.join(' · ')}</span></span><ChevronRight size={12} className="mt-0.5 shrink-0 text-[rgb(var(--muted))]" />
                </button>)}</div> : <p className="flex items-center gap-2 text-[11px] text-[rgb(var(--muted))]"><CheckCircle2 size={14} className="text-[rgb(var(--green))]" />No branch blockers reported.</p>}
              </Section>
              <Section title="Agents" meta={<span className="inspector-count">{projectAgents.length}</span>}>
                {projectAgents.length ? projectAgents.map((item) => <AgentRow key={item.id} agent={item} onClick={() => setSelection({ type: 'agent', id: item.id })} />) : <p className="inspector-empty">Start an agent to work on a branch.</p>}
              </Section>
            </>}

            {worktree && !agent && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--purple))]"><GitBranch size={13} />{worktree.kind}<span className="ml-auto text-[rgb(var(--muted))]">{worktree.lastActivity}</span></div>
                <h2 className="break-words font-mono text-[14px] font-semibold leading-6">{worktree.branch}</h2>
                <div className="mt-2 flex items-center gap-1.5 text-[10px] text-[rgb(var(--muted))]"><ArrowRight size={12} /><span className="truncate">{worktree.mergeTargetBranch}</span></div>
                <div className="mt-4"><QuickButton icon={Bot} label="Start agent" primary onClick={() => openStartAgentDialog(worktree.id)} /></div>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <Metric value={worktree.dirtyFiles} label="Uncommitted" icon={FileDiff} tone={worktree.dirtyFiles ? 'text-[rgb(var(--orange))]' : ''} />
                <Metric value={worktree.ahead} label="Ahead" icon={ArrowUp} />
                <Metric value={worktree.behind} label="Behind" icon={ArrowDown} tone={worktree.behind ? 'text-[rgb(var(--orange))]' : ''} />
              </div>
              {issues.length > 0 && <div className="inspector-alert"><div className="mb-2 flex items-center gap-2 text-[11px] font-medium text-[rgb(var(--orange))]"><AlertCircle size={13} />Before merging</div>{issues.map((issue) => <p key={issue} className="mt-1 text-[11px] leading-5 text-[rgb(var(--muted))]">{issue}</p>)}</div>}
              <Section title="Pull request" meta={pr && <span className="inspector-count">{pr.status}</span>}>
                {pr ? <>
                  <button onClick={() => inspectPullRequest(pr.id)} className="bonsai-focus inspector-link !items-start !px-0"><GitPullRequest size={15} className="mt-0.5 shrink-0 text-[rgb(var(--purple))]" /><span className="min-w-0 flex-1"><span className="block text-[11px] font-medium leading-5">#{pr.number} {pr.title}</span><span className="mt-1 block text-[10px] text-[rgb(var(--muted))]">{pr.files.length} files · {pr.commits.length} commits · View review</span></span><ChevronRight size={13} className="mt-1 shrink-0" /></button>
                  <div className="mt-2 space-y-2 rounded-lg bg-[rgb(var(--bg)/.6)] p-2.5">{pr.checks.length ? pr.checks.map((check) => <div key={check.name} className="flex items-center gap-2 text-[10px]">
                    {check.status === 'failed' ? <XCircle size={12} className="shrink-0 text-[rgb(var(--red))]" /> : check.status === 'success' ? <CheckCircle2 size={12} className="shrink-0 text-[rgb(var(--green))]" /> : <CircleDot size={12} className="shrink-0 text-[rgb(var(--orange))]" />}
                    <span className="min-w-0 flex-1 break-words text-[rgb(var(--muted))]">{check.name}</span><span className="text-[9px] text-[rgb(var(--muted))]">{check.status === 'success' ? 'Passed' : check.status === 'failed' ? 'Failed' : 'Running'}</span>
                  </div>) : <p className="inspector-empty">No checks reported.</p>}</div>
                </> : <p className="inspector-empty">{worktree.prNumber ? `PR #${worktree.prNumber} details are unavailable.` : 'No pull request linked to this branch.'}</p>}
              </Section>
              <Section title="Agents on this branch" meta={<span className="inspector-count">{branchAgents.length}</span>}>
                {branchAgents.length ? branchAgents.map((item) => <AgentRow key={item.id} agent={item} onClick={() => setSelection({ type: 'agent', id: item.id })} />) : <p className="inspector-empty">No active sessions. Start an agent above.</p>}
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
                      { value: 'auto', label: 'Automatic', description: 'Stack with other worktrees sharing this tag.' },
                      { value: 'never', label: 'Always keep separate', description: 'Never include this worktree in a collapsed stack.' },
                    ]}
                  />
                  {groupCount > 1 && (
                    <button
                      onClick={() => toggleTagGroup(project.id, worktree.tag)}
                      className="bonsai-focus mt-2 flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[10px] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
                    >
                      <Layers3 size={11} /> {groupCollapsed ? 'Expand' : 'Collapse'} {worktree.tag} group
                    </button>
                  )}
                </section>

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

            {agent && <>
              <div className="inspector-hero">
                <div className="mb-2 flex items-center gap-2 text-[10px] text-[rgb(var(--muted))]"><Bot size={13} />{agent.provider}<span className={'ml-auto flex items-center gap-1.5 capitalize ' + (agent.state === 'running' ? 'text-[rgb(var(--green))]' : '')}><span className="h-1.5 w-1.5 rounded-full bg-current" />{agent.state}</span></div>
                <h2 className="text-[16px] font-semibold tracking-tight">{agent.name}</h2>
                <p className="mt-2 text-[12px] leading-5 text-[rgb(var(--muted))]">{agent.task}</p>
                <div className="mt-3 flex items-center gap-1.5 text-[10px] text-[rgb(var(--muted))]"><Clock3 size={12} />{agent.runtime} runtime<span className="ml-auto capitalize">{presentation(agent)}</span></div>
                <div className="mt-4"><QuickButton icon={TerminalSquare} label="Open terminal" primary onClick={() => openTerminal(agent.id)} /></div>
              </div>
              {worktree && <button onClick={() => setSelection({ type: 'worktree', id: worktree.id })} className="bonsai-focus inspector-link rounded-lg border border-[rgb(var(--border))]"><GitBranch size={13} className="shrink-0 text-[rgb(var(--purple))]" /><span className="min-w-0 flex-1"><span className="block truncate font-mono text-[10px]">{worktree.branch}</span><span className="mt-1 block text-[10px] text-[rgb(var(--muted))]">{worktree.dirtyFiles} uncommitted · {issues.length ? issues.length + ' branch issues' : 'View branch details'}</span></span><ChevronRight size={12} /></button>}
              <Section title="Latest terminal output" meta={<TerminalSquare size={12} className="text-[rgb(var(--muted))]" />}>
                {output.length ? <pre className="inspector-output">{output.join('\n')}</pre> : <p className="inspector-empty">No output captured for this session yet.</p>}
              </Section>
              <Section title="Instructions"><p className="whitespace-pre-wrap break-words text-[11px] leading-[1.8] text-[rgb(var(--muted))]">{agent.prompt || 'No instructions recorded.'}</p></Section>
              <Section title="Session">
                <dl className="space-y-2.5 text-[10px]">
                  <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Model</dt><dd className="break-all text-right">{agent.model}</dd></div>
                  <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Reasoning</dt><dd>{agent.reasoningEffort}{agent.fastMode ? ' · Fast' : ''}</dd></div>
                  <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Started</dt><dd>{agent.createdAt}</dd></div>
                  {agent.state === 'finished' && agent.finishedAt && <div className="flex justify-between gap-3"><dt className="text-[rgb(var(--muted))]">Finished</dt><dd>{agent.finishedAt}</dd></div>}
                </dl>
              </Section>
              <div className="grid grid-cols-2 gap-2">
                {presentation(agent) === 'canvas' && (agent.state === 'running' ? <QuickButton icon={Square} label="Stop agent" onClick={() => setAgentState(agent.id, 'finished')} /> : <QuickButton icon={RotateCcw} label="Restart" onClick={() => setAgentState(agent.id, 'running')} />)}
                {presentation(agent) === 'canvas' && agent.state === 'finished' && <QuickButton icon={History} label="Move to history" onClick={() => moveAgentToHistory(agent.id)} />}
                {presentation(agent) === 'history' && <QuickButton icon={Undo2} label="Restore to canvas" onClick={() => restoreAgentFromHistory(agent.id)} />}
                {presentation(agent) === 'archived' ? <QuickButton icon={Undo2} label="Restore" onClick={() => restoreAgent(agent.id)} /> : <QuickButton icon={Archive} label={agent.state === 'running' ? 'Stop + archive' : 'Archive'} danger onClick={() => archiveAgent(agent.id)} />}
              </div>
            </>}
            {!worktree && !agent && selection.type !== 'project' && <p className="inspector-empty py-8 text-center">Select a project, worktree, or agent.</p>}
          </div>
        </ScrollArea.Viewport>
        <ScrollArea.Scrollbar orientation="vertical" className="w-1.5 p-[1px]"><ScrollArea.Thumb className="rounded bg-[rgb(var(--border-strong))]" /></ScrollArea.Scrollbar>
      </ScrollArea.Root>
    </aside>
  )
}
