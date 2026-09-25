import { useEffect, useState, type FormEvent } from 'react'
import * as ScrollArea from '@radix-ui/react-scroll-area'
import {
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
  type LucideIcon,
} from 'lucide-react'
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

function QuickButton({
  icon: Icon,
  label,
  onClick,
}: {
  icon: LucideIcon
  label: string
  onClick?: () => void
}) {
  return (
    <button
      onClick={onClick}
      className="bonsai-focus flex items-center justify-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] px-2 py-2 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-3))] hover:text-[rgb(var(--text))]"
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
          : { label: 'Waiting to launch', icon: Play, tone: 'text-[rgb(var(--orange))]' }
  const Icon = meta.icon
  return (
    <span className={'inline-flex items-center gap-1 rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[9px] ' + meta.tone}>
      <Icon size={10} className={ci === 'running' ? 'animate-spin' : ''} /> {meta.label}
    </span>
  )
}

export function Inspector() {
  const selection = useBonsaiStore((state) => state.selection)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const collapsedTagGroups = useBonsaiStore((state) => state.collapsedTagGroups)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const startMockAgent = useBonsaiStore((state) => state.startMockAgent)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const setWorktreeTag = useBonsaiStore((state) => state.setWorktreeTag)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const [tagDraft, setTagDraft] = useState('')

  const project = projects.find((item) => item.id === activeProjectId) ?? projects[0]
  const projectWorktrees = worktrees.filter((item) => item.projectId === project?.id)
  const projectWorktreeIds = new Set(projectWorktrees.map((item) => item.id))
  const projectAgents = agents.filter((agent) => projectWorktreeIds.has(agent.worktreeId))
  const worktree = selection.type === 'worktree' ? worktrees.find((item) => item.id === selection.id) : undefined
  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined

  useEffect(() => {
    setTagDraft(worktree?.tag ?? '')
  }, [worktree?.id, worktree?.tag])

  if (!project) return null

  const runningAgents = projectAgents.filter((item) => item.state === 'running').length
  const openPrs = projectWorktrees.filter((item) => item.prStatus === 'Open').length
  const draftPrs = projectWorktrees.filter((item) => item.prStatus === 'Draft').length
  const closedPrs = projectWorktrees.filter((item) => item.prStatus === 'Closed').length
  const failedChecks = projectWorktrees.reduce((sum, item) => sum + item.ciFailed, 0)
  const runningChecks = projectWorktrees.filter((item) => item.ciStatus === 'running').length
  const waitingChecks = projectWorktrees.filter((item) => item.ciStatus === 'waiting').length

  const submitTag = (event: FormEvent) => {
    event.preventDefault()
    if (worktree) setWorktreeTag(worktree.id, tagDraft)
  }

  const groupCount = worktree
    ? projectWorktrees.filter((item) => item.tag === worktree.tag).length
    : 0
  const groupCollapsed = worktree
    ? collapsedTagGroups.includes(project.id + ':' + worktree.tag)
    : false

  return (
    <aside className="desktop-inspector flex w-[292px] shrink-0 flex-col border-l border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="flex h-10 items-center border-b border-[rgb(var(--border))] px-3">
        <span className="font-medium">Inspector</span>
        <span className="ml-auto text-[10px] uppercase tracking-wider text-[rgb(var(--muted-2))]">
          {selection.type}
        </span>
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
                  <Row label="Pull requests" value={openPrs + ' open · ' + draftPrs + ' draft · ' + closedPrs + ' closed'} />
                  <Row label="CI queue" value={runningChecks + ' running · ' + waitingChecks + ' waiting'} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={GitBranch} label="Worktree" onClick={() => setWorktreeDialogOpen(true)} />
                  <QuickButton icon={Bot} label="Agent" onClick={startMockAgent} />
                </div>

                <section className="mt-5">
                  <div className="mb-2 text-[10px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">
                    Operational activity
                  </div>
                  <div className="space-y-1">
                    {activity.slice(0, 5).map((item) => (
                      <div key={item.id} className="rounded-md px-2 py-2 hover:bg-[rgb(var(--panel-2))]">
                        <div className="flex items-start gap-2">
                          <span className={'mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ' + statusClass[item.status]} />
                          <div className="min-w-0">
                            <div className="text-[11px] text-[rgb(var(--text))]">{item.title}</div>
                            <div className="mt-0.5 truncate text-[10px] text-[rgb(var(--muted-2))]">{item.detail}</div>
                          </div>
                          <span className="ml-auto shrink-0 text-[9px] text-[rgb(var(--muted-2))]">{item.time}</span>
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
                    <span className={'h-2 w-2 rounded-full ' + statusClass[worktree.status]} />
                    <h2 className="truncate font-medium">{worktree.branch}</h2>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <StatusBadge pr={worktree.prStatus} />
                    <StatusBadge ci={worktree.ciStatus} failed={worktree.ciFailed} />
                  </div>
                </div>

                <form onSubmit={submitTag} className="mb-3 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-2.5">
                  <div className="mb-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Worktree tag</div>
                  <div className="flex items-center gap-1.5">
                    <input
                      value={tagDraft}
                      onChange={(event) => setTagDraft(event.target.value)}
                      placeholder="feat, bug, review-code..."
                      className="min-w-0 flex-1 rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1.5 text-[10px] outline-none focus:border-[rgb(var(--purple))]"
                    />
                    <button type="submit" title="Save tag" className="grid h-7 w-7 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]">
                      <Save size={12} />
                    </button>
                  </div>
                  <div className="mt-1.5 text-[9px] text-[rgb(var(--muted-2))]">Tags are user-defined and drive canvas stacks.</div>
                </form>

                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Pull request" value={worktree.prNumber ? '#' + worktree.prNumber + ' · ' + worktree.prStatus : 'No PR'} />
                  <Row label="Sync" value={worktree.ahead + ' ahead · ' + worktree.behind + ' behind'} />
                  <Row label="Dirty files" value={worktree.dirtyFiles} />
                  <Row label="Agents" value={worktree.agentIds.length} />
                  <Row label="Last activity" value={worktree.lastActivity} />
                  <Row label="Git state" value={worktree.gitState ?? 'clean'} />
                </div>

                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={Bot} label="Start agent" onClick={startMockAgent} />
                  <QuickButton
                    icon={GitPullRequest}
                    label="Open PR"
                    onClick={() => setNotice(worktree.prNumber ? 'Opened PR #' + worktree.prNumber + ' (mock)' : 'No PR linked yet')}
                  />
                  {groupCount > 1 && (
                    <QuickButton
                      icon={Layers3}
                      label={groupCollapsed ? 'Expand stack' : 'Collapse stack'}
                      onClick={() => toggleTagGroup(project.id, worktree.tag)}
                    />
                  )}
                </div>
              </>
            )}

            {agent && (
              <>
                <div className="mb-3">
                  <div className="flex items-center gap-2">
                    <span
                      className={
                        'h-2 w-2 rounded-full ' +
                        (agent.state === 'running'
                          ? statusClass.healthy
                          : agent.state === 'finished'
                            ? statusClass.idle
                            : statusClass.warning)
                      }
                    />
                    <h2 className="font-medium">{agent.name}</h2>
                  </div>
                  <div className="mt-3 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-3">
                    <div className="text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Current task</div>
                    <p className="mt-1.5 text-[11px] leading-5 text-[rgb(var(--text))]">{agent.task}</p>
                  </div>
                </div>
                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Provider" value={agent.provider} />
                  <Row label="Status" value={agent.state} />
                  <Row
                    label="Worktree"
                    value={worktrees.find((item) => item.id === agent.worktreeId)?.branch ?? agent.worktreeId}
                  />
                  <Row
                    label="Tag"
                    value={worktrees.find((item) => item.id === agent.worktreeId)?.tag ?? '—'}
                  />
                  <Row label="Runtime" value={agent.runtime} />
                  <Row label="Terminal" value={agent.terminalId} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={TerminalSquare} label="Terminal" onClick={() => openTerminal(agent.id)} />
                  <QuickButton icon={RotateCcw} label="Restart" onClick={() => setAgentState(agent.id, 'running')} />
                  <QuickButton icon={Play} label="Run" onClick={() => setAgentState(agent.id, 'running')} />
                  <QuickButton icon={Square} label="Stop" onClick={() => setAgentState(agent.id, 'finished')} />
                </div>
              </>
            )}

            {!worktree && !agent && selection.type !== 'project' && (
              <div className="rounded-md border border-[rgb(var(--border))] p-3 text-[11px] text-[rgb(var(--muted))]">
                Select a node to inspect its operational state.
              </div>
            )}
          </div>
        </ScrollArea.Viewport>
        <ScrollArea.Scrollbar orientation="vertical" className="flex w-2.5 touch-none p-0.5">
          <ScrollArea.Thumb className="relative flex-1 rounded-full bg-[rgb(var(--border-strong))]" />
        </ScrollArea.Scrollbar>
      </ScrollArea.Root>
    </aside>
  )
}
