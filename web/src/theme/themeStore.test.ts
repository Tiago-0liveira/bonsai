import { beforeEach, describe, expect, it, vi } from 'vitest'
import { localThemeStorage, THEME_STORAGE_KEY, type ThemeStorage } from './storage'
import { bonsaiTheme, builtInThemes } from './themes'
import { terminalTheme, terminalVariable } from './terminalColors'
import { THEME_TOKENS, TERMINAL_COLORS } from './tokens'
import type { Theme } from './tokens'
import {
  getActiveTheme, getThemes, initTheme, removeCustomTheme, reloadTheme, resetThemeStoreForTests,
  saveCustomTheme, setActiveTheme, subscribeTheme, watchThemeStorage,
} from './themeStore'

const custom = (name = 'Paper'): Theme => ({
  ...structuredClone(bonsaiTheme),
  name,
  mode: 'light',
  colors: { ...bonsaiTheme.colors, bg: '#ffffff', panel: '#f5f5f5', 'panel-2': '#eeeeee', text: '#111111', muted: '#333333', 'accent-fg': '#000000' },
})

const root = () => document.documentElement

beforeEach(() => {
  window.localStorage.clear()
  resetThemeStoreForTests()
  root().removeAttribute('style')
  root().removeAttribute('data-theme')
  document.head.innerHTML = '<meta name="theme-color" content="#000000">'
})

describe('themeStore', () => {
  it('applies the default theme through CSSOM', () => {
    initTheme(localThemeStorage)
    expect(root().style.getPropertyValue('--accent')).toBe('107 251 154')
    expect(root().style.colorScheme).toBe('dark')
    expect(root().dataset.theme).toBe('bonsai')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#1c110b')
    expect(document.querySelector('style')).toBeNull()
  })

  it('is idempotent', () => {
    const load = vi.fn(() => undefined)
    const storage: ThemeStorage = { load, save: () => true }
    initTheme(storage)
    initTheme(storage)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('applies a stored custom theme at init', () => {
    const theme = { ...custom(), id: 'custom:paper' }
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'custom:paper', custom: [theme] }))
    initTheme(localThemeStorage)
    expect(getActiveTheme().id).toBe('custom:paper')
    expect(root().style.getPropertyValue('--bg')).toBe('255 255 255')
    expect(root().style.colorScheme).toBe('light')
  })

  it('falls back to bonsai on corrupt JSON', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, '{nope')
    initTheme(localThemeStorage)
    expect(getActiveTheme().id).toBe('bonsai')
  })

  it('falls back to bonsai on an unknown active id', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'custom:ghost', custom: [] }))
    initTheme(localThemeStorage)
    expect(getActiveTheme().id).toBe('bonsai')
  })

  it('drops invalid stored custom themes', () => {
    const bad = { ...custom(), id: 'custom:bad', colors: { ...custom().colors, text: '#ffffff' } }
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'custom:bad', custom: [bad] }))
    initTheme(localThemeStorage)
    expect(getThemes().map(theme => theme.id)).toEqual(builtInThemes.map(theme => theme.id))
    expect(getActiveTheme().id).toBe('bonsai')
  })

  it('survives localStorage throwing', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota') })
    expect(() => initTheme(localThemeStorage)).not.toThrow()
    const result = saveCustomTheme(custom())
    expect(result.ok).toBe(true)
    setActiveTheme('custom:paper')
    expect(getActiveTheme().name).toBe('Paper')
    getItem.mockRestore()
    setItem.mockRestore()
  })

  it('notifies subscribers once per change and persists', () => {
    initTheme(localThemeStorage)
    saveCustomTheme(custom())
    const listener = vi.fn()
    const unsubscribe = subscribeTheme(listener)
    setActiveTheme('custom:paper')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(root().dataset.theme).toBe('custom:paper')
    setActiveTheme('custom:paper')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(JSON.parse(window.localStorage.getItem(THEME_STORAGE_KEY)!).activeId).toBe('custom:paper')
    unsubscribe()
    setActiveTheme('bonsai')
    expect(listener).toHaveBeenCalledTimes(1)
  })

  it('ignores unknown ids', () => {
    initTheme(localThemeStorage)
    setActiveTheme('nope')
    expect(getActiveTheme().id).toBe('bonsai')
  })

  it('assigns unique custom ids and never overwrites built-ins', () => {
    initTheme(localThemeStorage)
    const first = saveCustomTheme(custom('Paper'))
    const second = saveCustomTheme(custom('Paper'))
    const spoof = saveCustomTheme({ ...custom('Bonsai'), id: 'bonsai' })
    expect(first.ok && first.theme.id).toBe('custom:paper')
    expect(second.ok && second.theme.id).toBe('custom:paper-2')
    expect(spoof.ok && spoof.theme.id).toBe('custom:bonsai')
    expect(getThemes()[0]).toBe(bonsaiTheme)
  })

  it('returns validation errors without storing', () => {
    initTheme(localThemeStorage)
    const result = saveCustomTheme({ ...custom(), name: '' })
    expect(result.ok).toBe(false)
    expect(getThemes()).toHaveLength(builtInThemes.length)
  })

  it('falls back to bonsai when the active custom theme is removed', () => {
    initTheme(localThemeStorage)
    saveCustomTheme(custom())
    setActiveTheme('custom:paper')
    removeCustomTheme('custom:paper')
    expect(getActiveTheme().id).toBe('bonsai')
    expect(root().style.getPropertyValue('--bg')).toBe('28 17 11')
  })

  it('reloads when another tab changes storage', () => {
    initTheme(localThemeStorage)
    const theme = { ...custom(), id: 'custom:paper' }
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'custom:paper', custom: [theme] }))
    reloadTheme()
    expect(getActiveTheme().id).toBe('custom:paper')
  })
})

