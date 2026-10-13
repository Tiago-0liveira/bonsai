import { ANSI_COLORS, type AnsiColor, type HexColor, type Theme } from './tokens'

export type AnsiPalette = Record<AnsiColor, HexColor>

/**
 * The ANSI colors are explicit values, never derived from the tonal UI tokens:
 * those are all light pastels on a dark ground, so red, blue and cyan collapsed
 * into one color and "bright" did nothing. Rules: six distinct hues, at least
 * 4.5:1 against the terminal background, bright lighter than normal.
 */

/** Tuned for the Bonsai `well` (#160c07). */
export const BONSAI_ANSI: AnsiPalette = {
  black: '#46362f',
  red: '#f0625a',
  green: '#5fcf6b',
  yellow: '#e3b341',
  blue: '#5c9cf5',
  magenta: '#c77de6',
  cyan: '#3fc4d4',
  white: '#d9c5ba',
  brightBlack: '#907b71',
  brightRed: '#ff857c',
  brightGreen: '#7be584',
  brightYellow: '#ffd166',
  brightBlue: '#85b4ff',
  brightMagenta: '#da86fa',
  brightCyan: '#6fdcea',
  brightWhite: '#f5ded4',
}

/** Neutral palette for custom dark themes that carry no `terminal` colors. */
export const DARK_ANSI: AnsiPalette = {
  black: '#3a3a3a',
  red: '#f26d6d',
  green: '#5ccf7a',
  yellow: '#e5c05a',
  blue: '#66a3ff',
  magenta: '#c47df0',
  cyan: '#4cc9d9',
  white: '#d4d4d4',
  brightBlack: '#8c8c8c',
  brightRed: '#ff8585',
  brightGreen: '#7be88f',
  brightYellow: '#ffd97a',
  brightBlue: '#86b2ff',
  brightMagenta: '#da86fa',
  brightCyan: '#73dfe8',
  brightWhite: '#f2f2f2',
}

/** Neutral palette for custom light themes: dark colors, `white` stays readable text. */
export const LIGHT_ANSI: AnsiPalette = {
  black: '#1f1f1f',
  red: '#a31f19',
  green: '#19692c',
  yellow: '#7a5800',
  blue: '#1a52b3',
  magenta: '#8a2da6',
  cyan: '#09616f',
  white: '#4a4a4a',
  brightBlack: '#5c5c5c',
  brightRed: '#c3352b',
  brightGreen: '#26803c',
  brightYellow: '#8c6700',
  brightBlue: '#2d63c8',
  brightMagenta: '#a044bd',
  brightCyan: '#16768a',
  brightWhite: '#767676',
}

/** The theme's own ANSI colors, with the neutral palette for its mode filling any it leaves out. */
export function resolveAnsiPalette(theme: Theme): AnsiPalette {
  const fallback = theme.mode === 'light' ? LIGHT_ANSI : DARK_ANSI
  const palette = { ...fallback }
  for (const name of ANSI_COLORS) {
    const override = theme.terminal?.[name]
    if (override) palette[name] = override
  }
  return palette
}
