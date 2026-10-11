import { expect, test } from '@playwright/test'
import { agents } from '../src/mock/agents'
import { bonsaiTheme } from '../src/theme/themes'
import { mockGitBackend, openConnectedApp } from './mockGit'
import { injectAgentPresentation } from './presentationFixtures'

const providers = ['Claude', 'Codex', 'Antigravity', 'Gemini'] as const
const sessions = providers.map((provider, index) => ({
  ...agents[index], id: `mark-${provider.toLowerCase()}`, provider, projectId: 'bonsai',
  worktreeId: 'wt-web', name: `${provider} session`, profileName: 'Work',
  terminalId: `mark-${provider.toLowerCase()}`, presentation: 'canvas' as const,
}))

// Presentation fixtures cover display-only providers without enabling their launch.
for (const mode of ['dark', 'light'] as const) {
  test(`provider marks on canvas, inspector and terminals in ${mode} theme`, async ({ page }, testInfo) => {
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.setViewportSize({ width: 1600, height: 960 })
    if (mode === 'light') {
      const theme = { ...bonsaiTheme, id: 'custom:paper', name: 'Paper', mode, colors: {
        ...bonsaiTheme.colors, bg: '#ffffff', well: '#f5f5f5', panel: '#ffffff',
        'panel-2': '#f5f5f5', 'panel-3': '#eeeeee', 'panel-4': '#e2e2e2',
        text: '#111111', muted: '#333333', 'muted-2': '#555555', 'accent-fg': '#000000',
      } }
      await page.addInitScript(theme => {
        localStorage.setItem('bonsai-theme-v1', JSON.stringify({ version: 1, activeId: theme.id, custom: [theme] }))
      }, theme)
    }
    await mockGitBackend(page)
    await openConnectedApp(page)
    await injectAgentPresentation(page)
    await page.evaluate(sessions => {
      window.__bonsaiTestStore.setState({ agents: sessions })
      const state = window.__bonsaiTestStore.getState()
      state.setDockWorktreeId('wt-web')
      state.setSelection({ type: 'worktree', id: 'wt-web' })
    }, sessions)
    await expect(page.locator('html')).toHaveCSS('color-scheme', mode)
    for (const provider of providers) {
      await expect(page.getByTestId(`rf__node-mark-${provider.toLowerCase()}`).locator(`[data-provider-logo="${provider.toLowerCase()}"]`)).toBeVisible()
    }
    await page.screenshot({ path: testInfo.outputPath(`canvas-${mode}.png`) })

    // The picker and tab headers use the same marks as the canvas badges.
    await page.getByRole('button', { name: 'Open runtime', exact: true }).click()
    for (const provider of providers) {
      const option = page.getByRole('option', { name: `${provider} session ${provider} · Work`, exact: true })
      await expect(option.locator(`[data-provider-logo="${provider.toLowerCase()}"]`)).toBeVisible()
    }
    await page.getByRole('option', { name: 'Claude session Claude · Work', exact: true }).click()
    await page.evaluate(sessions => {
      for (const session of sessions) window.__bonsaiTestStore.getState().openRuntime(session.id)
    }, sessions)
    for (const provider of providers) {
      const mark = `[data-provider-logo="${provider.toLowerCase()}"]`
      await expect(page.locator('.runtime-heading').locator(mark)).toBeVisible()
      await expect(page.locator('.dock-heading button').locator(mark)).toBeVisible()
      await page.evaluate(id => window.__bonsaiTestStore.getState().setSelection({ type: 'agent', id }), `mark-${provider.toLowerCase()}`)
      await expect(page.locator('.inspector-hero').locator(mark)).toBeVisible()
      await expect(page.locator('.inspector-hero .lucide-bot')).toHaveCount(0)
      await page.screenshot({ path: testInfo.outputPath(`inspector-${provider.toLowerCase()}-${mode}.png`) })
    }
    const codex = page.locator('.runtime-heading [data-provider-logo="codex"]')
    const colors = await codex.evaluate(svg => ({
      fill: getComputedStyle(svg).fill,
      text: getComputedStyle(svg).color,
      background: getComputedStyle(svg.parentElement!).backgroundColor,
    }))
    expect(colors.fill).toBe(colors.text)
    expect(colors.fill).not.toBe(colors.background)
    // Check actual contrast of the themed monochrome mark on its panel-4 tile.
    const luminance = (color: string) => {
      const channels = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(value => {
        const channel = value / 255
        return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
      })
      return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722
    }
    const foreground = luminance(colors.fill), background = luminance(colors.background)
    expect((Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)).toBeGreaterThan(4.5)
    await page.screenshot({ path: testInfo.outputPath(`terminals-${mode}.png`) })
    expect(errors).toEqual([])
  })
}
