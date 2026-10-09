import { Fragment, memo } from 'react'
import { openGitHub } from '../../../api/git'
import { isLiveAgent } from '../../../api/agents'
import * as ContextMenu from '@radix-ui/react-context-menu'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import {
  Archive,
  Bot,
  CircleCheck,
  CircleX,
  Clock3,
  ExternalLink,
  GitBranch,
  GitCommitHorizontal,
  GitPullRequest,
  History,
  KeyRound,
  Layers3,
  LoaderCircle,
  Move,
  Network,
  Play,
  Plus,
  RotateCcw,
  Settings2,
  Square,
  TerminalSquare,
  Trash2,
  Undo2,
  X,
} from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import { ProviderBadge } from '../../../components/ui/ProviderBadge'
import type { AgentProvider, AgentState, CiStatus, DefaultBranchInfo, Health, ProcessLifecycleStatus, PrStatus } from '../../../types'

export interface StackItemData {
  id: string
  branch: string
  prNumber?: number
  prStatus?: PrStatus
  ciStatus: CiStatus
  connectionLabel?: string
  dirtyFiles?: number
  hasRunningAgent: boolean
  processCount?: number
}

export interface HistoryItemData {
  id: string
  name: string
  provider: string
  finishedAt?: string
}

export interface BonsaiGraphData extends Record<string, unknown> {
  entityId: string
  kind: 'project' | 'worktree' | 'agent' | 'process' | 'runtime-shelf' | 'stack' | 'default-branch'
  command?: string
  processStatus?: ProcessLifecycleStatus
  associationLabel?: string
  title: string
  subtitle?: string
  health?: Health
  agentState?: AgentState
  provider?: string
  model?: string
  reasoningEffort?: string
  task?: string
  runtime?: string
  stats?: { label: string; value: string | number }[]
  defaultBranch?: string
  defaultBranchInfo?: DefaultBranchInfo
  ciSummary?: string
  groupCount?: number
  groupId?: string
  connectionLabel?: string
  stackCount?: number
  stackItems?: StackItemData[]
  historyItems?: HistoryItemData[]
  mergeTargetBranch?: string
  targetIsWorktree?: boolean
  ahead?: number
  behind?: number
  prNumber?: number
  prStatus?: PrStatus
  ciStatus?: CiStatus
  ciFailed?: number
  gitState?: string
  worktreeId?: string
}

const agentDot: Record<AgentState, string> = {
  running: 'bg-accent-solid shadow-[0_0_0_3px_rgb(var(--accent-solid)/.20)]',
  idle: 'bg-warn-solid shadow-[0_0_0_3px_rgb(var(--warn-solid)/.25)]',
  finished: 'bg-muted-2 shadow-[0_0_0_3px_rgb(var(--muted-2)/.15)]',
}

function MenuItem({ children, onSelect, unavailable = false }: { children: React.ReactNode; onSelect?: () => void; unavailable?: boolean }) {
  return (
    <ContextMenu.Item
      onSelect={onSelect}
      disabled={unavailable}
      className="flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 text-[12px] text-muted outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-accent/12 data-[highlighted]:text-text"
    >
      {children}{unavailable && <span className="ml-auto text-[9px]">unavailable</span>}
    </ContextMenu.Item>
  )
}

