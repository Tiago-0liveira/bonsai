import { useEffect, useState, type FormEvent } from 'react'
import * as ScrollArea from '@radix-ui/react-scroll-area'
import {
  Archive,
  Bot,
  CircleCheck,
  CircleX,
  ExternalLink,
  GitBranch,
  GitPullRequest,
  Layers3,
  LoaderCircle,
  Play,
  RotateCcw,
  Save,
  Square,
  TerminalSquare,
  Undo2,
  type LucideIcon,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { activity } from '../../mock/activity'
import { useBonsaiStore } from '../../stores/bonsai'
import type { CiStatus, Health, PrStatus } from '../../types'

const statusClass: Record<Health, string> = {
  healthy: 'bg-[rgb(var(--green))]',
  warning: 'bg-[rgb(var(--orange))]',
  error: 'bg-[rgb(var(--red))]',
  idle: 'bg-[rgb(var(--muted-2))]',
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-[rgb(var(--border))] py-2.5 last:border-0">
      <span className="text-[11px] text-[rgb(var(--muted-2))]">{label}</span>
      <span className="min-w-0 truncate text-right text-[11px] text-[rgb(var(--muted))]">{value}</span>
    </div>
  )
}

function QuickButton({ icon: Icon, label, onClick, danger = false }: { icon: LucideIcon; label: string; onClick?: () => void; danger?: boolean }) {
  return (
    <button
      onClick={onClick}
      className={
        'bonsai-focus flex items-center justify-center gap-1.5 rounded-md border bg-[rgb(var(--panel-2))] px-2 py-2 text-[11px] hover:bg-[rgb(var(--panel-3))] ' +
        (danger ? 'border-[rgb(var(--red)/.3)] text-[rgb(var(--red))]' : 'border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]')
      }
    >
      <Icon size={12} /> {label}
    </button>
  )
}

function StatusBadge({ pr, ci, failed = 0 }: { pr?: PrStatus; ci?: CiStatus; failed?: number }) {
  if (pr) {
    const tone =
      pr === 'Open'
        ? 'text-[rgb(var(--green))] border-[rgb(var(--green)/.25)]'
        : pr === 'Draft'
          ? 'text-[rgb(var(--purple))] border-[rgb(var(--purple)/.25)]'
          : pr === 'Merged'
            ? 'text-[rgb(var(--blue))] border-[rgb(var(--blue)/.25)]'
            : 'text-[rgb(var(--muted))] border-[rgb(var(--border))]'
    return <span className={'inline-flex rounded border bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[9px] ' + tone}>{pr} PR</span>
  }
  if (!ci) return null
  const meta =
    ci === 'passed'
      ? { label: 'CI passed', icon: CircleCheck, tone: 'text-[rgb(var(--green))]' }
      : ci === 'running'
        ? { label: 'CI running', icon: LoaderCircle, tone: 'text-[rgb(var(--blue))]' }
        : ci === 'failed'
          ? { label: 'CI failed · ' + failed, icon: CircleX, tone: 'text-[rgb(var(--red))]' }
          : { label: 'CI waiting', icon: Play, tone: 'text-[rgb(var(--orange))]' }
  const Icon = meta.icon
  return <span className={'inline-flex items-center gap-1 rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[9px] ' + meta.tone}><Icon size={10} className={ci === 'running' ? 'animate-spin' : ''} /> {meta.label}</span>
}

