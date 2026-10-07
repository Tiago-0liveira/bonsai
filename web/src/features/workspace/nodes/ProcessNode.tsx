import * as ContextMenu from '@radix-ui/react-context-menu'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useMemo } from 'react'
import { ExternalLink, GitBranch, LoaderCircle, RotateCcw, Square, TerminalSquare } from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import { processNodeSelector } from '../../../stores/projectSelectors'
import type { ProcessLifecycleStatus } from '../../../types'
import { useProcessActions } from '../../terminal/useProcessActions'
import { MoveSubtreeGrip, type BonsaiGraphData } from './BonsaiNode'

const statusPresentation: Record<ProcessLifecycleStatus, { label: string; tone: string }> = {
  starting: { label: 'Starting', tone: 'text-[rgb(var(--blue))] border-[rgb(var(--blue)/.25)] bg-[rgb(var(--blue)/.08)]' },
  running: { label: 'Running', tone: 'text-[rgb(var(--green))] border-[rgb(var(--green)/.25)] bg-[rgb(var(--green)/.08)]' },
  backoff: { label: 'Retrying', tone: 'text-[rgb(var(--orange))] border-[rgb(var(--orange)/.25)] bg-[rgb(var(--orange)/.08)]' },
  stopping: { label: 'Stopping', tone: 'text-[rgb(var(--orange))] border-[rgb(var(--orange)/.25)] bg-[rgb(var(--orange)/.08)]' },
  stopped: { label: 'Stopped', tone: 'text-[rgb(var(--muted))] border-[rgb(var(--border))] bg-[rgb(var(--bg)/.4)]' },
  done: { label: 'Completed', tone: 'text-[rgb(var(--muted))] border-[rgb(var(--border))] bg-[rgb(var(--bg)/.4)]' },
  failed: { label: 'Failed', tone: 'text-[rgb(var(--red))] border-[rgb(var(--red)/.25)] bg-[rgb(var(--red)/.08)]' },
  lost: { label: 'Lost', tone: 'text-[rgb(var(--red))] border-[rgb(var(--red)/.25)] bg-[rgb(var(--red)/.08)]' },
  orphan: { label: 'Orphaned', tone: 'text-[rgb(var(--orange))] border-[rgb(var(--orange)/.25)] bg-[rgb(var(--orange)/.08)]' },
}

const menuItemClass = 'flex cursor-default select-none items-center gap-2 rounded px-2 py-1.5 text-[12px] text-[rgb(var(--muted))] outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-[rgb(var(--purple)/.12)] data-[highlighted]:text-[rgb(var(--text))]'
const iconButtonClass = 'bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] transition-colors hover:bg-[rgb(var(--panel-3))] hover:text-[rgb(var(--text))] disabled:cursor-default disabled:opacity-30'

