import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import {
  alertContent, BROWSER_NOTIFICATIONS_KEY, browserPermission, getBrowserNotificationPrefs, reloadBrowserNotificationPrefs,
  requestBrowserPermission, setBrowserNotificationPrefs, showAgentAlert,
} from './browserNotifications'
import type { AgentAlert } from './agentTransitions'

const alert: AgentAlert = { agentId: 'a1', worktreeId: 'w', name: 'Work', kind: 'needs_input', since: '2099-01-01T00:00:00Z' }

class FakeNotification {
  static permission: NotificationPermission = 'granted'
  static requestPermission = vi.fn(async () => FakeNotification.permission)
  static shown: FakeNotification[] = []
  onclick: (() => void) | null = null
  close = vi.fn()
  constructor(public title: string, public options?: NotificationOptions) { FakeNotification.shown.push(this) }
}

beforeEach(() => {
  localStorage.clear()
  reloadBrowserNotificationPrefs()
  FakeNotification.permission = 'granted'
  FakeNotification.shown = []
  FakeNotification.requestPermission.mockClear()
  vi.stubGlobal('Notification', FakeNotification)
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('is off by default and persists the choice', () => {
  expect(getBrowserNotificationPrefs()).toEqual({ enabled: false, needsInput: true, turnComplete: true })
  setBrowserNotificationPrefs({ enabled: true, turnComplete: false })
  expect(JSON.parse(localStorage.getItem(BROWSER_NOTIFICATIONS_KEY) ?? '')).toEqual({ enabled: true, needsInput: true, turnComplete: false })
  reloadBrowserNotificationPrefs()
  expect(getBrowserNotificationPrefs()).toEqual({ enabled: true, needsInput: true, turnComplete: false })
})

it('ignores malformed stored values', () => {
  localStorage.setItem(BROWSER_NOTIFICATIONS_KEY, JSON.stringify({ enabled: 'yes', needsInput: false, extra: 1 }))
  reloadBrowserNotificationPrefs()
  expect(getBrowserNotificationPrefs()).toEqual({ enabled: false, needsInput: false, turnComplete: true })
  localStorage.setItem(BROWSER_NOTIFICATIONS_KEY, '{nope')
  reloadBrowserNotificationPrefs()
  expect(getBrowserNotificationPrefs().enabled).toBe(false)
})

it('keeps working for this page when storage throws', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied') })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
  reloadBrowserNotificationPrefs()
  expect(getBrowserNotificationPrefs().enabled).toBe(false)
  expect(() => setBrowserNotificationPrefs({ enabled: true })).not.toThrow()
  expect(getBrowserNotificationPrefs().enabled).toBe(true)
  expect(showAgentAlert(alert, 'feature')).toBe(true)
})

it('shows nothing when disabled, when the kind is off or when permission is not granted', () => {
  expect(showAgentAlert(alert, 'feature')).toBe(false)
  setBrowserNotificationPrefs({ enabled: true, needsInput: false })
  expect(showAgentAlert(alert, 'feature')).toBe(false)
  setBrowserNotificationPrefs({ needsInput: true })
  FakeNotification.permission = 'denied'
  expect(showAgentAlert(alert, 'feature')).toBe(false)
  FakeNotification.permission = 'default'
  expect(showAgentAlert(alert, 'feature')).toBe(false)
  expect(FakeNotification.shown).toEqual([])
})

it('reports unsupported browsers and never throws', async () => {
  vi.unstubAllGlobals()
  vi.stubGlobal('Notification', undefined)
  expect(browserPermission()).toBe('unsupported')
  await expect(requestBrowserPermission()).resolves.toBe('unsupported')
  setBrowserNotificationPrefs({ enabled: true })
  expect(showAgentAlert(alert, 'feature')).toBe(false)
})

it('returns the denied answer and survives a failing prompt', async () => {
  FakeNotification.permission = 'denied'
  await expect(requestBrowserPermission()).resolves.toBe('denied')
  FakeNotification.requestPermission.mockRejectedValueOnce(new Error('no gesture'))
  await expect(requestBrowserPermission()).resolves.toBe('denied')
})

it('shows a status title with the session and worktree names only, and focuses the window on click', () => {
  setBrowserNotificationPrefs({ enabled: true })
  const focus = vi.spyOn(window, 'focus').mockImplementation(() => undefined)
  expect(showAgentAlert({ ...alert, kind: 'turn_complete' }, 'feature/login')).toBe(true)
  const [shown] = FakeNotification.shown
  expect(shown.title).toBe('Turn complete')
  expect(shown.options).toEqual({ body: 'Work · feature/login', tag: 'bonsai-agent-a1-turn_complete' })
  shown.onclick?.()
  expect(focus).toHaveBeenCalled()
  expect(shown.close).toHaveBeenCalled()
})

it('returns false when the constructor throws', () => {
  setBrowserNotificationPrefs({ enabled: true })
  vi.stubGlobal('Notification', Object.assign(function () { throw new TypeError('Illegal constructor') }, { permission: 'granted' }))
  expect(showAgentAlert(alert, 'feature')).toBe(false)
})

it('builds content from status and names, with no room for prompts or commands', () => {
  expect(alertContent({ kind: 'needs_input', name: 'Work' }, 'main')).toEqual({ title: 'Needs input', body: 'Work · main' })
  expect(alertContent({ kind: 'needs_input', name: 'Work' }, '')).toEqual({ title: 'Needs input', body: 'Work' })
  expect(alertContent({ kind: 'needs_input', name: 'main' }, 'main').body).toBe('main')
})
