import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CommandPalette } from './CommandPalette'
import { useBonsaiStore } from '../../stores/bonsai'
import { getActiveTheme, initTheme, resetThemeStoreForTests, setActiveTheme } from '../../theme/themeStore'

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }))

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  HTMLElement.prototype.scrollIntoView = vi.fn()
  localStorage.clear()
  resetThemeStoreForTests()
  initTheme()
  useBonsaiStore.setState({ paletteOpen: true })
})
afterEach(() => { cleanup(); resetThemeStoreForTests(); vi.unstubAllGlobals() })

const enterThemes = async (query = 'theme') => {
  fireEvent.change(screen.getByPlaceholderText('Search commands and views…'), { target: { value: query } })
  const item = await screen.findByRole('option', { name: 'Change theme…' })
  fireEvent.click(item)
  return screen.findByPlaceholderText('Search themes…')
}

describe('nested theme commands', () => {
  it('enters from a theme-name alias, highlights current, filters and applies on selection', async () => {
    setActiveTheme('modern-dark')
    render(<CommandPalette />)
    const search = await enterThemes('Modern Light')
    expect(search).toHaveValue('')
    await waitFor(() => expect(screen.getByRole('option', { name: 'Modern Dark Current' })).toHaveAttribute('aria-selected', 'true'))
    fireEvent.change(search, { target: { value: 'light' } })
    const light = await screen.findByRole('option', { name: 'Modern Light' })
    expect(getActiveTheme().id).toBe('modern-dark')
    fireEvent.click(light)
    expect(getActiveTheme().id).toBe('modern-light')
    expect(useBonsaiStore.getState().paletteOpen).toBe(false)
  })

  it.each(['Escape', 'Backspace'])('%s returns to commands and restores search instead of closing', async key => {
    render(<CommandPalette />)
    const search = await enterThemes('appearance')
    fireEvent.keyDown(search, { key })
    await waitFor(() => expect(screen.getByPlaceholderText('Search commands and views…')).toHaveValue('appearance'))
    expect(useBonsaiStore.getState().paletteOpen).toBe(true)
  })

  it('keeps Backspace editing nonempty search and offers an explicit Back button', async () => {
    render(<CommandPalette />)
    const search = await enterThemes()
    fireEvent.change(search, { target: { value: 'dark' } })
    fireEvent.keyDown(search, { key: 'Backspace' })
    expect(screen.getByPlaceholderText('Search themes…')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Back to commands' }))
    await waitFor(() => expect(screen.getByPlaceholderText('Search commands and views…')).toHaveValue('theme'))
  })

  it('selecting the current theme closes without writing preferences', async () => {
    render(<CommandPalette />)
    await enterThemes()
    const save = vi.spyOn(Storage.prototype, 'setItem')
    fireEvent.click(screen.getByRole('option', { name: 'Bonsai Current' }))
    expect(useBonsaiStore.getState().paletteOpen).toBe(false)
    expect(save).not.toHaveBeenCalled()
    save.mockRestore()
  })

  it('root Escape dismisses and nested arrow navigation never applies the highlight', async () => {
    render(<CommandPalette />)
    const search = await enterThemes()
    fireEvent.keyDown(search, { key: 'ArrowDown' })
    await waitFor(() => expect(screen.getByRole('option', { name: 'Modern Light' })).toHaveAttribute('aria-selected', 'true'))
    expect(getActiveTheme().id).toBe('bonsai')
    fireEvent.keyDown(search, { key: 'Escape' })
    await waitFor(() => expect(screen.getByPlaceholderText('Search commands and views…')).toBeVisible())
    act(() => { fireEvent.keyDown(screen.getByPlaceholderText('Search commands and views…'), { key: 'Escape' }) })
    expect(useBonsaiStore.getState().paletteOpen).toBe(false)
  })
})