export function ProcessNode({ data: raw, selected }: NodeProps) {
  const data = raw as BonsaiGraphData
  const process = useBonsaiStore(useMemo(() => processNodeSelector(data.entityId), [data.entityId]))
  const worktree = useBonsaiStore(state => state.worktrees.find(tree => tree.id === data.worktreeId))
  const setSelection = useBonsaiStore(state => state.setSelection)
  const { pending, error, canStop, canRestart, openOutput, inspect, stop, restart } = useProcessActions(process)
  const lifecycle = process?.lifecycleStatus ?? data.processStatus ?? 'lost'
  const status = statusPresentation[pending === 'stop' ? 'stopping' : pending === 'restart' ? 'starting' : lifecycle]
  const statusLabel = pending === 'restart' ? 'Restarting' : status.label
  const transitioning = !!pending || lifecycle === 'starting' || lifecycle === 'stopping'
  const detail = [process ? `#${process.daemonId}` : '', process?.exitCode !== undefined ? `exit ${process.exitCode}` : '', data.associationLabel ? 'Unassigned' : ''].filter(Boolean).join(' · ')

  return <ContextMenu.Root>
    <ContextMenu.Trigger asChild>
      <div data-process-node-id={data.entityId} className={'flex h-[112px] w-[240px] flex-col rounded-lg border bg-[rgb(var(--panel-2))] shadow-[0_6px_20px_rgb(0_0_0/.10)] transition-[border-color,background-color] ' + (selected ? 'border-[rgb(var(--purple))] bg-[rgb(var(--panel-3))]' : 'border-[rgb(var(--border))] hover:border-[rgb(var(--border-strong))]')}>
        <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-[rgb(var(--border-strong))] !bg-[rgb(var(--panel-3))]" />
        <div className="flex h-10 shrink-0 items-center gap-2 px-3">
          <span className="grid h-6 w-6 shrink-0 place-items-center rounded-md border border-[rgb(var(--purple)/.20)] bg-[rgb(var(--purple)/.06)] text-[rgb(var(--purple))]"><TerminalSquare size={12} /></span>
          <span title={data.title} className="min-w-0 flex-1 truncate text-[11px] font-semibold">{data.title}</span>
          <span role="status" title={process?.exitError} className={'inline-flex shrink-0 items-center gap-1 rounded-full border px-1.5 py-0.5 text-[8px] font-medium ' + status.tone}>
            {transitioning ? <LoaderCircle size={8} className="animate-spin motion-reduce:animate-none" /> : <span className="h-1 w-1 rounded-full bg-current" />}
            {statusLabel}
          </span>
        </div>
        <div className="flex min-h-0 flex-1 items-center gap-2 px-3 pb-2">
          <span aria-hidden className="shrink-0 font-mono text-[11px] text-[rgb(var(--muted-2))]">›</span>
          <div className="min-w-0 flex-1">
            <code title={data.command} className={(error ? 'block truncate' : 'line-clamp-2') + ' break-all font-mono text-[10px] leading-[14px] text-[rgb(var(--muted))]'}>{data.command}</code>
            {error && <p role="alert" title={error} className="truncate text-[9px] text-[rgb(var(--red))]">{error}</p>}
          </div>
        </div>
        <div className="nodrag nopan flex h-8 shrink-0 items-center gap-1 rounded-b-lg border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.3)] px-2" onPointerDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
          <span title={process?.exitError || data.associationLabel || detail} className="min-w-0 flex-1 truncate pl-1 font-mono text-[8px] text-[rgb(var(--muted-2))]">{detail}</span>
          <button type="button" disabled={!process} onClick={openOutput} className="bonsai-focus flex h-6 shrink-0 items-center gap-1 rounded px-1.5 text-[9px] font-medium text-[rgb(var(--purple))] transition-colors hover:bg-[rgb(var(--purple)/.10)] disabled:opacity-30"><TerminalSquare size={10} />Open output</button>
          <button type="button" aria-label="Stop process" title={pending === 'stop' ? 'Stopping process…' : 'Stop process'} disabled={!canStop} onClick={stop} className={iconButtonClass}>{pending === 'stop' ? <LoaderCircle size={11} className="animate-spin motion-reduce:animate-none" /> : <Square size={11} />}</button>
          <button type="button" aria-label="Restart process" title={pending === 'restart' ? 'Restarting process…' : 'Restart process'} disabled={!canRestart} onClick={restart} className={iconButtonClass}><RotateCcw size={11} className={pending === 'restart' ? 'animate-spin motion-reduce:animate-none' : ''} /></button>
        </div>
      </div>
    </ContextMenu.Trigger>
    <ContextMenu.Portal>
      <ContextMenu.Content aria-label="Process actions" className="nodrag nopan z-[100] min-w-48 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-1 shadow-2xl" onClick={event => event.stopPropagation()} onPointerDown={event => event.stopPropagation()}>
        <ContextMenu.Item disabled={!process} onSelect={openOutput} className={menuItemClass}><TerminalSquare size={13} />Open output</ContextMenu.Item>
        <ContextMenu.Item disabled={!process} onSelect={inspect} className={menuItemClass}><ExternalLink size={13} />Open inspector</ContextMenu.Item>
        <ContextMenu.Item disabled={!worktree} onSelect={() => { if (worktree) setSelection({ type: 'worktree', id: worktree.id }) }} className={menuItemClass}><GitBranch size={13} />Open worktree</ContextMenu.Item>
        <ContextMenu.Separator className="my-1 h-px bg-[rgb(var(--border))]" />
        <ContextMenu.Item disabled={!canStop} onSelect={stop} className={menuItemClass}><Square size={13} />{pending === 'stop' ? 'Stopping process…' : 'Stop process'}</ContextMenu.Item>
        <ContextMenu.Item disabled={!canRestart} onSelect={restart} className={menuItemClass}><RotateCcw size={13} />{pending === 'restart' ? 'Restarting process…' : 'Restart process'}</ContextMenu.Item>
      </ContextMenu.Content>
    </ContextMenu.Portal>
  </ContextMenu.Root>
}

export function ProcessShelfNode({ data }: NodeProps) {
  return <div className="group relative h-[74px] w-[230px] rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-3">
    <MoveSubtreeGrip id={String(data.entityId)} />
    <Handle type="target" position={Position.Top} />
    <div className="text-[11px] font-semibold">{String(data.title)}</div>
    <div className="mt-1 text-[10px] text-[rgb(var(--muted))]">{String(data.subtitle)}</div>
    <Handle type="source" position={Position.Bottom} />
  </div>
}