function CiBadge({ status, failed = 0, compact = false, label = 'CI', iconOnly = false }: { status?: CiStatus; failed?: number; compact?: boolean; label?: string; iconOnly?: boolean }) {
  if (!status) return null
  const meta =
    status === 'passed'
      ? { text: compact ? 'passed' : label + ' passed', icon: CircleCheck, tone: 'text-ok border-ok/30', tint: 'bg-ok/10 text-ok' }
      : status === 'running'
        ? { text: compact ? 'running' : label + ' running', icon: LoaderCircle, tone: 'text-accent border-accent/30', tint: 'bg-accent/12 text-accent' }
        : status === 'failed'
          ? { text: compact ? 'failed' : label + ' failed' + (failed ? ' · ' + failed : ''), icon: CircleX, tone: 'text-danger border-danger/30', tint: 'bg-danger-solid/28 text-danger' }
          : status === 'none'
            ? { text: compact ? 'none' : label + ' no checks', icon: Clock3, tone: 'text-muted border-border', tint: 'bg-panel-3 text-muted-2' }
            : status === 'unknown'
              ? { text: compact ? 'unknown' : label + ' unknown', icon: Clock3, tone: 'text-muted border-border', tint: 'bg-panel-3 text-muted-2' }
              : { text: compact ? 'waiting' : label + ' waiting', icon: Clock3, tone: 'text-warn border-warn/30', tint: 'bg-panel-3 text-muted-2' }
  const Icon = meta.icon
  const text = compact ? label + ' ' + meta.text : meta.text
  const failedSuffix = status === 'failed' && failed && !compact ? ' · ' + failed : ''
  const coreText = failedSuffix && text.endsWith(failedSuffix) ? text.slice(0, -failedSuffix.length) : text
  if (iconOnly) {
    return (
      <span role="img" title={text} aria-label={text} className={'inline-grid h-5 w-[22px] shrink-0 place-items-center rounded-[5px] ' + meta.tint}>
        <Icon size={11} className={status === 'running' ? 'animate-spin motion-reduce:animate-none' : ''} />
      </span>
    )
  }
  return (
    <span className={'inline-flex items-center gap-1 rounded border bg-bg px-1.5 py-0.5 text-[8px] ' + meta.tone}>
      <Icon size={9} className={status === 'running' ? 'animate-spin' : ''} />
      <span>{coreText}</span>
      {failedSuffix && <span>{failedSuffix}</span>}
    </span>
  )
}

function PrBadge({ status, number }: { status?: PrStatus; number?: number }) {
  if (!status && !number) return null
  const tone =
    status === 'Open'
      ? 'bg-accent/12 text-accent'
      : status === 'Merged'
        ? 'bg-ok/10 text-ok'
        : status === 'Closed'
          ? 'bg-panel-3 text-muted-2'
          : 'bg-panel-3 text-muted'
  return (
    <span className={'inline-flex h-5 shrink-0 items-center gap-1 rounded-[5px] px-1.5 font-mono text-[10.5px] ' + tone}>
      <GitPullRequest size={10} />
      {number ? '#' + number : status}
    </span>
  )
}

export function MoveSubtreeGrip({ id }: { id: string }) {
  const setSubtreeMoveRoot = useBonsaiStore((state) => state.setSubtreeMoveRoot)
  return (
    <button
      type="button"
      onPointerDown={() => setSubtreeMoveRoot(id)}
      onClick={(event) => event.stopPropagation()}
      title="Move this node and all children"
      className="absolute right-1 top-1 z-20 grid h-6 w-6 cursor-grab place-items-center rounded-md border border-transparent bg-[rgb(var(--panel-2)/.92)] text-[rgb(var(--muted-2))] opacity-0 transition-all hover:border-[rgb(var(--border))] hover:text-[rgb(var(--text))] group-hover:opacity-100 active:cursor-grabbing"
    >
      <Move size={11} />
    </button>
  )
}

