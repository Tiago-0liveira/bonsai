import type { TerminalColor, Theme } from './tokens'

export function terminalTheme(theme: Theme): Record<TerminalColor, string> {
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

/** CSSOM and xterm use the same resolved palette, including custom overrides. */
export const terminalVariable = (name: TerminalColor) =>
  `--terminal-${name.replace(/[A-Z]/g, letter => `-${letter.toLowerCase()}`)}`
