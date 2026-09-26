import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Check, GitBranch, GitFork, Plus, Radio, X } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import type { WorktreeSourceType } from '../../types'
import { getTagPresentation } from './tagStyles'

const sourceOptions: Array<{ id: WorktreeSourceType; label: string; description: string; icon: typeof GitBranch }> = [
  { id: 'existing', label: 'Existing branch', description: 'Attach a local branch that is not checked out.', icon: GitBranch },
  { id: 'origin', label: 'Origin branch', description: 'Create the worktree from a remote tracking branch.', icon: Radio },
  { id: 'new', label: 'New branch', description: 'Create a branch from an existing base ref.', icon: Plus },
]

export function CreateWorktreeDialog() {
  const open = useBonsaiStore((state) => state.worktreeDialogOpen)
  const setOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const createWorktree = useBonsaiStore((state) => state.createMockWorktree)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const tags = useBonsaiStore((state) => state.worktreeTags)

  const project = projects.find((item) => item.id === activeProjectId)
  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const checkedOutBranches = useMemo(() => new Set(projectWorktrees.map((item) => item.branch)), [projectWorktrees])

  const localBranches = useMemo(
    () =>
      [project?.defaultBranch ?? 'main', 'feat/local-experiment', 'chore/docs-refresh', 'fix/cache-key']
        .filter((branch, index, list) => list.indexOf(branch) === index)
        .filter((branch) => !checkedOutBranches.has(branch)),
    [checkedOutBranches, project?.defaultBranch],
  )
  const originBranches = useMemo(
    () =>
      [
        'origin/' + (project?.defaultBranch ?? 'main'),
        ...projectWorktrees.map((item) => 'origin/' + item.branch),
        'origin/feat/new-dashboard',
        'origin/fix/login-error',
      ].filter((branch, index, list) => list.indexOf(branch) === index && !checkedOutBranches.has(branch.replace(/^origin\//, ''))),
    [checkedOutBranches, project?.defaultBranch, projectWorktrees],
  )
  const mergeTargets = [project?.defaultBranch ?? 'main', ...projectWorktrees.filter((item) => item.branch !== project?.defaultBranch).map((item) => item.branch)]

  const [sourceType, setSourceType] = useState<WorktreeSourceType>('new')
  const [sourceRef, setSourceRef] = useState(project?.defaultBranch ?? 'main')
  const [branchName, setBranchName] = useState('feat/new-worktree')
  const [tagId, setTagId] = useState('feat')
  const [mergeTargetBranch, setMergeTargetBranch] = useState(project?.defaultBranch ?? 'main')

  useEffect(() => {
    if (!open) return
    setSourceType('new')
    setSourceRef(project?.defaultBranch ?? 'main')
    setBranchName('feat/new-worktree')
    setTagId(tags.find((tag) => tag.id === 'feat')?.id ?? tags[0]?.id ?? '')
    setMergeTargetBranch(project?.defaultBranch ?? 'main')
  }, [open, project?.defaultBranch, tags])

  useEffect(() => {
    if (sourceType === 'existing') setSourceRef(localBranches[0] ?? '')
    if (sourceType === 'origin') setSourceRef(originBranches[0] ?? '')
    if (sourceType === 'new') setSourceRef(project?.defaultBranch ?? 'main')
  }, [localBranches, originBranches, project?.defaultBranch, sourceType])

  if (!open || !project) return null

  const submit = (event: FormEvent) => {
    event.preventDefault()
    createWorktree({ sourceType, sourceRef, branchName: sourceType === 'new' ? branchName : undefined, tagId, mergeTargetBranch })
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
    <div className="absolute inset-0 z-[80] grid place-items-center bg-black/55 p-6 backdrop-blur-[2px]">
      <form onSubmit={submit} className="w-full max-w-[620px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-[0_28px_90px_rgb(0_0_0/.62)]">
        <div className="flex h-12 items-center border-b border-[rgb(var(--border))] px-4">
          <GitFork size={15} className="mr-2 text-[rgb(var(--purple))]" />
          <div>
            <div className="text-[12px] font-semibold">New worktree</div>
            <div className="text-[9px] text-[rgb(var(--muted-2))]">{project.repository}</div>
          </div>
          <button type="button" onClick={() => setOpen(false)} aria-label="Close" className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">
            <X size={14} />
          </button>
        </div>

        <div className="space-y-5 p-4">
          <section>
            <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Branch source</div>
            <div className="grid grid-cols-3 gap-2">
              {sourceOptions.map((option) => {
                const Icon = option.icon
                const active = option.id === sourceType
                return (
                  <button type="button" key={option.id} onClick={() => setSourceType(option.id)} className={'bonsai-focus rounded-lg border p-3 text-left transition-colors ' + (active ? 'border-[rgb(var(--purple)/.55)] bg-[rgb(var(--purple)/.10)]' : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] hover:border-[rgb(var(--border-strong))]')}>
                    <div className="flex items-center gap-2 text-[11px] font-medium">
                      <Icon size={13} className={active ? 'text-[rgb(var(--purple))]' : 'text-[rgb(var(--muted))]'} />
                      {option.label}
                      {active && <Check size={11} className="ml-auto text-[rgb(var(--purple))]" />}
                    </div>
                    <p className="mt-1.5 text-[9px] leading-4 text-[rgb(var(--muted-2))]">{option.description}</p>
                  </button>
                )
              })}
            </div>
          </section>

          <div className="grid grid-cols-2 gap-3">
            {sourceType === 'new' ? (
              <>
                <label className="block">
                  <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">New branch name</span>
                  <input autoFocus value={branchName} onChange={(event) => setBranchName(event.target.value)} placeholder="feat/my-branch" className="bonsai-focus h-9 w-full rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 font-mono text-[11px] outline-none" />
                </label>
                <label className="block">
                  <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Branch from</span>
                  <BonsaiSelect ariaLabel="Branch from" searchable value={sourceRef} onChange={setSourceRef} options={mergeOptions} />
                </label>
              </>
            ) : (
              <label className="col-span-2 block">
                <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">{sourceType === 'existing' ? 'Local branch' : 'Remote branch'}</span>
                <BonsaiSelect ariaLabel={sourceType === 'existing' ? 'Local branch' : 'Remote branch'} searchable value={sourceRef} onChange={setSourceRef} options={branchOptions} />
              </label>
            )}
          </div>

          <section>
            <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Worktree tag</div>
            <div className="flex flex-wrap gap-2">
              {tags.map((tag) => {
                const presentation = getTagPresentation(tag)
                const active = tag.id === tagId
                return (
                  <button
                    type="button"
                    key={tag.id}
                    onClick={() => setTagId(tag.id)}
                    className="bonsai-focus rounded-md border px-2.5 py-1.5 text-[10px] font-medium"
                    style={{ color: presentation.foreground, borderColor: active ? presentation.foreground : presentation.border, background: active ? presentation.background : 'rgb(var(--bg))' }}
                  >
                    {tag.name}
                  </button>
                )
              })}
            </div>
          </section>

          <label className="block">
            <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Merges into</span>
            <BonsaiSelect ariaLabel="Merge target" searchable value={mergeTargetBranch} onChange={setMergeTargetBranch} options={mergeOptions} />
            <span className="mt-1.5 block text-[9px] text-[rgb(var(--muted-2))]">Non-default targets become explicit PR relationships on the canvas.</span>
          </label>
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.45)] px-4 py-3">
          <button type="button" onClick={() => setOpen(false)} className="bonsai-focus rounded-md px-3 py-2 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">Cancel</button>
          <button type="submit" disabled={!tagId || !sourceRef || (sourceType === 'new' && !branchName.trim())} className="bonsai-focus rounded-md border border-[rgb(var(--purple)/.45)] bg-[rgb(var(--purple)/.16)] px-3 py-2 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-40">Create worktree</button>
        </div>
      </form>
    </div>
  )
}
