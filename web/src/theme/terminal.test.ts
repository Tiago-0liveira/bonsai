import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeEach, describe, expect, it } from 'vitest'
import { BONSAI_ANSI, DARK_ANSI, LIGHT_ANSI, resolveAnsiPalette, type AnsiPalette } from './ansi'
import { contrastRatio, hexToTriplet, relativeLuminance } from './color'
import { localThemeStorage } from './storage'
import { bindTerminalAppearance, TERMINAL_FONT, terminalTheme } from './terminal'
import { bonsaiTheme } from './themes'
import { applyTheme, initTheme, resetThemeStoreForTests, saveCustomTheme, setActiveTheme } from './themeStore'
import { ANSI_COLORS, type AnsiColor, type Theme } from './tokens'

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
    expect(theme.cursorAccent).toBe('#160c07')
    expect(theme.selectionBackground).toBe('#6bfb9a47')
  })

  it('takes the 16 ANSI colors from the theme palette, not from the tonal tokens', () => {
    const theme = terminalTheme(bonsaiTheme)
    for (const name of ANSI_COLORS) expect(theme[name], name).toBe(BONSAI_ANSI[name])
    const tokens = new Set(Object.values(bonsaiTheme.colors))
    for (const name of ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan'] as const) expect(tokens.has(theme[name] as never), name).toBe(false)
  })

  it('lets theme overrides win', () => {
    expect(terminalTheme({ ...bonsaiTheme, terminal: { red: '#ff0000' } }).red).toBe('#ff0000')
    expect(terminalTheme({ ...bonsaiTheme, terminal: { background: '#000000' } }).background).toBe('#000000')
  })

  it('fills ANSI colors a custom theme leaves out from the neutral palette of its mode', () => {
    const { terminal: _terminal, ...bare } = bonsaiTheme
    expect(resolveAnsiPalette(bare)).toEqual(DARK_ANSI)
    expect(resolveAnsiPalette({ ...bare, mode: 'light' })).toEqual(LIGHT_ANSI)
    expect(resolveAnsiPalette({ ...bare, terminal: { red: '#ff0000' } })).toEqual({ ...DARK_ANSI, red: '#ff0000' })
    expect(terminalTheme(bare).background).toBe(bare.colors.well)
    expect(terminalTheme(bare).foreground).toBe(bare.colors.text)
  })
})

const HUES = ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan'] as const
const BRIGHT_HUES = HUES.map(hue => `bright${hue[0]!.toUpperCase()}${hue.slice(1)}` as AnsiColor)

function rgb(hex: string): [number, number, number] {
  return [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16) / 255) as [number, number, number]
}
/** (max - min) / max: stricter than HSL saturation, so passing it implies both. */
function saturation(hex: string) {
  const [r, g, b] = rgb(hex)
  const max = Math.max(r, g, b)
  return max === 0 ? 0 : (max - Math.min(r, g, b)) / max
}
function hue(hex: string) {
  const [r, g, b] = rgb(hex)
  const max = Math.max(r, g, b)
  const delta = max - Math.min(r, g, b)
  if (delta === 0) return 0
  const sector = max === r ? ((g - b) / delta) % 6 : max === g ? (b - r) / delta + 2 : (r - g) / delta + 4
  return (sector * 60 + 360) % 360
}
const hueDistance = (a: string, b: string) => { const d = Math.abs(hue(a) - hue(b)); return Math.min(d, 360 - d) }

