import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Bot, Gauge, Shield, Sparkles, X } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { agentAccounts, agentProviders, type AgentAccount, type AgentCapability } from '../../api/agents'
import { useBonsaiStore } from '../../stores/bonsai'

export function StartAgentDialog() {
  const open = useBonsaiStore(state => state.startAgentDialogOpen)
  return open ? <StartAgentForm /> : null
}

function StartAgentForm() {
  const setOpen = useBonsaiStore((state) => state.setStartAgentDialogOpen)
  const targetWorktreeId = useBonsaiStore((state) => state.startAgentTargetWorktreeId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const createAgent = useBonsaiStore((state) => state.createAgent)

  const availableWorktrees = worktrees.filter(item => item.projectId === activeProjectId && !item.missing)
  const [worktreeId, setWorktreeId] = useState(targetWorktreeId)
  const [accounts, setAccounts] = useState<AgentAccount[]>([])
  const [providers, setProviders] = useState<AgentCapability[]>([])
  const [accountId, setAccountId] = useState('')
  const [model, setModel] = useState('')
  const [fullAccess, setFullAccess] = useState(false)
  const [name, setName] = useState('')
  const [workType, setWorkType] = useState('Implementation')
  const [prompt, setPrompt] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const request = useRef({ fingerprint: '', key: '' })
  useEffect(() => {
    let disposed = false
    Promise.all([agentAccounts(), agentProviders()]).then(([accounts, providers]) => {
      if (disposed) return
      setAccounts(accounts)
      setProviders(providers)
      if (accounts.length === 1) {
        setAccountId(accounts[0].id)
        setFullAccess(accounts[0].full_access ?? false)
      }
    }).catch(error => { if (!disposed) setError(String(error.message)) })
      .finally(() => { if (!disposed) setLoading(false) })
    return () => { disposed = true }
  }, [])
  const available = providers.find(item => item.id === 'antigravity')
  const canStart = !loading && !pending && available?.available && accountId && availableWorktrees.some(item => item.id === worktreeId)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (busy.current || !canStart) return
    busy.current = true
    setPending(true)
    setError('')
    const fingerprint = JSON.stringify({ worktreeId, accountId, name, model, fullAccess, workType, prompt })
    if (request.current.fingerprint !== fingerprint) request.current = { fingerprint, key: crypto.randomUUID() }
    try {
      await createAgent({ worktreeId, accountId, requestKey: request.current.key, name: name.trim(), provider: 'Antigravity', model: model.trim(), reasoningEffort: '', fastMode: false, fullAccess, workType, prompt: prompt.trim() })
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  return (
    <div className="absolute inset-0 z-[85] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <form
        onSubmit={submit}
        role="dialog" aria-modal="true" aria-labelledby="start-agent-title"
        className="w-full max-w-[720px] overflow-hidden rounded-[14px] border border-border-strong bg-panel shadow-overlay"
      >
        <div className="flex h-11 items-center border-b border-border-subtle px-4">
          <Bot size={15} className="mr-2 text-accent" />
          <div>
            <div id="start-agent-title" className="text-[13px] font-semibold">Start agent</div>
            <div className="font-mono text-[10px] text-muted-2">Choose the worktree, profile and task.</div>
          </div>
          <button
            type="button"
            disabled={pending}
            onClick={() => setOpen(false)}
            aria-label="Close start agent"
            className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-panel-3 hover:text-text"
          >
            <X size={14} />
          </button>
        </div>

        <div className="max-h-[72vh] space-y-5 overflow-auto p-4">
          <label className="block">
            <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Worktree</span>
            <BonsaiSelect
              ariaLabel="Worktree and branch"
              disabled={pending}
              searchable
              value={worktreeId}
              onChange={setWorktreeId}
              placeholder="Choose a worktree"
              options={availableWorktrees.map((item) => ({
                value: item.id,
                label: item.branch,
                description: item.mergeTargetBranch ? 'merges into ' + item.mergeTargetBranch : item.path,
                meta: item.tag,
              }))}
            />
            {!targetWorktreeId && (
              <span className="mt-1.5 block font-mono text-[10px] text-muted-2">
                Project-level launches require an explicit branch choice.
              </span>
            )}
          </label>

          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Provider</div>
            <div className="grid grid-cols-3 gap-1 rounded-[9px] border border-border bg-well p-1">
              {['Antigravity', 'Claude', 'Codex'].map((label) => {
                const item = { id: label, label, connected: label === 'Antigravity' && !!available?.available, description: label === 'Antigravity' ? 'Launch with your saved profile.' : 'Not available yet' }
                const active = item.id === 'Antigravity'
                return (
                  <button
                    type="button"
                    key={item.id}
                    disabled={!item.connected}
                    aria-pressed={active}
                    title={item.description}
                    className={'bonsai-focus flex h-[30px] items-center justify-center gap-2 rounded-[7px] text-[12px] transition-colors disabled:opacity-40 ' + (active ? 'bg-panel-3 text-text' : 'text-muted hover:text-text')}
                  >
                    <Sparkles size={13} className={active ? 'text-accent' : 'text-muted'} />
                    <span>{item.label}</span>
                    {item.connected ? (
                      <span className="flex items-center gap-1 font-mono text-[10px] text-accent"><span className="h-1.5 w-1.5 rounded-full bg-accent-solid" />connected</span>
                    ) : (
                      <span className="font-mono text-[10px] text-muted-2">not connected</span>
                    )}
                    <span className="sr-only">{item.description}</span>
                  </button>
                )
              })}
            </div>
          </section>

          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Profile</span>
              <BonsaiSelect ariaLabel="Profile" disabled={pending || loading} value={accountId} placeholder={loading ? 'Loading profiles…' : 'Choose a profile'} onChange={id => { setAccountId(id); setFullAccess(accounts.find(account => account.id === id)?.full_access ?? false) }} options={accounts.map(account => ({ value: account.id, label: account.name }))} />
            </label>
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Model</span>
              <input aria-label="Agent model" disabled={pending} value={model} onChange={event => setModel(event.target.value)} placeholder="Use profile default" className="bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55" />
            </label>
          </div>
          <div className="flex items-center gap-3 rounded-[10px] border border-border bg-well p-3">
            <Shield size={15} className={fullAccess ? 'text-warn' : 'text-muted'} />
            <div className="flex-1"><div className="text-[12px] font-medium">Full access</div><p className="mt-1 font-mono text-[10px] text-muted-2">Allow Antigravity to run tools without permission prompts.</p></div>
            <button type="button" role="switch" aria-label="Antigravity full access" aria-checked={fullAccess} disabled={pending} onClick={() => setFullAccess(value => !value)} className={'bonsai-focus flex h-5 w-9 items-center rounded-full border p-0.5 transition-colors ' + (fullAccess ? 'border-warn-solid/22 bg-warn-solid/30' : 'border-border-strong bg-panel-3')}><span className={'h-3.5 w-3.5 rounded-full bg-text transition-transform ' + (fullAccess ? 'translate-x-4' : '')} /></button>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Agent name</span>
              <input
                aria-label="Agent name"
                maxLength={128}
                disabled={pending}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Login refactor"
                className="bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55"
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Work type</span>
              <BonsaiSelect
                ariaLabel="Agent work type"
                disabled={pending}
                value={workType}
                onChange={setWorkType}
                options={['Implementation', 'Debugging', 'Testing', 'Review', 'Research', 'Refactor', 'Documentation', 'Release'].map((item) => ({
                  value: item,
                  label: item,
                }))}
              />
            </label>
          </div>

          <label className="block">
            <span className="mb-1.5 flex items-center gap-1.5 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">
              <Gauge size={10} /> Prompt
            </span>
            <textarea
              aria-label="Prompt"
              disabled={pending}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="Describe the exact work this agent should execute…"
              rows={6}
              className="bonsai-focus w-full resize-y rounded-[7px] border border-border bg-well px-3 py-2.5 text-[12px] leading-5 outline-none placeholder:text-muted-2 focus:border-accent/55"
            />
          </label>
          {!loading && !accounts.length && !error && <p className="text-[10px] text-muted">Set up a profile with <code>bonsai agent account add antigravity &lt;name&gt;</code></p>}
          {available && !available.available && <p className="text-[10px] text-muted">{available.unavailable_reason?.message}</p>}
          {error && <p role="alert" className="text-[11px] text-danger">{error}</p>}
        </div>

        <div className="flex items-center justify-between border-t border-border-subtle bg-well/45 px-4 py-3">
          <div className="font-mono text-[10px] text-muted-2">
            Antigravity · {fullAccess ? 'Full access' : 'Ask permissions'}
          </div>
          <div className="flex gap-2">
            <button type="button" disabled={pending} onClick={() => setOpen(false)} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canStart}
              className="bonsai-focus btn-primary h-8 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40"
            >
              {pending ? 'Starting…' : 'Start agent'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
