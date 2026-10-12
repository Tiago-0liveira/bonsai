import { request } from './git'

export interface EnvironmentEditorSettings {
  environment: { id: string; label: string }
  revision: number
  editors: { id: string; name: string; command: string; folders: boolean }[]
  preferred: string
  saved_preferred: string
}

export function loadEditors() {
  return request<EnvironmentEditorSettings>('/api/settings/editors')
}

export function saveEditor(settings: EnvironmentEditorSettings, editor: string) {
  return request<EnvironmentEditorSettings>('/api/settings/editors', {
    environment_id: settings.environment.id, revision: settings.revision, editor,
  })
}

export function openFileInEditor(worktreeId: string, path: string, environmentId: string) {
  return request<{ editor: string; environment: string }>(`/api/worktrees/${encodeURIComponent(worktreeId)}/open-editor`, {
    environment_id: environmentId, path,
  })
}

/** Opens the worktree directory. `editor` overrides the default for this launch only. */
export function openFolderInEditor(worktreeId: string, environmentId: string, editor?: string) {
  return request<{ editor: string; environment: string }>(`/api/worktrees/${encodeURIComponent(worktreeId)}/open-editor`, {
    environment_id: environmentId, folder: true, ...(editor ? { editor } : {}),
  })
}
