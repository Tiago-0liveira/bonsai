import { describe, expect, it } from 'vitest'
import { formatCheckDuration, formatPrTime } from './prTime'

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

describe('formatCheckDuration', () => {
  const at = (seconds: number) => new Date(Date.UTC(2026, 0, 1, 10, 0, 0) + seconds * 1000).toISOString()
  const start = at(0)

  it('formats seconds, minutes and hours', () => {
    expect(formatCheckDuration(start, at(0))).toBe('0s')
    expect(formatCheckDuration(start, at(45))).toBe('45s')
    expect(formatCheckDuration(start, at(128))).toBe('2m 08s')
    expect(formatCheckDuration(start, at(3900))).toBe('1h 05m')
  })

  it('has nothing to say without both times or with nonsense', () => {
    expect(formatCheckDuration(start, undefined)).toBeUndefined()
    expect(formatCheckDuration(undefined, start)).toBeUndefined()
    expect(formatCheckDuration(null, null)).toBeUndefined()
    expect(formatCheckDuration('soon', start)).toBeUndefined()
    expect(formatCheckDuration(at(10), start)).toBeUndefined()
  })
})
