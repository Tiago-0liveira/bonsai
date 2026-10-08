import { describe, expect, it } from 'vitest'
import { bonsaiTheme } from './themes'
import { validateTheme } from './validate'

const clone = () => structuredClone(bonsaiTheme) as unknown as Record<string, any>

describe('validateTheme', () => {
  it('accepts the built-in theme with no warnings', () => {
    const result = validateTheme(bonsaiTheme)
    expect(result).toMatchObject({ ok: true, warnings: [] })
  })

  it('rejects non-objects', () => {
    for (const input of [null, undefined, 'x', 4, []]) expect(validateTheme(input).ok).toBe(false)
  })

  it('rejects a missing token', () => {
    const theme = clone()
    delete theme.colors.faint
    const result = validateTheme(theme)
    expect(result.ok).toBe(false)
    expect(!result.ok && result.errors.join()).toContain('colors.faint')
  })

  it('rejects 3-digit hex', () => {
    const theme = clone()
    theme.colors.bg = '#fff'
    expect(validateTheme(theme).ok).toBe(false)
  })

  it('rejects text equal to bg', () => {
    const theme = clone()
    theme.colors.text = theme.colors.bg
    const result = validateTheme(theme)
    expect(!result.ok && result.errors.join()).toContain('text on bg')
  })

  it('rejects invalid mode and name length', () => {
    expect(validateTheme({ ...bonsaiTheme, mode: 'sepia' }).ok).toBe(false)
    expect(validateTheme({ ...bonsaiTheme, name: '   ' }).ok).toBe(false)
    expect(validateTheme({ ...bonsaiTheme, name: 'x'.repeat(41) }).ok).toBe(false)
  })

  it('rejects invalid terminal overrides', () => {
    expect(validateTheme({ ...bonsaiTheme, terminal: { red: 'red' } }).ok).toBe(false)
  })

  it('warns, not errors, on low muted-2 contrast', () => {
    const theme = clone()
    theme.colors['muted-2'] = theme.colors.panel
    const result = validateTheme(theme)
    expect(result.ok).toBe(true)
    expect(result.ok && result.warnings.join()).toContain('muted-2 on panel')
  })

  it('lower-cases hex, trims name, and ignores unknown keys', () => {
    const theme = clone()
    theme.name = '  Mine  '
    theme.colors.bg = '#1C110B'
    theme.extra = true
    const result = validateTheme(theme)
    expect(result.ok && result.theme.name).toBe('Mine')
    expect(result.ok && result.theme.colors.bg).toBe('#1c110b')
    expect(result.ok && 'extra' in result.theme).toBe(false)
  })
})
