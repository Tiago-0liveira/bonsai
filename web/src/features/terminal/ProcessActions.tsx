import { useProcessActions, type ProcessActionTarget } from './useProcessActions'

export function ProcessActions({ process }: { process: ProcessActionTarget }) {
  const { pending, error, canStop, canRestart, openOutput, stop, restart, remove, deleteLabel } = useProcessActions(process)
  const buttonClass = 'bonsai-focus rounded border border-[rgb(var(--border))] px-2 py-1 text-[10px] hover:bg-[rgb(var(--panel-3))] disabled:opacity-40'
  return <div className="nodrag nopan" onClick={event => event.stopPropagation()} onPointerDown={event => event.stopPropagation()}>
    <div className="flex flex-wrap gap-1">
      <button type="button" className={buttonClass} onClick={openOutput}>Open output</button>
      <button type="button" className={buttonClass} disabled={!canStop} onClick={stop}>{pending === 'stop' ? 'Stopping…' : 'Stop'}</button>
      <button type="button" className={buttonClass} disabled={!canRestart} onClick={restart}>{pending === 'restart' ? 'Restarting…' : 'Restart'}</button>
      <button type="button" className={buttonClass} disabled={!!pending} onClick={remove}>{pending === 'remove' ? 'Deleting…' : deleteLabel}</button>
    </div>
    {error && <p role="alert" title={error} className="mt-1 truncate text-[10px] text-[rgb(var(--red))]">{error}</p>}
  </div>
}
