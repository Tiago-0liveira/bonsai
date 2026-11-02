import { useState, useSyncExternalStore } from 'react'
import {
  browserPermission, getBrowserNotificationPrefs, requestBrowserPermission, setBrowserNotificationPrefs, subscribeBrowserNotificationPrefs,
  type BrowserPermission,
} from '../../lib/browserNotifications'

const NOTE: Partial<Record<BrowserPermission, string>> = {
  unsupported: 'This browser does not support notifications.',
  denied: 'Notifications are blocked for this site. Allow them in the browser site settings, then turn this on again.',
}

// Per-browser setting, kept in this browser only; the desktop notifications
// above are decided by the server.
export function BrowserNotificationSettings() {
  const prefs = useSyncExternalStore(subscribeBrowserNotificationPrefs, getBrowserNotificationPrefs)
  const [permission, setPermission] = useState(browserPermission)
  // The permission can be revoked behind our back, so an enabled setting only counts while it is granted.
  const active = prefs.enabled && permission === 'granted'
  const enable = async () => {
    const result = await requestBrowserPermission()
    setPermission(result)
    if (result === 'granted') setBrowserNotificationPrefs({ enabled: true })
  }
  return <div role="group" aria-labelledby="browser-notifications-title" className="space-y-2">
    <h3 id="browser-notifications-title" className="text-[12px] font-semibold">Browser notifications</h3>
    <p className="text-xs text-muted">When this tab is in the background, show a browser notification when an agent needs input or finishes a turn. It names the session and worktree only. Stored in this browser.</p>
    {active
      ? <button type="button" className="bonsai-focus rounded-[7px] border border-border px-2.5 py-1 text-[12px]" onClick={() => setBrowserNotificationPrefs({ enabled: false })}>Turn off browser notifications</button>
      : <button type="button" className="bonsai-focus rounded-[7px] border border-border px-2.5 py-1 text-[12px] disabled:opacity-50" disabled={permission === 'unsupported'} onClick={() => { void enable() }}>Turn on browser notifications</button>}
    {NOTE[permission] && <p className="text-xs text-warn">{NOTE[permission]}</p>}
    {active && <>
      <label className="flex items-center gap-2 text-[12px]">
        <input type="checkbox" checked={prefs.needsInput} onChange={event => setBrowserNotificationPrefs({ needsInput: event.target.checked })} />
        Browser: when an agent needs input
      </label>
      <label className="flex items-center gap-2 text-[12px]">
        <input type="checkbox" checked={prefs.turnComplete} onChange={event => setBrowserNotificationPrefs({ turnComplete: event.target.checked })} />
        Browser: when an agent finishes a turn
      </label>
    </>}
  </div>
}
