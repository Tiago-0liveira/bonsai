import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { Boxes, FolderTree, GitBranch, Package, Play, RefreshCw, SquareTerminal, Wrench } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { ChoiceChips } from '../../components/ui/ChoiceChips'
import { DialogFrame, Field, Reveal, TEXT_INPUT } from '../../components/ui/Dialog'
import { NumberStepper } from '../../components/ui/NumberStepper'
import { OptionList } from '../../components/ui/OptionList'
import { Segmented } from '../../components/ui/Segmented'
import { processCommands, startProcess, previewProcess, startCustomProcess, previewCustomProcess, applyProcessSummary, type ProcessCatalog, type ProcessPolicy } from '../../api/processes'
import { useBonsaiStore } from '../../stores/bonsai'

type Mode = 'package' | 'custom'
type Preview = Awaited<ReturnType<typeof previewProcess>>

const POLICY_OPTIONS = [
  { value: 'inherit', label: 'Project default' },
  { value: 'no', label: 'Never' },
  { value: 'on-failure', label: 'On failure' },
  { value: 'always', label: 'Always' },
]
const SAFE_WORD = /^[a-zA-Z0-9_./:=@-]+$/
const quoted = (value: string) => (SAFE_WORD.test(value) ? value : JSON.stringify(value))

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
  const [mode, setMode] = useState<Mode>('package')
  const [catalog, setCatalog] = useState<ProcessCatalog | null>(null)
  const [packageId, setPackageId] = useState('')
  const [commandId, setCommandId] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const [customCommand, setCustomCommand] = useState('')
  const [customName, setCustomName] = useState('')
  const [customDir, setCustomDir] = useState('')
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [reload, setReload] = useState(0)
  const busy = useRef(false)
  const [policyMode, setPolicyMode] = useState<'inherit' | ProcessPolicy['mode']>('inherit')
  const [maxRetries, setMaxRetries] = useState('5')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [customPreview, setCustomPreview] = useState<Preview | null>(null)
  const [customError, setCustomError] = useState('')
  const commandInputId = useId()
  const nameInputId = useId()
  const dirInputId = useId()

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

  const providers = catalogPackages(catalog)
  const commands = (catalog?.commands ?? []).filter(command => JSON.stringify([command.provider, command.project_root || command.invocation.working_dir]) === packageId)
  const selected = commands.find(command => command.id === commandId)
  const argumentsById: Record<string, string[]> = {}
  for (const arg of selected?.args ?? []) {
    const value = values[arg.id]
    if (value !== undefined && value !== '') argumentsById[arg.id] = arg.variadic || arg.kind === 'passthrough' ? value.split('\n').filter(Boolean) : [value]
  }
  const serializedArguments = JSON.stringify(argumentsById)
  const previewCommandId = mode === 'package' ? commandId : ''
  useEffect(() => {
    let disposed = false
    setPreview(null); setPreviewError('')
    if (!previewCommandId) return
    const timer = setTimeout(() => {
      void previewProcess(projectId, worktreeId, previewCommandId, JSON.parse(serializedArguments)).then(value => {
        if (!disposed) setPreview(value)
      }).catch(error => { if (!disposed) setPreviewError(error instanceof Error ? error.message : String(error)) })
    }, 150)
    return () => { disposed = true; clearTimeout(timer) }
  }, [projectId, worktreeId, previewCommandId, serializedArguments])

  const typed = customCommand.trim()
  const dir = customDir.trim()
  const custom = { command: typed, name: customName.trim() || undefined, dir: dir && dir !== '.' ? dir : undefined }
  const serializedCustom = JSON.stringify(custom)
  const customActive = mode === 'custom' && !!typed
  useEffect(() => {
    let disposed = false
    setCustomPreview(null); setCustomError('')
    if (!customActive || !worktreeId) return
    const timer = setTimeout(() => {
      void previewCustomProcess(projectId, worktreeId, JSON.parse(serializedCustom)).then(value => {
        if (!disposed) setCustomPreview(value)
      }).catch(error => { if (!disposed) setCustomError(error instanceof Error ? error.message : String(error)) })
    }, 250)
    return () => { disposed = true; clearTimeout(timer) }
  }, [projectId, worktreeId, customActive, serializedCustom])

  const inherited = mode === 'custom' ? customPreview?.default_policy : preview?.default_policy ?? catalog?.default_policies?.[commandId]
  const effectiveMode = policyMode === 'inherit' ? inherited?.mode : policyMode
  const validRetries = /^\d+$/.test(maxRetries) && Number(maxRetries) <= 100
  const policyOk = policyMode === 'inherit' || policyMode === 'no' || validRetries
  const worktreeOk = availableWorktrees.some(tree => tree.id === worktreeId)
  const canStart = !pending && policyOk && worktreeOk && (mode === 'package' ? !loading && !!selected && !!preview : !!typed && !customError)
  const scope = (root: string) => root === catalog?.location.input_dir ? '.' : root.startsWith((catalog?.location.input_dir ?? '') + '/') ? root.slice(catalog!.location.input_dir.length + 1) : root
  const policyOverride = policyMode === 'inherit' ? undefined : { mode: policyMode, max_restarts: policyMode === 'no' ? 0 : Number(maxRetries) }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canStart || busy.current) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      const summary = mode === 'package' && selected
        ? await startProcess(projectId, worktreeId, selected.id, argumentsById, policyOverride)
        : await startCustomProcess(projectId, worktreeId, custom, policyOverride)
      applyProcessSummary(summary, true)
      close(false)
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  const commandLine = mode === 'package'
    ? preview ? [preview.program, ...(preview.args ?? [])].map(quoted).join(' ') : previewError || 'Resolving command…'
    : typed
  const runDir = mode === 'package' ? selected?.invocation.working_dir : customPreview?.dir
  const policyText = !effectiveMode ? '' : effectiveMode === 'no' ? 'no restart' : `${effectiveMode} · up to ${policyMode === 'inherit' ? inherited?.max_restarts : maxRetries} retries`
  const summary = mode === 'custom' && typed
    ? [typed, policyText].filter(Boolean).join(' · ')
    : mode === 'package' && selected
      ? [preview ? commandLine : selected.name, policyText].filter(Boolean).join(' · ')
      : catalog ? `${providers.length} packages · ${catalog.commands?.length ?? 0} commands` : 'Run a package command in your worktree'
  const setArg = (id: string, value: string) => setValues(current => ({ ...current, [id]: value }))
  const dirChoices = Array.from(new Set(['.', ...providers.map(provider => scope(provider.root))].filter(item => !item.startsWith('/'))))

  return (
    <DialogFrame
      icon={Play}
      title="Start process"
      subtitle="Run a package command, or your own, in a worktree."
      closeLabel="Close start process"
      onClose={() => close(false)}
      closeDisabled={pending}
      width="lg"
      onSubmit={submit}
      summary={summary}
      actions={
        <>
          <button type="button" disabled={pending} onClick={() => close(false)} className="bonsai-focus btn-bordered h-9 rounded-[8px] px-3.5 text-[12px] text-muted hover:text-text">Cancel</button>
          <button type="submit" disabled={!canStart} className="bonsai-focus btn-primary inline-flex h-9 items-center gap-1.5 px-4 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">
            <Play size={12} aria-hidden="true" />
            {pending ? 'Starting…' : 'Start process'}
          </button>
        </>
      }
    >
      <div className="space-y-5">
        <Field label="Worktree">
          <BonsaiSelect
            ariaLabel="Process worktree and branch"
            disabled={pending}
            searchable
            value={worktreeId}
            onChange={id => { if (id !== worktreeId) { setCatalog(null); setCommandId(''); setWorktreeId(id) } }}
            placeholder="Choose a worktree"
            options={availableWorktrees.map(tree => ({ value: tree.id, label: tree.branch, description: tree.path, icon: <GitBranch size={13} className="text-muted" /> }))}
          />
        </Field>

        <Field label="What to run">
          <Segmented
            ariaLabel="What to run"
            disabled={pending}
            value={mode}
            onChange={value => setMode(value as Mode)}
            options={[
              { value: 'package', title: 'Run a command discovered in the worktree', label: <><Boxes size={13} className={mode === 'package' ? 'text-accent' : 'text-muted'} /><span>Package command</span></> },
              { value: 'custom', title: 'Type any command line', label: <><SquareTerminal size={13} className={mode === 'custom' ? 'text-accent' : 'text-muted'} /><span>Custom command</span></> },
            ]}
          />
        </Field>

        <div key={mode} className="dialog-swap space-y-5">
          {mode === 'package' ? (
            <div className="dialog-cols dialog-cols-wide-left">
              <Field
                label="Packages"
                aside={
                  <button type="button" disabled={!worktreeId || loading || pending} onClick={() => setReload(value => value + 1)} className="bonsai-focus inline-flex h-5 items-center gap-1 rounded-[5px] px-1.5 font-mono text-[10px] text-muted transition-colors hover:text-text disabled:opacity-40">
                    <RefreshCw size={10} className={loading ? 'dialog-spin' : ''} aria-hidden="true" />Refresh
                  </button>
                }
              >
                {!worktreeId && <p className="text-[11px] text-muted">Choose a worktree to discover its packages and build tools.</p>}
                {loading && <div role="status" className="space-y-2"><div className="skeleton h-[58px]" /><div className="skeleton h-[58px]" /><span className="sr-only">Finding packages…</span></div>}
                {catalog && !providers.length && <p className="rounded-[10px] border border-dashed border-border px-3 py-5 text-center text-[11px] text-muted">No packages or build tools found in this worktree.</p>}
                <div className="space-y-2">
                  {providers.map(provider => {
                    const id = JSON.stringify([provider.id, provider.root])
                    const active = packageId === id
                    const count = (catalog?.commands ?? []).filter(command => command.provider === provider.id && (command.project_root || command.invocation.working_dir) === provider.root).length
                    return (
                      <button
                        type="button"
                        key={id}
                        disabled={pending}
                        aria-pressed={active}
                        data-selected={active}
                        onClick={() => { setPackageId(id); setCommandId(''); setValues({}) }}
                        className="profile-card bonsai-focus disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-[8px] border border-border bg-panel-2"><Package size={15} className={active ? 'text-accent' : 'text-muted'} /></span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-[12.5px] font-medium text-text">{provider.name}</span>
                          <span title={provider.root} className="mt-0.5 flex items-center gap-1 font-mono text-[10px] text-muted-2"><FolderTree size={10} className="shrink-0" aria-hidden="true" /><span className="truncate">{scope(provider.root)}</span></span>
                        </span>
                        <span className="shrink-0 font-mono text-[10px] text-muted-2">{count} commands</span>
                      </button>
                    )
                  })}
                </div>
              </Field>
              <Field label="Command" aside={packageId && <span className="font-mono text-[10px] text-muted-2">{commands.length} in this package</span>}>
                {packageId ? (
                  <OptionList
                    searchable
                    mono
                    ariaLabel="Process command"
                    filterLabel="Filter commands"
                    placeholder="Filter commands…"
                    emptyText="No commands found in this package."
                    disabled={pending || !commands.length}
                    options={commands.map(command => ({ value: command.id, label: command.name, description: command.raw || command.description }))}
                    value={commandId}
                    onChange={id => { setCommandId(id); setValues({}) }}
                  />
                ) : (
                  <div className="grid h-[262px] place-items-center rounded-[10px] border border-dashed border-border bg-well/40 px-4 text-center">
                    <span className="flex flex-col items-center gap-2 font-mono text-[10.5px] text-muted-2"><Wrench size={16} aria-hidden="true" />Choose a package to list its commands.</span>
                  </div>
                )}
              </Field>
            </div>
          ) : (
            <>
              <Field label="Command" htmlFor={commandInputId} hint="Runs through your shell, so pipes, && and environment assignments work.">
                <div className="relative">
                  <span aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 font-mono text-[13px] text-accent">$</span>
                  <input
                    id={commandInputId}
                    autoFocus
                    spellCheck={false}
                    autoComplete="off"
                    disabled={pending}
                    value={customCommand}
                    onChange={event => setCustomCommand(event.target.value)}
                    placeholder="pnpm dev --port 4000"
                    className={TEXT_INPUT + ' h-10 pl-7 font-mono text-[13px]'}
                  />
                </div>
              </Field>
              <div className="dialog-cols">
                <Field label="Name" htmlFor={nameInputId} hint="Shown on the canvas. Defaults to the command.">
                  <input id={nameInputId} disabled={pending} maxLength={128} value={customName} onChange={event => setCustomName(event.target.value)} placeholder="e.g. Dev server" className={TEXT_INPUT} />
                </Field>
                <Field label="Directory" htmlFor={dirInputId} hint="Relative to the worktree root.">
                  <input id={dirInputId} disabled={pending} spellCheck={false} value={customDir} onChange={event => setCustomDir(event.target.value)} placeholder="." className={TEXT_INPUT + ' font-mono'} />
                  {dirChoices.length > 1 && (
                    <div className="mt-2">
                      <ChoiceChips
                        ariaLabel="Directory suggestions"
                        disabled={pending}
                        value={dir || '.'}
                        onChange={value => setCustomDir(value === '.' ? '' : value)}
                        options={dirChoices.map(item => ({ value: item, label: item === '.' ? 'worktree root' : item }))}
                      />
                    </div>
                  )}
                </Field>
              </div>
              {customError && <p role="alert" className="text-[11px] text-danger">{customError}</p>}
            </>
          )}

          {(mode === 'custom' ? !!typed : !!selected) && (
            <Reveal>
              <div className="rounded-[10px] border border-border bg-well px-3.5 py-3">
                <div className="mb-1.5 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Run command</div>
                <div className="flex items-start gap-2 font-mono">
                  <span aria-hidden="true" className="text-[12px] text-accent">$</span>
                  <code className="min-w-0 break-all text-[12px] text-text">{commandLine}</code>
                </div>
                {runDir && <div className="mt-1.5 flex items-center gap-1.5 break-all font-mono text-[10px] text-muted-2"><FolderTree size={10} className="shrink-0" aria-hidden="true" />{runDir}</div>}
              </div>
            </Reveal>
          )}

          {mode === 'package' && !!selected?.args?.length && (
            <div className="dialog-cols">
              {selected.args.map(arg => {
                const label = arg.name || arg.id
                const wide = arg.variadic || arg.kind === 'passthrough'
                return (
                  <div key={arg.id} className={wide ? 'col-span-full' : undefined}>
                    <Field label={<>{label}{arg.required ? ' *' : ''}</>} hint={arg.description}>
                      {arg.type === 'bool' ? (
                        <ChoiceChips ariaLabel={label} disabled={pending} value={values[arg.id] ?? ''} onChange={value => setArg(arg.id, value)} options={[{ value: '', label: 'Use default' }, { value: 'true', label: 'Enabled' }, { value: 'false', label: 'Disabled' }]} />
                      ) : arg.choices?.length ? (
                        <ChoiceChips ariaLabel={label} disabled={pending} value={values[arg.id] ?? ''} onChange={value => setArg(arg.id, value)} options={[{ value: '', label: arg.default ? `Default (${arg.default})` : 'None' }, ...arg.choices.map(value => ({ value, label: value }))]} />
                      ) : wide ? (
                        <textarea aria-label={label} disabled={pending} value={values[arg.id] ?? ''} onChange={event => setArg(arg.id, event.target.value)} placeholder="One argument per line" rows={3} className="bonsai-focus w-full rounded-[8px] border border-border bg-well px-3 py-2 font-mono text-[12px] outline-none transition-colors placeholder:text-muted-2 hover:border-border-strong focus:border-accent/55" />
                      ) : (
                        <input aria-label={label} required={arg.required && arg.default === undefined} disabled={pending} value={values[arg.id] ?? ''} onChange={event => setArg(arg.id, event.target.value)} placeholder={arg.default} className={TEXT_INPUT} />
                      )}
                    </Field>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        <div className="flex flex-wrap items-start gap-x-6 gap-y-4 border-t border-border-subtle pt-5">
          <div className="min-w-[320px] flex-1">
            <Field label="Restart policy" hint={policyMode === 'inherit' ? (inherited ? `Project default: ${inherited.mode} · maximum ${inherited.max_restarts} retries` : mode === 'custom' ? 'Type a command to resolve the default.' : 'Choose a command to resolve the default.') : undefined}>
              <Segmented ariaLabel="Restart policy" disabled={pending} value={policyMode} onChange={value => setPolicyMode(value as typeof policyMode)} options={POLICY_OPTIONS} itemClassName="h-8 text-[11.5px]" />
            </Field>
          </div>
          {effectiveMode && effectiveMode !== 'no' && (
            <div className="dialog-enter">
              <Field label="Maximum retries" hint="Extra attempts, 0–100.">
                <NumberStepper
                  ariaLabel="Maximum retries"
                  disabled={pending}
                  readOnly={policyMode === 'inherit'}
                  value={policyMode === 'inherit' ? String(inherited?.max_restarts ?? 5) : maxRetries}
                  onChange={setMaxRetries}
                />
              </Field>
            </div>
          )}
        </div>

        {previewError && mode === 'package' && <p role="alert" className="text-[11px] text-danger">{previewError}</p>}
        {mode === 'package' && catalog?.warnings?.map((warning, index) => <p key={index} className="text-[10px] text-warn">{warning.message}</p>)}
        {error && <p role="alert" className="text-[11px] text-danger">{error}</p>}
      </div>
    </DialogFrame>
  )
}
