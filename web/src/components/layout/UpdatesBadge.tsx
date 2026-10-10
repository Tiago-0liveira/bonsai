import { useBonsaiStore } from '../../stores/bonsai'
import { intervalText, liveRepositoryFor, TUNNEL_NAMES, useUpdateSettings, whyNotLive } from '../../features/settings/updates'

// UpdatesBadge says how GitHub changes reach the active project: Live
// (GitHub's notifications through the tunnel) or Standard polling, with the
// reason in its tooltip.
export function UpdatesBadge() {
  const settings = useUpdateSettings()
  const projectId = useBonsaiStore(state => state.activeProjectId)
  if (!settings) return null
  const standard = `every ${intervalText(settings.standard_interval_seconds)}`
  const repository = liveRepositoryFor(settings, projectId)
  const live = settings.live
  let label = 'Standard'
  let detail: string = standard
  let dot = 'bg-muted-2'
  let title = `Bonsai checks GitHub ${standard} while it is open. Turn on live updates with bonsai web setup.`
  if (live && repository?.healthy) {
    label = 'Live'
    detail = ''
    dot = 'bg-accent-solid'
    title = `GitHub notifies Bonsai through ${live.public_host ?? 'the tunnel'} (${TUNNEL_NAMES[live.tunnel] ?? live.tunnel}); Bonsai also checks every ${intervalText(live.safety_poll_seconds)} as a safety net.`
  } else if (live) {
    dot = repository ? 'bg-warn-solid' : 'bg-muted-2'
    title = `${whyNotLive(settings, repository)} Bonsai checks GitHub ${standard} meanwhile.`
  }
  return <span title={title} aria-label={`Updates: ${label}${detail ? ` (${detail})` : ''}. ${title}`} className="flex h-[26px] items-center gap-2 px-2 font-mono text-[11px] text-muted">
    <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${dot}`} />
    <span>{label}</span>
    {detail && <span className="hidden min-[1180px]:inline">({detail})</span>}
  </span>
}
