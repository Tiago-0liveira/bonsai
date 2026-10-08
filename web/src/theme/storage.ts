import { DEFAULT_THEME_ID } from './themes'
import type { ThemePreferences } from './tokens'
import { validateTheme } from './validate'

export interface ThemeStorage {
  load(): ThemePreferences | undefined
  /** Returns false when the write failed; callers keep state in memory. */
  save(preferences: ThemePreferences): boolean
  subscribe?(onChange: () => void): () => void
}

export const THEME_STORAGE_KEY = 'bonsai-theme-v1'

/** Drops invalid custom themes and returns undefined for anything that is not a v1 preferences object. */
export function sanitizePreferences(raw: unknown): ThemePreferences | undefined {
  if (typeof raw !== 'object' || raw === null) return undefined
  const input = raw as { version?: unknown; activeId?: unknown; custom?: unknown }
  if (input.version !== 1) return undefined
  const custom = Array.isArray(input.custom)
    ? input.custom.flatMap(candidate => {
      const result = validateTheme(candidate)
      return result.ok && result.theme.id.startsWith('custom:') ? [result.theme] : []
    })
    : []
  return { version: 1, activeId: typeof input.activeId === 'string' ? input.activeId : DEFAULT_THEME_ID, custom }
}

export const localThemeStorage: ThemeStorage = {
  load() {
    try {
      const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
      return stored ? sanitizePreferences(JSON.parse(stored)) : undefined
    } catch {
      return undefined
    }
  },
  save(preferences) {
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(preferences))
      return true
    } catch {
      return false
    }
  },
  subscribe(onChange) {
    const listener = (event: StorageEvent) => { if (event.key === THEME_STORAGE_KEY || event.key === null) onChange() }
    window.addEventListener('storage', listener)
    return () => window.removeEventListener('storage', listener)
  },
}
