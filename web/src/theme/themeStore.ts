import { useSyncExternalStore } from 'react'
import { hexToTriplet } from './color'
import { localThemeStorage, type ThemeStorage } from './storage'
import { DEFAULT_THEME_ID, bonsaiTheme, builtInThemes } from './themes'
import { THEME_TOKENS, type Theme, type ThemePreferences } from './tokens'
import { validateTheme, type ThemeValidation } from './validate'

type Listener = () => void

const defaultPreferences = (): ThemePreferences => ({ version: 1, activeId: DEFAULT_THEME_ID, custom: [] })

let storage: ThemeStorage = localThemeStorage
let preferences: ThemePreferences = defaultPreferences()
let initialized = false
const listeners = new Set<Listener>()

/**
 * Applies a theme through CSSOM only. The production CSP forbids injected
 * <style> elements and inline scripts, but allows style attribute writes.
 */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement) {
  for (const token of THEME_TOKENS) root.style.setProperty(`--${token}`, hexToTriplet(theme.colors[token]))
  root.style.colorScheme = theme.mode
  root.dataset.theme = theme.id
  root.ownerDocument.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme.colors.bg)
}

export function getThemes(): Theme[] {
  return [...builtInThemes, ...preferences.custom]
}

export function getActiveTheme(): Theme {
  return getThemes().find(theme => theme.id === preferences.activeId) ?? bonsaiTheme
}

function notify() {
  for (const listener of [...listeners]) listener()
}

function commit(next: ThemePreferences, persist: boolean) {
  const previous = getActiveTheme()
  preferences = next
  const active = getActiveTheme()
  if (active !== previous) applyTheme(active)
  if (persist) storage.save(preferences)
  notify()
}

function resolve(loaded: ThemePreferences | undefined): ThemePreferences {
  const base = loaded ?? defaultPreferences()
  const known = [...builtInThemes, ...base.custom].some(theme => theme.id === base.activeId)
  return known ? base : { ...base, activeId: DEFAULT_THEME_ID }
}

/** Synchronous and idempotent. Call before the first React render. */
export function initTheme(themeStorage: ThemeStorage = localThemeStorage) {
  if (initialized && themeStorage === storage) return
  storage = themeStorage
  initialized = true
  preferences = resolve(storage.load())
  applyTheme(getActiveTheme())
  notify()
}

/** Re-reads storage, for example after another tab changed it. */
export function reloadTheme() {
  const next = resolve(storage.load())
  if (JSON.stringify(next) === JSON.stringify(preferences)) return
  commit(next, false)
}

export function setActiveTheme(id: string) {
  if (!getThemes().some(theme => theme.id === id) || id === preferences.activeId) return
  commit({ ...preferences, activeId: id }, true)
}

const slugify = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'theme'

/** Validates input (id is ignored and assigned here), stores the theme, and returns the validation result. */
export function saveCustomTheme(input: Omit<Theme, 'id'> & { id?: string }): ThemeValidation {
  const result = validateTheme({ ...input, id: 'custom:pending' })
  if (!result.ok) return result
  const taken = new Set(getThemes().map(theme => theme.id))
  const requested = input.id?.startsWith('custom:') && input.id !== 'custom:pending' ? input.id : undefined
  const replacing = requested !== undefined && preferences.custom.some(theme => theme.id === requested)
  let id = requested ?? `custom:${slugify(result.theme.name)}`
  if (!replacing) {
    const base = id
    for (let suffix = 2; taken.has(id); suffix++) id = `${base}-${suffix}`
  }
  const theme: Theme = { ...result.theme, id }
  const custom = replacing ? preferences.custom.map(existing => existing.id === id ? theme : existing) : [...preferences.custom, theme]
  const previous = getActiveTheme()
  preferences = { ...preferences, custom }
  if (getActiveTheme() !== previous || preferences.activeId === id) applyTheme(getActiveTheme())
  storage.save(preferences)
  notify()
  return { ok: true, theme, warnings: result.warnings }
}

export function removeCustomTheme(id: string) {
  if (!preferences.custom.some(theme => theme.id === id)) return
  commit({ version: 1, activeId: preferences.activeId === id ? DEFAULT_THEME_ID : preferences.activeId, custom: preferences.custom.filter(theme => theme.id !== id) }, true)
}

/** Listens for preference changes made elsewhere (another tab) through the active storage backend. */
export function watchThemeStorage(): () => void {
  return storage.subscribe?.(reloadTheme) ?? (() => {})
}

export function subscribeTheme(listener: Listener): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function useActiveTheme(): Theme {
  return useSyncExternalStore(subscribeTheme, getActiveTheme, getActiveTheme)
}

export function useThemes(): Theme[] {
  return useSyncExternalStore(subscribeTheme, getThemesSnapshot, getThemesSnapshot)
}

let themesSnapshot: Theme[] = []
function getThemesSnapshot(): Theme[] {
  const current = getThemes()
  if (themesSnapshot.length !== current.length || themesSnapshot.some((theme, index) => theme !== current[index])) themesSnapshot = current
  return themesSnapshot
}

/** Test helper: forget module state. */
export function resetThemeStoreForTests() {
  storage = localThemeStorage
  preferences = defaultPreferences()
  initialized = false
  listeners.clear()
  themesSnapshot = []
}
