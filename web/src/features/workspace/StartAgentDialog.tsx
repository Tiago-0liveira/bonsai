import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Bot, Check, Gauge, Sparkles, X, Zap } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { agentProviders } from '../../mock/agentProviders'
import { useBonsaiStore } from '../../stores/bonsai'
import type { AgentProvider } from '../../types'

export function StartAgentDialog() {
  const open = useBonsaiStore((state) => state.startAgentDialogOpen)
  const setOpen = useBonsaiStore((state) => state.setStartAgentDialogOpen)
  const targetWorktreeId = useBonsaiStore((state) => state.startAgentTargetWorktreeId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const projects = useBonsaiStore((state) => state.projects)
  const createAgent = useBonsaiStore((state) => state.createAgent)

  const project = projects.find((item) => item.id === activeProjectId)
  const availableWorktrees = worktrees.filter(
    (item) => item.projectId === activeProjectId && item.branch !== project?.defaultBranch,
  )

  const [worktreeId, setWorktreeId] = useState('')
  const [provider, setProvider] = useState<AgentProvider>('Codex')
  const [model, setModel] = useState('')
  const [reasoning, setReasoning] = useState('')
  const [fastMode, setFastMode] = useState(false)
  const [name, setName] = useState('')
  const [workType, setWorkType] = useState('Implementation')
  const [prompt, setPrompt] = useState('')

  const providerConfig = useMemo(
    () => agentProviders.find((item) => item.id === provider) ?? agentProviders[0],
    [provider],
  )
  const modelConfig = providerConfig.models.find((item) => item.id === model) ?? providerConfig.models[0]

  useEffect(() => {
    if (!open) return
    const nextProvider = agentProviders.find((item) => item.connected) ?? agentProviders[0]
    const nextModel = nextProvider.models[0]
    setWorktreeId(targetWorktreeId)
    setProvider(nextProvider.id)
    setModel(nextModel.id)
    setReasoning(nextModel.reasoning[0])
    setFastMode(false)
    setName('')
    setWorkType('Implementation')
    setPrompt('')
  }, [open, targetWorktreeId])

  useEffect(() => {
    const nextModel = providerConfig.models[0]
    if (!providerConfig.models.some((item) => item.id === model)) {
      setModel(nextModel.id)
      setReasoning(nextModel.reasoning[0])
      setFastMode(false)
    }
  }, [model, providerConfig])

  useEffect(() => {
    if (!modelConfig.reasoning.includes(reasoning)) setReasoning(modelConfig.reasoning[0])
    if (!modelConfig.supportsFast && fastMode) setFastMode(false)
  }, [fastMode, modelConfig, reasoning])

  if (!open) return null

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!worktreeId || !prompt.trim()) return
    createAgent({
      worktreeId,
      name: name.trim() || provider + ' ' + workType.toLowerCase(),
      provider,
      model,
      reasoningEffort: reasoning,
      fastMode,
      workType,
      prompt: prompt.trim(),
    })
  }

  return (
    <div className="absolute inset-0 z-[85] grid place-items-center bg-black/60 p-6 backdrop-blur-[2px]">
      <form
        onSubmit={submit}
        className="w-full max-w-[720px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-[0_30px_100px_rgb(0_0_0/.65)]"
      >
        <div className="flex h-12 items-center border-b border-[rgb(var(--border))] px-4">
          <Bot size={15} className="mr-2 text-[rgb(var(--purple))]" />
          <div>
            <div className="text-[12px] font-semibold">Start agent</div>
            <div className="text-[9px] text-[rgb(var(--muted-2))]">Choose the exact worktree, provider, model and task.</div>
          </div>
          <button
            type="button"
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
              ariaLabel="Agent worktree"
              searchable
              value={worktreeId}
              onChange={setWorktreeId}
              placeholder="Choose a worktree"
              options={availableWorktrees.map((item) => ({
                value: item.id,
                label: item.branch,
                description: 'merges into ' + item.mergeTargetBranch,
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
              {agentProviders.map((item) => {
                const active = item.id === provider
                return (
                  <button
                    type="button"
                    key={item.id}
                    disabled={!item.connected}
                    onClick={() => setProvider(item.id)}
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

          <div className="grid grid-cols-[1.2fr_1fr_auto] gap-3">
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Model</span>
              <BonsaiSelect
                ariaLabel="Agent model"
                value={model}
                onChange={setModel}
                options={providerConfig.models.map((item) => ({
                  value: item.id,
                  label: item.label,
                  meta: item.supportsFast ? 'Fast' : undefined,
                }))}
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Reasoning</span>
              <BonsaiSelect
                ariaLabel="Reasoning effort"
                value={reasoning}
                onChange={setReasoning}
                options={modelConfig.reasoning.map((item) => ({ value: item, label: item }))}
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Mode</span>
              <button
                type="button"
                disabled={!modelConfig.supportsFast}
                onClick={() => setFastMode((value) => !value)}
                className={
                  'bonsai-focus flex h-9 min-w-[86px] items-center justify-center gap-1.5 rounded-md border px-2 text-[10px] disabled:cursor-not-allowed disabled:opacity-35 ' +
                  (fastMode
                    ? 'border-[rgb(var(--orange)/.5)] bg-[rgb(var(--orange)/.11)] text-[rgb(var(--orange))]'
                    : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--muted))]')
                }
              >
                <Zap size={11} /> Fast
                {fastMode && <Check size={10} />}
              </button>
            </label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block text-[9px] font-semibold uppercase tracking-[.1em] text-[rgb(var(--muted-2))]">Agent name</span>
              <input
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
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="Describe the exact work this agent should execute…"
              rows={6}
              className="bonsai-focus w-full resize-y rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3 py-2.5 text-[11px] leading-5 outline-none"
            />
          </label>
        </div>

        <div className="flex items-center justify-between border-t border-[rgb(var(--border))] bg-[rgb(var(--bg)/.45)] px-4 py-3">
          <div className="text-[9px] text-[rgb(var(--muted-2))]">
            {providerConfig.label} · {modelConfig.label} · {reasoning}{fastMode ? ' · Fast' : ''}
          </div>
          <div className="flex gap-2">
            <button type="button" onClick={() => setOpen(false)} className="bonsai-focus rounded-md px-3 py-2 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">
              Cancel
            </button>
            <button
              type="submit"
              disabled={!worktreeId || !prompt.trim()}
              className="bonsai-focus rounded-md border border-[rgb(var(--purple)/.45)] bg-[rgb(var(--purple)/.16)] px-3 py-2 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-40"
            >
              Start agent
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
