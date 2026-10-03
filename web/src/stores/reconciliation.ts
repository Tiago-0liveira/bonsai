// JSON-shaped application data only. Retain equal nested values as well as the
// outer entity; timestamps alone never establish content equality.
export function shareEqual<T>(previous: T, next: T): T {
  if (Object.is(previous, next)) return previous
  if (!previous || !next || typeof previous !== 'object' || typeof next !== 'object') return next
  if (Array.isArray(previous) !== Array.isArray(next)) return next
  const before = previous as Record<string, unknown>
  const after = next as Record<string, unknown>
  if (Object.hasOwn(before, 'id') && Object.hasOwn(after, 'id') && before.id !== after.id) return next
  const keys = Object.keys(after)
  let equal = keys.length === Object.keys(before).length
  const shared: Record<string, unknown> = Array.isArray(next) ? [] as unknown as Record<string, unknown> : {}
  for (const key of keys) {
    shared[key] = shareEqual(before[key], after[key])
    if (!Object.hasOwn(before, key) || shared[key] !== before[key]) equal = false
  }
  return equal ? previous : shared as T
}

export function reconcileById<T extends { id: string }>(previous: T[], next: T[]): T[] {
  const byId = new Map(previous.map(value => [value.id, value]))
  const shared = next.map(value => {
    const before = byId.get(value.id)
    return before ? shareEqual(before, value) : value
  })
  return shared.length === previous.length && shared.every((value, index) => value === previous[index]) ? previous : shared
}

// Replacing one project does not move the other projects to new positions.
export function replaceScope<T extends { id: string }>(previous: T[], next: T[], belongs: (value: T) => boolean): T[] {
  const scoped = reconcileById(previous.filter(belongs), next)
  let index = 0
  const result = previous.flatMap(value => belongs(value) ? index < scoped.length ? [scoped[index++]] : [] : [value])
  result.push(...scoped.slice(index))
  return result.length === previous.length && result.every((value, index) => value === previous[index]) ? previous : result
}

export function changedPatch<T extends object>(state: T, candidate: Partial<T>): Partial<T> {
  const patch: Partial<T> = {}
  for (const key of Object.keys(candidate) as (keyof T)[]) {
    const value = shareEqual(state[key], candidate[key])
    if (value !== state[key]) patch[key] = value
  }
  return patch
}
