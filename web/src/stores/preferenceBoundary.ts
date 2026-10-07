import type { StoreApi } from 'zustand'
import type { BonsaiState } from './bonsai'
import { workspaceStorage, mergeWorkspacePreferences, sanitizeWorkspacePreferences, WORKSPACE_STORAGE_KEY, WORKSPACE_STORAGE_VERSION, type WorkspacePreferences } from './workspacePersistence'
import { panelPreferences } from './panelPreferences'

export const PREFERENCE_KEYS = [
  'selection', 'activeWorkspaceId', 'activeProjectId', 'sidebarCollapsed', 'dockState', 'dockHeight', 'activeDockTab', 'dockWorktreeId',
  'collapsedBranchIds', 'rightPanels', 'selectedFilePath', 'editorPreference', 'nodePlacements', 'viewport', 'boardItems', 'boardLists',
  'boardPriorities', 'boardTypes', 'collapsedTagGroups', 'detachedStackWorktreeIds', 'expandedAutomaticGroups',
  'terminalViewPreferences',
] as const satisfies readonly (keyof WorkspacePreferences)[]
const TRANSIENT_KEYS = new Set<string>(['dockHeight', 'nodePlacements', 'viewport'])
export const PREFERENCE_WRITE_DELAY = 180

// The live store has no persistence middleware. Only changes to these owned
// fields reach serialization/storage; transport updates never invoke the writer.
export function createPreferenceBoundary(store: StoreApi<BonsaiState>) {
  let hydrating = false
  let pending = false
  let timer: ReturnType<typeof setTimeout> | undefined
  let saved = ''
  const serialize = () => JSON.stringify(sanitizeWorkspacePreferences(store.getState()))
  const cancel = () => { clearTimeout(timer); timer = undefined; pending = false }
  const flush = () => {
    panelPreferences.flush()
    if (!pending) return
    cancel()
    const state = sanitizeWorkspacePreferences(store.getState())
    const payload = JSON.stringify(state)
    if (payload === saved) return
    if (workspaceStorage.setItem(WORKSPACE_STORAGE_KEY, { state, version: WORKSPACE_STORAGE_VERSION })) saved = payload
    // Keep failed saves eligible for an explicit or lifecycle flush. Do not
    // schedule retries on live updates or spin while storage is unavailable.
    else pending = true
  }
  const rehydrate = () => {
    cancel()
    hydrating = true
    try {
      const value = workspaceStorage.getItem(WORKSPACE_STORAGE_KEY)
      // This adapter is synchronous: legacy cleanup precedes every state merge.
      if (value && 'then' in value) throw new Error('Workspace hydration must be synchronous')
      store.setState(mergeWorkspacePreferences(value?.state ?? {}, store.getState()), true)
      saved = serialize()
    } finally { hydrating = false }
  }
  const unsubscribe = store.subscribe((state, previous) => {
    if (hydrating) return
    const changed = PREFERENCE_KEYS.filter(key => state[key] !== previous[key])
    if (!changed.length) return
    pending = true
    if (changed.every(key => TRANSIENT_KEYS.has(key))) {
      clearTimeout(timer)
      timer = setTimeout(flush, PREFERENCE_WRITE_DELAY)
    } else flush()
  })
  rehydrate()
  return {
    flush,
    rehydrate,
    cancel,
    attachLifecycle() {
      const onVisibility = () => { if (document.visibilityState === 'hidden') flush() }
      window.addEventListener('pagehide', flush)
      window.addEventListener('beforeunload', flush)
      document.addEventListener('visibilitychange', onVisibility)
      return () => {
        flush()
        window.removeEventListener('pagehide', flush)
        window.removeEventListener('beforeunload', flush)
        document.removeEventListener('visibilitychange', onVisibility)
      }
    },
    dispose() { flush(); unsubscribe() },
  }
}