function DefaultBranchCard({ data }: { data: BonsaiGraphData }) {
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const info = data.defaultBranchInfo
  return (
    <div className="w-[232px] overflow-hidden rounded-xl border border-border bg-panel shadow-card">
      <Handle type="source" position={Position.Right} className="!h-2 !w-2" />
      <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2.5">
        <span className="grid h-7 w-7 place-items-center rounded-md bg-accent/12 text-accent">
          <GitBranch size={13} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-[11px] font-semibold">{data.title}</span>
            <span className="chip bg-accent-solid/14 text-accent">default</span>
          </div>
          <div className="mt-0.5 text-[8px] text-[rgb(var(--muted-2))]">read-only</div>
        </div>
      </div>
      <button
        type="button"
        onClick={() => info?.commitSha ? openGitHub('commit/' + info.commitSha) : setNotice('No commit loaded')}
        className="flex w-full items-start gap-2 px-3 py-2.5 text-left hover:bg-panel-2"
      >
        <GitCommitHorizontal size={11} className="mt-0.5 shrink-0 text-[rgb(var(--muted-2))]" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[9px] text-[rgb(var(--text))]">{info?.commitMessage ?? 'No commit metadata'}</span>
          <span className="mt-1 block font-mono text-[8px] text-[rgb(var(--muted-2))]">{info?.commitSha ?? '—'} · {info?.lastActivity ?? '—'}</span>
        </span>
      </button>
      <div className="flex flex-wrap items-center gap-1.5 border-t border-border-subtle px-3 py-2">
        <CiBadge status={info?.ciStatus} compact label="CI" />
        {info?.cdStatus && <CiBadge status={info.cdStatus} compact label="CD" />}
        {info?.releaseTag && <span className="rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-1.5 py-0.5 text-[8px] text-[rgb(var(--muted))]">{info.releaseTag}</span>}
        {info?.deployedAt && <span className="ml-auto text-[7px] text-[rgb(var(--muted-2))]">{info.deployedAt}</span>}
      </div>
    </div>
  )
}

function StackCard({ data }: { data: BonsaiGraphData }) {
  const toggleAutomaticGroup = useBonsaiStore(state => state.toggleAutomaticGroup)
  const setDeleteWorktreeId = useBonsaiStore(state => state.setDeleteWorktreeId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const ejectWorktreeFromStack = useBonsaiStore((state) => state.ejectWorktreeFromStack)
  const setSelection = useBonsaiStore((state) => state.setSelection)

  return (
    <div className="group relative w-[286px] overflow-hidden rounded-xl border border-border bg-panel shadow-card">
      <Handle type="target" position={Position.Top} className="!h-2 !w-2" />
      <MoveSubtreeGrip id={data.entityId} />
      <button
        type="button"
        onClick={(event) => { event.stopPropagation(); if (data.groupId) toggleAutomaticGroup(data.groupId) }}
        className="flex w-full items-center gap-2 border-b border-border-subtle px-3 py-2.5 text-left"
        style={{ boxShadow: 'inset 3px 0 0 rgb(var(--accent))' }}
      >
        <Layers3 size={12} className="text-accent" />
        <span className="font-semibold text-accent">{data.title}</span>
        <span className="chip bg-panel-4 text-muted">{data.stackCount}</span>
        <span className="ml-auto text-[8px] text-[rgb(var(--muted-2))]">Expand</span>
      </button>
      <div>
        {(data.stackItems ?? []).map((item) => (
          <div key={item.id} className="flex items-center gap-2 border-b border-border-subtle px-3 py-2 last:border-0">
            <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + (item.hasRunningAgent ? 'bg-[rgb(var(--accent-solid))] shadow-[0_0_7px_rgb(var(--accent-solid)/.8)]' : 'bg-[rgb(var(--muted-2))]')} title={item.hasRunningAgent ? 'Agent running' : 'No agent running'} />
            <button
              type="button"
              onClick={(event) => { event.stopPropagation(); setSelection({ type: 'worktree', id: item.id }) }}
              className="min-w-0 flex-1 truncate text-left font-mono text-[9px] hover:text-[rgb(var(--text))]"
            >
              <span className="block truncate">{item.branch}</span>
              {!!item.processCount && <span className="text-[8px] text-[rgb(var(--muted))]">{item.processCount} processes</span>}
              {item.connectionLabel && <span className="block text-[8px] text-[rgb(var(--warn))]">{item.connectionLabel} · {item.dirtyFiles ?? 0} changed</span>}
            </button>
            <ContextMenu.Root>
              <ContextMenu.Trigger asChild><button type="button" aria-label={'Manage ' + item.branch} onClick={event => { event.stopPropagation(); setDeleteWorktreeId(item.id) }} className="nodrag text-[rgb(var(--muted))]" title="Delete worktree"><Trash2 size={10} /></button></ContextMenu.Trigger>
              <ContextMenu.Portal><ContextMenu.Content className="z-[100] rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay"><MenuItem onSelect={() => setDeleteWorktreeId(item.id)}>Delete worktree</MenuItem></ContextMenu.Content></ContextMenu.Portal>
            </ContextMenu.Root>
            <PrBadge status={item.prStatus} number={item.prNumber} />
            <CiBadge status={item.ciStatus} compact />
            <button
              type="button"
              onClick={(event) => { event.stopPropagation(); ejectWorktreeFromStack(item.id) }}
              title="Detach this worktree from the stack until the group is toggled"
              className="nodrag grid h-5 w-5 shrink-0 place-items-center rounded text-[rgb(var(--muted-2))] hover:bg-panel-3 hover:text-text"
            >
              <X size={10} />
            </button>
          </div>
        ))}
      </div>
      <Handle type="source" position={Position.Bottom} className="!h-2 !w-2" />
    </div>
  )
}