describe('built-in switching and storage events', () => {
  it.each(builtInThemes)('applies and restores every $name token and terminal override', theme => {
    initTheme()
    setActiveTheme(theme.id)
    for (const token of THEME_TOKENS) {
      const value = theme.colors[token]
      const triplet = [1, 3, 5].map(index => parseInt(value.slice(index, index + 2), 16)).join(' ')
      expect(root().style.getPropertyValue(`--${token}`)).toBe(triplet)
    }
    for (const name of TERMINAL_COLORS) expect(root().style.getPropertyValue(terminalVariable(name))).toBe(terminalTheme(theme)[name])
    expect(root().style.colorScheme).toBe(theme.mode)
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe(theme.colors.bg)
    resetThemeStoreForTests()
    initTheme()
    expect(getActiveTheme().id).toBe(theme.id)
    setActiveTheme('bonsai')
    expect(root().dataset.theme).toBe('bonsai')
  })

  it('does no CSSOM writes, persistence, or notifications for the current theme', () => {
    const save = vi.fn(() => true)
    initTheme({ load: () => undefined, save })
    const notify = vi.fn()
    const unsubscribe = subscribeTheme(notify)
    const setProperty = vi.spyOn(root().style, 'setProperty')
    setActiveTheme('bonsai')
    expect(setProperty).not.toHaveBeenCalled()
    expect(save).not.toHaveBeenCalled()
    expect(notify).not.toHaveBeenCalled()
    setProperty.mockRestore()
    unsubscribe()
  })

  it('responds to storage events, ignores other keys, and unsubscribes', () => {
    initTheme()
    const unwatch = watchThemeStorage()
    const notify = vi.fn()
    const unsubscribe = subscribeTheme(notify)
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'modern-light', custom: [] }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'unrelated' }))
    expect(getActiveTheme().id).toBe('bonsai')
    window.dispatchEvent(new StorageEvent('storage', { key: THEME_STORAGE_KEY }))
    expect(getActiveTheme().id).toBe('modern-light')
    expect(notify).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new StorageEvent('storage', { key: THEME_STORAGE_KEY }))
    expect(notify).toHaveBeenCalledTimes(1)
    window.localStorage.clear()
    window.dispatchEvent(new StorageEvent('storage', { key: null }))
    expect(getActiveTheme().id).toBe('bonsai')
    unwatch()
    window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ version: 1, activeId: 'modern-dark', custom: [] }))
    window.dispatchEvent(new StorageEvent('storage', { key: THEME_STORAGE_KEY }))
    expect(getActiveTheme().id).toBe('bonsai')
    unsubscribe()
  })

  it('applies built-ins in memory even when storage fails', () => {
    initTheme({ load: () => undefined, save: () => false })
    setActiveTheme('modern-light')
    expect(getActiveTheme().id).toBe('modern-light')
    expect(root().dataset.theme).toBe('modern-light')
  })
})

it('suppresses palette blending for one painted frame and cancels stale restoration', () => {
  const frames = new Map<number, FrameRequestCallback>()
  let id = 0
  const request = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { frames.set(++id, callback); return id })
  const cancel = vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(frame => { frames.delete(frame) })
  try {
    initTheme()
    setActiveTheme('modern-light')
    expect(root()).toHaveAttribute('data-theme-switching')
    expect(frames.size).toBe(1)
    const first = frames.get(id)!
    frames.delete(id)
    first(0)
    const stale = id
    setActiveTheme('modern-dark')
    expect(cancel).toHaveBeenCalledWith(stale)
    expect(frames.size).toBe(1)
    const next = frames.get(id)!
    frames.delete(id)
    next(16)
    const finish = frames.get(id)!
    frames.delete(id)
    finish(32)
    expect(root()).not.toHaveAttribute('data-theme-switching')
    expect(root().dataset.theme).toBe('modern-dark')
  } finally { request.mockRestore(); cancel.mockRestore() }
})
