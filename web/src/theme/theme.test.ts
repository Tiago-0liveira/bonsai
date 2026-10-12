import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { contrastRatio, hexToTriplet } from './color'
import { bonsaiTheme, builtInThemes } from './themes'
import { validateTheme } from './validate'
import { THEME_TOKENS } from './tokens'

// Vitest blanks `?raw` CSS imports, so read the file directly (tests run from web/).
const css = readFileSync(resolve(process.cwd(), 'src/styles/globals.css'), 'utf8')
const root = css.slice(css.indexOf(':root'), css.indexOf('}', css.indexOf(':root')))

describe('theme core', () => {
  it.each(THEME_TOKENS)('globals.css :root matches bonsaiTheme for --%s', token => {
    expect(root).toContain(`--${token}: ${hexToTriplet(bonsaiTheme.colors[token])};`)
  })

  it.each(builtInThemes)('$name defines every token and passes contrast validation', theme => {
    expect(Object.keys(theme.colors).sort()).toEqual([...THEME_TOKENS].sort())
    expect(validateTheme(theme)).toMatchObject({ ok: true, warnings: [] })
  })

  it('keeps stable built-in ordering and IDs', () => {
    expect(builtInThemes.map(theme => theme.id)).toEqual(['bonsai', 'modern-light', 'modern-dark'])
  })

  it('converts hex to an rgb triplet', () => {
    expect(hexToTriplet('#1c110b')).toBe('28 17 11')
  })

  it('computes WCAG contrast', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio(bonsaiTheme.colors.text, bonsaiTheme.colors.bg)).toBeCloseTo(14.35, 1)
    expect(contrastRatio(bonsaiTheme.colors['accent-fg'], bonsaiTheme.colors['accent-solid'])).toBeCloseTo(9.86, 1)
  })
})
