import { describe, expect, it } from 'vitest'
import { formatPrTime } from './prTime'

// Local-time constructors keep these assertions independent of the machine's timezone.
const now = new Date(2026, 9, 9, 12, 0)

describe('formatPrTime', () => {
  it('shows the clock time for today', () => {
    expect(formatPrTime(new Date(2026, 9, 9, 7, 5).toISOString(), now)).toBe('07:05')
  })

  it('shows month and day earlier this year', () => {
    expect(formatPrTime(new Date(2026, 8, 29, 22, 15).toISOString(), now)).toBe('Sep 29')
  })

  it('adds the year for other years', () => {
    expect(formatPrTime(new Date(2025, 11, 31, 10, 0).toISOString(), now)).toBe('Dec 31, 2025')
  })

  it('passes through text that is not a date and tolerates missing values', () => {
    expect(formatPrTime('8m ago', now)).toBe('8m ago')
    expect(formatPrTime('', now)).toBe('')
    expect(formatPrTime(undefined, now)).toBe('')
    expect(formatPrTime(null, now)).toBe('')
  })

  it('defaults to the current time', () => {
    expect(formatPrTime(new Date().toISOString())).toMatch(/^\d{2}:\d{2}$/)
  })
})
