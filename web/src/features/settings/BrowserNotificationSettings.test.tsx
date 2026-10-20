import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserNotificationSettings } from './BrowserNotificationSettings'
import { BROWSER_NOTIFICATIONS_KEY, getBrowserNotificationPrefs, reloadBrowserNotificationPrefs } from '../../lib/browserNotifications'

let permission: NotificationPermission
let answer: NotificationPermission
const request = vi.fn(async () => { permission = answer; return answer })

beforeEach(() => {
  localStorage.clear()
  reloadBrowserNotificationPrefs()
  permission = 'default'
  answer = 'granted'
  request.mockClear()
  vi.stubGlobal('Notification', { get permission() { return permission }, requestPermission: request })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('is off until the viewer clicks, then asks for permission and stores the choice', async () => {
  render(<BrowserNotificationSettings />)
  expect(request).not.toHaveBeenCalled()
  expect(screen.queryByRole('checkbox')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Turn on browser notifications' }))
  expect(await screen.findByRole('button', { name: 'Turn off browser notifications' })).toBeVisible()
  expect(request).toHaveBeenCalledTimes(1)
  expect(JSON.parse(localStorage.getItem(BROWSER_NOTIFICATIONS_KEY) ?? '')).toMatchObject({ enabled: true })
  fireEvent.click(screen.getByRole('checkbox', { name: 'Browser: when an agent finishes a turn' }))
  expect(getBrowserNotificationPrefs().turnComplete).toBe(false)
  fireEvent.click(screen.getByRole('button', { name: 'Turn off browser notifications' }))
  expect(getBrowserNotificationPrefs().enabled).toBe(false)
})

it('explains a denied permission and stays off', async () => {
  answer = 'denied'
  render(<BrowserNotificationSettings />)
  fireEvent.click(screen.getByRole('button', { name: 'Turn on browser notifications' }))
  expect(await screen.findByText(/blocked for this site/)).toBeVisible()
  expect(getBrowserNotificationPrefs().enabled).toBe(false)
  expect(localStorage.getItem(BROWSER_NOTIFICATIONS_KEY)).toBeNull()
})

it('disables the button where notifications are unsupported', () => {
  vi.stubGlobal('Notification', undefined)
  render(<BrowserNotificationSettings />)
  expect(screen.getByRole('button', { name: 'Turn on browser notifications' })).toBeDisabled()
  expect(screen.getByText(/blocked|does not support/)).toBeVisible()
})

it('still enables for this page when storage is unavailable', async () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
  render(<BrowserNotificationSettings />)
  fireEvent.click(screen.getByRole('button', { name: 'Turn on browser notifications' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Turn off browser notifications' })).toBeVisible())
})

it('shows the setting as off when the permission was revoked after enabling', () => {
  localStorage.setItem(BROWSER_NOTIFICATIONS_KEY, JSON.stringify({ enabled: true, needsInput: true, turnComplete: true }))
  reloadBrowserNotificationPrefs()
  permission = 'denied'
  render(<BrowserNotificationSettings />)
  expect(screen.getByRole('button', { name: 'Turn on browser notifications' })).toBeVisible()
  expect(screen.getByText(/blocked|does not support/)).toBeVisible()
})
