import type { ITheme } from '@xterm/xterm'
import { getActiveTheme, subscribeTheme } from './themeStore'
import type { Theme } from './tokens'

/** xterm does not resolve `var(--font-mono)`, so this repeats the literal list from globals.css. */
export const TERMINAL_FONT = '"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, SFMono-Regular, Consolas, monospace'

export function terminalTheme(theme: Theme): ITheme {
  const c = theme.colors
  return {
    background: c.well,
    foreground: c.text,
    cursor: c.accent,
    cursorAccent: c.well,
    selectionBackground: c.accent + '47',
    black: c.well,
    red: c.danger,
    green: c['accent-solid'],
    yellow: c.warn,
    blue: c.ok,
    magenta: c['warn-solid'],
    cyan: c.ok,
    white: c.muted,
    brightBlack: c['muted-2'],
    brightRed: c.danger,
    brightGreen: c.accent,
    brightYellow: c.warn,
    brightBlue: c.ok,
    brightMagenta: c.warn,
    brightCyan: c.ok,
    brightWhite: c.text,
    ...theme.terminal,
  }
}

interface AppearanceTarget { options: { theme?: unknown; fontFamily?: string } }

/**
 * Keeps a terminal's theme and font in sync with the active theme. Re-measures
 * once web fonts have loaded, because cell size depends on the font. Returns
 * an unsubscribe function.
 */
export function bindTerminalAppearance(terminal: AppearanceTarget, onMetrics: () => void): () => void {
  let active = true
  const apply = () => {
    terminal.options.theme = terminalTheme(getActiveTheme())
    terminal.options.fontFamily = TERMINAL_FONT
  }
  apply()
  const unsubscribe = subscribeTheme(apply)
  void document.fonts?.ready.then(() => {
    if (!active) return
    terminal.options.fontFamily = TERMINAL_FONT
    onMetrics()
  }).catch(() => { /* font loading is best effort */ })
  return () => { active = false; unsubscribe() }
}
