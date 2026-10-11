export const THEME_TOKENS = [
  'bg', 'well', 'panel', 'panel-2', 'panel-3', 'panel-4',
  'border-subtle', 'border', 'border-strong',
  'text', 'muted', 'muted-2', 'faint',
  'accent', 'accent-solid', 'accent-fg',
  'ok', 'warn', 'warn-solid', 'danger', 'danger-solid',
] as const

export type ThemeToken = typeof THEME_TOKENS[number]

/** Validated at runtime against /^#[0-9a-f]{6}$/i. */
export type HexColor = `#${string}`

/** The 16 ANSI colors in xterm palette order (index 0-15). */
export const ANSI_COLORS = [
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const

export type AnsiColor = typeof ANSI_COLORS[number]

export const TERMINAL_COLORS = [
  ...ANSI_COLORS,
  'background', 'foreground', 'cursor', 'cursorAccent', 'selectionBackground',
] as const

export type TerminalColor = typeof TERMINAL_COLORS[number]

export interface Theme {
  /** Built-in: 'bonsai'. User themes: 'custom:<slug>'. */
  id: string
  name: string
  /** Drives color-scheme and React Flow's colorMode. */
  mode: 'dark' | 'light'
  colors: Record<ThemeToken, HexColor>
  /** Optional overrides on top of the xterm theme: ANSI colors replace the fallback palette, the rest replace the token-derived colors. */
  terminal?: Partial<Record<TerminalColor, HexColor>>
}

export interface ThemePreferences {
  version: 1
  activeId: string
  custom: Theme[]
}
