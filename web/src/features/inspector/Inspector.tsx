import * as ScrollArea from '@radix-ui/react-scroll-area'
import {
  Bot,
  Branch,
  ExternalLink,
  GitPullRequest,
  Play,
  RotateCcw,
  Square,
  TerminalSquare,
} from 'lucide-react'
import { activity } from '../../mock/activity'
import { projects } from '../../mock/projects'
import { useBonsaiStore } from '../../stores/bonsai'
import type { Health } from '../../types'

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
  icon: React.ComponentType<{ size?: number }>
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

export function Inspector() {
  const selection = useBonsaiStore((state) => state.selection)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const startMockAgent = useBonsaiStore((state) => state.startMockAgent)
  const createMockWorktree = useBonsaiStore((state) => state.createMockWorktree)

  const project = projects[0]
  const worktree = selection.type === 'worktree' ? worktrees.find((item) => item.id === selection.id) : undefined
  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : undefined

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
                    <span className={`h-2 w-2 rounded-full ${statusClass[project.health]}`} />
                    <h2 className="font-medium">{project.name}</h2>
                  </div>
                  <p className="mt-2 text-[11px] leading-5 text-[rgb(var(--muted))]">{project.description}</p>
                </div>
                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Repository" value={project.repository} />
                  <Row label="Worktrees" value={worktrees.length} />
                  <Row label="Agents" value={agents.length} />
                  <Row label="Open PRs" value={project.openPrCount} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={Branch} label="Worktree" onClick={createMockWorktree} />
                  <QuickButton icon={Bot} label="Agent" onClick={startMockAgent} />
                </div>
                <section className="mt-5">
                  <div className="mb-2 text-[10px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Recent activity</div>
                  <div className="space-y-1">
                    {activity.slice(0, 5).map((item) => (
                      <div key={item.id} className="rounded-md px-2 py-2 hover:bg-[rgb(var(--panel-2))]">
                        <div className="flex items-start gap-2">
                          <span className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${statusClass[item.status]}`} />
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
                <div className="mb-4">
                  <div className="flex items-center gap-2">
                    <span className={`h-2 w-2 rounded-full ${statusClass[worktree.status]}`} />
                    <h2 className="truncate font-medium">{worktree.branch}</h2>
                  </div>
                  <div className="mt-2 inline-flex rounded border border-[rgb(var(--border))] px-1.5 py-0.5 text-[9px] text-[rgb(var(--muted))]">
                    {worktree.kind}
                  </div>
                </div>
                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Branch" value={worktree.branch} />
                  <Row label="Status" value={worktree.status} />
                  <Row label="Ahead / behind" value={`${worktree.ahead} / ${worktree.behind}`} />
                  <Row
                    label="Pull request"
                    value={worktree.prNumber ? <span className="inline-flex items-center gap-1"><GitPullRequest size={11} />#{worktree.prNumber}</span> : '—'}
                  />
                  <Row label="Agents" value={worktree.agentIds.length} />
                  <Row label="Git state" value={worktree.gitState ?? 'clean'} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={Bot} label="Start agent" onClick={startMockAgent} />
                  <QuickButton icon={ExternalLink} label="Open PR" />
                </div>
              </>
            )}

            {agent && (
              <>
                <div className="mb-4">
                  <div className="flex items-center gap-2">
                    <span
                      className={`h-2 w-2 rounded-full ${
                        agent.state === 'running'
                          ? statusClass.healthy
                          : agent.state === 'finished'
                            ? statusClass.idle
                            : statusClass.warning
                      }`}
                    />
                    <h2 className="font-medium">{agent.name}</h2>
                  </div>
                  <p className="mt-2 text-[11px] leading-5 text-[rgb(var(--muted))]">{agent.task}</p>
                </div>
                <div className="rounded-md border border-[rgb(var(--border))] px-3">
                  <Row label="Provider" value={agent.provider} />
                  <Row label="Status" value={agent.state} />
                  <Row label="Worktree" value={worktrees.find((item) => item.id === agent.worktreeId)?.branch ?? agent.worktreeId} />
                  <Row label="Runtime" value={agent.runtime} />
                  <Row label="Terminal" value={agent.terminalId} />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <QuickButton icon={TerminalSquare} label="Terminal" onClick={() => openTerminal(agent.id)} />
                  <QuickButton icon={RotateCcw} label="Restart" onClick={() => setAgentState(agent.id, 'running')} />
                  <QuickButton icon={Play} label="Focus" onClick={() => setAgentState(agent.id, 'running')} />
                  <QuickButton icon={Square} label="Stop" onClick={() => setAgentState(agent.id, 'finished')} />
                </div>
              </>
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
