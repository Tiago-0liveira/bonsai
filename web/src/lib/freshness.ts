import type { SyncFreshness } from '../types'

type FreshnessView = Pick<SyncFreshness, 'state' | 'source'> | undefined

// Stale data stays on screen, labeled, until its own component revalidates.
// Data restored from the API's warm-start cache ("disk") is stale by definition.
export function isStale(value: FreshnessView): boolean {
  return value?.state === 'stale' || value?.source === 'disk'
}

export function staleLabel(value: FreshnessView): string {
  return value?.source === 'disk' ? 'saved' : 'stale'
}

export function staleDescription(value: FreshnessView): string {
  return value?.source === 'disk' ? 'Saved from the last session; refreshing' : 'Not confirmed recently; refreshing'
}
