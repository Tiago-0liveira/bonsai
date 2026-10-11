import { lazy } from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { acknowledgePreloadFailure, FeatureBoundary, OverlayBoundary, PreloadRecoveryNotice, registerChunkRecovery } from './chunkRecovery'

afterEach(() => { cleanup(); acknowledgePreloadFailure(); vi.restoreAllMocks() })

it('keeps surrounding state visible after a rejected lazy import', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  const Failed = lazy(() => Promise.reject(new Error('missing hashed chunk')))
  render(<><aside>Retained branches</aside><FeatureBoundary loading="Loading files…"><Failed /></FeatureBoundary></>)
  expect(await screen.findByText('This screen could not load')).toBeVisible()
  expect(screen.getByText('Retained branches')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Reload app' })).toBeVisible()
})

it('shows eager recovery for an effect import without preventing Vite rejection', () => {
  const unregister = registerChunkRecovery()
  render(<PreloadRecoveryNotice />)
  const event = new Event('vite:preloadError', { cancelable: true })
  act(() => { window.dispatchEvent(event) })
  expect(event.defaultPrevented).toBe(false)
  expect(screen.getByRole('button', { name: 'Reload app' })).toBeVisible()
  unregister()
  act(() => { acknowledgePreloadFailure(); window.dispatchEvent(event) })
  expect(screen.queryByRole('alert')).toBeNull()
})

it('allows dismissal while an overlay chunk is pending and restores trigger focus', async () => {
  const Pending = lazy(() => new Promise<never>(() => {}))
  const trigger = document.createElement('button')
  document.body.append(trigger)
  trigger.focus()
  const dismiss = vi.fn()
  const view = render(<OverlayBoundary loading="Loading commands…" onDismiss={dismiss}><Pending /></OverlayBoundary>)
  expect(screen.getByRole('status')).toHaveTextContent('Loading commands…')
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(dismiss).toHaveBeenCalledOnce()
  view.unmount()
  await Promise.resolve()
  expect(trigger).toHaveFocus()
  trigger.remove()
})
