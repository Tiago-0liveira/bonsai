import { useAgentSettings, useRunningAgents, willStopNotice } from './agentSettings'

// AgentSettings decides whether `bonsai web stop` ends agent sessions. Kept
// running, they stay reachable with `bonsai agents attach` and reappear here
// when Bonsai web starts again.
export function AgentSettings() {
  const { settings, saving, error, save } = useAgentSettings()
  const running = Math.max(useRunningAgents(), settings?.active_sessions ?? 0)
  if (!settings) return null
  return <section className="mx-auto w-full max-w-2xl space-y-3 border-b border-border-subtle p-6" aria-labelledby="agent-settings-title">
    <h2 id="agent-settings-title" className="text-[13px] font-semibold">Agents</h2>
    <label className="flex items-center gap-2 text-[12px]">
      <input type="checkbox" checked={settings.keep_running} disabled={saving} onChange={event => { void save(event.target.checked) }} />
      Keep agents running when Bonsai web stops
    </label>
    <p className="text-xs text-muted">
      Running agents stay available from any terminal with <code className="font-mono">bonsai agents attach &lt;id&gt;</code> (<code className="font-mono">bonsai agents</code> lists them), and reappear here when Bonsai web starts again. Restarting Bonsai web never stops them.
    </p>
    {!settings.keep_running && running > 0 && <p role="status" className="text-xs text-warn">{willStopNotice(running)}</p>}
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
  </section>
}
