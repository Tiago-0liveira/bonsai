import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { UpdatesBadge } from './UpdatesBadge'
import { useBonsaiStore } from '../../stores/bonsai'
import { loadUpdateSettings, type LiveRepositoryUpdates, type UpdateSettings } from '../../api/settings'

vi.mock('../../api/settings', () => ({ loadUpdateSettings: vi.fn() }))
const load = vi.mocked(loadUpdateSettings)

const standard: UpdateSettings = { mode: 'standard', standard_interval_seconds: 120, live: null }
function live(repositories: LiveRepositoryUpdates[], tunnel: Partial<NonNullable<UpdateSettings['live']>> = {}): UpdateSettings {
  return {
    mode: 'live',
    standard_interval_seconds: 120,
    live: { tunnel: 'cloudflared-quick', public_host: 'quiet-river.trycloudflare.com', tunnel_up: true, safety_poll_seconds: 600, repositories, ...tunnel },
  }
}
const bonsaiRepo: LiveRepositoryUpdates = { full_name: 'octo/bonsai', state: 'live', healthy: true, project_ids: ['bonsai'] }

describe('updates badge', () => {
  beforeEach(() => {
    load.mockReset()
    useBonsaiStore.setState({ activeProjectId: 'bonsai' })
  })
  afterEach(() => vi.useRealTimers())

  it('says Standard with the polling interval', async () => {
    load.mockResolvedValue(standard)
    render(<UpdatesBadge />)
    const badge = await screen.findByLabelText(/^Updates: Standard \(every 2 min\)/)
    expect(badge).toHaveAttribute('title', expect.stringContaining('bonsai web setup'))
  })

  it('says Live for a project whose hook works', async () => {
    load.mockResolvedValue(live([bonsaiRepo]))
    render(<UpdatesBadge />)
    const badge = await screen.findByLabelText(/^Updates: Live\./)
    expect(badge).toHaveAttribute('title', expect.stringContaining('quiet-river.trycloudflare.com'))
    expect(badge).toHaveAttribute('title', expect.stringContaining('every 10 min as a safety net'))
  })

  it('falls back to Standard with the reason', async () => {
    load.mockResolvedValue(live([{ ...bonsaiRepo, healthy: false }], { tunnel_up: false, tunnel_error: 'the tunnel is not running' }))
    const { unmount } = render(<UpdatesBadge />)
    expect(await screen.findByLabelText(/^Updates: Standard/)).toHaveAttribute('title', 'Live updates are paused: the tunnel is not running. Bonsai checks GitHub every 2 min meanwhile.')
    unmount()

    load.mockResolvedValue(live([{ ...bonsaiRepo, state: 'needs_admin', healthy: false, last_error: 'you are not an admin of octo/bonsai' }]))
    const second = render(<UpdatesBadge />)
    expect(await screen.findByLabelText(/^Updates: Standard/)).toHaveAttribute('title', expect.stringContaining('octo/bonsai: needs admin (you are not an admin of octo/bonsai)'))
    second.unmount()

    load.mockResolvedValue(live([{ ...bonsaiRepo, project_ids: ['other'] }]))
    render(<UpdatesBadge />)
    expect(await screen.findByLabelText(/^Updates: Standard/)).toHaveAttribute('title', expect.stringContaining('do not cover this project'))
  })

  it('stays hidden on an API without the endpoint', async () => {
    load.mockRejectedValue(new Error('Not Found'))
    const { container } = render(<UpdatesBadge />)
    await waitFor(() => expect(load).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })

  it('refetches on focus and every 30 s', async () => {
    vi.useFakeTimers()
    load.mockResolvedValue(standard)
    render(<UpdatesBadge />)
    expect(load).toHaveBeenCalledTimes(1)
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    expect(load).toHaveBeenCalledTimes(2)
    await act(async () => { vi.advanceTimersByTime(30_000) })
    expect(load).toHaveBeenCalledTimes(3)
  })
})
