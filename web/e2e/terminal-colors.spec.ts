import { expect, test } from '@playwright/test'
import { BONSAI_ANSI } from '../src/theme/ansi'
import { mockGitBackend, openConnectedApp } from './mockGit'

test('terminal renders ANSI red and blue as two distinct non-white colors', async ({ page }) => {
  await mockGitBackend(page)
  // Registered after the shared fixture, so this handler wins for agent terminals.
  await page.routeWebSocket(/\/api\/projects\/[^/]+\/agents\/[^/]+\/terminal$/, terminal => {
    terminal.onMessage(message => {
      if (JSON.parse(String(message)).type !== 'attach') return
      const output = Buffer.from('\x1b[31mred\x1b[34mblue\x1b[0m plain\r\n')
      terminal.send(JSON.stringify({ type: 'ready', version: 1, generation: 'e2e-colors', writer: true }))
      terminal.send(JSON.stringify({ type: 'output', offset: 0, data: output.toString('base64') }))
    })
  })
  await openConnectedApp(page)
  await page.getByTestId('rf__node-wt-daemon').click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Start agent', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Start agent' })
  await dialog.getByRole('button', { name: /Claude/ }).click()
  await dialog.getByLabel('Agent name').fill('Colors')
  await dialog.getByRole('button', { name: 'Start agent', exact: true }).click()
  await expect(page.locator('.xterm-screen')).toContainText('redblue plain')

  const colorOf = (text: string) => page.locator('.xterm-rows span', { hasText: new RegExp(`^${text}$`) }).first().evaluate(el => getComputedStyle(el).color)
  const [red, blue, plain] = [await colorOf('red'), await colorOf('blue'), await page.locator('.xterm-rows').evaluate(el => getComputedStyle(el).color)]
  const channels = (color: string) => (color.match(/\d+/g) ?? []).slice(0, 3).map(Number)
  expect(red).not.toBe(blue)
  for (const color of [red, blue]) {
    expect(color).not.toBe(plain)
    const [r = 0, g = 0, b = 0] = channels(color)
    // A near-white would have all three channels high and close together.
    expect(Math.max(r, g, b) - Math.min(r, g, b)).toBeGreaterThan(60)
  }
  // The static CSS rules read the palette that applyTheme() put on :root.
  const rgb = (hex: string) => `rgb(${[1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16)).join(', ')})`
  expect(red).toBe(rgb(BONSAI_ANSI.red))
  expect(blue).toBe(rgb(BONSAI_ANSI.blue))
  expect(channels(red)[0]).toBeGreaterThan(channels(red)[2]!)
  expect(channels(blue)[2]).toBeGreaterThan(channels(blue)[0]!)
})
