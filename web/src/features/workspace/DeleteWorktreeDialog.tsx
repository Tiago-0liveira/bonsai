import { useEffect, useRef, useState } from 'react'
import { APIError, deleteWorktree, refreshProject, stopWorktreeProcesses } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'

const activeProcessStates = new Set(['starting', 'running', 'backoff', 'stopping', 'orphan'])

export function DeleteWorktreeDialog() {
  const visible = useBonsaiStore(state => state.deleteWorktreeId)
  return visible ? <DeleteWorktreeDialogBody /> : null
}

function DeleteWorktreeDialogBody() {
  const id = useBonsaiStore(state => state.deleteWorktreeId)
  const close = useBonsaiStore(state => state.setDeleteWorktreeId)
  const worktree = useBonsaiStore(state => state.worktrees.find(tree => tree.id === id))
  const processes = useBonsaiStore(state => state.processes)
  const agents = useBonsaiStore(state => state.agents)
  const [discard, setDiscard] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const submitting = useRef(false)
  const key = useRef('')
  useEffect(() => { setDiscard(false); setError(''); key.current = crypto.randomUUID() }, [id])
  if (!id || !worktree) return null
  const running = processes.filter(process => process.worktreeId === id && activeProcessStates.has(process.lifecycleStatus))
  const runningAgents = agents.filter(agent => agent.worktreeId === id && agent.state === 'running')
  const busy = !worktree.missing && worktree.gitState && worktree.gitState !== 'normal'
  const remove = async () => {
    if (submitting.current) return
    submitting.current = true
    setPending(true)
    setError('')
    try {
      if (running.length) await stopWorktreeProcesses(worktree.projectId, running.map(process => process.daemonId))
      const result = await deleteWorktree(id, discard, key.current)
      close('')
      useBonsaiStore.getState().setNotice(result.metadata_error ? `Worktree removed. Metadata cleanup failed: ${result.metadata_error}` : 'Worktree removed; branch retained')
      await refreshProject(worktree.projectId)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      // Preserve the request identity when the server may have removed the
      // worktree but its response was lost. Known refusals can be retried.
      if (cause instanceof APIError && !['outcome_unknown', 'daemon_unavailable', 'backend_unavailable', 'invalid_daemon_response', 'request_failed'].includes(cause.code)) {
        key.current = crypto.randomUUID()
      }
    } finally { submitting.current = false; setPending(false) }
  }
  return (
    <div className="absolute inset-0 z-[90] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <div role="dialog" aria-modal="true" aria-label="Delete worktree" className="w-full max-w-lg space-y-4 rounded-[14px] border border-border-strong bg-panel shadow-overlay p-5">
        <h2 className="text-[13px] font-semibold">Delete worktree</h2>
        <p className="break-all font-mono text-xs">{worktree.branch}<br /><span className="text-muted">{worktree.path ?? 'Path unavailable'}</span></p>
        <p className="text-xs text-muted">The local branch and remote pull request will be retained.</p>
        {worktree.missing && <p className="text-xs text-warn">The worktree directory is missing. Only its stale Git registration will be removed.</p>}
        {worktree.main && <p role="alert" className="text-xs text-danger">The main working copy cannot be deleted.</p>}
        {busy && <p role="alert" className="text-xs text-warn">Finish the active Git operation before deleting this worktree.</p>}
        {!!runningAgents.length && <p role="alert" className="text-xs text-warn">Stop active agents before deleting this worktree.</p>}
        {!!running.length && <p className="text-xs text-warn">{running.length} running process(es) will be stopped before removal.</p>}
        {!worktree.missing && <label className="flex items-start gap-2 text-xs"><input type="checkbox" checked={discard} disabled={pending} onChange={event => { setDiscard(event.target.checked); key.current = crypto.randomUUID() }} />Discard uncommitted changes, including untracked files{worktree.dirtyFiles ? ` (${worktree.dirtyFiles} changed files)` : ''}</label>}
        {error && <p role="alert" className="text-xs text-danger">{error}</p>}
        <div className="flex justify-end gap-3">
          <button type="button" disabled={pending} onClick={() => close('')} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">Cancel</button>
          <button type="button" disabled={pending || worktree.main || !!busy || !!runningAgents.length || (!worktree.missing && !!worktree.dirtyFiles && !discard)} onClick={() => void remove()} className="bonsai-focus btn-danger-tint h-8 rounded-[7px] px-3 text-[12px] disabled:opacity-40">{pending ? 'Deleting…' : running.length ? 'Stop and delete' : worktree.missing ? 'Remove registration' : 'Delete worktree'}</button>
        </div>
      </div>
    </div>
  )
}
