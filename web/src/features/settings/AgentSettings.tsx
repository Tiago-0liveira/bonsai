import type { AgentNotificationSettings } from '../../api/settings'
import { BrowserNotificationSettings } from './BrowserNotificationSettings'
import { useAgentSettings, useRunningAgents, willStopNotice } from './useAgentSettings'

const NOTIFICATION_TYPES: [Exclude<keyof AgentNotificationSettings, 'enabled'>, string][] = [
  ['needs_input', 'When an agent needs input'],
  ['turn_complete', 'When an agent finishes a turn'],
]

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
      <input type="checkbox" checked={settings.keep_running} disabled={saving} onChange={event => { void save({ keep_running: event.target.checked }) }} />
      Keep agents running when Bonsai web stops
    </label>
    <p className="text-xs text-muted">
      Running agents stay available from any terminal with <code className="font-mono">bonsai agents attach &lt;id&gt;</code> (<code className="font-mono">bonsai agents</code> lists them), and reappear here when Bonsai web starts again. Restarting Bonsai web never stops them.
    </p>
    {!settings.keep_running && running > 0 && <p role="status" className="text-xs text-warn">{willStopNotice(running)}</p>}
    <div role="group" aria-labelledby="desktop-notifications-title" className="space-y-2">
      <h3 id="desktop-notifications-title" className="text-[12px] font-semibold">Desktop notifications</h3>
      <p className="text-xs text-muted">Sent by each agent session while it runs, even without Bonsai web. Applies to sessions started after the change.</p>
      <label className="flex items-center gap-2 text-[12px]">
        <input type="checkbox" checked={settings.notifications.enabled} disabled={saving} onChange={event => { void save({ notifications: { enabled: event.target.checked } }) }} />
        Send desktop notifications
      </label>
      {NOTIFICATION_TYPES.map(([key, label]) => <label key={key} className="flex items-center gap-2 pl-5 text-[12px]">
        <input type="checkbox" checked={settings.notifications[key]} disabled={saving || !settings.notifications.enabled} onChange={event => { void save({ notifications: { [key]: event.target.checked } }) }} />
        {label}
      </label>)}
    </div>
    <BrowserNotificationSettings />
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
  </section>
}
