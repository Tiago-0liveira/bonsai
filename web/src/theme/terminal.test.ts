import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeEach, describe, expect, it } from 'vitest'
import { localThemeStorage } from './storage'
import { bindTerminalAppearance, TERMINAL_FONT, terminalTheme } from './terminal'
import { bonsaiTheme } from './themes'
import { initTheme, resetThemeStoreForTests, saveCustomTheme, setActiveTheme } from './themeStore'

beforeEach(() => {
  window.localStorage.clear()
  resetThemeStoreForTests()
})

describe('terminalTheme', () => {
  it('derives colors from tokens', () => {
    const theme = terminalTheme(bonsaiTheme)
    expect(theme.background).toBe('#160c07')
    expect(theme.foreground).toBe('#f5ded4')
    expect(theme.cursor).toBe('#6bfb9a')
    expect(theme.green).toBe('#4ade80')
    expect(theme.brightBlack).toBe('#869486')
  })

  it('lets theme overrides win', () => {
    expect(terminalTheme({ ...bonsaiTheme, terminal: { red: '#ff0000' } }).red).toBe('#ff0000')
  })
})

describe('bindTerminalAppearance', () => {
  it('sets appearance now and on theme changes, and stops after unbind', () => {
    initTheme(localThemeStorage)
    saveCustomTheme({
      ...structuredClone(bonsaiTheme),
      name: 'Paper',
      mode: 'light',
      colors: { ...bonsaiTheme.colors, well: '#ffffff', bg: '#ffffff', panel: '#f5f5f5', 'panel-2': '#eeeeee', text: '#111111', muted: '#333333', 'accent-fg': '#000000' },
    })
    const terminal: { options: { theme?: unknown; fontFamily?: string } } = { options: {} }
    const unbind = bindTerminalAppearance(terminal, () => {})
    expect(terminal.options.fontFamily).toBe(TERMINAL_FONT)
    expect((terminal.options.theme as { background: string }).background).toBe('#160c07')
    setActiveTheme('custom:paper')
    expect((terminal.options.theme as { background: string }).background).toBe('#ffffff')
    unbind()
    setActiveTheme('bonsai')
    expect((terminal.options.theme as { background: string }).background).toBe('#ffffff')
  })
})

const ANSI = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white', 'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'] as const
const css = readFileSync(resolve(process.cwd(), 'src/styles/globals.css'), 'utf8')

// The production CSP blocks xterm's injected <style>, so globals.css carries static copies of the ANSI colors.
describe('static xterm CSS fallback', () => {
  it.each(ANSI.map((name, index) => [name, index] as const))('ANSI %s (%i) matches terminalTheme', (name, index) => {
    const expected = terminalTheme(bonsaiTheme)[name]
    for (const [prefix, property] of [['fg', 'color'], ['bg', 'background-color']] as const) {
      const match = css.match(new RegExp(`\\.xterm \\.xterm-${prefix}-${index} \\{ ${property}: rgb\\(var\\(--([a-z0-9-]+)\\)\\); \\}`))
      expect(match, `.xterm-${prefix}-${index}`).not.toBeNull()
      expect(bonsaiTheme.colors[match![1] as keyof typeof bonsaiTheme.colors]).toBe(expected)
    }
  })
})
