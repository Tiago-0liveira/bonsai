const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const pad = (value: number) => String(value).padStart(2, '0')

/**
 * Compact local time for list rows: `22:15` today, `Sep 29` this year,
 * `Sep 29, 2025` otherwise. Text that is not a date (a relative label from an
 * older backend, or nothing) is returned unchanged.
 */
export function formatPrTime(value: string | null | undefined, now: Date = new Date()): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  if (date.toDateString() === now.toDateString()) return `${pad(date.getHours())}:${pad(date.getMinutes())}`
  const day = `${MONTHS[date.getMonth()]} ${date.getDate()}`
  return date.getFullYear() === now.getFullYear() ? day : `${day}, ${date.getFullYear()}`
}

/**
 * How long a finished check ran: `45s`, `2m 08s`, `1h 05m`. Nothing for checks
 * that have not finished or carry no (or inconsistent) times.
 */
export function formatCheckDuration(startedAt: string | null | undefined, completedAt: string | null | undefined): string | undefined {
  if (!startedAt || !completedAt) return undefined
  const ms = new Date(completedAt).getTime() - new Date(startedAt).getTime()
  if (!Number.isFinite(ms) || ms < 0) return undefined
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${pad(seconds % 60)}s`
  return `${Math.floor(minutes / 60)}h ${pad(minutes % 60)}m`
}
