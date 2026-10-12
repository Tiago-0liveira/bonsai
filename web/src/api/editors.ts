import { request } from './git'

export interface EnvironmentEditorSettings {
  environment: { id: string; label: string }
  revision: number
  editors: { id: string; name: string; command: string; folders: boolean }[]
  preferred: string
  saved_preferred: string
  /** The folder browser of this environment, e.g. "Explorer" (Windows and WSL) or "Finder". */
  file_manager?: { label: string }
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

/** Reveals the worktree directory in the environment's file manager. */
export function openFolderInFileManager(worktreeId: string, environmentId: string) {
  return request<{ file_manager: string; environment: string }>(`/api/worktrees/${encodeURIComponent(worktreeId)}/open-file-manager`, {
    environment_id: environmentId,
  })
}

/** "Reveal in Explorer" / "Reveal in Finder", from the environment running Bonsai. */
export function fileManagerAction(label?: string) {
  return label === 'Explorer' || label === 'Finder' ? 'Reveal in ' + label : 'Open in file manager'
}
