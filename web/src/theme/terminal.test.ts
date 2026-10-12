import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeEach, describe, expect, it } from 'vitest'
import { localThemeStorage } from './storage'
import { bindTerminalAppearance, TERMINAL_FONT, terminalTheme } from './terminal'
import { bonsaiTheme, builtInThemes, modernLightTheme, modernDarkTheme } from './themes'
import { terminalVariable } from './terminalColors'
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
  it.each(ANSI.map((name, index) => [name, index] as const))('ANSI %s (%i) resolves overrides with a Bonsai fallback', (name, index) => {
    for (const [prefix, property] of [['fg', 'color'], ['bg', 'background-color']] as const) {
      const selector = `.xterm .xterm-${prefix}-${index} { ${property}: var(${terminalVariable(name)}, rgb(var(--`
      const rule = css.slice(css.indexOf(selector), css.indexOf('}', css.indexOf(selector)))
      expect(rule).toContain(selector)
      const fallback = rule.slice(selector.length).split(')')[0] as keyof typeof bonsaiTheme.colors
      expect(bonsaiTheme.colors[fallback]).toBe(terminalTheme(bonsaiTheme)[name])
    }
  })

  it.each([modernLightTheme, modernDarkTheme])('$name uses conventional green and blue ANSI colors', theme => {
    const resolved = terminalTheme(theme)
    expect(resolved.green).toBe(theme.colors.ok)
    expect(resolved.blue).toBe(theme.colors.accent)
    expect(resolved.green).not.toBe(resolved.blue)
  })

  it('updates an existing terminal across all built-ins without measuring it', () => {
    initTheme()
    const onMetrics = () => { throw new Error('Theme selection must not resize a terminal') }
    const terminal = { options: { theme: {} } }
    const unbind = bindTerminalAppearance(terminal, onMetrics)
    for (const theme of builtInThemes) {
      setActiveTheme(theme.id)
      expect(terminal.options.theme).toEqual(terminalTheme(theme))
    }
    unbind()
  })
})
