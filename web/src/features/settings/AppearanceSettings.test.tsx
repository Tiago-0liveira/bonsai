import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppearanceSettings } from './AppearanceSettings'
import { SettingsPage } from './SettingsPage'
import { getActiveTheme, initTheme, resetThemeStoreForTests, saveCustomTheme } from '../../theme/themeStore'
import { bonsaiTheme } from '../../theme/themes'
import { THEME_STORAGE_KEY } from '../../theme/storage'

vi.mock('./EnvironmentEditorSettings', () => ({ EnvironmentEditorSettings: () => null }))
vi.mock('./ProjectRootsSettings', () => ({ ProjectRootsSettings: () => null }))
vi.mock('./LiveUpdatesSettings', () => ({ LiveUpdatesSettings: () => null }))

beforeEach(() => { localStorage.clear(); resetThemeStoreForTests(); initTheme() })
afterEach(() => { cleanup(); resetThemeStoreForTests() })

describe('Appearance settings', () => {
  it('offers all themes even without a selected project', () => {
    render(<SettingsPage />)
    expect(screen.getByRole('heading', { name: 'Appearance' })).toBeVisible()
    expect(screen.getAllByRole('radio').map(input => input.getAttribute('aria-label'))).toEqual(['Bonsai', 'Modern Light', 'Modern Dark'])
    expect(screen.getByRole('radio', { name: 'Bonsai' })).toBeChecked()
    expect(screen.getByText('Default')).toBeVisible()
  })

  it('applies and persists immediately without applying on hover or focus', () => {
    render(<AppearanceSettings />)
    const light = screen.getByRole('radio', { name: 'Modern Light' })
    fireEvent.mouseEnter(light.closest('label')!)
    light.focus()
    expect(getActiveTheme().id).toBe('bonsai')
    fireEvent.click(light)
    expect(light).toBeChecked()
    expect(document.documentElement.dataset.theme).toBe('modern-light')
    expect(JSON.parse(localStorage.getItem(THEME_STORAGE_KEY)!).activeId).toBe('modern-light')
    fireEvent.click(screen.getByRole('radio', { name: 'Bonsai' }))
    expect(getActiveTheme().id).toBe('bonsai')
  })

  it('shows custom themes after built-ins and keeps them selectable', () => {
    render(<AppearanceSettings />)
    act(() => { saveCustomTheme({ ...bonsaiTheme, name: 'My theme' }) })
    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(4)
    expect(radios[3]).toHaveAccessibleName('My theme')
    fireEvent.click(radios[3])
    expect(getActiveTheme().id).toBe('custom:my-theme')
  })
})
