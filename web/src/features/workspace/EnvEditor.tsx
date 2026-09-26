import { useState } from 'react'
import { Eye, EyeOff, KeyRound, Lock, LockOpen, Plus, Trash2, X } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'

const EMPTY_ENV_VARIABLES: never[] = []

export function EnvEditor() {
  const open = useBonsaiStore((state) => state.envEditorOpen)
  const setOpen = useBonsaiStore((state) => state.setEnvEditorOpen)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const projects = useBonsaiStore((state) => state.projects)
  const storedVariables = useBonsaiStore((state) => state.envVariables[activeProjectId])
  const variables = storedVariables ?? EMPTY_ENV_VARIABLES
  const addVariable = useBonsaiStore((state) => state.addEnvVariable)
  const updateVariable = useBonsaiStore((state) => state.updateEnvVariable)
  const removeVariable = useBonsaiStore((state) => state.removeEnvVariable)
  const [revealed, setRevealed] = useState<string[]>([])

  if (!open) return null
  const project = projects.find((item) => item.id === activeProjectId)

  const toggleReveal = (id: string) => {
    setRevealed((items) => items.includes(id) ? items.filter((item) => item !== id) : [...items, id])
  }

  return (
    <aside className="absolute right-3 top-14 z-50 flex max-h-[calc(100%-68px)] w-[420px] flex-col overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-[0_24px_80px_rgb(0_0_0/.55)]">
      <div className="flex h-11 shrink-0 items-center border-b border-[rgb(var(--border))] px-3">
        <KeyRound size={14} className="mr-2 text-[rgb(var(--orange))]" />
        <div>
          <div className="text-[11px] font-semibold">Environment variables</div>
          <div className="text-[9px] text-[rgb(var(--muted-2))]">{project?.name ?? activeProjectId} · project scope</div>
        </div>
        <button
          onClick={() => setOpen(false)}
          className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
          title="Close environment editor"
        >
          <X size={13} />
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-3">
        <div className="space-y-2">
          {variables.map((variable) => {
            const show = !variable.secret || revealed.includes(variable.id)
            return (
              <div key={variable.id} className="rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-2.5">
                <div className="grid grid-cols-[minmax(0,.9fr)_minmax(0,1.2fr)_auto] gap-2">
                  <input
                    value={variable.key}
                    onChange={(event) => updateVariable(activeProjectId, variable.id, { key: event.target.value })}
                    placeholder="VARIABLE_NAME"
                    className="bonsai-focus h-8 min-w-0 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] px-2 font-mono text-[10px] outline-none"
                  />
                  <div className="relative min-w-0">
                    <input
                      type={show ? 'text' : 'password'}
                      value={variable.value}
                      onChange={(event) => updateVariable(activeProjectId, variable.id, { value: event.target.value })}
                      placeholder="value"
                      className="bonsai-focus h-8 w-full rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] px-2 pr-8 font-mono text-[10px] outline-none"
                    />
                    {variable.secret && (
                      <button
                        type="button"
                        onClick={() => toggleReveal(variable.id)}
                        className="absolute right-1 top-1 grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted-2))] hover:text-[rgb(var(--text))]"
                        title={show ? 'Hide value' : 'Reveal value'}
                      >
                        {show ? <EyeOff size={11} /> : <Eye size={11} />}
                      </button>
                    )}
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      onClick={() => updateVariable(activeProjectId, variable.id, { secret: !variable.secret })}
                      className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
                      title={variable.secret ? 'Mark as plain text' : 'Mark as secret'}
                    >
                      {variable.secret ? <Lock size={12} /> : <LockOpen size={12} />}
                    </button>
                    <button
                      type="button"
                      onClick={() => removeVariable(activeProjectId, variable.id)}
                      className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:border-[rgb(var(--red)/.45)] hover:text-[rgb(var(--red))]"
                      title="Remove variable"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                </div>
              </div>
            )
          })}
          {variables.length === 0 && (
            <div className="rounded-lg border border-dashed border-[rgb(var(--border))] p-5 text-center text-[10px] text-[rgb(var(--muted-2))]">
              No project environment variables yet.
            </div>
          )}
        </div>
      </div>

      <div className="shrink-0 border-t border-[rgb(var(--border))] p-3">
        <button
          onClick={() => addVariable(activeProjectId)}
          className="bonsai-focus flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[10px] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"
        >
          <Plus size={12} /> Add variable
        </button>
      </div>
    </aside>
  )
}
