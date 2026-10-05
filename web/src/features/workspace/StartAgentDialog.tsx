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
    <div className="absolute inset-0 z-[85] grid place-items-center bg-black/60 p-6 backdrop-blur-[2px]">
      <form
        onSubmit={submit}
        role="dialog" aria-modal="true" aria-labelledby="start-agent-title"
        className="w-full max-w-[720px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-[0_30px_100px_rgb(0_0_0/.65)]"
      >
        <div className="flex h-12 items-center border-b border-[rgb(var(--border))] px-4">
          <Bot size={15} className="mr-2 text-[rgb(var(--purple))]" />
          <div>
            <div id="start-agent-title" className="text-[12px] font-semibold">Start agent</div>
            <div className="text-[9px] text-[rgb(var(--muted-2))]">Choose the worktree, profile and task.</div>
          </div>
          <button
            type="button"
            disabled={pending}
            onClick={() => setOpen(false)}
            aria-label="Close start agent"
            className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
          >
            <X size={14} />
          </button>
        </div>

        <div className="max-h-[72vh] space-y-5 overflow-auto p-4">
          <label className="block">
            <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.11em] text-[rgb(var(--muted-2))]">Worktree</span>
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
              <span className="mt-1.5 block text-[9px] text-[rgb(var(--muted-2))]">
                Project-level launches require an explicit branch choice.
              </span>
            )}
          </label>

          <section>
            <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.11em] text-[rgb(var(--muted-2))]">Provider</div>
            <div className="grid grid-cols-3 gap-2">
              {['Antigravity', 'Claude', 'Codex'].map((label) => {
                const item = { id: label, label, connected: label === 'Antigravity' && !!available?.available, description: label === 'Antigravity' ? 'Launch with your saved profile.' : 'Not available yet' }
                const active = item.id === 'Antigravity'
                return (
                  <button
                    type="button"
                    key={item.id}
                    disabled={!item.connected}
                    aria-pressed={active}
                    className={
                      'bonsai-focus rounded-lg border p-3 text-left transition-colors disabled:opacity-40 ' +
                      (active
                        ? 'border-[rgb(var(--purple)/.56)] bg-[rgb(var(--purple)/.10)]'
                        : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] hover:border-[rgb(var(--border-strong))]')
                    }
                  >
                    <div className="flex items-center gap-2 text-[11px] font-medium">
                      <Sparkles size={13} className={active ? 'text-[rgb(var(--purple))]' : 'text-[rgb(var(--muted))]'} />
                      {item.label}
                      {item.connected ? (
                        <span className="ml-auto flex items-center gap-1 text-[8px] text-[rgb(var(--green))]"><span className="h-1.5 w-1.5 rounded-full bg-[rgb(var(--green))]" />connected</span>
                      ) : (
                        <span className="ml-auto text-[8px] text-[rgb(var(--muted-2))]">not connected</span>
                      )}
                    </div>
                    <div className="mt-1.5 text-[9px] leading-4 text-[rgb(var(--muted-2))]">{item.description}</div>
                  </button>
                )
              })}
            </div>
          </section>

          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Profile</span>
              <BonsaiSelect ariaLabel="Profile" disabled={pending || loading} value={accountId} placeholder={loading ? 'Loading profiles…' : 'Choose a profile'} onChange={id => { setAccountId(id); setFullAccess(accounts.find(account => account.id === id)?.full_access ?? false) }} options={accounts.map(account => ({ value: account.id, label: account.name }))} />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Model</span>
              <input aria-label="Agent model" disabled={pending} value={model} onChange={event => setModel(event.target.value)} placeholder="Use profile default" className="bonsai-focus h-9 w-full rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 text-[11px] outline-none" />
            </label>
          </div>
          <div className="flex items-center gap-3 rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-3">
            <Shield size={15} className={fullAccess ? 'text-[rgb(var(--orange))]' : 'text-[rgb(var(--muted))]'} />
            <div className="flex-1"><div className="text-[11px] font-medium">Full access</div><p className="mt-1 text-[9px] text-[rgb(var(--muted-2))]">Allow Antigravity to run tools without permission prompts.</p></div>
            <button type="button" role="switch" aria-label="Antigravity full access" aria-checked={fullAccess} disabled={pending} onClick={() => setFullAccess(value => !value)} className={'bonsai-focus flex h-5 w-9 items-center rounded-full border p-0.5 transition-colors ' + (fullAccess ? 'border-[rgb(var(--orange)/.5)] bg-[rgb(var(--orange)/.3)]' : 'border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))]')}><span className={'h-3.5 w-3.5 rounded-full bg-[rgb(var(--text))] transition-transform ' + (fullAccess ? 'translate-x-4' : '')} /></button>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Agent name</span>
              <input
                aria-label="Agent name"
                maxLength={128}
                disabled={pending}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Login refactor"
                className="bonsai-focus h-9 w-full rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 text-[11px] outline-none"
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Work type</span>
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
            <span className="mb-1.5 flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">
              <Gauge size={10} /> Prompt
            </span>
            <textarea
              aria-label="Prompt"
              disabled={pending}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="Describe the exact work this agent should execute…"
              rows={6}
              className="bonsai-focus w-full resize-y rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3 py-2.5 text-[11px] leading-5 outline-none"
            />
          </label>
          {!loading && !accounts.length && !error && <p className="text-[10px] text-[rgb(var(--muted))]">Set up a profile with <code>bonsai agent account add antigravity &lt;name&gt;</code></p>}
          {available && !available.available && <p className="text-[10px] text-[rgb(var(--muted))]">{available.unavailable_reason?.message}</p>}
          {error && <p role="alert" className="text-[11px] text-[rgb(var(--red))]">{error}</p>}
        </div>

        <div className="flex items-center justify-between border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.45)] px-4 py-3">
          <div className="text-[9px] text-[rgb(var(--muted-2))]">
            Antigravity · {fullAccess ? 'Full access' : 'Ask permissions'}
          </div>
          <div className="flex gap-2">
            <button type="button" disabled={pending} onClick={() => setOpen(false)} className="bonsai-focus rounded-md px-3 py-2 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canStart}
              className="bonsai-focus rounded-md border border-[rgb(var(--purple)/.45)] bg-[rgb(var(--purple)/.16)] px-3 py-2 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-40"
            >
              {pending ? 'Starting…' : 'Start agent'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
