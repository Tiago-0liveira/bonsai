import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { LiveUpdatesSettings } from './LiveUpdatesSettings'
import { loadUpdateSettings } from '../../api/settings'

vi.mock('../../api/settings', () => ({ loadUpdateSettings: vi.fn() }))
const load = vi.mocked(loadUpdateSettings)

describe('live updates settings', () => {
  beforeEach(() => load.mockReset())

  it('describes standard updates and how to change them', async () => {
    load.mockResolvedValue({ mode: 'standard', standard_interval_seconds: 120, live: null })
    render(<LiveUpdatesSettings />)
    expect(await screen.findByText('Standard: Bonsai checks GitHub every 2 min while it is open.')).toBeVisible()
    expect(screen.getByText('bonsai web setup')).toBeVisible()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('lists each live repository read-only', async () => {
    load.mockResolvedValue({
      mode: 'live',
      standard_interval_seconds: 120,
      live: {
        tunnel: 'cloudflared-quick', public_host: 'quiet-river.trycloudflare.com', tunnel_up: true, safety_poll_seconds: 600,
        repositories: [
          { full_name: 'octo/bonsai', state: 'live', healthy: true, last_ping_at: '2026-01-02T10:00:00Z', last_delivery_at: '2026-01-02T11:00:00Z', project_ids: ['bonsai'] },
          { full_name: 'acme/web', state: 'needs_admin', healthy: false, last_error: 'you are not an admin of acme/web, so it stays on standard updates', project_ids: [] },
        ],
      },
    })
    render(<LiveUpdatesSettings />)
    expect(await screen.findByText(/GitHub notifies Bonsai through quiet-river\.trycloudflare\.com \(Cloudflare quick tunnel\)/)).toBeVisible()
    expect(screen.getByText('Tunnel: up')).toBeVisible()
    const rows = screen.getAllByRole('row')
    expect(within(rows[1]).getByText('octo/bonsai')).toBeVisible()
    expect(within(rows[1]).getByText('Live')).toBeVisible()
    expect(within(rows[1]).getByText(/^delivery /)).toBeVisible()
    expect(within(rows[2]).getByText('Needs admin')).toBeVisible()
    expect(within(rows[2]).getByText(/not an admin of acme\/web/)).toBeVisible()
    expect(within(rows[2]).getByText('none yet')).toBeVisible()
    expect(screen.queryByRole('button')).toBeNull()
  })
})
