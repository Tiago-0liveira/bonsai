import { getActiveTheme, subscribeTheme } from './themeStore'
import { terminalTheme } from './terminalColors'
export { terminalTheme } from './terminalColors'

/** xterm does not resolve `var(--font-mono)`, so this repeats the literal list from globals.css. */
export const TERMINAL_FONT = '"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, SFMono-Regular, Consolas, monospace'

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
    // xterm ignores assigning the current value, so switch away and back to force a cell re-measure.
    terminal.options.fontFamily = 'monospace'
    terminal.options.fontFamily = TERMINAL_FONT
    onMetrics()
  }).catch(() => { /* font loading is best effort */ })
  return () => { active = false; unsubscribe() }
}
