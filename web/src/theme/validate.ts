import { contrastRatio, isHexColor } from './color'
import { TERMINAL_COLORS, THEME_TOKENS, type HexColor, type TerminalColor, type Theme, type ThemeToken } from './tokens'

export type ThemeValidation =
  | { ok: true; theme: Theme; warnings: string[] }
  | { ok: false; errors: string[] }

/** Pairs that must pass (reject) or should pass (warn) at the given WCAG ratio. */
const ERROR_CONTRAST: readonly [ThemeToken, ThemeToken, number][] = [
  ['text', 'bg', 7], ['text', 'panel', 7], ['text', 'panel-2', 7],
  ['accent-fg', 'accent-solid', 4.5], ['muted', 'panel', 4.5],
]
const WARNING_CONTRAST: readonly [ThemeToken, ThemeToken, number][] = [
  ['muted-2', 'panel', 3], ['accent', 'panel', 3], ['ok', 'panel', 3], ['warn', 'panel', 3], ['danger', 'panel', 3],
]

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

export function validateTheme(input: unknown): ThemeValidation {
  if (!isRecord(input)) return { ok: false, errors: ['Theme must be an object.'] }
  const errors: string[] = []
  const warnings: string[] = []

  const id = typeof input.id === 'string' ? input.id : ''
  if (!id) errors.push('id must be a non-empty string.')
  const name = typeof input.name === 'string' ? input.name.trim() : ''
  if (name.length < 1 || name.length > 40) errors.push('name must be 1 to 40 characters.')
  const mode = input.mode
  if (mode !== 'dark' && mode !== 'light') errors.push("mode must be 'dark' or 'light'.")

  const colors = {} as Record<ThemeToken, HexColor>
  if (!isRecord(input.colors)) errors.push('colors must be an object.')
  else {
    for (const token of THEME_TOKENS) {
      const value = input.colors[token]
      if (isHexColor(value)) colors[token] = value.toLowerCase() as HexColor
      else errors.push(`colors.${token} must be a 6-digit hex color like #1c110b.`)
    }
  }

  let terminal: Theme['terminal']
  if (input.terminal !== undefined) {
    if (!isRecord(input.terminal)) errors.push('terminal must be an object.')
    else {
      terminal = {}
      for (const key of TERMINAL_COLORS) {
        const value = input.terminal[key]
        if (value === undefined) continue
        if (isHexColor(value)) terminal[key as TerminalColor] = value.toLowerCase() as HexColor
        else errors.push(`terminal.${key} must be a 6-digit hex color.`)
      }
    }
  }

  if (errors.length === 0) {
    for (const [foreground, background, minimum] of ERROR_CONTRAST) {
      const ratio = contrastRatio(colors[foreground], colors[background])
      if (ratio < minimum) errors.push(`${foreground} on ${background} has contrast ${ratio.toFixed(2)}; at least ${minimum} is required.`)
    }
    for (const [foreground, background, minimum] of WARNING_CONTRAST) {
      const ratio = contrastRatio(colors[foreground], colors[background])
      if (ratio < minimum) warnings.push(`${foreground} on ${background} has contrast ${ratio.toFixed(2)}; at least ${minimum} is recommended.`)
    }
  }

  if (errors.length > 0) return { ok: false, errors }
  const theme: Theme = { id, name, mode: mode as Theme['mode'], colors, ...(terminal ? { terminal } : {}) }
  return { ok: true, theme, warnings }
}
