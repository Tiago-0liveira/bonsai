import * as ContextMenu from '@radix-ui/react-context-menu'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import {
  Archive,
  Bot,
  CircleCheck,
  CircleDot,
  CircleX,
  Clock3,
  ExternalLink,
  FolderGit2,
  GitBranch,
  GitPullRequest,
  Layers3,
  LoaderCircle,
  Network,
  Play,
  Plus,
  RotateCcw,
  Settings2,
  Square,
  TerminalSquare,
} from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import type { AgentState, CiStatus, Health, PrStatus } from '../../../types'

export interface BonsaiGraphData extends Record<string, unknown> {
  entityId: string
  kind: 'project' | 'worktree' | 'agent' | 'stack'
  title: string
  subtitle?: string
  health?: Health
  agentState?: AgentState
  provider?: string
  task?: string
  runtime?: string
  stats?: { label: string; value: string | number }[]
  defaultBranch?: string
  ciSummary?: string
  tag?: string
  tagCount?: number
  stackCount?: number
  prNumber?: number
  prStatus?: PrStatus
  ciStatus?: CiStatus
  ciFailed?: number
  gitState?: string
}

const healthColor: Record<Health, string> = {
  healthy: 'bg-[rgb(var(--green))]',
  warning: 'bg-[rgb(var(--orange))]',
  error: 'bg-[rgb(var(--red))]',
  idle: 'bg-[rgb(var(--muted-2))]',
}

function MenuItem({
  children,
  onSelect,
}: {
  children: React.ReactNode
  onSelect?: () => void
}) {
  return (
    <ContextMenu.Item
      onSelect={onSelect}
      className="flex cursor-default select-none items-center gap-2 rounded px-2 py-1.5 text-[12px] text-[rgb(var(--muted))] outline-none data-[highlighted]:bg-[rgb(var(--purple)/.12)] data-[highlighted]:text-[rgb(var(--text))]"
    >
      {children}
    </ContextMenu.Item>
  )
}

function PrBadge({ status, number }: { status?: PrStatus; number?: number }) {
  if (!status) return null
  const tone =
    status === 'Open'
      ? 'border-[rgb(var(--green)/.25)] bg-[rgb(var(--green)/.08)] text-[rgb(var(--green))]'
      : status === 'Draft'
        ? 'border-[rgb(var(--purple)/.25)] bg-[rgb(var(--purple)/.08)] text-[rgb(var(--purple))]'
        : status === 'Merged'
          ? 'border-[rgb(var(--blue)/.25)] bg-[rgb(var(--blue)/.08)] text-[rgb(var(--blue))]'
          : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--muted))]'
  return (
    <span className={'inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[9px] ' + tone}>
      <GitPullRequest size={10} />
      {status}{number ? ' #' + number : ''}
    </span>
  )
}

function CiBadge({ status, failed = 0 }: { status?: CiStatus; failed?: number }) {
  if (!status) return null
  const meta =
    status === 'passed'
      ? { label: 'CI passed', icon: CircleCheck, tone: 'text-[rgb(var(--green))] border-[rgb(var(--green)/.25)]' }
      : status === 'running'
        ? { label: 'CI running', icon: LoaderCircle, tone: 'text-[rgb(var(--blue))] border-[rgb(var(--blue)/.25)]' }
        : status === 'failed'
          ? { label: 'CI failed' + (failed ? ' · ' + failed : ''), icon: CircleX, tone: 'text-[rgb(var(--red))] border-[rgb(var(--red)/.25)]' }
          : { label: 'Waiting to launch', icon: Clock3, tone: 'text-[rgb(var(--orange))] border-[rgb(var(--orange)/.25)]' }
  const Icon = meta.icon
  return (
    <span className={'inline-flex items-center gap-1 rounded border bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[9px] ' + meta.tone}>
      <Icon size={10} className={status === 'running' ? 'animate-spin' : ''} />
      {meta.label}
    </span>
  )
}

