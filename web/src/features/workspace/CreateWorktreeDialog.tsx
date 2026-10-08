import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { GitBranch, GitFork, Plus, Radio, X } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { WorktreeMetadataError } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'
import type { WorktreeSourceType } from '../../types'
import { getTagPresentation } from './tagStyles'

const sourceOptions: Array<{ id: WorktreeSourceType; label: string; description: string; icon: typeof GitBranch }> = [
  { id: 'existing', label: 'Existing branch', description: 'Attach a local branch that is not checked out.', icon: GitBranch },
  { id: 'origin', label: 'Remote branch', description: 'Create the worktree from a fetched tracking branch.', icon: Radio },
  { id: 'new', label: 'New branch', description: 'Create a branch from an existing base ref.', icon: Plus },
]

export function CreateWorktreeDialog() {
  const visible = useBonsaiStore(state => state.worktreeDialogOpen)
  return visible ? <CreateWorktreeDialogBody /> : null
}

function CreateWorktreeDialogBody() {
  const open = useBonsaiStore((state) => state.worktreeDialogOpen)
  const setOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const createWorktree = useBonsaiStore((state) => state.createWorktree)
  const projects = useBonsaiStore((state) => state.projects)
  const target = useBonsaiStore(state => state.worktreeDialogTarget)
  const activeProjectId = target?.projectId ?? ''
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const tags = useBonsaiStore((state) => state.worktreeTags)

  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const checkedOutBranches = useMemo(() => new Set(projectWorktrees.map((item) => item.branch)), [projectWorktrees])

  const allBranches = useBonsaiStore(state => state.gitBranches[activeProjectId])
  const defaultBase = allBranches?.find(branch => !branch.remote && branch.name === project?.defaultBranch)?.ref ?? project?.defaultBranch ?? 'main'
  const localBranches = useMemo(() => (allBranches ?? []).filter(b => !b.remote && !checkedOutBranches.has(b.name)).map(b => b.name), [allBranches, checkedOutBranches])
  const originBranches = useMemo(() => (allBranches ?? []).filter(b => b.remote && !checkedOutBranches.has(b.name.replace(/^origin\//, ''))).map(b => b.name), [allBranches, checkedOutBranches])
  const mergeTargets = [project?.defaultBranch ?? 'main', ...projectWorktrees.filter((item) => item.branch !== project?.defaultBranch).map((item) => item.branch)]

  const [sourceType, setSourceType] = useState<WorktreeSourceType>('new')
  const [sourceRef, setSourceRef] = useState(project?.defaultBranch ?? 'main')
  const [branchName, setBranchName] = useState('feat/new-worktree')
  const [tagId, setTagId] = useState('feat')
  const [mergeTargetBranch, setMergeTargetBranch] = useState(project?.defaultBranch ?? 'main')

  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [createdId, setCreatedId] = useState('')
  const submitting = useRef(false)
  const submission = useRef({ payload: '', key: '' })
  useEffect(() => {
    if (!open) return
    setSourceType(target?.sourceType ?? 'new')
    setSourceRef(target?.sourceRef ?? defaultBase)
    setBranchName(target?.branchName ?? 'feat/new-worktree')
    setTagId(tags.find(tag => tag.id === 'feat')?.id ?? tags[0]?.id ?? '')
    setMergeTargetBranch(project?.defaultBranch ?? 'main')
    setError('')
    setCreatedId('')
    submission.current = { payload: '', key: '' }
    // Initialize once per opening. Live inventory changes never replace a choice.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, target])

  if (!open || !project) return null
  const selectedLocal = sourceType === 'existing' ? sourceRef : sourceType === 'origin' ? (target?.sourceRef === sourceRef && target.branchName ? target.branchName : sourceRef.split('/').slice(1).join('/')) : ''
  const existingWorktree = projectWorktrees.find(item => item.branch === selectedLocal)
  const changeSourceType = (type: WorktreeSourceType) => {
    setSourceType(type)
    setSourceRef(type === 'existing' ? localBranches[0] ?? '' : type === 'origin' ? originBranches[0] ?? '' : defaultBase)
    setError('')
  }
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (submitting.current) return
    if (existingWorktree && !createdId) {
      if (useBonsaiStore.getState().activeProjectId === project.id) useBonsaiStore.getState().setSelection({ type: 'worktree', id: existingWorktree.id })
      setOpen(false)
      return
    }
    const input = { projectId: project.id, sourceType, sourceRef, branchName: sourceType === 'new' ? branchName : selectedLocal, tagId, mergeTargetBranch }
    const payload = JSON.stringify(input)
    if (submission.current.payload !== payload) submission.current = { payload, key: crypto.randomUUID() }
    submitting.current = true
    setPending(true)
    setError('')
    try {
      await createWorktree(input, { idempotencyKey: submission.current.key, worktreeId: createdId || undefined })
      if (useBonsaiStore.getState().worktreeDialogTarget === target) setOpen(false)
      useBonsaiStore.getState().setNotice('Worktree created')
    } catch (cause) {
      if (cause instanceof WorktreeMetadataError) setCreatedId(cause.worktreeId)
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      submitting.current = false
      setPending(false)
    }
  }

  const branchOptions = (sourceType === 'existing' ? localBranches : originBranches).map((branch) => ({
    value: branch,
    label: branch,
    description: sourceType === 'origin' ? 'remote tracking branch' : 'local branch',
  }))
  const mergeOptions = mergeTargets.map((branch) => ({
    value: branch,
    label: branch,
    description: branch === project.defaultBranch ? 'default branch' : 'worktree branch',
  }))

  return (
    <div className="absolute inset-0 z-[80] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <form role="dialog" aria-modal="true" aria-label="New worktree" onSubmit={submit} className="w-full max-w-[620px] overflow-hidden rounded-[14px] border border-border-strong bg-panel shadow-overlay">
        <div className="flex h-11 items-center border-b border-border-subtle px-4">
          <GitFork size={15} className="mr-2 text-accent" />
          <div>
            <div className="text-[13px] font-semibold">New worktree</div>
            <div className="font-mono text-[10px] text-muted-2">{project.repository}</div>
          </div>
          <button type="button" onClick={() => setOpen(false)} aria-label="Close" className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-panel-3 hover:text-text">
            <X size={14} />
          </button>
        </div>

        <fieldset disabled={pending || !!createdId} className="space-y-5 p-4">
          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Branch source</div>
            <div className="grid grid-cols-3 gap-1 rounded-[9px] border border-border bg-well p-1">
              {sourceOptions.map((option) => {
                const Icon = option.icon
                const active = option.id === sourceType
                return (
                  <button type="button" key={option.id} title={option.description} onClick={() => changeSourceType(option.id)} className={'bonsai-focus flex h-[30px] items-center justify-center gap-2 rounded-[7px] text-[12px] transition-colors ' + (active ? 'bg-panel-3 text-text' : 'text-muted hover:text-text')}>
                    <Icon size={13} className={active ? 'text-accent' : 'text-muted'} />
                    <span>{option.label}</span>
                    <span className="sr-only">{option.description}</span>
                  </button>
                )
              })}
            </div>
          </section>

          <div className="grid grid-cols-2 gap-3">
            {sourceType === 'new' ? (
              <>
                <label className="block">
                  <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">New branch name</span>
                  <input autoFocus value={branchName} onChange={(event) => setBranchName(event.target.value)} placeholder="feat/my-branch" className="bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 font-mono text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55" />
                </label>
                <label className="block">
                  <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Branch from</span>
                  <BonsaiSelect ariaLabel="Branch from" searchable value={sourceRef} onChange={setSourceRef} options={(allBranches ?? []).map(branch => ({ value: branch.ref || branch.name, label: branch.name, description: branch.remote ? 'fetched remote base' : 'local base' }))} />
                </label>
              </>
            ) : (
              <label className="col-span-2 block">
                <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">{sourceType === 'existing' ? 'Local branch' : 'Remote branch'}</span>
                <BonsaiSelect ariaLabel={sourceType === 'existing' ? 'Local branch' : 'Remote branch'} searchable value={sourceRef} onChange={setSourceRef} options={branchOptions} />
              </label>
            )}
          </div>

          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Worktree tag</div>
            <div className="flex flex-wrap gap-2">
              {tags.map((tag) => {
                const presentation = getTagPresentation(tag)
                const active = tag.id === tagId
                return (
                  <button
                    type="button"
                    key={tag.id}
                    onClick={() => setTagId(tag.id)}
                    className="bonsai-focus rounded-[7px] border px-2.5 py-1.5 text-[11px] font-medium"
                    style={{ color: presentation.foreground, borderColor: active ? presentation.foreground : presentation.border, background: active ? presentation.background : 'rgb(var(--well))' }}
                  >
                    {tag.name}
                  </button>
                )
              })}
            </div>
          </section>

          <label className="block">
            <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Merges into</span>
            <BonsaiSelect ariaLabel="Merge target" searchable value={mergeTargetBranch} onChange={setMergeTargetBranch} options={mergeOptions} />
            <span className="mt-1.5 block font-mono text-[10px] text-muted-2">Non-default targets become explicit PR relationships on the canvas.</span>
          </label>
        </fieldset>
        {error && <p role="alert" className="px-4 pb-3 text-[11px] text-danger">{error}</p>}

        <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-well/45 px-4 py-3">
          <button type="button" onClick={() => setOpen(false)} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">Cancel</button>
          <button type="submit" disabled={pending || !tagId || !sourceRef || (sourceType === 'new' && !branchName.trim())} className="bonsai-focus btn-primary h-8 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">{pending ? 'Creating…' : createdId ? 'Retry saving settings' : existingWorktree ? 'Open worktree' : 'Create worktree'}</button>
        </div>
      </form>
    </div>
  )
}
