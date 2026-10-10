import { expect, test } from '@playwright/test'
import { mockGitBackend, mockUpdateSettings, openConnectedApp } from './mockGit'

test('empty onboarding, multiple folders, Settings and last-root removal', async ({ page }) => {
  await mockGitBackend(page, true)
  await page.route('https://api.bonsai.dev/**', route => route.fulfill({ status: 401, body: '' }))
  await page.goto('/app')
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  const onboarding = page.getByRole('dialog', { name: 'Choose project folders' })
  await expect(onboarding).toBeVisible()
  await onboarding.getByRole('button', { name: '/projects', exact: true }).click()
  await onboarding.getByRole('button', { name: 'Add folder' }).click()
  await expect(onboarding.getByRole('heading', { name: 'Repositories found' })).toBeVisible()
  await onboarding.getByRole('button', { name: 'Select all' }).click()
  await onboarding.getByRole('button', { name: 'Use selected repositories' }).click()
  await expect(onboarding).toBeHidden()
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/settings$/)
  await expect(page.getByText('/projects', { exact: true })).toBeVisible()
  await page.getByLabel('Folder path').fill('/another')
  await page.getByRole('button', { name: 'Add folder' }).click()
  await expect(page.getByRole('heading', { name: 'Repositories found' })).toBeVisible()
  await page.getByRole('button', { name: 'Back to folders' }).click()
  await expect(page.getByRole('button', { name: 'Remove /another' })).toBeVisible()
  await page.getByRole('button', { name: 'Remove /projects' }).click()
  await expect(page.getByRole('heading', { name: 'Repositories found' })).toBeVisible()
  await page.getByRole('button', { name: 'Back to folders' }).click()
  await page.getByRole('button', { name: 'Remove /another' }).click()
  await expect(onboarding).toBeVisible()
  await onboarding.getByRole('button', { name: 'Later' }).click()
  await expect(onboarding).toBeHidden()
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Project folders' })).toBeVisible()
  await expect(page.getByText('No project folders configured yet.')).toBeVisible()
})

test('updates badge and the read-only Live updates section', async ({ page }) => {
  await mockGitBackend(page)
  let tunnelUp = true
  await mockUpdateSettings(page, () => ({
    mode: 'live',
    standard_interval_seconds: 120,
    live: {
      tunnel: 'cloudflared-quick',
      public_host: 'quiet-river.trycloudflare.com',
      tunnel_up: tunnelUp,
      ...(tunnelUp ? {} : { tunnel_error: 'the tunnel is not running' }),
      safety_poll_seconds: 600,
      repositories: [
        { full_name: 'octo/bonsai', state: 'live', healthy: tunnelUp, last_delivery_at: '2026-10-10T09:30:00Z', project_ids: ['bonsai'] },
        { full_name: 'acme/web', state: 'needs_admin', healthy: false, last_error: 'you are not an admin of acme/web, so it stays on standard updates', project_ids: [] },
      ],
    },
  }))
  await openConnectedApp(page)
  const badge = page.getByLabel(/^Updates: /)
  await expect(badge).toHaveAccessibleName(/^Updates: Live\./)
  await expect(badge).toHaveAttribute('title', /quiet-river\.trycloudflare\.com/)

  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  const section = page.getByRole('region', { name: 'Live updates' })
  await expect(section.getByText(/GitHub notifies Bonsai through quiet-river\.trycloudflare\.com/)).toBeVisible()
  await expect(section.getByRole('row', { name: /octo\/bonsai Live delivery/ })).toBeVisible()
  await expect(section.getByRole('row', { name: /acme\/web Needs admin/ })).toBeVisible()
  await expect(section.getByText('bonsai web setup')).toBeVisible()
  await expect(section.getByRole('button')).toHaveCount(0)

  // The tunnel goes down: the next refetch (focus) falls back to Standard.
  tunnelUp = false
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(badge).toHaveAccessibleName(/^Updates: Standard \(every 2 min\)/)
  await expect(badge).toHaveAttribute('title', /Live updates are paused: the tunnel is not running/)
  await expect(section.getByText('Tunnel: down (the tunnel is not running)')).toBeVisible()
})
