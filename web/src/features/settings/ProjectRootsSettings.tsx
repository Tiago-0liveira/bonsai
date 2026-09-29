import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { changeProjectRoot, changeProjectSelection, loadProjectRoots } from '../../api/settings'
import { useBonsaiStore } from '../../stores/bonsai'

export function ProjectRootsSettings({ onDismiss }: { onDismiss?: () => void }) {
  const settings = useBonsaiStore(s => s.rootSettings)
  const loading = useBonsaiStore(s => s.rootsLoading)
  const saving = useBonsaiStore(s => s.rootsSaving)
  const error = useBonsaiStore(s => s.rootsError)
  const [path, setPath] = useState('')
  const [selectingRepositories, setSelectingRepositories] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  useEffect(() => {
    if (!settings) void loadProjectRoots().catch(() => undefined)
  }, [settings])

  useEffect(() => {
    setSelectedIds(new Set(settings?.repositories.filter(repository => repository.selected).map(repository => repository.id) ?? []))
  }, [settings?.selection_revision, settings?.repositories])

  const discovered = settings?.repositories ?? []
  const discoveryNotes = settings?.diagnostics.flatMap(diagnostic => diagnostic.messages) ?? []
  const selectedCount = useMemo(() => discovered.filter(repository => selectedIds.has(repository.id)).length, [discovered, selectedIds])

  const add = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const next = await changeProjectRoot(path)
      setPath('')
      if (next?.repositories.length) setSelectingRepositories(true)
    } catch {
      // Store shows the save error inline.
    }
  }

  const remove = async (rootId: string) => {
    try {
      const next = await changeProjectRoot(undefined, rootId)
      if (next?.repositories.length) setSelectingRepositories(true)
    } catch {
      // Store shows the save error inline.
    }
  }

  const saveSelection = async () => {
    try {
      await changeProjectSelection([...selectedIds])
      setSelectingRepositories(false)
    } catch {
      // Store shows the save error inline.
    }
  }

  const toggleRepository = (id: string) => {
    setSelectedIds(current => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return <section className="mx-auto w-full max-w-2xl space-y-5 p-6" aria-labelledby="project-roots-title">
    <div className="flex items-center justify-between gap-4">
      <h1 id="project-roots-title" className="text-xl font-semibold">{selectingRepositories ? 'Repositories found' : 'Project folders'}</h1>
      {onDismiss && <button className="bonsai-focus rounded border px-3 py-1" onClick={onDismiss}>Later</button>}
    </div>

    {error && <div role="alert" className="rounded border border-red-400 p-3 text-sm">{error} <button onClick={() => void loadProjectRoots().catch(() => undefined)} className="underline">Reload settings</button></div>}
    {loading && <p role="status">Loading project folders…</p>}

    {settings && selectingRepositories ? <>
      <p className="text-sm text-[rgb(var(--muted))]">Choose which discovered repositories Bonsai should activate, watch, and synchronize. Unchecked repositories remain discovered but inactive.</p>
      {discoveryNotes.length > 0 && <div role="status" className="rounded border border-amber-400 p-3 text-sm">
        <p className="font-medium">Discovery notes</p>
        {discoveryNotes.map(message => <p className="mt-1 text-[rgb(var(--muted))]" key={message}>{message}</p>)}
      </div>}
      {discovered.length > 0 ? <>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm">{selectedCount} of {discovered.length} selected</p>
          <div className="flex gap-2">
            <button disabled={saving} onClick={() => setSelectedIds(new Set(discovered.map(repository => repository.id)))} className="bonsai-focus rounded border px-3 py-1 text-sm">Select all</button>
            <button disabled={saving} onClick={() => setSelectedIds(new Set())} className="bonsai-focus rounded border px-3 py-1 text-sm">Select none</button>
          </div>
        </div>
        <ul className="space-y-2">
          {discovered.map(repository => <li key={repository.id} className="rounded border border-[rgb(var(--border))] p-3">
            <label className="flex cursor-pointer items-start gap-3">
              <input type="checkbox" checked={selectedIds.has(repository.id)} onChange={() => toggleRepository(repository.id)} disabled={saving} className="mt-1" />
              <span className="min-w-0">
                <span className="block font-medium">{repository.name}</span>
                <span className="block break-all font-mono text-xs text-[rgb(var(--muted))]">{repository.path}</span>
              </span>
            </label>
          </li>)}
        </ul>
      </> : <p>No Git repositories were found in the configured folders.</p>}
      <div className="flex justify-between gap-3">
        <button disabled={saving} onClick={() => setSelectingRepositories(false)} className="bonsai-focus rounded border px-4 py-2">Back to folders</button>
        <button disabled={saving} onClick={() => void saveSelection()} className="bonsai-focus rounded bg-[rgb(var(--purple))] px-4 py-2 text-white disabled:opacity-50">{saving ? 'Saving…' : 'Use selected repositories'}</button>
      </div>
    </> : settings && <>
      <p className="text-sm text-[rgb(var(--muted))]">Choose folders on this computer where Bonsai should discover Git repositories. Discovery includes up to four levels of subfolders; discovered repositories are activated only after you select them.</p>
      <ul className="space-y-3">
        {settings.roots.map(root => {
          const diagnostic = settings.diagnostics.find(d => d.root_id === root.id)
          return <li key={root.id} className="rounded border border-[rgb(var(--border))] p-3">
            <div className="flex items-center justify-between gap-3">
              <span className="break-all font-mono text-sm">{root.path}</span>
              <button disabled={saving} onClick={() => void remove(root.id)} className="bonsai-focus rounded border px-3 py-1" aria-label={`Remove ${root.path}`}>Remove</button>
            </div>
            {diagnostic && !diagnostic.available && <p className="mt-2 text-sm">Folder unavailable</p>}
            {diagnostic?.messages.map(message => <p className="mt-2 text-sm text-[rgb(var(--muted))]" key={message}>{message}</p>)}
          </li>
        })}
      </ul>
      {settings.roots.length === 0 && <p>No project folders configured yet.</p>}
      {discovered.length > 0 && <button disabled={saving} onClick={() => setSelectingRepositories(true)} className="bonsai-focus w-full rounded border border-[rgb(var(--border))] px-4 py-3 text-left">
        <span className="block font-medium">Review discovered repositories</span>
        <span className="block text-sm text-[rgb(var(--muted))]">{settings.repositories.filter(repository => repository.selected).length} of {discovered.length} active</span>
      </button>}
      {settings.suggestions.length > 0 && <div className="space-y-2">
        <p className="text-sm font-medium">Suggested folders</p>
        <div className="flex flex-wrap gap-2">{settings.suggestions.filter(suggestion => !settings.roots.some(root => root.path === suggestion)).map(suggestion => <button key={suggestion} disabled={saving} onClick={() => setPath(suggestion)} className="bonsai-focus rounded border border-[rgb(var(--border))] px-3 py-2 text-sm">{suggestion}</button>)}</div>
      </div>}
      <form onSubmit={add} className="space-y-2">
        <label htmlFor="project-root-path" className="block text-sm font-medium">Folder path</label>
        <div className="flex gap-2">
          <input id="project-root-path" value={path} onChange={event => setPath(event.target.value)} placeholder="~/projects or an absolute path" className="bonsai-focus min-w-0 flex-1 rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3 py-2" />
          <button type="submit" disabled={saving || !path.trim()} className="bonsai-focus rounded bg-[rgb(var(--purple))] px-4 py-2 text-white disabled:opacity-50">{saving ? 'Scanning…' : 'Add folder'}</button>
        </div>
      </form>
      <p className="text-xs text-[rgb(var(--muted))]">Removing a folder only changes discovery and future worktree placement. Your files, worktrees and running processes stay on disk.</p>
    </>}
  </section>
}