function describePalette(label: string, palette: AnsiPalette, well: string, dark: boolean) {
  describe(`${label} ANSI palette on ${well}`, () => {
    it('has six pairwise distinct hues, blue apart from cyan', () => {
      expect(palette.blue).not.toBe(palette.cyan)
      for (const a of HUES) for (const b of HUES) {
        if (a < b) expect(hueDistance(palette[a], palette[b]), `${a}/${b}`).toBeGreaterThanOrEqual(25)
      }
      expect(new Set(HUES.map(name => palette[name])).size).toBe(6)
    })
    it('keeps every colored entry saturated and readable', () => {
      for (const name of [...HUES, ...BRIGHT_HUES]) {
        expect(saturation(palette[name]), `${name} saturation`).toBeGreaterThanOrEqual(0.45)
        expect(contrastRatio(palette[name], well), `${name} contrast`).toBeGreaterThanOrEqual(4.5)
      }
    })
    it('makes bright variants lighter than their normal color', () => {
      for (const hueName of HUES) {
        const bright = palette[`bright${hueName[0]!.toUpperCase()}${hueName.slice(1)}` as AnsiColor]
        expect(relativeLuminance(bright), hueName).toBeGreaterThan(relativeLuminance(palette[hueName]))
      }
      expect(relativeLuminance(palette.brightBlack)).toBeGreaterThan(relativeLuminance(palette.black))
      expect(relativeLuminance(palette.brightWhite)).toBeGreaterThan(relativeLuminance(palette.white))
    })
    it('keeps black apart from the background and white readable', () => {
      expect(palette.black).not.toBe(well)
      expect(contrastRatio(palette.black, well)).toBeGreaterThan(1.2)
      expect(contrastRatio(palette.white, well)).toBeGreaterThanOrEqual(4.5)
      expect(contrastRatio(palette.brightWhite, well)).toBeGreaterThanOrEqual(4.5)
      if (dark) expect(contrastRatio(palette.brightBlack, well)).toBeGreaterThanOrEqual(3)
    })
  })
}

describePalette('Bonsai', BONSAI_ANSI, bonsaiTheme.colors.well, true)
describePalette('dark fallback', DARK_ANSI, bonsaiTheme.colors.well, true)
describePalette('dark fallback', DARK_ANSI, '#111111', true)
describePalette('light fallback', LIGHT_ANSI, '#ffffff', false)

describe('Bonsai palette against the theme text', () => {
  it('keeps white and brightWhite near the text color', () => {
    expect(BONSAI_ANSI.brightWhite).toBe(bonsaiTheme.colors.text)
    expect(contrastRatio(BONSAI_ANSI.white, bonsaiTheme.colors.text)).toBeLessThan(1.5)
  })
  it('is the palette the built-in theme ships', () => {
    expect(bonsaiTheme.terminal).toEqual(BONSAI_ANSI)
  })
})

describe('applyTheme ANSI variables', () => {
  it('sets --ansi-0..15 from the theme palette and follows theme switches', () => {
    const root = document.createElement('div')
    applyTheme(bonsaiTheme, root)
    ANSI_COLORS.forEach((name, index) => expect(root.style.getPropertyValue(`--ansi-${index}`), name).toBe(hexToTriplet(BONSAI_ANSI[name])))
    const light: Theme = { ...bonsaiTheme, mode: 'light', terminal: { red: '#aa0000' } }
    applyTheme(light, root)
    expect(root.style.getPropertyValue('--ansi-1')).toBe('170 0 0')
    expect(root.style.getPropertyValue('--ansi-2')).toBe(hexToTriplet(LIGHT_ANSI.green))
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

const css = readFileSync(resolve(process.cwd(), 'src/styles/globals.css'), 'utf8')

// The production CSP blocks xterm's injected <style>, so globals.css carries static rules for the ANSI colors.
describe('static xterm CSS fallback', () => {
  it.each(ANSI_COLORS.map((name, index) => [name, index] as const))('ANSI %s (%i) uses a --ansi variable defaulting to the Bonsai palette', (name, index) => {
    for (const [prefix, property] of [['fg', 'color'], ['bg', 'background-color']] as const) {
      expect(css, `.xterm-${prefix}-${index}`).toContain(`.xterm .xterm-${prefix}-${index} { ${property}: rgb(var(--ansi-${index})); }`)
    }
    const declared = css.match(new RegExp(`^\\s*--ansi-${index}: (\\d+ \\d+ \\d+);`, 'm'))
    expect(declared, `:root --ansi-${index}`).not.toBeNull()
    expect(declared![1]).toBe(hexToTriplet(terminalTheme(bonsaiTheme)[name] as string))
  })
})
