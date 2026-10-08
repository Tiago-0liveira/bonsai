import { beforeEach, describe, expect, it, vi } from 'vitest'
import { localThemeStorage, THEME_STORAGE_KEY, type ThemeStorage } from './storage'
import { bonsaiTheme } from './themes'
import type { Theme } from './tokens'
import {
  getActiveTheme, getThemes, initTheme, removeCustomTheme, reloadTheme, resetThemeStoreForTests,
  saveCustomTheme, setActiveTheme, subscribeTheme,
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
    expect(getThemes().map(theme => theme.id)).toEqual(['bonsai'])
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
    expect(getThemes()).toHaveLength(1)
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
