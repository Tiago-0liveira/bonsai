import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Package, Play, X } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { processCommands, startProcess, previewProcess, applyProcessSummary, type ProcessCatalog, type ProcessPolicy } from '../../api/processes'
import { useBonsaiStore } from '../../stores/bonsai'

function catalogPackages(catalog: ProcessCatalog | null) {
  const packages = [...(catalog?.providers ?? [])]
  for (const command of catalog?.commands ?? []) {
    const root = command.project_root || command.invocation.working_dir
    if (!packages.some(item => item.id === command.provider && item.root === root)) {
      packages.push({ id: command.provider, name: command.provider === 'override' ? 'Custom commands' : command.provider, root })
    }
  }
  return packages
}

export function StartProcessDialog() {
  const open = useBonsaiStore(state => state.startProcessDialogOpen)
  return open ? <StartProcessForm /> : null
}

function StartProcessForm() {
  const close = useBonsaiStore(state => state.setStartProcessDialogOpen)
  const projectId = useBonsaiStore(state => state.startProcessTargetProjectId)
  const target = useBonsaiStore(state => state.startProcessTargetWorktreeId)
  const worktrees = useBonsaiStore(state => state.worktrees)
  const availableWorktrees = worktrees.filter(tree => tree.projectId === projectId && !tree.missing)
  const [worktreeId, setWorktreeId] = useState(target)
  const [catalog, setCatalog] = useState<ProcessCatalog | null>(null)
  const [packageId, setPackageId] = useState('')
  const [commandId, setCommandId] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [reload, setReload] = useState(0)
  const busy = useRef(false)
  const [policyMode, setPolicyMode] = useState<'inherit' | ProcessPolicy['mode']>('inherit')
  const [maxRetries, setMaxRetries] = useState('5')
  const [preview, setPreview] = useState<Awaited<ReturnType<typeof previewProcess>> | null>(null)
  const [previewError, setPreviewError] = useState('')

  useEffect(() => {
    let disposed = false
    setCatalog(null)
    setPackageId('')
    setCommandId('')
    setValues({})
    setError('')
    if (!worktreeId) { setLoading(false); return }
    setLoading(true)
    processCommands(projectId, worktreeId).then(result => {
      if (disposed) return
      setCatalog(result)
      const packages = catalogPackages(result)
      if (packages.length === 1) setPackageId(JSON.stringify([packages[0].id, packages[0].root]))
    }).catch(error => { if (!disposed) setError(error instanceof Error ? error.message : String(error)) })
      .finally(() => { if (!disposed) setLoading(false) })
    return () => { disposed = true }
  }, [projectId, worktreeId, reload])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy.current) close(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [close])

  const providers = catalogPackages(catalog)
  const commands = (catalog?.commands ?? []).filter(command => JSON.stringify([command.provider, command.project_root || command.invocation.working_dir]) === packageId)
  const selected = commands.find(command => command.id === commandId)
  const argumentsById: Record<string, string[]> = {}
  for (const arg of selected?.args ?? []) {
    const value = values[arg.id]
    if (value !== undefined && value !== '') argumentsById[arg.id] = arg.variadic || arg.kind === 'passthrough' ? value.split('\n').filter(Boolean) : [value]
  }
  const serializedArguments = JSON.stringify(argumentsById)
  useEffect(() => {
    let disposed = false
    setPreview(null); setPreviewError('')
    if (!commandId) return
    const timer = setTimeout(() => {
      void previewProcess(projectId, worktreeId, commandId, JSON.parse(serializedArguments)).then(value => {
        if (!disposed) setPreview(value)
      }).catch(error => { if (!disposed) setPreviewError(error instanceof Error ? error.message : String(error)) })
    }, 150)
    return () => { disposed = true; clearTimeout(timer) }
  }, [projectId, worktreeId, commandId, serializedArguments])
  const inherited = preview?.default_policy ?? catalog?.default_policies?.[commandId]
  const effectiveMode = policyMode === 'inherit' ? inherited?.mode : policyMode
  const validRetries = /^\d+$/.test(maxRetries) && Number(maxRetries) <= 100
  const canStart = !loading && !pending && !!selected && !!preview && (policyMode === 'inherit' || policyMode === 'no' || validRetries) && availableWorktrees.some(tree => tree.id === worktreeId)
  const scope = (root: string) => root === catalog?.location.input_dir ? '.' : root.startsWith((catalog?.location.input_dir ?? '') + '/') ? root.slice(catalog!.location.input_dir.length + 1) : root

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canStart || busy.current || !selected) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      const summary = await startProcess(projectId, worktreeId, selected.id, argumentsById,
        policyMode === 'inherit' ? undefined : { mode: policyMode, max_restarts: policyMode === 'no' ? 0 : Number(maxRetries) })
      applyProcessSummary(summary, true)
      close(false)
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  const labelClass = 'mb-1.5 block text-[9px] font-semibold uppercase tracking-[.11em] text-[rgb(var(--muted-2))]'
  const inputClass = 'bonsai-focus w-full rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 py-2 text-[11px] outline-none'

  return (
    <div className="absolute inset-0 z-[85] grid place-items-center bg-black/60 p-6 backdrop-blur-[2px]">
      <form onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="start-process-title" className="w-full max-w-[720px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-[0_30px_100px_rgb(0_0_0/.65)]">
        <div className="flex h-12 items-center border-b border-[rgb(var(--border))] px-4">
          <Play size={15} className="mr-2 text-[rgb(var(--accent))]" />
          <div><div id="start-process-title" className="text-[12px] font-semibold">Start process</div><div className="text-[9px] text-[rgb(var(--muted-2))]">Choose the worktree, package and command.</div></div>
          <button type="button" disabled={pending} onClick={() => close(false)} aria-label="Close start process" className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"><X size={14} /></button>
        </div>
        <div className="max-h-[72vh] space-y-5 overflow-auto p-4">
          <label className="block"><span className={labelClass}>Worktree</span>
            <BonsaiSelect ariaLabel="Process worktree and branch" disabled={pending} searchable value={worktreeId} onChange={id => { if (id !== worktreeId) { setCatalog(null); setCommandId(''); setWorktreeId(id) } }} placeholder="Choose a worktree" options={availableWorktrees.map(tree => ({ value: tree.id, label: tree.branch, description: tree.path, meta: tree.tag }))} />
          </label>
          <section>
            <div className="mb-2 flex items-center justify-between"><span className={labelClass}>Packages</span><button type="button" disabled={!worktreeId || loading || pending} onClick={() => setReload(value => value + 1)} className="bonsai-focus rounded px-2 text-[9px] text-[rgb(var(--muted))] disabled:opacity-40">Refresh</button></div>
            {!worktreeId && <p className="text-[11px] text-[rgb(var(--muted))]">Choose a worktree to discover its packages and build tools.</p>}
            {loading && <p role="status" className="text-[11px] text-[rgb(var(--muted))]">Finding packages…</p>}
            {catalog && !providers.length && <p className="text-[11px] text-[rgb(var(--muted))]">No packages or build tools found in this worktree.</p>}
            <div className="grid grid-cols-2 gap-2">
              {providers.map(provider => {
                const id = JSON.stringify([provider.id, provider.root])
                const active = packageId === id
                const count = (catalog?.commands ?? []).filter(command => command.provider === provider.id && (command.project_root || command.invocation.working_dir) === provider.root).length
                return <button type="button" key={id} disabled={pending} aria-pressed={active} onClick={() => { setPackageId(id); setCommandId(''); setValues({}) }} className={'bonsai-focus min-w-0 rounded-lg border p-3 text-left transition-colors ' + (active ? 'border-[rgb(var(--accent)/.56)] bg-[rgb(var(--accent)/.10)]' : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] hover:border-[rgb(var(--border-strong))]')}>
                  <div className="flex items-center gap-2 text-[11px] font-medium"><Package size={13} className="shrink-0 text-[rgb(var(--accent))]" />{provider.name}<span className="ml-auto text-[9px] text-[rgb(var(--muted-2))]">{count} commands</span></div>
                  <div title={provider.root} className="mt-1.5 truncate font-mono text-[9px] text-[rgb(var(--muted-2))]">{scope(provider.root)}</div>
                </button>
              })}
            </div>
          </section>
          {packageId && <label className="block"><span className={labelClass}>Command</span><BonsaiSelect ariaLabel="Process command" disabled={pending || !commands.length} searchable value={commandId} onChange={id => { setCommandId(id); setValues({}) }} placeholder={commands.length ? 'Choose a command' : 'No commands found in this package'} options={commands.map(command => ({ value: command.id, label: command.name, description: command.raw || command.description, meta: command.provider }))} /></label>}
          {selected && <div className="rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-3"><div className={labelClass}>Run command</div><code className="block break-all text-[11px]">{preview ? [preview.program, ...(preview.args ?? [])].map(arg => /^[a-zA-Z0-9_./:=@-]+$/.test(arg) ? arg : JSON.stringify(arg)).join(' ') : previewError || 'Resolving command…'}</code><div className="mt-2 break-all text-[9px] text-[rgb(var(--muted-2))]">{selected.invocation.working_dir}</div></div>}
          {selected?.args?.map(arg => <label key={arg.id} className="block"><span className={labelClass}>{arg.name || arg.id}{arg.required ? ' *' : ''}</span>
            {arg.type === 'bool' ? <BonsaiSelect ariaLabel={arg.name || arg.id} disabled={pending} value={values[arg.id] ?? ''} onChange={value => setValues(current => ({ ...current, [arg.id]: value }))} options={[{ value: '', label: 'Use default' }, { value: 'true', label: 'Enabled' }, { value: 'false', label: 'Disabled' }]} />
              : arg.choices?.length ? <BonsaiSelect ariaLabel={arg.name || arg.id} disabled={pending} value={values[arg.id] ?? ''} onChange={value => setValues(current => ({ ...current, [arg.id]: value }))} placeholder={arg.default || 'Choose a value'} options={arg.choices.map(value => ({ value, label: value }))} />
                : arg.variadic || arg.kind === 'passthrough' ? <textarea aria-label={arg.name || arg.id} disabled={pending} value={values[arg.id] ?? ''} onChange={event => setValues(current => ({ ...current, [arg.id]: event.target.value }))} placeholder="One argument per line" rows={3} className={inputClass} />
                  : <input aria-label={arg.name || arg.id} required={arg.required && arg.default === undefined} disabled={pending} value={values[arg.id] ?? ''} onChange={event => setValues(current => ({ ...current, [arg.id]: event.target.value }))} placeholder={arg.default} className={inputClass} />}
            {arg.description && <span className="mt-1 block text-[9px] text-[rgb(var(--muted-2))]">{arg.description}</span>}
          </label>)}
          <label className="block"><span className={labelClass}>Restart policy</span>
            <BonsaiSelect ariaLabel="Restart policy" disabled={pending} value={policyMode} onChange={value => setPolicyMode(value as typeof policyMode)} options={[
              { value: 'inherit', label: 'Use project default', description: inherited ? `${inherited.mode} · ${inherited.max_restarts} additional attempts` : 'Choose a command to resolve the default' },
              { value: 'no', label: 'Never' }, { value: 'on-failure', label: 'On failure' }, { value: 'always', label: 'Always' },
            ]} />
            {policyMode === 'inherit' && inherited && <span className="mt-1 block text-[10px] text-[rgb(var(--muted))]">Project default: {inherited.mode} · maximum {inherited.max_restarts} retries</span>}
          </label>
          {effectiveMode && effectiveMode !== 'no' && <label className="block"><span className={labelClass}>Maximum retries</span>
            <input aria-label="Maximum retries" type="number" min={0} max={100} step={1} disabled={pending || policyMode === 'inherit'} value={policyMode === 'inherit' ? inherited?.max_restarts ?? 5 : maxRetries} onChange={event => setMaxRetries(event.target.value)} className={inputClass} />
            <span className="mt-1 block text-[9px] text-[rgb(var(--muted))]">Additional attempts after the initial launch (0–100). Zero disables retries.</span>
          </label>}
          {previewError && <p role="alert" className="text-[11px] text-[rgb(var(--red))]">{previewError}</p>}
          {catalog?.warnings?.map((warning, index) => <p key={index} className="text-[10px] text-[rgb(var(--orange))]">{warning.message}</p>)}
          {error && <p role="alert" className="text-[11px] text-[rgb(var(--red))]">{error}</p>}
        </div>
        <div className="flex items-center justify-between border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.45)] px-4 py-3">
          <div className="text-[9px] text-[rgb(var(--muted-2))]">{catalog ? `${providers.length} packages · ${catalog.commands?.length ?? 0} commands` : 'Run a package command in your worktree'}</div>
          <div className="flex gap-2"><button type="button" disabled={pending} onClick={() => close(false)} className="bonsai-focus rounded-md px-3 py-2 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">Cancel</button><button type="submit" disabled={!canStart} className="bonsai-focus rounded-md border border-[rgb(var(--accent)/.45)] bg-[rgb(var(--accent)/.16)] px-3 py-2 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-40">{pending ? 'Starting…' : 'Start process'}</button></div>
        </div>
      </form>
    </div>
  )
}
