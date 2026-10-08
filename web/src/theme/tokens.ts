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

export const TERMINAL_COLORS = [
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
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
  /** Optional ANSI overrides on top of the token-derived xterm theme. */
  terminal?: Partial<Record<TerminalColor, HexColor>>
}

export interface ThemePreferences {
  version: 1
  activeId: string
  custom: Theme[]
}
