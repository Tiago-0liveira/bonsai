import { useEffect, useState, type FormEvent } from 'react'
import { changeProjectRoot, loadProjectRoots } from '../../api/settings'
import { useBonsaiStore } from '../../stores/bonsai'

export function ProjectRootsSettings({ onDismiss }: { onDismiss?: () => void }) {
  const settings = useBonsaiStore(s => s.rootSettings)
  const loading = useBonsaiStore(s => s.rootsLoading)
  const saving = useBonsaiStore(s => s.rootsSaving)
  const error = useBonsaiStore(s => s.rootsError)
  const [path, setPath] = useState('')
  useEffect(() => { if (!settings) void loadProjectRoots().catch(() => undefined) }, [settings])
  const add = async (event: FormEvent) => {
    event.preventDefault()
    try { await changeProjectRoot(path); setPath('') } catch { /* Store shows the save error inline. */ }
  }
  return <section className="mx-auto w-full max-w-2xl space-y-5 p-6" aria-labelledby="project-roots-title">
    <div className="flex items-center justify-between gap-4">
      <h1 id="project-roots-title" className="text-xl font-semibold">Project folders</h1>
      {onDismiss && <button className="bonsai-focus rounded border px-3 py-1" onClick={onDismiss}>Later</button>}
    </div>
    <p className="text-sm text-[rgb(var(--muted))]">Choose folders on this computer where Bonsai should find Git repositories. Discovery includes up to four levels of subfolders.</p>
    {error && <div role="alert" className="rounded border border-red-400 p-3 text-sm">{error} <button onClick={() => void loadProjectRoots().catch(() => undefined)} className="underline">Reload settings</button></div>}
    {loading && <p role="status">Loading project folders…</p>}
    {settings && <>
      <ul className="space-y-3">
        {settings.roots.map(root => {
          const diagnostic = settings.diagnostics.find(d => d.root_id === root.id)
          return <li key={root.id} className="rounded border border-[rgb(var(--border))] p-3">
            <div className="flex items-center justify-between gap-3"><span className="break-all font-mono text-sm">{root.path}</span><button disabled={saving} onClick={() => void changeProjectRoot(undefined, root.id).catch(() => undefined)} className="bonsai-focus rounded border px-3 py-1" aria-label={`Remove ${root.path}`}>Remove</button></div>
            {diagnostic && !diagnostic.available && <p className="mt-2 text-sm">Folder unavailable</p>}
            {diagnostic?.messages.map(message => <p className="mt-2 text-sm text-[rgb(var(--muted))]" key={message}>{message}</p>)}
          </li>
        })}
      </ul>
      {settings.roots.length === 0 && <p>No project folders configured yet.</p>}
      {settings.suggestions.length > 0 && <div className="space-y-2"><p className="text-sm font-medium">Suggested folders</p><div className="flex flex-wrap gap-2">{settings.suggestions.filter(suggestion => !settings.roots.some(root => root.path === suggestion)).map(suggestion => <button key={suggestion} disabled={saving} onClick={() => setPath(suggestion)} className="bonsai-focus rounded border border-[rgb(var(--border))] px-3 py-2 text-sm">{suggestion}</button>)}</div></div>}
      <form onSubmit={add} className="space-y-2">
        <label htmlFor="project-root-path" className="block text-sm font-medium">Folder path</label>
        <div className="flex gap-2"><input id="project-root-path" value={path} onChange={event => setPath(event.target.value)} placeholder="~/projects or an absolute path" className="bonsai-focus min-w-0 flex-1 rounded border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3 py-2" /><button type="submit" disabled={saving || !path.trim()} className="bonsai-focus rounded bg-[rgb(var(--purple))] px-4 py-2 text-white disabled:opacity-50">{saving ? 'Saving…' : 'Add folder'}</button></div>
      </form>
      <p className="text-xs text-[rgb(var(--muted))]">Removing a folder only changes discovery and future worktree placement. Your files, worktrees and running processes stay on disk.</p>
    </>}
  </section>
}
