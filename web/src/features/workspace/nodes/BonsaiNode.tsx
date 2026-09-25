import * as ContextMenu from '@radix-ui/react-context-menu'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import {
  Bot,
  GitBranch,
  CircleDot,
  ExternalLink,
  FolderGit2,
  GitPullRequest,
  Play,
  RotateCcw,
  Square,
  TerminalSquare,
} from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import type { AgentState, Health, WorktreeKind } from '../../../types'

export interface BonsaiGraphData extends Record<string, unknown> {
  entityId: string
  kind: 'project' | 'worktree' | 'agent'
  title: string
  subtitle?: string
  health?: Health
  worktreeKind?: WorktreeKind
  agentState?: AgentState
  provider?: string
  task?: string
  runtime?: string
  stats?: { label: string; value: string | number }[]
  prNumber?: number
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

function NodeShell({ data, selected }: { data: BonsaiGraphData; selected: boolean }) {
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)

  const icon =
    data.kind === 'project' ? (
      <FolderGit2 size={14} />
    ) : data.kind === 'worktree' ? (
      <GitBranch size={14} />
    ) : (
      <Bot size={14} />
    )

  const status =
    data.kind === 'agent'
      ? data.agentState === 'running'
        ? 'healthy'
        : data.agentState === 'finished'
          ? 'idle'
          : 'warning'
      : (data.health ?? 'idle')

  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>
        <div
          className={`w-[236px] rounded-lg border bg-[rgb(var(--panel-2))] shadow-[0_6px_24px_rgb(0_0_0/.12)] transition-[border-color,background-color] ${
            selected
              ? 'border-[rgb(var(--purple))] bg-[rgb(var(--panel-3))]'
              : 'border-[rgb(var(--border))] hover:border-[rgb(var(--border-strong))]'
          }`}
        >
          <Handle type="target" position={Position.Left} className="!h-2 !w-2 !border-[rgb(var(--border-strong))] !bg-[rgb(var(--panel-3))]" />

          <div className="flex items-start gap-2.5 border-b border-[rgb(var(--border))] p-3">
            <div className="mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--muted))]">
              {icon}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className={`h-1.5 w-1.5 rounded-full ${healthColor[status]}`} />
                <span className="truncate text-[12px] font-medium text-[rgb(var(--text))]">{data.title}</span>
              </div>
              {data.subtitle && (
                <div className="mt-1 truncate font-mono text-[10px] text-[rgb(var(--muted-2))]">
                  {data.subtitle}
                </div>
              )}
            </div>
            {data.worktreeKind && (
              <span className="rounded border border-[rgb(var(--border))] px-1.5 py-0.5 text-[9px] text-[rgb(var(--muted))]">
                {data.worktreeKind}
              </span>
            )}
          </div>

          {data.kind === 'agent' ? (
            <div className="space-y-2 p-3">
              <div className="line-clamp-2 min-h-8 text-[11px] leading-4 text-[rgb(var(--muted))]">
                {data.task}
              </div>
              <div className="flex items-center justify-between text-[10px] text-[rgb(var(--muted-2))]">
                <span>{data.provider}</span>
                <span>{data.runtime}</span>
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-3 divide-x divide-[rgb(var(--border))]">
              {(data.stats ?? []).slice(0, 3).map((stat) => (
                <div key={stat.label} className="px-2 py-2 text-center">
                  <div className="text-[12px] font-medium">{stat.value}</div>
                  <div className="mt-0.5 text-[9px] uppercase tracking-wide text-[rgb(var(--muted-2))]">{stat.label}</div>
                </div>
              ))}
            </div>
          )}

          {(data.prNumber || data.gitState) && (
            <div className="flex items-center gap-3 border-t border-[rgb(var(--border))] px-3 py-2 text-[10px] text-[rgb(var(--muted-2))]">
              {data.prNumber && (
                <span className="flex items-center gap-1"><GitPullRequest size={11} />#{data.prNumber}</span>
              )}
              {data.gitState && <span className="flex items-center gap-1"><CircleDot size={10} />{data.gitState}</span>}
            </div>
          )}

          <Handle type="source" position={Position.Right} className="!h-2 !w-2 !border-[rgb(var(--border-strong))] !bg-[rgb(var(--panel-3))]" />
        </div>
      </ContextMenu.Trigger>

      <ContextMenu.Portal>
        <ContextMenu.Content className="z-50 min-w-44 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-1 shadow-2xl">
          <MenuItem
            onSelect={() =>
              setSelection({ type: data.kind, id: data.entityId } as Parameters<typeof setSelection>[0])
            }
          >
            <ExternalLink size={13} /> Open inspector
          </MenuItem>
          {data.kind === 'agent' && (
            <>
              <MenuItem onSelect={() => openTerminal(data.entityId)}>
                <TerminalSquare size={13} /> Open terminal
              </MenuItem>
              <ContextMenu.Separator className="my-1 h-px bg-[rgb(var(--border))]" />
              <MenuItem onSelect={() => setAgentState(data.entityId, 'running')}>
                <Play size={13} /> Start
              </MenuItem>
              <MenuItem onSelect={() => setAgentState(data.entityId, 'idle')}>
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
