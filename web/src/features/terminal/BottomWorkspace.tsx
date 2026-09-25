import * as ScrollArea from '@radix-ui/react-scroll-area'
import * as Tabs from '@radix-ui/react-tabs'
import {
  Bot,
  CheckCircle2,
  ChevronDown,
  FileCode2,
  GitPullRequest,
  Maximize2,
  Minus,
  ScrollText,
  TestTube2,
  TerminalSquare,
} from 'lucide-react'
import { activity } from '../../mock/activity'
import { flattenFiles, repoFiles } from '../../mock/files'
import { pullRequests } from '../../mock/pullRequests'
import { useBonsaiStore } from '../../stores/bonsai'
import type { DockTab } from '../../types'
import { FakeTerminal } from './FakeTerminal'

const tabs: { id: DockTab; label: string; icon: React.ComponentType<{ size?: number }> }[] = [
  { id: 'agent', label: 'Agent', icon: Bot },
  { id: 'terminal', label: 'Terminal', icon: TerminalSquare },
  { id: 'tests', label: 'Tests', icon: TestTube2 },
  { id: 'files', label: 'Files', icon: FileCode2 },
  { id: 'pr', label: 'PR', icon: GitPullRequest },
  { id: 'checks', label: 'Checks', icon: CheckCircle2 },
  { id: 'logs', label: 'Logs', icon: ScrollText },
]

function DockScroll({ children }: { children: React.ReactNode }) {
  return (
    <ScrollArea.Root className="h-full overflow-hidden">
      <ScrollArea.Viewport className="h-full w-full">{children}</ScrollArea.Viewport>
      <ScrollArea.Scrollbar orientation="vertical" className="flex w-2.5 p-0.5">
        <ScrollArea.Thumb className="flex-1 rounded-full bg-[rgb(var(--border-strong))]" />
      </ScrollArea.Scrollbar>
    </ScrollArea.Root>
  )
}

