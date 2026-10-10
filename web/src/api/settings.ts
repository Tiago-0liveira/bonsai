import { useBonsaiStore } from '../stores/bonsai'
import { request } from './git'

export interface ProjectRoot { id: string; path: string }
export interface ProjectCandidate {
  id: string
  root_id: string
  name: string
  path: string
  selected: boolean
  available: boolean
}
export interface ProjectRootsSettings {
  version: number
  revision: number
  selection_revision: number
  roots: ProjectRoot[]
  diagnostics: { root_id: string; available: boolean; truncated: boolean; messages: string[] }[]
  suggestions: string[]
  repositories: ProjectCandidate[]
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
    const next = await request<ProjectRootsSettings>(
      `/api/settings/project-roots${removeId ? `/${encodeURIComponent(removeId)}` : ''}`,
      { revision: settings.revision, ...(removeId ? {} : { path }) },
      removeId ? 'DELETE' : 'POST',
    )
    useBonsaiStore.setState({ rootSettings: next })
    return next
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    await loadProjectRoots().catch(() => undefined)
    useBonsaiStore.setState({ rootsError: message })
    throw error
  } finally { useBonsaiStore.setState({ rootsSaving: false }) }
}
export async function changeProjectSelection(projectIds: string[]) {
  const settings = useBonsaiStore.getState().rootSettings
  if (!settings) return
  useBonsaiStore.setState({ rootsSaving: true, rootsError: '' })
  try {
    const next = await request<ProjectRootsSettings>('/api/settings/project-selection', {
      selection_revision: settings.selection_revision,
      project_ids: projectIds,
    })
    useBonsaiStore.setState({ rootSettings: next })
    return next
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    await loadProjectRoots().catch(() => undefined)
    useBonsaiStore.setState({ rootsError: message })
    throw error
  } finally { useBonsaiStore.setState({ rootsSaving: false }) }
}

// GET /api/settings/updates: how GitHub changes reach this Bonsai. Read-only;
// `bonsai web setup` changes it.
export type LiveHookState = 'live' | 'waiting_for_ping' | 'failing' | 'needs_admin' | 'scope_missing' | 'pending'
export interface LiveRepositoryUpdates {
  full_name: string
  state: LiveHookState
  healthy: boolean
  last_error?: string
  last_ping_at?: string
  last_delivery_at?: string
  checked_at?: string
  project_ids: string[]
}
export interface UpdateSettings {
  mode: 'standard' | 'live'
  standard_interval_seconds: number
  live: null | {
    tunnel: string
    public_host?: string
    tunnel_up: boolean
    tunnel_error?: string
    safety_poll_seconds: number
    repositories: LiveRepositoryUpdates[]
  }
}
export function loadUpdateSettings() {
  return request<UpdateSettings>('/api/settings/updates')
}
