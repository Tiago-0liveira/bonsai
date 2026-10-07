import { localFetch } from './localClient'
import type { ProcessSummary } from './git'
import { mapProcess } from './snapshotReconciliation'
import { useBonsaiStore } from '../stores/bonsai'
import { openRuntimePatch } from '../stores/runtimePreferences'
import { changedPatch } from '../stores/reconciliation'

export interface ProcessArgument {
  id: string
  name?: string
  description?: string
  kind: 'flag' | 'positional' | 'passthrough'
  type: string
  flags?: string[]
  position?: number
  required?: boolean
  variadic?: boolean
  default?: string
  choices?: string[]
}

export interface ProcessCommand {
  id: string
  name: string
  description?: string
  provider: string
  project_root?: string
  raw?: string
  args?: ProcessArgument[]
  invocation: { program: string; prefix?: string[]; working_dir: string; pass_through?: string }
}

export interface ProcessPolicy { mode: 'no' | 'on-failure' | 'always'; max_restarts: number }

export interface ProcessCatalog {
  default_policies?: Record<string, ProcessPolicy>
  location: { input_dir: string }
  providers: { id: string; name: string; root: string }[] | null
  commands: ProcessCommand[] | null
  warnings?: { message: string }[]
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await localFetch(path, init)
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error?.message ?? `Process request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}

export const processCommands = (projectId: string, worktreeId: string) => request<ProcessCatalog>(
  `/api/projects/${encodeURIComponent(projectId)}/process-commands?worktree_id=${encodeURIComponent(worktreeId)}`,
)

export const startProcess = (projectId: string, worktreeId: string, commandId: string, values: Record<string, string[]>, policy?: ProcessPolicy) => request<ProcessSummary>(
  `/api/projects/${encodeURIComponent(projectId)}/processes`,
  { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ worktree_id: worktreeId, command_id: commandId, values, ...(policy ? { policy } : {}) }) },
)

export function applyProcessSummary(summary: ProcessSummary, open = false) {
  useBonsaiStore.setState(state => {
    const old = state.processes.find(p => p.id === summary.id)
    if (!open && old && summary.revision !== undefined && (old.revision ?? 0) >= summary.revision) return state
    const process = old && (old.revision ?? 0) > (summary.revision ?? 0) ? old : { ...mapProcess(summary), pendingSnapshot: old?.pendingSnapshot ?? !old }
    const processes = old ? state.processes.map(p => p.id === process.id ? process : p) : [...state.processes, process]
    if (!open) {
      const patch = changedPatch(state, { processes })
      return Object.keys(patch).length ? patch : state
    }
    const active = state.activeProjectId === process.projectId
    const viewPatch = openRuntimePatch({ ...state, processes }, process.id, active)
    const reveal = !old || state.selection.type !== 'process' || state.selection.id !== process.id || Object.keys(viewPatch).length > 0
    const patch = changedPatch(state, {
      processes,
      ...viewPatch,
      ...(active ? {
        selection: { type: 'process' as const, id: process.id },
        dockState: state.dockState === 'collapsed' ? 'normal' as const : state.dockState,
        ...(reveal ? { canvasReveal: { projectId: process.projectId, nodeId: process.id, nonce: state.canvasReveal.nonce + 1 } } : {}),
      } : { notice: 'Process started: ' + process.name }),
    })
    return Object.keys(patch).length ? patch : state
  })
}

export const restartProcess = (project: string, id: number) => request<ProcessSummary>(
  `/api/projects/${encodeURIComponent(project)}/processes/${id}/restart`, { method: 'POST' },
).then(summary => { applyProcessSummary(summary); return summary })
export const stopProcess = (project: string, id: number) => request(
  `/api/projects/${encodeURIComponent(project)}/processes/${id}`, { method: 'DELETE' },
)
export const processHistory = (project: string, id: number) => request<{ content: string }>(
  `/api/projects/${encodeURIComponent(project)}/processes/${id}/logs?n=0`,
)
export const previewProcess = (project: string, worktree: string, command: string, values: Record<string, string[]>) => request<{ program: string; args: string[]; dir: string; default_policy: ProcessPolicy }>(
  `/api/projects/${encodeURIComponent(project)}/process-preview`,
  { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ worktree_id: worktree, command_id: command, values }) },
)