export function BottomWorkspace() {
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const activeDockTab = useBonsaiStore((state) => state.activeDockTab)
  const setActiveDockTab = useBonsaiStore((state) => state.setActiveDockTab)
  const selection = useBonsaiStore((state) => state.selection)
  const agents = useBonsaiStore((state) => state.agents)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const terminalSessions = useBonsaiStore((state) => state.terminalSessions)
  const activeTerminalId = useBonsaiStore((state) => state.activeTerminalId)
  const setActiveTerminalId = useBonsaiStore((state) => state.setActiveTerminalId)
  const selectedFilePath = useBonsaiStore((state) => state.selectedFilePath)

  const agent = selection.type === 'agent' ? agents.find((item) => item.id === selection.id) : agents.find((item) => item.state === 'running')
  const worktree = agent ? worktrees.find((item) => item.id === agent.worktreeId) : selection.type === 'worktree' ? worktrees.find((item) => item.id === selection.id) : undefined
  const pr = pullRequests.find((item) => item.number === worktree?.prNumber) ?? pullRequests[0]
  const selectedFile = flattenFiles(repoFiles).find((file) => file.path === selectedFilePath) ?? flattenFiles(repoFiles).find((file) => file.type === 'file')

  return (
    <Tabs.Root
      value={activeDockTab}
      onValueChange={(value) => setActiveDockTab(value as DockTab)}
      className="flex h-full min-h-0 flex-col bg-[rgb(var(--panel))]"
    >
      <div className="flex h-9 shrink-0 items-center border-b border-[rgb(var(--border))] px-2">
        <Tabs.List className="flex h-full items-center">
          {tabs.map(({ id, label, icon: Icon }) => (
            <Tabs.Trigger
              key={id}
              value={id}
              className="bonsai-focus relative flex h-full items-center gap-1.5 px-2.5 text-[11px] text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))] data-[state=active]:text-[rgb(var(--text))] data-[state=active]:after:absolute data-[state=active]:after:bottom-0 data-[state=active]:after:left-2 data-[state=active]:after:right-2 data-[state=active]:after:h-px data-[state=active]:after:bg-[rgb(var(--purple))]"
            >
              <Icon size={12} /> {label}
            </Tabs.Trigger>
          ))}
        </Tabs.List>

        <div className="ml-auto flex items-center gap-1">
          <button
            onClick={() => setDockState(dockState === 'collapsed' ? 'normal' : 'collapsed')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title={dockState === 'collapsed' ? 'Expand dock' : 'Collapse dock'}
          >
            {dockState === 'collapsed' ? <ChevronDown size={13} /> : <Minus size={13} />}
          </button>
          <button
            onClick={() => setDockState(dockState === 'maximized' ? 'normal' : 'maximized')}
            className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
            title="Maximize dock"
          >
            <Maximize2 size={13} />
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1">
        <Tabs.Content value="terminal" className="h-full outline-none">
          <div className="flex h-full min-h-0">
            <div className="w-44 shrink-0 border-r border-[rgb(var(--border))] p-1.5">
              {terminalSessions.map((session) => (
                <button
                  key={session.id}
                  onClick={() => setActiveTerminalId(session.id)}
                  className={`bonsai-focus flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[11px] ${
                    activeTerminalId === session.id
                      ? 'bg-[rgb(var(--purple)/.10)] text-[rgb(var(--text))]'
                      : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]'
                  }`}
                >
                  <TerminalSquare size={12} />
                  <span className="truncate">{session.label}</span>
                </button>
              ))}
            </div>
            <div className="min-w-0 flex-1"><FakeTerminal /></div>
          </div>
        </Tabs.Content>

        <Tabs.Content value="agent" className="h-full outline-none">
          <DockScroll>
            <div className="grid gap-3 p-4 md:grid-cols-[1fr_220px]">
              <div>
                <div className="text-[10px] uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Current task</div>
                <div className="mt-2 text-[13px]">{agent?.task ?? 'Select an agent on the canvas.'}</div>
                <div className="mt-3 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-3 font-mono text-[11px] leading-5 text-[rgb(var(--muted))]">
                  {agent ? `[${agent.provider}] ${agent.name}\nstate: ${agent.state}\nruntime: ${agent.runtime}\n\nThinking through the next mock interaction…` : 'No active session.'}
                </div>
              </div>
              <div className="rounded-md border border-[rgb(var(--border))] p-3 text-[11px] text-[rgb(var(--muted))]">
                <div className="font-medium text-[rgb(var(--text))]">Session</div>
                <div className="mt-3 space-y-2">
                  <div>Provider <span className="float-right">{agent?.provider ?? '—'}</span></div>
                  <div>Status <span className="float-right">{agent?.state ?? '—'}</span></div>
                  <div>Worktree <span className="float-right max-w-28 truncate">{worktree?.branch ?? '—'}</span></div>
                </div>
              </div>
            </div>
          </DockScroll>
        </Tabs.Content>

        <Tabs.Content value="tests" className="h-full outline-none">
          <DockScroll>
            <div className="p-4 font-mono text-[11px] leading-6 text-[rgb(var(--muted))]">
              <div className="text-[rgb(var(--green))]">✓ workspace state <span className="text-[rgb(var(--muted-2))]">3 tests</span></div>
              <div className="text-[rgb(var(--green))]">✓ graph layout <span className="text-[rgb(var(--muted-2))]">2 tests</span></div>
              <div className="text-[rgb(var(--green))]">✓ command palette <span className="text-[rgb(var(--muted-2))]">4 tests</span></div>
              <div className="mt-2">Test Files <span className="text-[rgb(var(--green))]">3 passed</span> (3)</div>
            </div>
          </DockScroll>
        </Tabs.Content>

        <Tabs.Content value="files" className="h-full outline-none">
          <DockScroll>
            <div className="p-4">
              <div className="mb-2 font-mono text-[10px] text-[rgb(var(--muted-2))]">{selectedFile?.path}</div>
              <pre className="whitespace-pre-wrap rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-3 font-mono text-[11px] leading-5 text-[rgb(var(--muted))]">{selectedFile?.content ?? 'Select a file from the Files view.'}</pre>
            </div>
          </DockScroll>
        </Tabs.Content>

        <Tabs.Content value="pr" className="h-full outline-none">
          <DockScroll>
            <div className="p-4">
              <div className="flex items-center gap-2"><GitPullRequest size={14} /><span className="font-medium">#{pr.number} {pr.title}</span></div>
              <div className="mt-2 text-[11px] text-[rgb(var(--muted))]">{pr.branch} → {pr.base}</div>
              <div className="mt-4 space-y-2">
                {pr.commits.map((commit) => (
                  <div key={commit.sha} className="flex gap-3 rounded-md border border-[rgb(var(--border))] px-3 py-2 text-[11px]">
                    <span className="font-mono text-[rgb(var(--purple))]">{commit.sha}</span>
                    <span>{commit.message}</span>
                    <span className="ml-auto text-[rgb(var(--muted-2))]">{commit.author}</span>
                  </div>
                ))}
              </div>
            </div>
          </DockScroll>
        </Tabs.Content>

        <Tabs.Content value="checks" className="h-full outline-none">
          <DockScroll>
            <div className="p-4">
              {pr.checks.map((check) => (
                <div key={check.name} className="flex items-center gap-2 border-b border-[rgb(var(--border))] py-2.5 text-[11px] last:border-0">
                  <span className={`h-2 w-2 rounded-full ${check.status === 'success' ? 'bg-[rgb(var(--green))]' : check.status === 'failed' ? 'bg-[rgb(var(--red))]' : 'bg-[rgb(var(--orange))]'}`} />
                  {check.name}
                  <span className="ml-auto capitalize text-[rgb(var(--muted-2))]">{check.status}</span>
                </div>
              ))}
            </div>
          </DockScroll>
        </Tabs.Content>

        <Tabs.Content value="logs" className="h-full outline-none">
          <DockScroll>
            <div className="p-3 font-mono text-[10px] leading-6 text-[rgb(var(--muted))]">
              {activity.map((item) => (
                <div key={item.id}><span className="mr-3 text-[rgb(var(--muted-2))]">{item.time.padStart(4, ' ')}</span>{item.title} — {item.detail}</div>
              ))}
            </div>
          </DockScroll>
        </Tabs.Content>
      </div>
    </Tabs.Root>
  )
}
