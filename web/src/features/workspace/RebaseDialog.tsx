import { useState, type FormEvent } from 'react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { localCommand } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'

export function RebaseDialog() {
  const visible = useBonsaiStore(state => state.rebaseWorktreeId)
  return visible ? <RebaseDialogBody /> : null
}

/** Rebases a worktree onto any local or fetched remote branch. */
function RebaseDialogBody() {
  const id = useBonsaiStore(state => state.rebaseWorktreeId)
  const close = useBonsaiStore(state => state.setRebaseWorktreeId)
  const worktree = useBonsaiStore(state => state.worktrees.find(tree => tree.id === id))
  const branches = useBonsaiStore(state => state.gitBranches[worktree?.projectId ?? ''])
  const defaultBranch = useBonsaiStore(state => state.projects.find(project => project.id === worktree?.projectId)?.defaultBranch)
  const options = (branches ?? [])
    .filter(branch => branch.name !== worktree?.branch)
    .map(branch => ({ value: branch.ref || branch.name, label: branch.name, description: branch.remote ? 'remote branch' : 'local branch' }))
  const initial = worktree?.mergeTargetBranch || defaultBranch || ''
  const [target, setTarget] = useState(() => options.find(option => option.label === initial)?.value ?? options[0]?.value ?? '')
  const [pending, setPending] = useState(false)
  if (!id || !worktree) return null
  const label = options.find(option => option.value === target)?.label ?? target
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!target || pending) return
    setPending(true)
    try {
      await localCommand('rebase', { target }, worktree.id)
    } finally {
      setPending(false)
      close('')
    }
  }
  return (
    <div className="absolute inset-0 z-[90] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <form role="dialog" aria-modal="true" aria-label="Rebase worktree" onSubmit={submit} className="w-full max-w-lg space-y-4 rounded-[14px] border border-border-strong bg-panel p-5 shadow-overlay">
        <h2 className="text-[13px] font-semibold">Rebase <span className="font-mono">{worktree.branch}</span></h2>
        <label className="block">
          <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Onto branch</span>
          <BonsaiSelect ariaLabel="Rebase onto" searchable value={target} onChange={setTarget} options={options} />
        </label>
        {!options.length && <p className="text-xs text-muted">No other branches are known for this project yet. Fetch, then try again.</p>}
        {!!worktree.dirtyFiles && <p role="alert" className="text-xs text-warn">Commit or stash {worktree.dirtyFiles} changed files first; Git refuses to rebase a dirty worktree.</p>}
        <div className="flex justify-end gap-3">
          <button type="button" disabled={pending} onClick={() => close('')} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">Cancel</button>
          <button type="submit" disabled={pending || !target} className="bonsai-focus btn-primary h-8 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">{pending ? 'Rebasing…' : target ? 'Rebase onto ' + label : 'Rebase'}</button>
        </div>
      </form>
    </div>
  )
}
