import { ExternalLink, FileCode2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useBonsaiStore } from '../../stores/bonsai'

export function EditorPreferenceDialog() {
  const open = useBonsaiStore(state => state.editorPromptOpen)
  const pending = useBonsaiStore(state => state.pendingOpenFile)
  const settings = useBonsaiStore(state => state.editorSettings)
  const loading = useBonsaiStore(state => state.editorSettingsLoading)
  const error = useBonsaiStore(state => state.editorSettingsError)
  const choose = useBonsaiStore(state => state.chooseFileEditor)
  const load = useBonsaiStore(state => state.loadEditorSettings)
  const close = useBonsaiStore(state => state.closeEditorPrompt)
  const [busy, setBusy] = useState(false)
  const dialog = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const previous = document.activeElement
    const first = dialog.current?.querySelector<HTMLButtonElement>('[data-editor-choice]') ?? dialog.current?.querySelector<HTMLButtonElement>('button')
    first?.focus()
    return () => { if (previous instanceof HTMLElement) previous.focus() }
  }, [open])
  if (!open) return null

  return <div className="fixed inset-0 z-[120] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
    <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="editor-dialog-title" className="w-full max-w-[430px] overflow-hidden rounded-[14px] border border-border-strong bg-panel shadow-overlay" onKeyDown={event => {
      if (event.key === 'Escape' && !busy) { event.stopPropagation(); close() }
      if (event.key !== 'Tab') return
      const buttons = [...(dialog.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])]
      const first = buttons[0], last = buttons.at(-1)
      if (!first) { event.preventDefault(); return }
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }}>
      <div className="flex h-11 items-center border-b border-border-subtle px-3">
        <FileCode2 size={13} className="mr-2 text-accent" />
        <div>
          <div id="editor-dialog-title" className="text-[13px] font-semibold">Open files with…</div>
          <div className="max-w-[300px] truncate font-mono text-[10px] text-muted-2">{pending?.path}</div>
        </div>
        <button type="button" aria-label="Close editor chooser" disabled={busy} onClick={close} className="bonsai-focus ml-auto grid h-6 w-6 place-items-center rounded text-muted hover:bg-panel-3 hover:text-text"><X size={11} /></button>
      </div>
      <div className="space-y-2 p-3">
        <p className="text-xs text-muted">Installed editors in {settings?.environment.label ?? 'this environment'}</p>
        {settings?.editors.map(editor => <button type="button" data-editor-choice key={editor.id} disabled={busy || loading}
          onClick={() => { setBusy(true); void choose(editor.id).finally(() => setBusy(false)) }}
          className="bonsai-focus flex w-full items-center gap-3 rounded-[10px] border border-transparent px-3 py-2.5 text-left hover:border-border hover:bg-panel-2 disabled:opacity-50">
          <span className="grid h-8 w-8 place-items-center rounded-[7px] border border-border bg-well"><ExternalLink size={12} /></span>
          <span className="text-[12px] font-medium">{editor.name}</span>
        </button>)}
        {settings?.editors.length === 0 && <p className="text-xs text-muted">No supported editors were found. Install a GUI editor on PATH inside this environment, then refresh. Restart Bonsai if PATH changed.</p>}
        {error && <p role="alert" className="text-xs text-danger">{error}</p>}
        <button type="button" className="bonsai-focus btn-ghost text-xs" disabled={busy || loading} onClick={() => { void load().catch(() => undefined) }}>{loading ? 'Finding editors…' : 'Refresh editors'}</button>
      </div>
      <div className="border-t border-border-subtle px-3 py-2 text-[10px] text-muted-2">This opens the file and saves your choice for this environment. Change it in Settings → Environment editor.</div>
    </div>
  </div>
}