const NodeShell = memo(function NodeShell({ data, selected }: { data: BonsaiGraphData; selected: boolean }) {
  const historyOpen = useBonsaiStore((state) => state.expandedHistoryWorktreeIds.includes(data.entityId))
  const toggleWorktreeHistory = useBonsaiStore((state) => state.toggleWorktreeHistory)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const moveAgentToHistory = useBonsaiStore((state) => state.moveAgentToHistory)
  const restoreAgentFromHistory = useBonsaiStore((state) => state.restoreAgentFromHistory)
  const archiveAgent = useBonsaiStore((state) => state.archiveAgent)
  const restoreAgent = useBonsaiStore((state) => state.restoreAgent)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const openStartAgentDialog = useBonsaiStore((state) => state.openStartAgentDialog)
  const openStartProcessDialog = useBonsaiStore((state) => state.openStartProcessDialog)
  const requestCanvasAction = useBonsaiStore((state) => state.requestCanvasAction)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const toggleAutomaticGroup = useBonsaiStore(state => state.toggleAutomaticGroup)
  const setDeleteWorktreeId = useBonsaiStore(state => state.setDeleteWorktreeId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const agent = useBonsaiStore((state) => data.kind === 'agent' ? state.agents.find((item) => item.id === data.entityId) : undefined)

  if (data.kind === 'default-branch') return <DefaultBranchCard data={data} />
  if (data.kind === 'stack') return <StackCard data={data} />

  const shellWidth = data.kind === 'agent' ? 'w-[153px] h-[54px] flex flex-col px-2 pb-[5px] pt-[7px]' : data.kind === 'project' ? 'w-[370px] px-3.5 py-3' : 'w-[300px]'
  const waiting = data.kind === 'agent' && data.agentState === 'idle'
  const hubCiRunning = data.kind === 'project' && !!data.ciSummary?.includes('running')
  const hubCiFailed = data.kind === 'project' && !!data.ciSummary?.includes('failed')
  const hubCiTone = hubCiFailed ? 'bg-danger-solid/28 text-danger' : hubCiRunning ? 'bg-accent/12 text-accent' : 'bg-ok/10 text-ok'
  const HubCiIcon = hubCiFailed ? CircleX : hubCiRunning ? LoaderCircle : CircleCheck
  const agentTitle = data.kind === 'agent'
    ? [[data.provider, data.model].filter(Boolean).join(' · '), [data.reasoningEffort, data.runtime].filter(Boolean).join(' · ')].filter(Boolean).join('\n')
    : undefined
  const shellTone = data.kind === 'worktree'
    ? 'rounded-xl bg-panel ' + (selected ? 'border-accent/55 shadow-[0_0_0_3px_rgb(var(--accent)/.10),var(--shadow-card)]' : 'border-border shadow-card hover:border-border-strong')
    : data.kind === 'project'
      ? 'rounded-[14px] bg-panel ' + (selected ? 'border-accent-solid' : 'border-accent-solid/55') + ' shadow-[0_0_0_4px_rgb(var(--accent-solid)/.08),inset_0_1px_0_rgb(var(--accent-solid)/.16),0_14px_28px_-16px_rgb(10_7_5/.85)]'
    : data.kind === 'agent'
      ? 'rounded-[10px] bg-panel-2 ' + (selected ? 'border-accent/55 shadow-[0_0_0_3px_rgb(var(--accent)/.10),var(--shadow-card)]' : (waiting ? 'border-warn-solid/55' : 'border-border-strong') + ' shadow-card hover:border-border-strong')
      : 'rounded-lg bg-[rgb(var(--panel-2))] shadow-card ' + (selected ? 'border-[rgb(var(--accent))] bg-[rgb(var(--panel-3))]' : 'border-[rgb(var(--border))] hover:border-[rgb(var(--border-strong))]')

  const selectNode = () => {
    if (data.kind === 'project' || data.kind === 'worktree' || data.kind === 'agent') {
      setSelection({ type: data.kind, id: data.entityId })
    }
  }

  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>
        <div
          title={agentTitle}
          className={'group relative ' + shellWidth + ' border transition-[border-color,background-color,box-shadow] ' + shellTone}
        >
          {data.kind !== 'project' && (
            <Handle type="target" position={Position.Top} className="!h-2 !w-2" />
          )}
          {data.kind === 'project' && (
            <Handle type="target" position={Position.Left} className="!h-2 !w-2" />
          )}
          {data.kind === 'worktree' && (
            <>
              <Handle id="pr-source" type="source" position={Position.Left} className="!h-2.5 !w-2.5 !border-ok/60" title="PR merge source" />
              <Handle id="pr-target" type="target" position={Position.Right} className="!h-2.5 !w-2.5 !border-ok/60" title="PR merge target" />
            </>
          )}

          {(data.kind === 'project' || data.kind === 'worktree') && <MoveSubtreeGrip id={data.entityId} />}

          {data.kind === 'project' && (
            <>
              <div className="flex h-5 items-center gap-2">
                <GitBranch size={13} className="shrink-0 text-accent" />
                <span title={data.defaultBranch ?? data.title} className="min-w-0 truncate font-mono text-[13px] font-semibold text-text">{data.defaultBranch ?? data.title}</span>
                <span className="chip shrink-0 bg-accent-solid/14 uppercase tracking-[.04em] text-accent">default</span>
                <span className={'chip ml-auto inline-flex shrink-0 items-center gap-1 ' + hubCiTone}>
                  <HubCiIcon size={10} className={hubCiRunning ? 'animate-spin motion-reduce:animate-none' : ''} />
                  {data.ciSummary}
                </span>
              </div>
              <div title={data.title + ' · ' + data.subtitle} className="mt-1 h-4 truncate font-mono text-[10px] leading-4 text-muted-2">{data.title} · {data.subtitle}</div>
              <div className="mt-1 flex h-4 items-center gap-1.5 whitespace-nowrap font-mono text-[10px] leading-4 text-muted-2">
                {(data.stats ?? []).slice(0, 4).map((stat, index) => (
                  <Fragment key={stat.label}>
                    {index > 0 && <span aria-hidden>·</span>}
                    <span className={stat.label === 'ci failed' && Number(stat.value) > 0 ? 'text-danger' : ''}>{stat.value} {stat.label}</span>
                  </Fragment>
                ))}
              </div>
            </>
          )}

          {data.kind === 'worktree' && (
            <>
              <div className="px-3 py-[9px]">
                <div className="flex h-5 items-center gap-1.5">
                  <GitBranch size={13} className="shrink-0 text-muted-2" />
                  <span title={data.title} className="min-w-0 flex-1 truncate font-mono text-[12px] font-semibold text-text">{data.title}</span>
                  {data.connectionLabel && <span className="shrink-0 font-mono text-[10px] text-warn">{data.connectionLabel}</span>}
                </div>
                <div className="mt-1.5 flex h-5 items-center gap-1.5">
                  <CiBadge status={data.ciStatus} failed={data.ciFailed} iconOnly />
                  <PrBadge status={data.prStatus} number={data.prNumber} />
                  <span className="shrink-0 font-mono text-[10px] text-muted-2">
                    ↑{data.ahead ?? 0} <span className={(data.behind ?? 0) > 0 ? 'text-warn' : ''}>↓{data.behind ?? 0}</span>
                  </span>
                  <span title={'Merge target ' + data.mergeTargetBranch} className={'ml-auto min-w-0 truncate font-mono text-[10px] ' + (data.targetIsWorktree ? 'text-ok' : 'text-muted-2')}>→ {data.mergeTargetBranch}</span>
                </div>
              </div>
              {(data.historyItems?.length ?? 0) > 0 && (
                <div>
                  <button
                    type="button"
                    onClick={(event) => {
                      event.stopPropagation()
                      toggleWorktreeHistory(data.entityId)
                    }}
                    className="nodrag flex h-[26px] w-full items-center gap-1.5 border-t border-border-subtle px-3 text-left text-[10px] text-muted hover:bg-panel-2 hover:text-text"
                  >
                    <History size={10} />
                    <span>History</span>
                    <span className="chip bg-panel-4 text-muted">{data.historyItems?.length}</span>
                    <span className="ml-auto font-mono text-[9.5px] text-muted-2">{historyOpen ? 'Hide' : 'Show'}</span>
                  </button>
                  {historyOpen && (
                    <div>
                      {(data.historyItems ?? []).map((item) => (
                        <div key={item.id} className="flex h-6 items-center gap-1.5 px-3 text-[10px] hover:bg-panel-2">
                          <Bot size={10} className="shrink-0 text-muted-2" />
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation()
                              setSelection({ type: 'agent', id: item.id })
                            }}
                            className="nodrag min-w-0 flex-1 truncate text-left text-muted hover:text-text"
                          >
                            {item.name}
                          </button>
                          <span className="shrink-0 font-mono text-[9.5px] text-muted-2">{item.finishedAt}</span>
                          <button
                            type="button"
                            title="Restore agent to canvas"
                            onClick={(event) => {
                              event.stopPropagation()
                              restoreAgentFromHistory(item.id)
                            }}
                            className="nodrag grid h-5 w-5 place-items-center rounded text-muted-2 hover:bg-panel-3 hover:text-text"
                          >
                            <Undo2 size={10} />
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}

          {data.kind === 'agent' && (
            <>
              <div className="flex h-5 shrink-0 items-center gap-1.5">
                <ProviderBadge provider={(agent?.provider ?? data.provider) as AgentProvider} size={18} />
                <span title={data.title} className="min-w-0 flex-1 truncate text-[11.5px] font-semibold text-text">{data.title}</span>
                <span aria-hidden className={'h-1.5 w-1.5 shrink-0 rounded-full ' + agentDot[data.agentState ?? 'finished']} />
              </div>
              <div className="flex h-5 shrink-0 items-center gap-[3px]">
                <span title={data.task} className={'min-w-0 flex-1 truncate font-mono text-[9.5px] ' + (waiting ? 'text-warn' : 'text-muted-2')}>{data.task}</span>
                <div className="nodrag nopan flex shrink-0" onPointerDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
                  <button
                    type="button"
                    aria-label="Open terminal"
                    title="Open terminal"
                    disabled={!isLiveAgent(agent)}
                    onClick={() => openTerminal(data.entityId)}
                    className="bonsai-focus icon-btn-20 transition-colors hover:bg-panel-3 hover:text-text disabled:cursor-default disabled:opacity-30 disabled:hover:bg-panel-2 disabled:hover:text-muted"
                  >
                    <TerminalSquare size={11} />
                  </button>
                </div>
              </div>
            </>
          )}

          {(data.kind === 'project' || data.kind === 'worktree') && (
            <Handle type="source" position={Position.Bottom} className="!h-2 !w-2" />
          )}
        </div>
      </ContextMenu.Trigger>

      <ContextMenu.Portal>
        <ContextMenu.Content className="z-[100] min-w-48 rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay">
          <MenuItem onSelect={selectNode}><ExternalLink size={13} /> Open inspector</MenuItem>

          {data.kind === 'project' && (
            <>
              <MenuItem onSelect={() => setWorktreeDialogOpen(true)}><Plus size={13} /> Add worktree</MenuItem>
              <MenuItem onSelect={() => openStartAgentDialog()}><Bot size={13} /> Start agent</MenuItem>
              <MenuItem onSelect={() => openStartProcessDialog('', data.entityId)}><Play size={13} /> Start process</MenuItem>
              <ContextMenu.Separator className="my-1 h-px bg-border-subtle" />
              <MenuItem onSelect={() => requestCanvasAction('layout')}><Network size={13} /> Auto-layout children</MenuItem>
              <MenuItem onSelect={() => setNotice('Project settings are mocked')}><Settings2 size={13} /> Project settings</MenuItem>
            </>
          )}

          {data.kind === 'worktree' && (
            <>
              <MenuItem onSelect={() => openStartAgentDialog(data.entityId)}><Play size={13} /> Start agent</MenuItem>
              <MenuItem onSelect={() => openStartProcessDialog(data.entityId)}><Play size={13} /> Start process</MenuItem>
              <MenuItem onSelect={() => data.prNumber ? openGitHub('pull/' + data.prNumber) : setNotice('No PR linked yet')}><GitPullRequest size={13} /> Open pull request</MenuItem>
              {data.groupId && (data.groupCount ?? 0) > 1 && <MenuItem onSelect={() => toggleAutomaticGroup(data.groupId as string)}><Layers3 size={13} /> Toggle Local / unlinked stack</MenuItem>}
              <MenuItem onSelect={() => setDeleteWorktreeId(data.entityId)}><Trash2 size={13} /> Delete worktree</MenuItem>
            </>
          )}

          {data.kind === 'agent' && (
            <>
              <MenuItem unavailable={!isLiveAgent(agent)} onSelect={() => openTerminal(data.entityId)}><TerminalSquare size={13} /> Open terminal</MenuItem>
              <ContextMenu.Separator className="my-1 h-px bg-border-subtle" />
              <MenuItem unavailable onSelect={() => setAgentState(data.entityId, 'running')}><Play size={13} /> Start</MenuItem>
              <MenuItem unavailable onSelect={() => setAgentState(data.entityId, 'running')}><RotateCcw size={13} /> Restart</MenuItem>
              <MenuItem unavailable={!isLiveAgent(agent) || agent?.state === 'finished'} onSelect={() => setAgentState(data.entityId, 'finished')}><Square size={13} /> Stop</MenuItem>
              {agent?.state === 'finished' && (agent.presentation ?? 'canvas') === 'canvas' && (
                <MenuItem onSelect={() => moveAgentToHistory(data.entityId)}><History size={13} /> Move to history</MenuItem>
              )}
              <ContextMenu.Separator className="my-1 h-px bg-border-subtle" />
              {(agent?.presentation ?? (agent?.archived ? 'archived' : 'canvas')) === 'archived' ? (
                <MenuItem onSelect={() => restoreAgent(data.entityId)}><Undo2 size={13} /> Restore agent</MenuItem>
              ) : (
                <MenuItem onSelect={() => archiveAgent(data.entityId)}><Archive size={13} /> Archive agent</MenuItem>
              )}
            </>
          )}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  )
})

export function ProjectNode(props: NodeProps) { return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} /> }
export function WorktreeNode(props: NodeProps) { return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} /> }
export function AgentNode(props: NodeProps) { return <NodeShell data={props.data as BonsaiGraphData} selected={props.selected} /> }
export function StackNode(props: NodeProps) { return <NodeShell data={props.data as BonsaiGraphData} selected={false} /> }
export function DefaultBranchNode(props: NodeProps) { return <NodeShell data={props.data as BonsaiGraphData} selected={false} /> }
