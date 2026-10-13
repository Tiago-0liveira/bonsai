import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { GitBranch, GitFork, GitMerge, Plus, Radio } from 'lucide-react'
import { DialogFrame, Field, Reveal, TEXT_INPUT } from '../../components/ui/Dialog'
import { OptionList } from '../../components/ui/OptionList'
import { Segmented } from '../../components/ui/Segmented'
import { WorktreeMetadataError, type Branch } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'
import type { WorktreeSourceType } from '../../types'

const sourceOptions: Array<{ id: WorktreeSourceType; label: string; description: string; icon: typeof GitBranch }> = [
  { id: 'existing', label: 'Existing branch', description: 'Attach a local branch that is not checked out.', icon: GitBranch },
  { id: 'origin', label: 'Remote branch', description: 'Create the worktree from a fetched tracking branch.', icon: Radio },
  { id: 'new', label: 'New branch', description: 'Create a branch from an existing base ref.', icon: Plus },
]

/** The branch a base ref names, without its remote: `origin/feat/x` and `refs/heads/feat/x` are `feat/x`. */
export function baseBranchName(ref: string, branches: Branch[] = []) {
  const branch = branches.find(item => (item.ref || item.name) === ref)
  if (branch?.remote) {
    const remote = branch.remote_name ?? branch.name.split('/')[0]
    return branch.name.startsWith(remote + '/') ? branch.name.slice(remote.length + 1) : branch.name
  }
  if (branch) return branch.name
  return ref.replace(/^refs\/heads\//, '').replace(/^refs\/remotes\/[^/]+\//, '')
}

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

  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const checkedOutBranches = useMemo(() => new Set(projectWorktrees.map((item) => item.branch)), [projectWorktrees])

  const allBranches = useBonsaiStore(state => state.gitBranches[activeProjectId])
  const defaultBase = allBranches?.find(branch => !branch.remote && branch.name === project?.defaultBranch)?.ref ?? project?.defaultBranch ?? 'main'
  const localBranches = useMemo(() => (allBranches ?? []).filter(b => !b.remote && !checkedOutBranches.has(b.name)).map(b => b.name), [allBranches, checkedOutBranches])
  const originBranches = useMemo(() => (allBranches ?? []).filter(b => b.remote && !checkedOutBranches.has(b.name.replace(/^origin\//, ''))).map(b => b.name), [allBranches, checkedOutBranches])
  const worktreeTargets = [project?.defaultBranch ?? 'main', ...projectWorktrees.filter((item) => item.branch !== project?.defaultBranch).map((item) => item.branch)]

  const [sourceType, setSourceType] = useState<WorktreeSourceType>('new')
  const [sourceRef, setSourceRef] = useState(project?.defaultBranch ?? 'main')
  const [branchName, setBranchName] = useState('feat/new-worktree')
  const [mergeTargetBranch, setMergeTargetBranch] = useState(project?.defaultBranch ?? 'main')
  // A new branch merges into, and rebases onto, the base it was cut from
  // until the merge target is picked by hand.
  const mergeTargetChosen = useRef(false)
  const chooseMergeTarget = (branch: string) => { mergeTargetChosen.current = true; setMergeTargetBranch(branch) }
  const chooseSourceRef = (ref: string) => {
    setSourceRef(ref)
    if (sourceType === 'new' && !mergeTargetChosen.current) setMergeTargetBranch(baseBranchName(ref, allBranches))
  }

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
    const initialType = target?.sourceType ?? 'new'
    setMergeTargetBranch(initialType === 'new' ? baseBranchName(target?.sourceRef ?? defaultBase, allBranches) : project?.defaultBranch ?? 'main')
    mergeTargetChosen.current = false
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
    if (!mergeTargetChosen.current) setMergeTargetBranch(type === 'new' ? baseBranchName(defaultBase, allBranches) : project.defaultBranch)
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
    const input = { projectId: project.id, sourceType, sourceRef, branchName: sourceType === 'new' ? branchName : selectedLocal, mergeTargetBranch }
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

  const branchBadge = (text: string, tone: 'plain' | 'accent' = 'plain') => (
    <span className={'rounded-[4px] border px-1.5 py-px font-mono text-[9.5px] ' + (tone === 'accent' ? 'border-accent/35 text-accent' : 'border-border text-muted-2')}>{text}</span>
  )
  const isDefault = (name: string) => name === project.defaultBranch
  const baseOptions = (allBranches ?? []).map(branch => ({
    value: branch.ref || branch.name,
    label: branch.name,
    icon: branch.remote ? <Radio size={12} /> : <GitBranch size={12} />,
    meta: isDefault(branch.name) ? branchBadge('default', 'accent') : branchBadge(branch.remote ? 'remote' : 'local'),
  }))
  const pickOptions = (sourceType === 'existing' ? localBranches : originBranches).map(branch => ({
    value: branch,
    label: branch,
    icon: sourceType === 'origin' ? <Radio size={12} /> : <GitBranch size={12} />,
    meta: isDefault(branch) ? branchBadge('default', 'accent') : branchBadge(sourceType === 'origin' ? 'remote' : 'local'),
  }))
  const mergeTargets = worktreeTargets.includes(mergeTargetBranch) ? worktreeTargets : [...worktreeTargets, mergeTargetBranch]
  const mergeOptions = mergeTargets.map(branch => ({
    value: branch,
    label: branch,
    icon: <GitMerge size={12} />,
    meta: branch === project.defaultBranch ? branchBadge('default', 'accent') : branchBadge(worktreeTargets.includes(branch) ? 'worktree' : 'base'),
  }))
  const sourceLabel = sourceType === 'new' ? 'Branch from' : sourceType === 'existing' ? 'Local branch' : 'Remote branch'
  const sourceNote = sourceType === 'existing' ? 'Every local branch already has a worktree.' : sourceType === 'origin' ? 'No remote branches left. Fetch to refresh the list.' : 'No branches found.'
  const branchLabel = sourceType === 'new' ? branchName.trim() || 'new branch' : selectedLocal || 'no branch'
  const summary = [branchLabel, sourceType === 'new' && sourceRef && `from ${sourceRef.replace(/^refs\/(heads|remotes)\//, '')}`, mergeTargetBranch && `merges into ${mergeTargetBranch}`].filter(Boolean).join(' · ')

  return (
    <DialogFrame
      icon={GitFork}
      title="New worktree"
      subtitle={project.repository}
      closeLabel="Close"
      onClose={() => setOpen(false)}
      closeDisabled={pending}
      layer={80}
      width="md"
      onSubmit={submit}
      summary={summary}
      actions={
        <>
          <button type="button" onClick={() => setOpen(false)} className="bonsai-focus btn-bordered h-9 rounded-[8px] px-3.5 text-[12px] text-muted hover:text-text">Cancel</button>
          <button type="submit" disabled={pending || !sourceRef || (sourceType === 'new' && !branchName.trim())} className="bonsai-focus btn-primary h-9 px-4 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">{pending ? 'Creating…' : createdId ? 'Retry saving settings' : existingWorktree ? 'Open worktree' : 'Create worktree'}</button>
        </>
      }
    >
      <fieldset disabled={pending || !!createdId} className="m-0 min-w-0 space-y-5 border-0 p-0">
        <Field label="Branch source">
          <Segmented
            ariaLabel="Branch source"
            value={sourceType}
            onChange={value => changeSourceType(value as WorktreeSourceType)}
            options={sourceOptions.map(option => {
              const Icon = option.icon
              return {
                value: option.id,
                title: option.description,
                label: <><Icon size={13} className={option.id === sourceType ? 'text-accent' : 'text-muted'} /><span>{option.label}</span></>,
              }
            })}
          />
        </Field>

        <div key={sourceType} className="dialog-swap space-y-5">
          {sourceType === 'new' && (
            <Field label="New branch name" htmlFor="worktree-branch-name">
              <div className="relative">
                <GitBranch size={13} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-2" />
                <input id="worktree-branch-name" autoFocus value={branchName} onChange={(event) => setBranchName(event.target.value)} placeholder="feat/my-branch" spellCheck={false} className={TEXT_INPUT + ' pl-8 font-mono'} />
              </div>
            </Field>
          )}
          <div className="dialog-cols">
            <Field label={sourceLabel} aside={<span className="font-mono text-[10px] text-muted-2">{sourceType === 'new' ? 'the new branch starts here' : 'checked out into the new worktree'}</span>}>
              <OptionList
                searchable
                mono
                ariaLabel={sourceLabel}
                filterLabel="Filter branches"
                placeholder="Filter branches…"
                emptyText={sourceNote}
                options={sourceType === 'new' ? baseOptions : pickOptions}
                value={sourceRef}
                onChange={chooseSourceRef}
              />
            </Field>
            <Field label="Merges into" aside={<span className="font-mono text-[10px] text-muted-2">where its PR lands</span>}>
              <OptionList mono ariaLabel="Merge target" options={mergeOptions} value={mergeTargetBranch} onChange={chooseMergeTarget} height={224 + 36} />
            </Field>
          </div>
        </div>
        <p className="font-mono text-[10px] leading-4 text-muted-2">Follows the base branch. Rebase onto in the worktree menu uses it; non-default targets become explicit PR relationships on the canvas.</p>
        {existingWorktree && !createdId && (
          <Reveal><p role="status" className="rounded-[8px] border border-accent/30 bg-accent/10 px-3 py-2 text-[11px] text-accent">{selectedLocal} already has a worktree. Creating opens it instead.</p></Reveal>
        )}
      </fieldset>
      {error && <p role="alert" className="mt-4 text-[11px] text-danger">{error}</p>}
    </DialogFrame>
  )
}