export function Inspector() {
  const selection = useBonsaiStore((state) => state.selection)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const tags = useBonsaiStore((state) => state.worktreeTags)
  const agents = useBonsaiStore((state) => state.agents)
  const collapsedTagGroups = useBonsaiStore((state) => state.collapsedTagGroups)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const archiveAgent = useBonsaiStore((state) => state.archiveAgent)
  const restoreAgent = useBonsaiStore((state) => state.restoreAgent)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const openStartAgentDialog = useBonsaiStore((state) => state.openStartAgentDialog)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const setWorktreeTag = useBonsaiStore((state) => state.setWorktreeTag)
  const setWorktreeStackPreference = useBonsaiStore((state) => state.setWorktreeStackPreference)
  const setWorktreeMergeTarget = useBonsaiStore((state) => state.setWorktreeMergeTarget)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const [tagDraft, setTagDraft] = useState('')

  const project = projects.find((item) => item.id === activeProjectId) ?? projects[0]
  const projectWorktrees = worktrees.filter((item) => item.projectId === project?.id)
  const projectWorktreeIds = new Set(projectWorktrees.map((item) => item.id))
  const projectAgents = agents.filter((agent) => projectWorktreeIds.has(agent.worktreeId) && !agent.archived)
  const worktree = selection.type === 'worktree' ? worktrees.find((item) => item.id === selection.id) : undefined
  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined

  useEffect(() => {
    setTagDraft(worktree?.tag ?? '')
  }, [worktree?.id, worktree?.tag])

  if (!project) return null

  const runningAgents = projectAgents.filter((item) => item.state === 'running').length
  const openPrs = projectWorktrees.filter((item) => item.prStatus === 'Open').length
  const draftPrs = projectWorktrees.filter((item) => item.prStatus === 'Draft').length
  const failedChecks = projectWorktrees.reduce((sum, item) => sum + item.ciFailed, 0)
  const runningChecks = projectWorktrees.filter((item) => item.ciStatus === 'running').length
  const waitingChecks = projectWorktrees.filter((item) => item.ciStatus === 'waiting').length

  const submitTag = (event: FormEvent) => {
    event.preventDefault()
    if (worktree) setWorktreeTag(worktree.id, tagDraft)
  }

  const groupCount = worktree ? projectWorktrees.filter((item) => item.tag === worktree.tag).length : 0
  const groupCollapsed = worktree ? collapsedTagGroups.includes(project.id + ':' + worktree.tag) : false
  const mergeTargets = projectWorktrees.filter((item) => item.id !== worktree?.id).map((item) => item.branch)

  return (
    <aside className="desktop-inspector flex w-[292px] shrink-0 flex-col border-l border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="flex h-10 items-center border-b border-[rgb(var(--border))] px-3">
        <span className="font-medium">Inspector</span>
        <span className="ml-auto text-[10px] uppercase tracking-wider text-[rgb(var(--muted-2))]">{selection.type}</span>
      </div>

      <ScrollArea.Root className="min-h-0 flex-1 overflow-hidden">
        <ScrollArea.Viewport className="h-full w-full">
          <div className="p-3">
            {selection.type === 'project' && (
              <>
                <div className="mb-4">
                  <div className="flex items-center gap-2">
                    <span className={'h-2 w-2 rounded-full ' + statusClass[project.health]} />
                    <h2 className="font-medium">{project.name}</h2>
                  </div>
                  <p className="mt-2 text-[11px] leading-5 text-[rgb(var(--muted))]">{project.description}</p>
                </div>

                <div className="mb-3 grid grid-cols-3 gap-2">
                  <div className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-2 text-center">
                    <div className="text-sm font-semibold">{projectWorktrees.length}</div>
                    <div className="text-[9px] text-[rgb(var(--muted-2))]">worktrees</div>
                  </div>
                  <div className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-2 text-center">
                    <div className="text-sm font-semibold text-[rgb(var(--green))]">{runningAgents}</div>
                    <div className="text-[9px] text-[rgb(var(--muted-2))]">running</div>
                  </div>
                  <div className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-2 text-center">
                    <div className="text-sm font-semibold text-[rgb(var(--red))]">{failedChecks}</div>
                    <div className="text-[9px] text-[rgb(var(--muted-2))]">CI failed</div>
                  </div>
                </div>

                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Repository" value={project.repository} />
                  <Row label="Default branch" value={project.defaultBranch} />
                  <Row label="Pull requests" value={openPrs + ' open · ' + draftPrs + ' draft'} />
                  <Row label="CI queue" value={runningChecks + ' running · ' + waitingChecks + ' waiting'} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={GitBranch} label="Worktree" onClick={() => setWorktreeDialogOpen(true)} />
                  <QuickButton icon={Bot} label="Agent" onClick={() => openStartAgentDialog()} />
                </div>

                <section className="mt-5">
                  <div className="mb-2 text-[10px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Operational activity</div>
                  <div className="space-y-1">
                    {activity.slice(0, 5).map((item) => (
                      <div key={item.id} className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 py-2">
                        <div className="text-[10px]">{item.title}</div>
                        <div className="mt-1 flex items-center justify-between gap-2 text-[8px] text-[rgb(var(--muted-2))]">
                          <span className="truncate">{item.detail}</span>
                          <span className="shrink-0">{item.time}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </section>
              </>
            )}

            {worktree && (
              <>
                <div className="mb-3">
                  <div className="flex items-center gap-2">
                    <GitBranch size={14} className="text-[rgb(var(--purple))]" />
                    <h2 className="min-w-0 truncate font-mono text-[12px] font-semibold">{worktree.branch}</h2>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <StatusBadge pr={worktree.prStatus} />
                    <StatusBadge ci={worktree.ciStatus} failed={worktree.ciFailed} />
                  </div>
                </div>

                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Tag" value={worktree.tag} />
                  <Row label="Agents" value={agents.filter((item) => item.worktreeId === worktree.id && !item.archived).length} />
                  <Row label="Git state" value={worktree.gitState ?? 'clean'} />
                  <Row label="Last activity" value={worktree.lastActivity} />
                  <Row label="Pull request" value={worktree.prNumber ? '#' + worktree.prNumber : 'none'} />
                </div>

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
                    <input value={tagDraft} onChange={(event) => setTagDraft(event.target.value)} className="bonsai-focus h-8 min-w-0 flex-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[10px] outline-none" />
                    <button type="submit" className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[rgb(var(--muted))]"><Save size={11} /></button>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {tags.map((tag) => (
                      <button key={tag.id} type="button" onClick={() => { setTagDraft(tag.name); setWorktreeTag(worktree.id, tag.name) }} className="rounded border border-[rgb(var(--border))] px-1.5 py-0.5 text-[8px] text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))]">{tag.name}</button>
                    ))}
                  </div>
                </form>

                <div className="mt-4 grid grid-cols-2 gap-2">
                  <QuickButton icon={Bot} label="Start agent" onClick={() => openStartAgentDialog(worktree.id)} />
                  <QuickButton icon={GitPullRequest} label="Open PR" onClick={() => setNotice(worktree.prNumber ? 'Opened PR #' + worktree.prNumber + ' (mock)' : 'No pull request linked')} />
                </div>
              </>
            )}

            {agent && (
              <>
                <div className="mb-3">
                  <div className="flex items-center gap-2">
                    <span className={'h-2 w-2 rounded-full ' + statusClass[agent.state === 'running' ? 'healthy' : agent.state === 'finished' ? 'idle' : 'warning']} />
                    <h2 className="truncate font-medium">{agent.name}</h2>
                    {agent.archived && <span className="ml-auto rounded bg-[rgb(var(--panel-3))] px-1.5 py-0.5 text-[8px] text-[rgb(var(--muted-2))]">archived</span>}
                  </div>
                  <p className="mt-2 text-[10px] leading-5 text-[rgb(var(--muted))]">{agent.task}</p>
                </div>
                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Provider" value={agent.provider} />
                  <Row label="Model" value={agent.model} />
                  <Row label="Reasoning" value={agent.reasoningEffort + (agent.fastMode ? ' · Fast' : '')} />
                  <Row label="Work type" value={agent.workType} />
                  <Row label="State" value={agent.state} />
                  <Row label="Runtime" value={agent.runtime} />
                </div>
                <section className="mt-3 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-2.5">
                  <div className="mb-1 text-[8px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Prompt</div>
                  <div className="text-[10px] leading-4 text-[rgb(var(--muted))]">{agent.prompt}</div>
                </section>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={TerminalSquare} label="Terminal" onClick={() => openTerminal(agent.id)} />
                  <QuickButton icon={RotateCcw} label="Restart" onClick={() => setAgentState(agent.id, 'running')} />
                  <QuickButton icon={Square} label="Stop" onClick={() => setAgentState(agent.id, 'finished')} />
                  {agent.archived ? (
                    <QuickButton icon={Undo2} label="Restore" onClick={() => restoreAgent(agent.id)} />
                  ) : (
                    <QuickButton icon={Archive} label={agent.state === 'running' ? 'Stop + archive' : 'Archive'} danger onClick={() => archiveAgent(agent.id)} />
                  )}
                </div>
              </>
            )}

            {!worktree && !agent && selection.type !== 'project' && (
              <div className="py-10 text-center text-[10px] text-[rgb(var(--muted-2))]">Select a project, worktree, or agent.</div>
            )}
          </div>
        </ScrollArea.Viewport>
        <ScrollArea.Scrollbar orientation="vertical" className="w-1.5 p-[1px]">
          <ScrollArea.Thumb className="rounded bg-[rgb(var(--border-strong))]" />
        </ScrollArea.Scrollbar>
      </ScrollArea.Root>
    </aside>
  )
}
