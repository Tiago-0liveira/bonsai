import type { AgentAlert, AgentAlertKind } from './agentTransitions'

// Per-viewer browser notifications. Separate from the server's desktop
// notifications (/api/settings/agents), which the session holders send.

export interface BrowserNotificationPrefs {
  enabled: boolean
  needsInput: boolean
  turnComplete: boolean
}
export type BrowserPermission = NotificationPermission | 'unsupported'

export const BROWSER_NOTIFICATIONS_KEY = 'bonsai-browser-notifications-v1'
const DEFAULTS: BrowserNotificationPrefs = { enabled: false, needsInput: true, turnComplete: true }

function read(): BrowserNotificationPrefs {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(BROWSER_NOTIFICATIONS_KEY) ?? 'null')
    if (typeof stored !== 'object' || stored === null) return DEFAULTS
    const values = stored as Partial<Record<keyof BrowserNotificationPrefs, unknown>>
    const flag = (key: keyof BrowserNotificationPrefs) => typeof values[key] === 'boolean' ? values[key] : DEFAULTS[key]
    return { enabled: flag('enabled'), needsInput: flag('needsInput'), turnComplete: flag('turnComplete') }
  } catch { return DEFAULTS }
}

// The cached object is the useSyncExternalStore snapshot, so it only changes
// when a preference does. It also keeps the choice for this page when the
// storage is unavailable or full.
let current: BrowserNotificationPrefs | undefined
const listeners = new Set<() => void>()
const emit = () => { for (const listener of listeners) listener() }

export function getBrowserNotificationPrefs(): BrowserNotificationPrefs {
  return current ??= read()
}

export function setBrowserNotificationPrefs(patch: Partial<BrowserNotificationPrefs>) {
  current = { ...getBrowserNotificationPrefs(), ...patch }
  try { localStorage.setItem(BROWSER_NOTIFICATIONS_KEY, JSON.stringify(current)) } catch { /* per-viewer convenience only */ }
  emit()
}

/** Drops the cache so the next read comes from storage; another tab may have changed it. */
export function reloadBrowserNotificationPrefs() {
  current = undefined
  emit()
}

export function subscribeBrowserNotificationPrefs(listener: () => void) {
  listeners.add(listener)
  window.addEventListener('storage', reloadBrowserNotificationPrefs)
  return () => {
    listeners.delete(listener)
    if (!listeners.size) window.removeEventListener('storage', reloadBrowserNotificationPrefs)
  }
}

export function browserPermission(): BrowserPermission {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
}

/** Must run from a user gesture (a click): browsers ignore or block the prompt otherwise. */
export async function requestBrowserPermission(): Promise<BrowserPermission> {
  if (typeof Notification === 'undefined') return 'unsupported'
  try { return await Notification.requestPermission() } catch { return browserPermission() }
}

const TITLES: Record<AgentAlertKind, string> = { needs_input: 'Needs input', turn_complete: 'Turn complete' }

/** Only the status and the session and worktree names: never a prompt, command or terminal output. */
export function alertContent(alert: Pick<AgentAlert, 'kind' | 'name'>, worktree: string): { title: string; body: string } {
  return { title: TITLES[alert.kind], body: worktree && worktree !== alert.name ? `${alert.name} · ${worktree}` : alert.name }
}

/** Shows one notification if the viewer enabled it and the browser allows it. */
export function showAgentAlert(alert: AgentAlert, worktree: string): boolean {
  const prefs = getBrowserNotificationPrefs()
  if (!prefs.enabled || browserPermission() !== 'granted') return false
  if (!(alert.kind === 'needs_input' ? prefs.needsInput : prefs.turnComplete)) return false
  const { title, body } = alertContent(alert, worktree)
  try {
    const notification = new Notification(title, { body, tag: `bonsai-agent-${alert.agentId}-${alert.kind}` })
    notification.onclick = () => { window.focus(); notification.close() }
    return true
  } catch { return false }
}