function NodeShell({ data, selected }: { data: BonsaiGraphData; selected: boolean }) {
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const createMockWorktree = useBonsaiStore((state) => state.createMockWorktree)
  const startMockAgent = useBonsaiStore((state) => state.startMockAgent)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const requestCanvasAction = useBonsaiStore((state) => state.requestCanvasAction)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)

  const status: Health =
    data.kind === 'agent'
      ? data.agentState === 'running'
        ? 'healthy'
        : data.agentState === 'finished'
          ? 'idle'
          : 'warning'
      : (data.health ?? 'idle')

  const shellWidth =
    data.kind === 'project'
      ? 'w-[300px]'
      : data.kind === 'agent'
        ? 'w-[172px]'
        : 'w-[210px]'

  const icon =
    data.kind === 'project' ? (
      <FolderGit2 size={15} />
    ) : data.kind === 'worktree' ? (
      <GitBranch size={13} />
    ) : data.kind === 'stack' ? (
      <Layers3 size={13} />
    ) : (
      <Bot size={13} />
    )

  const selectNode = () => {
    if (data.kind === 'stack') {
      if (data.tag) toggleTagGroup(activeProjectId, data.tag)
      return
    }
    setSelection({ type: data.kind, id: data.entityId })
  }

  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>
        <div
          onDoubleClick={data.kind === 'stack' ? selectNode : undefined}
          className={
            shellWidth +
            ' rounded-lg border bg-[rgb(var(--panel-2))] shadow-[0_6px_20px_rgb(0_0_0/.10)] transition-[border-color,background-color] ' +
            (selected
              ? 'border-[rgb(var(--purple))] bg-[rgb(var(--panel-3))]'
              : 'border-[rgb(var(--border))] hover:border-[rgb(var(--border-strong))]')
          }
        >
          {data.kind !== 'project' && (
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-[rgb(var(--border-strong))] !bg-[rgb(var(--panel-3))]" />
          )}

          {data.kind === 'project' && (
            <>
              <div className="flex items-start gap-3 border-b border-[rgb(var(--border))] p-3.5">
                <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-[rgb(var(--green)/.28)] bg-[rgb(var(--green)/.07)] text-[rgb(var(--green))]">
                  {icon}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className={'h-2 w-2 rounded-full ' + healthColor[status]} />
                    <span className="truncate text-[13px] font-semibold">{data.title}</span>
                  </div>
                  <div className="mt-1 truncate font-mono text-[10px] text-[rgb(var(--muted-2))]">{data.subtitle}</div>
                  <div className="mt-1.5 flex items-center gap-2 text-[9px] text-[rgb(var(--muted))]">
                    <span>{data.defaultBranch}</span>
                    <span>·</span>
                    <span>{data.ciSummary}</span>
                  </div>
                </div>
              </div>
              <div className="grid grid-cols-4 divide-x divide-[rgb(var(--border))]">
                {(data.stats ?? []).slice(0, 4).map((stat) => (
                  <div key={stat.label} className="px-2 py-2 text-center">
                    <div className="text-[12px] font-semibold">{stat.value}</div>
                    <div className="mt-0.5 truncate text-[8px] uppercase tracking-wide text-[rgb(var(--muted-2))]">{stat.label}</div>
                  </div>
                ))}
              </div>
            </>
          )}

          {data.kind === 'worktree' && (
            <>
              <div className="flex items-start gap-2 border-b border-[rgb(var(--border))] p-2.5">
                <div className="grid h-7 w-7 shrink-0 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--blue))]">
                  {icon}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[11px] font-semibold">{data.title}</div>
                  <div className="mt-1 flex items-center gap-1.5">
                    <span className="rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[8px] font-medium text-[rgb(var(--muted))]">
                      {data.tag}
                    </span>
                    <span className={'h-1.5 w-1.5 rounded-full ' + healthColor[status]} />
                  </div>
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5 px-2.5 py-2">
                <PrBadge status={data.prStatus} number={data.prNumber} />
                <CiBadge status={data.ciStatus} failed={data.ciFailed} />
              </div>
              <div className="flex items-center justify-between border-t border-[rgb(var(--border))] px-2.5 py-1.5 text-[9px] text-[rgb(var(--muted-2))]">
                <span>{data.subtitle}</span>
                <span>{data.stats?.[0]?.value ?? 0} agents</span>
              </div>
            </>
          )}

          {data.kind === 'stack' && (
            <button onClick={selectNode} className="block w-full cursor-pointer text-left">
              <div className="flex items-center gap-2.5 border-b border-[rgb(var(--border))] p-3">
                <div className="grid h-7 w-7 place-items-center rounded-md border border-[rgb(var(--purple)/.25)] bg-[rgb(var(--purple)/.08)] text-[rgb(var(--purple))]">
                  {icon}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-[11px] font-semibold">{data.title}</span>
                    <span className="rounded bg-[rgb(var(--purple)/.12)] px-1.5 py-0.5 text-[8px] text-[rgb(var(--purple))]">
                      {data.stackCount}
                    </span>
                  </div>
                  <div className="mt-1 text-[9px] text-[rgb(var(--muted-2))]">stacked worktrees</div>
                </div>
              </div>
              <div className="flex items-center justify-between px-3 py-2 text-[9px] text-[rgb(var(--muted))]">
                <span>{data.subtitle}</span>
                <span className="text-[rgb(var(--purple))]">Expand</span>
              </div>
            </button>
          )}

          {data.kind === 'agent' && (
            <div className="p-2.5">
              <div className="flex items-center gap-2">
                <div className="grid h-6 w-6 shrink-0 place-items-center rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--muted))]">
                  {icon}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className={'h-1.5 w-1.5 rounded-full ' + healthColor[status]} />
                    <span className="truncate text-[10px] font-semibold">{data.title}</span>
                  </div>
                  <div className="mt-0.5 flex items-center justify-between text-[8px] text-[rgb(var(--muted-2))]">
                    <span>{data.provider}</span>
                    <span>{data.runtime}</span>
                  </div>
                </div>
              </div>
              <div className="mt-2 line-clamp-1 text-[9px] text-[rgb(var(--muted))]">{data.task}</div>
            </div>
          )}

          {(data.kind === 'project' || data.kind === 'worktree') && (
            <Handle type="source" position={Position.Bottom} className="!h-2 !w-2 !border-[rgb(var(--border-strong))] !bg-[rgb(var(--panel-3))]" />
          )}
        </div>
      </ContextMenu.Trigger>

      <ContextMenu.Portal>
        <ContextMenu.Content className="z-50 min-w-48 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-1 shadow-2xl">
          {data.kind !== 'stack' && (
            <MenuItem onSelect={selectNode}>
              <ExternalLink size={13} /> Open inspector
            </MenuItem>
          )}

          {data.kind === 'project' && (
            <>
              <MenuItem onSelect={() => createMockWorktree('feat')}>
                <Plus size={13} /> Add worktree
              </MenuItem>
              <MenuItem
                onSelect={() => {
                  selectNode()
                  startMockAgent()
                }}
              >
                <Bot size={13} /> Start agent
              </MenuItem>
              <ContextMenu.Separator className="my-1 h-px bg-[rgb(var(--border))]" />
              <MenuItem onSelect={() => requestCanvasAction('layout')}>
                <Network size={13} /> Auto-layout children
              </MenuItem>
              <MenuItem onSelect={() => setNotice('Project settings are mocked')}>
                <Settings2 size={13} /> Project settings
              </MenuItem>
            </>
          )}

          {data.kind === 'worktree' && (
            <>
              <MenuItem
                onSelect={() => {
                  selectNode()
                  startMockAgent()
                }}
              >
                <Play size={13} /> Start agent
              </MenuItem>
              <MenuItem onSelect={() => setNotice(data.prNumber ? 'Opened PR #' + data.prNumber + ' (mock)' : 'No PR linked yet')}>
                <GitPullRequest size={13} /> Open pull request
              </MenuItem>
              {data.tag && (data.tagCount ?? 0) > 1 && (
                <MenuItem onSelect={() => toggleTagGroup(activeProjectId, data.tag as string)}>
                  <Layers3 size={13} /> Collapse {data.tag} stack
                </MenuItem>
              )}
              <ContextMenu.Separator className="my-1 h-px bg-[rgb(var(--border))]" />
              <MenuItem onSelect={() => setNotice('Archive is frontend-only in this prototype')}>
                <Archive size={13} /> Archive worktree
              </MenuItem>
            </>
          )}

          {data.kind === 'stack' && data.tag && (
            <MenuItem onSelect={() => toggleTagGroup(activeProjectId, data.tag as string)}>
              <Layers3 size={13} /> Expand {data.tag} worktrees
            </MenuItem>
          )}

          {data.kind === 'agent' && (
            <>
              <MenuItem onSelect={() => openTerminal(data.entityId)}>
                <TerminalSquare size={13} /> Open terminal
              </MenuItem>
              <ContextMenu.Separator className="my-1 h-px bg-[rgb(var(--border))]" />
              <MenuItem onSelect={() => setAgentState(data.entityId, 'running')}>
                <Play size={13} /> Start
              </MenuItem>
              <MenuItem onSelect={() => setAgentState(data.entityId, 'running')}>
                <RotateCcw size={13} /> Restart mock
              </MenuItem>
              <MenuItem onSelect={() => setAgentState(data.entityId, 'finished')}>
                <Square size={13} /> Stop
              </MenuItem>
            </>
          )}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  )
}

export function ProjectNode(props: NodeProps) {
  return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} />
}

export function WorktreeNode(props: NodeProps) {
  return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} />
}

export function AgentNode(props: NodeProps) {
  return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} />
}

export function StackNode(props: NodeProps) {
  return <NodeShell data={props.data as BonsaiGraphData} selected={false} />
}
