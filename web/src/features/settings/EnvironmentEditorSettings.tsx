import { useEffect } from 'react'
import { useBonsaiStore } from '../../stores/bonsai'

export function EnvironmentEditorSettings() {
  const settings = useBonsaiStore(state => state.editorSettings)
  const loading = useBonsaiStore(state => state.editorSettingsLoading)
  const saving = useBonsaiStore(state => state.editorSettingsSaving)
  const error = useBonsaiStore(state => state.editorSettingsError)
  const load = useBonsaiStore(state => state.loadEditorSettings)
  const save = useBonsaiStore(state => state.setEnvironmentEditor)
  useEffect(() => { void load().catch(() => undefined) }, [load])

  return <section className="mx-auto w-full max-w-2xl space-y-3 border-b border-border-subtle p-6" aria-labelledby="environment-editor-title">
    <h2 id="environment-editor-title" className="text-[13px] font-semibold">Environment editor</h2>
    <p className="text-sm text-muted">Open project files with an editor installed in {settings?.environment.label ?? 'the environment running Bonsai'}. This choice applies to projects in this environment.</p>
    {loading && <p role="status" className="text-xs text-muted">Finding installed editors…</p>}
    {settings && <>
      <label className="flex items-center gap-3 text-[12px]">Default editor
        <select aria-label="Default editor" className="bonsai-focus h-8 rounded-[7px] border border-border bg-well px-2.5 text-[12px] focus:border-accent/55"
          value={settings.preferred} disabled={loading || saving || settings.editors.length === 0}
          onChange={event => { void save(event.target.value).catch(() => undefined) }}>
          <option value="">Ask when opening a file</option>
          {settings.editors.map(editor => <option key={editor.id} value={editor.id}>{editor.name}</option>)}
        </select>
      </label>
      {settings.saved_preferred && !settings.preferred && <p className="text-xs text-muted">Your previous editor is unavailable in this environment. Choose another installed editor.</p>}
      {settings.editors.length === 0 && <p className="text-sm text-muted">No supported editors were found. Install a GUI editor on this environment’s PATH, then refresh. Restart Bonsai if PATH changed.</p>}
      {settings.environment.id.startsWith('wsl:') && <p className="text-xs text-muted">Only editors installed inside WSL are available here. Linux GUI editors need a working graphical session, such as WSLg.</p>}
    </>}
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    <button type="button" className="bonsai-focus btn-ghost text-[12px]" disabled={loading || saving} onClick={() => { void load().catch(() => undefined) }}>Refresh editors</button>
  </section>
}
