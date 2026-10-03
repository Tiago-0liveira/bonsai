import { useWorkspaceStorageStatus } from './workspacePersistence'

export const PANEL_STORAGE_KEY = 'react-resizable-panels:bonsai-bottom-panels-v1'
type Layouts = Record<string, { layout: number[]; expandToSizes: Record<string, number> }>

// Retain the installed panel library's existing key/format, but own its writes
// so its deferred autosave cannot lose the last layout during navigation.
export function createPanelPreferences() {
  let layouts: Layouts | undefined
  let saved = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  let pending = false
  const load = () => {
    if (layouts) return layouts
    layouts = {}
    try {
      const raw = localStorage.getItem(PANEL_STORAGE_KEY)
      const value: unknown = raw ? JSON.parse(raw) : {}
      if (value && typeof value === 'object') for (const [key, row] of Object.entries(value)) {
        const ids = key.split(',')
        const sizes: unknown = row?.layout
        if (ids.includes('branches') && ids.includes('runtime') && ids.every(id => ['branches', 'runtime', 'files', 'prs'].includes(id)) &&
          Array.isArray(sizes) && sizes.length === ids.length && sizes.every(size => typeof size === 'number' && Number.isFinite(size) && size >= 0 && size <= 100) && Math.abs(sizes.reduce((a, b) => a + b, 0) - 100) < 0.01) {
          layouts[key] = { layout: sizes, expandToSizes: {} }
        }
      }
      saved = JSON.stringify(layouts)
    } catch {
      useWorkspaceStorageStatus.setState({ error: 'Panel preferences could not be loaded. Default sizes are in use.' })
    }
    return layouts
  }
  const flush = () => {
    clearTimeout(timer)
    timer = undefined
    if (!pending) return
    pending = false
    const payload = JSON.stringify(load())
    if (payload === saved) return
    try { localStorage.setItem(PANEL_STORAGE_KEY, payload); saved = payload } catch {
      useWorkspaceStorageStatus.setState({ error: 'Panel preferences could not be saved. Your sizes remain in memory.' })
      pending = true
    }
  }
  return {
    storage: { getItem: () => JSON.stringify(load()), setItem: () => undefined },
    remember(ids: string[], layout: number[]) {
      if (layout.length !== ids.length) return
      const key = [...ids].sort((a, b) => a.localeCompare(b)).join(',')
      const previous = load()[key]?.layout
      if (previous?.length === layout.length && layout.every((size, index) => size === previous[index])) return
      layouts![key] = { layout: [...layout], expandToSizes: {} }
      pending = true
      clearTimeout(timer)
      timer = setTimeout(flush, 180)
    },
    flush,
  }
}

export const panelPreferences = createPanelPreferences()
