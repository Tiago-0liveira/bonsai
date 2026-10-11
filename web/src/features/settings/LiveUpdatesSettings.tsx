import { formatPrTime } from '../../lib/github/prTime'
import { HOOK_STATE_LABELS, intervalText, TUNNEL_NAMES, useUpdateSettings } from './updates'

function lastEvent(ping?: string, delivery?: string) {
  const [kind, at] = (ping ?? '') > (delivery ?? '') ? ['ping', ping] : ['delivery', delivery]
  return at ? `${kind} ${formatPrTime(at)}` : 'none yet'
}

// LiveUpdatesSettings shows how GitHub changes reach Bonsai, per repository
// in live mode. It is read-only: bonsai web setup changes it.
export function LiveUpdatesSettings() {
  const settings = useUpdateSettings()
  if (!settings) return null
  const live = settings.live
  return <section className="mx-auto w-full max-w-2xl space-y-3 border-b border-border-subtle p-6" aria-labelledby="live-updates-title">
    <h2 id="live-updates-title" className="text-[13px] font-semibold">Live updates</h2>
    {!live ? <p className="text-sm text-muted">Standard: Bonsai checks GitHub every {intervalText(settings.standard_interval_seconds)} while it is open.</p> : <>
      <p className="text-sm text-muted">
        GitHub notifies Bonsai through {live.public_host ?? 'the tunnel'} ({TUNNEL_NAMES[live.tunnel] ?? live.tunnel}).
        Live projects are also checked every {intervalText(live.safety_poll_seconds)} as a safety net; the others every {intervalText(settings.standard_interval_seconds)}.
      </p>
      <p className="text-sm">Tunnel: {live.tunnel_up ? 'up' : `down${live.tunnel_error ? ` (${live.tunnel_error})` : ''}`}</p>
      {live.repositories.length === 0 ? <p className="text-sm">No repositories use live updates yet.</p> : <table className="w-full text-left text-[12px]">
        <thead className="text-muted"><tr><th className="py-1 font-medium">Repository</th><th className="py-1 font-medium">State</th><th className="py-1 font-medium">Last event</th></tr></thead>
        <tbody>{live.repositories.map(repository => <tr key={repository.full_name} className="border-t border-border-subtle align-top">
          <td className="py-1.5 pr-3 font-mono">{repository.full_name}</td>
          <td className="py-1.5 pr-3">
            {HOOK_STATE_LABELS[repository.state] ?? repository.state}
            {repository.last_error && <span className="block text-muted">{repository.last_error}</span>}
          </td>
          <td className="py-1.5 text-muted">{lastEvent(repository.last_ping_at, repository.last_delivery_at)}</td>
        </tr>)}</tbody>
      </table>}
    </>}
    <p className="text-xs text-muted">Change with <code className="font-mono">bonsai web setup</code>.</p>
  </section>
}
