import { useBonsaiStore } from '../stores/bonsai'
import { refreshCatalog, request } from './git'

export interface ProjectRoot { id: string; path: string }
export interface ProjectRootsSettings {
  version: number
  revision: number
  roots: ProjectRoot[]
  diagnostics: { root_id: string; available: boolean; truncated: boolean; messages: string[] }[]
  suggestions: string[]
}
export async function loadProjectRoots() {
  useBonsaiStore.setState({ rootsLoading: true })
  try {
    const settings = await request<ProjectRootsSettings>('/api/settings/project-roots')
    useBonsaiStore.setState({ rootSettings: settings, rootsError: '' })
    return settings
  } catch (error) {
    useBonsaiStore.setState({ rootsError: error instanceof Error ? error.message : String(error) })
    throw error
  } finally { useBonsaiStore.setState({ rootsLoading: false }) }
}
export async function changeProjectRoot(path?: string, removeId?: string) {
  const settings = useBonsaiStore.getState().rootSettings
  if (!settings) return
  useBonsaiStore.setState({ rootsSaving: true, rootsError: '' })
  try {
    const next = await request<ProjectRootsSettings>(`/api/settings/project-roots${removeId ? `/${encodeURIComponent(removeId)}` : ''}`, { revision: settings.revision, ...(removeId ? {} : { path }) }, removeId ? 'DELETE' : 'POST')
    useBonsaiStore.setState({ rootSettings: next })
    await refreshCatalog()
  } catch (error) {
    // A stale revision requires the latest settings before the next attempt.
    const message = error instanceof Error ? error.message : String(error)
    await loadProjectRoots().catch(() => undefined)
    useBonsaiStore.setState({ rootsError: message })
    throw error
  } finally { useBonsaiStore.setState({ rootsSaving: false }) }
}
