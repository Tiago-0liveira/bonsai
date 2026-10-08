import * as ContextMenu from '@radix-ui/react-context-menu'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useMemo } from 'react'
import { ExternalLink, GitBranch, LoaderCircle, RotateCcw, Square, TerminalSquare } from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import { processNodeSelector } from '../../../stores/projectSelectors'
import type { ProcessLifecycleStatus } from '../../../types'
import { useProcessActions } from '../../terminal/useProcessActions'
import { MoveSubtreeGrip, type BonsaiGraphData } from './BonsaiNode'

const statusLabels: Record<ProcessLifecycleStatus, string> = {
  starting: 'Starting',
  running: 'Running',
  backoff: 'Retrying',
  stopping: 'Stopping',
  stopped: 'Stopped',
  done: 'Completed',
  failed: 'Failed',
  lost: 'Lost',
  orphan: 'Orphaned',
}

const menuItemClass = 'flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 text-[12px] text-muted outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-accent/12 data-[highlighted]:text-text'
const iconButtonClass = 'bonsai-focus icon-btn-20 transition-colors hover:bg-panel-3 hover:text-text disabled:cursor-default disabled:opacity-30 disabled:hover:bg-panel-2 disabled:hover:text-muted'

export function ProcessNode({ data: raw, selected }: NodeProps) {
  const data = raw as BonsaiGraphData
  const process = useBonsaiStore(useMemo(() => processNodeSelector(data.entityId), [data.entityId]))
  const worktree = useBonsaiStore(state => state.worktrees.find(tree => tree.id === data.worktreeId))
  const setSelection = useBonsaiStore(state => state.setSelection)
  const { pending, error, canStop, canRestart, openOutput, inspect, stop, restart, remove, deleteLabel } = useProcessActions(process)
  const lifecycle = process?.lifecycleStatus ?? data.processStatus ?? 'lost'
  const shown = pending === 'stop' ? 'stopping' : pending === 'restart' ? 'starting' : lifecycle
  const statusLabel = pending === 'restart' ? 'Restarting' : statusLabels[shown]
  const spinning = !!pending || shown === 'starting' || shown === 'stopping' || shown === 'backoff'
  const failed = shown === 'failed'
  const detail = [process ? `#${process.daemonId}` : '', process?.exitCode !== undefined ? `exit ${process.exitCode}` : '', data.associationLabel ? 'Unassigned' : ''].filter(Boolean).join(' · ')
  const stateSquare = shown === 'running' ? 'bg-accent-solid' : failed || shown === 'lost' ? 'bg-warn' : 'bg-muted-2'
  const tone = selected
    ? 'border-accent/55 shadow-[0_0_0_3px_rgb(var(--accent)/.10),var(--shadow-card)]'
    : (failed ? 'border-warn/40' : 'border-panel-3') + ' shadow-card hover:border-border-strong'

  return <ContextMenu.Root>
    <ContextMenu.Trigger asChild>
      <div data-process-node-id={data.entityId} title={data.command} className={'group relative flex h-[54px] w-[153px] flex-col rounded-[10px] border bg-bg px-2 pb-[5px] pt-[7px] transition-[border-color,box-shadow] ' + tone}>
        <Handle type="target" position={Position.Top} className="!h-2 !w-2" />
        <div className="flex h-5 shrink-0 items-center gap-1.5">
          {spinning
            ? <LoaderCircle size={8} aria-hidden className="shrink-0 animate-spin text-accent motion-reduce:animate-none" />
            : <span aria-hidden className={'h-[7px] w-[7px] shrink-0 rounded-sm ' + stateSquare} />}
          <span role="status" className="sr-only">{statusLabel}</span>
          <span title={data.title} className="min-w-0 flex-1 truncate font-mono text-[11px] font-semibold text-text">{data.title}</span>
          {process?.port !== undefined && <span className="shrink-0 font-mono text-[10px] text-ok">{process.port}</span>}
        </div>
        <div className="flex h-5 shrink-0 items-center gap-[3px]">
          {error
            ? <p role="alert" title={error} className="min-w-0 flex-1 truncate font-mono text-[9.5px] text-warn">{error}</p>
            : <span title={process?.exitError || data.associationLabel || detail} className={'min-w-0 flex-1 truncate font-mono text-[9.5px] ' + (failed ? 'text-warn' : 'text-muted-2')}>{statusLabel}{detail && ' · ' + detail}</span>}
          <div className="nodrag nopan flex shrink-0 items-center gap-[3px]" onPointerDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
            <button type="button" aria-label="Open output" title="Open output" disabled={!process} onClick={openOutput} className={iconButtonClass}><TerminalSquare size={11} /></button>
            <button type="button" aria-label="Stop process" title={pending === 'stop' ? 'Stopping process…' : 'Stop process'} disabled={!canStop} onClick={stop} className={iconButtonClass}>{pending === 'stop' ? <LoaderCircle size={11} className="animate-spin motion-reduce:animate-none" /> : <Square size={11} />}</button>
            <button type="button" aria-label="Restart process" title={pending === 'restart' ? 'Restarting process…' : 'Restart process'} disabled={!canRestart} onClick={restart} className={iconButtonClass + (failed && canRestart ? ' !border-warn-solid/55 !bg-warn-solid/14 !text-warn' : '')}><RotateCcw size={11} className={pending === 'restart' ? 'animate-spin motion-reduce:animate-none' : ''} /></button>
          </div>
        </div>
      </div>
    </ContextMenu.Trigger>
    <ContextMenu.Portal>
      <ContextMenu.Content aria-label="Process actions" className="nodrag nopan z-[100] min-w-48 rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay" onClick={event => event.stopPropagation()} onPointerDown={event => event.stopPropagation()}>
        <ContextMenu.Item disabled={!process} onSelect={openOutput} className={menuItemClass}><TerminalSquare size={13} />Open output</ContextMenu.Item>
        <ContextMenu.Item disabled={!process} onSelect={inspect} className={menuItemClass}><ExternalLink size={13} />Open inspector</ContextMenu.Item>
        <ContextMenu.Item disabled={!worktree} onSelect={() => { if (worktree) setSelection({ type: 'worktree', id: worktree.id }) }} className={menuItemClass}><GitBranch size={13} />Open worktree</ContextMenu.Item>
        <ContextMenu.Separator className="my-1 h-px bg-border-subtle" />
        <ContextMenu.Item disabled={!canStop} onSelect={stop} className={menuItemClass}><Square size={13} />{pending === 'stop' ? 'Stopping process…' : 'Stop process'}</ContextMenu.Item>
        <ContextMenu.Item disabled={!canRestart} onSelect={restart} className={menuItemClass}><RotateCcw size={13} />{pending === 'restart' ? 'Restarting process…' : 'Restart process'}</ContextMenu.Item>
        <ContextMenu.Item disabled={!process || !!pending} onSelect={remove} className={menuItemClass}>{pending === 'remove' ? 'Deleting process…' : deleteLabel}</ContextMenu.Item>
      </ContextMenu.Content>
    </ContextMenu.Portal>
  </ContextMenu.Root>
}

export function ProcessShelfNode({ data }: NodeProps) {
  return <div className="group relative h-[74px] w-[230px] rounded-xl border border-border bg-panel p-3 shadow-card">
    <MoveSubtreeGrip id={String(data.entityId)} />
    <Handle type="target" position={Position.Top} />
    <div className="text-[11px] font-semibold text-text">{String(data.title)}</div>
    <div className="mt-1 text-[10px] text-muted">{String(data.subtitle)}</div>
    <Handle type="source" position={Position.Bottom} />
  </div>
}
