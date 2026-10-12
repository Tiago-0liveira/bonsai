import { startAgent, stopAgent, mapAgent, isLiveAgent, providerIdFor } from '../api/agents'
import type { ProjectRootsSettings } from '../api/settings'
import { loadEditors, saveEditor, openFileInEditor, openFolderInEditor, type EnvironmentEditorSettings } from '../api/editors'
import { create } from 'zustand'
import { SHELL_UNAVAILABLE, ENV_UNAVAILABLE } from './execution'
import { createPreferenceBoundary } from './preferenceBoundary'
import { closeRuntimePatch, focusRuntimePatch, openRuntimePatch, reorderRuntimePatch, switchRuntimeScope } from './runtimePreferences'
import { changedPatch } from './reconciliation'
import { localCommand, createWorktree, changePullRequest, reviewPullRequest, updateMetadata, report, type Branch, type BranchCandidate, type RepositorySync, type WorktreeGroup } from '../api/git'
import type {
  Agent,
  AgentState,
  CanvasPreferences,
  CreateWorktreeInput,
  DockPanelKey,
  DockState,
  DockTab,
  EnvVariable,
  NodePlacement,
  Project,
  Process,
  PullRequest,
  Selection,
  StartAgentInput,
  SyncFreshness,
  TerminalViewPreferences,
  ViewportState,
  Worktree,
} from '../types'
import { DEFAULT_CANVAS_PREFERENCES } from '../types'

interface TerminalSession {
  id: string
  label: string
  agentId?: string
}

export interface BonsaiState {
  rootSettings: ProjectRootsSettings | null
  rootsLoading: boolean
  rootsSaving: boolean
  rootsError: string
  selection: Selection
  setSelection: (selection: Selection) => void
  projectQuery: string
  setProjectQuery: (query: string) => void

  branchCandidates: Record<string, BranchCandidate[]>
  worktreeGroups: Record<string, WorktreeGroup[]>
  repositorySync: Record<string, RepositorySync>
  expandedAutomaticGroups: string[]
  toggleAutomaticGroup: (id: string) => void
  gitBranches: Record<string, Branch[]>
  gitOnline: Record<string, boolean>
  gitRevision: number
  gitError: string
  syncFreshness: Record<string, Record<string, SyncFreshness>>
  processes: Process[]
  processAuthorityReady: Record<string, boolean>
  processVisibility: Record<string, { cutoffs: Record<string, number>; deleted: Record<string, boolean> }>
  visitOpenedRuntimeIds: string[]
  projects: Project[]
  activeWorkspaceId: string
  activeProjectId: string
  setActiveWorkspace: (id: string) => void
  setActiveProject: (id: string) => void
  createMockProject: (name?: string) => void

  sidebarCollapsed: boolean
  toggleSidebar: () => void

  worktrees: Worktree[]
  agents: Agent[]
  detachedStackWorktreeIds: string[]
  ejectWorktreeFromStack: (id: string) => void
  setWorktreeStackPreference: (id: string, preference: 'auto' | 'never') => void
  setWorktreeMergeTarget: (id: string, branch: string) => void

  worktreeDialogOpen: boolean
  setWorktreeDialogOpen: (open: boolean) => void
  worktreeDialogTarget: { projectId: string; sourceType?: CreateWorktreeInput['sourceType']; sourceRef?: string; branchName?: string } | null
  openCreateWorktree: (projectId: string, candidate?: BranchCandidate) => void
  deleteWorktreeId: string
  setDeleteWorktreeId: (id: string) => void
  createWorktree: typeof createWorktree

  startAgentDialogOpen: boolean
  startProcessDialogOpen: boolean
  startProcessTargetWorktreeId: string
  startProcessTargetProjectId: string
  openStartProcessDialog: (worktreeId?: string, projectId?: string) => void
  setStartProcessDialogOpen: (open: boolean) => void
  startAgentTargetWorktreeId: string
  openStartAgentDialog: (worktreeId?: string) => void
  setStartAgentDialogOpen: (open: boolean) => void
  createAgent: (input: StartAgentInput) => Promise<void>
  startMockAgent: () => void
  moveAgentToHistory: (id: string) => void
  restoreAgentFromHistory: (id: string) => void
  archiveAgent: (id: string) => void
  restoreAgent: (id: string) => void
  setAgentState: (id: string, state: AgentState) => void

  envEditorOpen: boolean
  setEnvEditorOpen: (open: boolean) => void
  envVariables: Record<string, EnvVariable[]>
  addEnvVariable: (projectId: string) => void
  updateEnvVariable: (projectId: string, id: string, patch: Partial<Pick<EnvVariable, 'key' | 'value' | 'secret'>>) => void
  removeEnvVariable: (projectId: string, id: string) => void

  dockState: DockState
  setDockState: (state: DockState) => void
  dockHeight: number
  setDockHeight: (height: number) => void
  activeDockTab: DockTab
  setActiveDockTab: (tab: DockTab) => void
  dockWorktreeId: string
  setDockWorktreeId: (id: string) => void
  dockRuntimeId: string
  setDockRuntimeId: (id: string) => void
  openRuntimeIds: string[]
  terminalViewPreferences: TerminalViewPreferences
  focusRuntime: (id: string) => void
  openRuntime: (id: string) => void
  closeRuntime: (id: string) => void
  reorderOpenRuntime: (activeId: string, overId: string) => void
  collapsedBranchIds: string[]
  toggleBranchCollapsed: (id: string) => void
  rightPanels: Record<DockPanelKey, boolean>
  toggleRightPanel: (panel: DockPanelKey) => void
  setRightPanel: (panel: DockPanelKey, open: boolean) => void

  selectedFilePath: string
  setSelectedFilePath: (path: string) => void
  editorSettings: EnvironmentEditorSettings | null
  editorSettingsLoading: boolean
  editorSettingsSaving: boolean
  editorSettingsError: string
  loadEditorSettings: () => Promise<EnvironmentEditorSettings>
  setEnvironmentEditor: (editor: string) => Promise<void>
  editorPromptOpen: boolean
  pendingOpenFile: { worktreeId: string; path: string; folder?: boolean } | null
  requestOpenFile: (path: string) => Promise<void>
  requestOpenFolder: (worktreeId: string) => Promise<void>
  chooseFileEditor: (editor: string) => Promise<void>
  closeEditorPrompt: () => void

  pullRequests: PullRequest[]
  inspectedPullRequestId: string | null
  pullRequestFocusNonce: number
  setInspectedPullRequestId: (id: string | null) => void
  inspectPullRequest: (id: string) => void
  setPullRequestStatus: (id: string, status: PullRequest['status']) => void
  addPullRequestReview: (id: string, body: string, kind: 'comment' | 'approve' | 'request-changes') => void

  nodePlacements: Record<string, NodePlacement>
  setManualNodePlacement: (id: string, position: { x: number; y: number }) => void
  setManualNodePlacements: (positions: Record<string, { x: number; y: number }>) => void
  setGeneratedNodePlacements: (positions: Record<string, { x: number; y: number }>) => void
  removeNodePlacement: (id: string) => void
  removeNodePlacements: (ids: string[]) => void
  subtreeMoveRootId: string | null
  setSubtreeMoveRoot: (id: string | null) => void
  viewport: ViewportState
  setViewport: (viewport: ViewportState) => void
  expandedHistoryWorktreeIds: string[]
  toggleWorktreeHistory: (id: string) => void
  canvasCommand: { type: 'fit' | 'layout'; nonce: number }
  /** `reveal` scrolls a node into view only when hidden; `center` always centres it. `nodeId` may be an entity id. */
  canvasReveal: { projectId: string; nodeId: string; nonce: number; mode?: 'reveal' | 'center' }
  requestCanvasAction: (type: 'fit' | 'layout') => void
  requestCanvasFocus: (projectId: string, entityId: string) => void
  canvasPreferences: CanvasPreferences
  setCanvasPreferences: (patch: Partial<CanvasPreferences>) => void

  paletteOpen: boolean
  setPaletteOpen: (open: boolean) => void
  notice: string
  setNotice: (notice: string) => void

  terminalSessions: TerminalSession[]
  activeTerminalId: string
  terminalOutput: Record<string, string[]>
  openTerminal: (agentId?: string) => void
  setActiveTerminalId: (id: string) => void
  appendTerminalCommand: (command: string) => void
}

function uniqueAdd(items: string[], id: string) {
  return items.includes(id) ? items : [...items, id]
}

function withoutPlacements(placements: Record<string, NodePlacement>, ids: string[]) {
  const next = { ...placements }
  ids.forEach((id) => delete next[id])
  return next
}

export const useBonsaiStore = create<BonsaiState>()(
    (set, get) => ({
      selection: { type: 'project', id: '' },
      setSelection: (selection) => {
        const state = get()
        let scope: Partial<BonsaiState> = {}
        if (selection.type === 'worktree') {
          const tree = state.worktrees.find(item => item.id === selection.id)
          scope = switchRuntimeScope(state, tree?.projectId ?? state.activeProjectId, selection.id)
        }
        if (selection.type === 'agent' || selection.type === 'process') {
          scope = openRuntimePatch(state, selection.id)
        }
        const patch = changedPatch(state, { ...scope, selection, dockState: 'normal' })
        if (Object.keys(patch).length) set(patch)
      },
      projectQuery: '',
      setProjectQuery: (projectQuery) => set({ projectQuery }),

      branchCandidates: {},
      worktreeGroups: {},
      repositorySync: {},
      expandedAutomaticGroups: [],
      toggleAutomaticGroup: (id) => set(state => {
        const memberIds = new Set(Object.values(state.worktreeGroups).flat().filter(group => group.id === id).flatMap(group => group.worktree_ids))
        return {
          expandedAutomaticGroups: state.expandedAutomaticGroups.includes(id) ? state.expandedAutomaticGroups.filter(value => value !== id) : [...state.expandedAutomaticGroups, id],
          detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter(worktreeId => !memberIds.has(worktreeId)),
        }
      }),
      gitBranches: {},
      gitOnline: {},
      gitRevision: 0,
      gitError: '',
      syncFreshness: {},
      processes: [],
      processAuthorityReady: {},
      processVisibility: {},
      visitOpenedRuntimeIds: [],
      rootSettings: null,
      rootsLoading: false,
      rootsSaving: false,
      rootsError: '',
      projects: [],
      activeWorkspaceId: '',
      activeProjectId: '',
      setActiveWorkspace: (activeWorkspaceId) => {
        const state = get()
        const nextProject = state.projects.find(project => project.id === state.activeProjectId && project.workspaceId === activeWorkspaceId)
          ?? state.projects.find(project => project.workspaceId === activeWorkspaceId)
        set({
          ...(nextProject ? switchRuntimeScope(state, nextProject.id) : {}),
          activeWorkspaceId,
          selection: nextProject ? { type: 'project', id: nextProject.id } : state.selection,
          notice: nextProject ? 'Switched workspace to ' + activeWorkspaceId : 'Workspace selected',
        })
      },
      setActiveProject: (activeProjectId) => {
        const state = get()
        const project = state.projects.find((item) => item.id === activeProjectId)
        if (!project) return
        set({
          ...switchRuntimeScope(state, project.id),
          selection: { type: 'project', id: project.id },
          notice: 'Opened project ' + project.name,
        })
      },
      createMockProject: () => set({ notice: 'Add repositories in the server configuration, then enroll a device.' }),

      sidebarCollapsed: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      worktrees: [],
      agents: [],
      detachedStackWorktreeIds: [],
      ejectWorktreeFromStack: (id) =>
        set((state) => ({
          detachedStackWorktreeIds: uniqueAdd(state.detachedStackWorktreeIds, id),
          notice: 'Detached worktree from this stack until the group is toggled',
        })),
      setWorktreeStackPreference: (id, stackPreference) => { void updateMetadata(id, { stack_preference: stackPreference }).catch(report) },
      setWorktreeMergeTarget: (id, branch) => {
        const state = get()
        const worktree = state.worktrees.find((item) => item.id === id)
        const project = state.projects.find((item) => item.id === worktree?.projectId)
        if (!worktree || !project) return
        if (branch === worktree.branch) {
          set({ notice: 'A worktree cannot merge into itself' })
          return
        }
        let cursor = branch
        const visited = new Set<string>()
        while (cursor !== project.defaultBranch && !visited.has(cursor)) {
          visited.add(cursor)
          if (cursor === worktree.branch) {
            set({ notice: 'That merge target would create a branch cycle' })
            return
          }
          const parent = state.worktrees.find((item) => item.projectId === project.id && item.branch === cursor)
          if (!parent) break
          cursor = parent.mergeTargetBranch
        }
        void updateMetadata(id, { merge_target_branch: branch }).catch(report)
      },

      worktreeDialogOpen: false,
      worktreeDialogTarget: null,
      setWorktreeDialogOpen: (worktreeDialogOpen) => set({ worktreeDialogOpen, worktreeDialogTarget: worktreeDialogOpen ? { projectId: get().activeProjectId } : null }),
      openCreateWorktree: (projectId, candidate) => set({ worktreeDialogOpen: true, worktreeDialogTarget: { projectId, sourceType: candidate ? (candidate.creation_mode === 'existing' ? 'existing' : 'origin') : undefined, sourceRef: candidate?.source_ref, branchName: candidate ? (candidate.local_branch || candidate.name) : undefined } }),
      deleteWorktreeId: '',
      setDeleteWorktreeId: (deleteWorktreeId) => set({ deleteWorktreeId }),
      createWorktree,

      startAgentDialogOpen: false,
      startProcessDialogOpen: false,
      startProcessTargetWorktreeId: '',
      startProcessTargetProjectId: '',
      openStartProcessDialog: (worktreeId = '', projectId) => set(state => ({ startProcessDialogOpen: true, startProcessTargetWorktreeId: worktreeId, startProcessTargetProjectId: projectId ?? state.worktrees.find(tree => tree.id === worktreeId)?.projectId ?? state.activeProjectId })),
      setStartProcessDialogOpen: (startProcessDialogOpen) => set({ startProcessDialogOpen }),
      startAgentTargetWorktreeId: '',
      openStartAgentDialog: (worktreeId = '') => set({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktreeId }),
      setStartAgentDialogOpen: (startAgentDialogOpen) => set({ startAgentDialogOpen }),
      createAgent: async (input) => {
        const tree = get().worktrees.find(w => w.id === input.worktreeId)
        const providerId = providerIdFor(input.provider)
        if (!tree || !providerId || !input.accountId) throw new Error('Select a profile and worktree.')
        // Launch options come from the provider's launch fields; the API rejects the ones it does not support.
        const result = await startAgent(tree.projectId, { worktree_id: tree.id, account_id: input.accountId, name: input.name, model: input.model, prompt: input.prompt, options: input.options, cols: 80, rows: 24 }, input.requestKey ?? crypto.randomUUID())
        if (!result.id) throw new Error(result.error || 'Start was interrupted. Close this dialog and start again.')
        set(state => {
          const agents = state.agents.some(a => a.id === result.id) ? state.agents : [...state.agents, mapAgent(result)]
          const active = state.activeProjectId === tree.projectId
          return {
            agents, worktrees: state.worktrees.map(w => w.id === tree.id ? { ...w, agentIds: uniqueAdd(w.agentIds, result.id) } : w), startAgentDialogOpen: false,
            ...openRuntimePatch({ ...state, agents }, result.id, active),
            ...(active ? { selection: { type: 'agent' as const, id: result.id }, dockState: 'normal' as const } : { notice: 'Agent started in ' + tree.branch }),
          }
        })
      },
      startMockAgent: () => {
        const state = get()
        const target =
          state.selection.type === 'worktree'
            ? state.selection.id
            : state.selection.type === 'agent'
              ? state.agents.find((item) => item.id === state.selection.id)?.worktreeId ?? ''
              : ''
        set({ startAgentDialogOpen: true, startAgentTargetWorktreeId: target })
      },
      moveAgentToHistory: (id) =>
        set((state) => ({
          agents: state.agents.map((agent) =>
            agent.id === id
              ? { ...agent, presentation: 'history', archived: false }
              : agent,
          ),
          ...closeRuntimePatch(state, id),
          nodePlacements: withoutPlacements(state.nodePlacements, [id]),
          notice: 'Agent moved to history',
        })),
      restoreAgentFromHistory: (id) =>
        set((state) => ({
          agents: state.agents.map((agent) => agent.id === id ? { ...agent, presentation: 'canvas', archived: false } : agent),
          notice: 'Agent restored to canvas',
        })),
      archiveAgent: (id) =>
        set((state) => ({
          agents: state.agents.map((agent) =>
            agent.id === id
              ? { ...agent, presentation: 'archived', archived: true }
              : agent,
          ),
          ...closeRuntimePatch(state, id),
          nodePlacements: withoutPlacements(state.nodePlacements, [id]),
          notice: 'Agent archived',
        })),
      restoreAgent: (id) =>
        set((state) => ({
          agents: state.agents.map((agent) => agent.id === id ? { ...agent, presentation: 'canvas', archived: false } : agent),
          notice: 'Agent restored',
        })),
      setAgentState: (id, next) => {
        const agent = get().agents.find(a => a.id === id)
        const project = agent?.projectId ?? get().worktrees.find(w => w.id === agent?.worktreeId)?.projectId
        if (project && isLiveAgent(agent) && next === 'finished') void stopAgent(project, id, `stop-${id}`).catch(report)
        else set({ notice: 'This agent action is unavailable.' })
      },

      envEditorOpen: false,
      setEnvEditorOpen: (envEditorOpen) => set({ envEditorOpen }),
      envVariables: {},
      addEnvVariable: () => set({ notice: ENV_UNAVAILABLE }),
      updateEnvVariable: () => set({ notice: ENV_UNAVAILABLE }),
      removeEnvVariable: () => set({ notice: ENV_UNAVAILABLE }),

      dockState: 'normal',
      setDockState: (dockState) => set((state) => state.dockState === dockState ? state : { dockState }),
      dockHeight: 30,
      setDockHeight: (height) => set((state) => {
        if (!Number.isFinite(height)) return state
        const dockHeight = Math.min(72, Math.max(14, height))
        return Math.abs(state.dockHeight - dockHeight) < 0.01 ? state : { dockHeight }
      }),
      activeDockTab: 'terminal',
      setActiveDockTab: (activeDockTab) => set({ activeDockTab }),
      dockWorktreeId: '',
      setDockWorktreeId: (dockWorktreeId) =>
        set((state) => {
          const patch = switchRuntimeScope(state, state.activeProjectId, dockWorktreeId)
          return Object.keys(patch).length ? patch : state
        }),
      dockRuntimeId: '',
      setDockRuntimeId: (id) => get().focusRuntime(id),
      openRuntimeIds: [],
      terminalViewPreferences: {},
      focusRuntime: (id) => set(state => { const patch = focusRuntimePatch(state, id); return Object.keys(patch).length ? patch : state }),
      openRuntime: (id) => set(state => { const patch = openRuntimePatch(state, id); return Object.keys(patch).length ? patch : state }),
      closeRuntime: (id) => set(state => { const patch = closeRuntimePatch(state, id); return Object.keys(patch).length ? patch : state }),
      reorderOpenRuntime: (id, overId) => set(state => { const patch = reorderRuntimePatch(state, id, overId); return Object.keys(patch).length ? patch : state }),
      collapsedBranchIds: [],
      toggleBranchCollapsed: (id) =>
        set((state) => ({
          collapsedBranchIds: state.collapsedBranchIds.includes(id)
            ? state.collapsedBranchIds.filter((item) => item !== id)
            : [...state.collapsedBranchIds, id],
        })),
      rightPanels: { files: true, prs: true },
      toggleRightPanel: (panel) => set((state) => ({ rightPanels: { ...state.rightPanels, [panel]: !state.rightPanels[panel] } })),
      setRightPanel: (panel, open) => set((state) => ({ rightPanels: { ...state.rightPanels, [panel]: open } })),

      selectedFilePath: 'package.json',
      setSelectedFilePath: (selectedFilePath) => set({ selectedFilePath }),
      editorSettings: null,
      editorSettingsLoading: false,
      editorSettingsSaving: false,
      editorSettingsError: '',
      loadEditorSettings: async () => {
        set({ editorSettingsLoading: true, editorSettingsError: '' })
        try {
          const editorSettings = await loadEditors()
          set({ editorSettings })
          return editorSettings
        } catch (error) {
          set({ editorSettingsError: error instanceof Error ? error.message : String(error) })
          throw error
        } finally { set({ editorSettingsLoading: false }) }
      },
      setEnvironmentEditor: async (editor) => {
        set({ editorSettingsSaving: true, editorSettingsError: '' })
        try {
          const settings = get().editorSettings ?? await get().loadEditorSettings()
          const editorSettings = await saveEditor(settings, editor)
          set({ editorSettings, notice: 'Editor preference saved for ' + editorSettings.environment.label })
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error)
          await get().loadEditorSettings().catch(() => undefined)
          set({ editorSettingsError: message })
          throw error
        } finally { set({ editorSettingsSaving: false }) }
      },
      editorPromptOpen: false,
      pendingOpenFile: null,
      requestOpenFile: async (path) => {
        const state = get()
        const worktreeId = state.dockWorktreeId
        if (!worktreeId) {
          set({ notice: 'Select a worktree before opening a file.' })
          return
        }
        set({ selectedFilePath: path })
        try {
          // Backend preferences belong to this environment; browser storage
          // must never select an editor from another environment.
          const settings = await get().loadEditorSettings()
          if (!settings.preferred) {
            set({ editorPromptOpen: true, pendingOpenFile: { worktreeId, path } })
            return
          }
          const result = await openFileInEditor(worktreeId, path, settings.environment.id)
          set({ notice: 'Requested ' + result.editor + ' to open ' + path })
        } catch (error) { report(error) }
      },
      requestOpenFolder: async (worktreeId) => {
        try {
          const settings = await get().loadEditorSettings()
          const preferred = settings.editors.find(editor => editor.id === settings.preferred)
          // Some installed editors cannot open directories; ask which one to use.
          if (!preferred?.folders) {
            set({ editorPromptOpen: true, pendingOpenFile: { worktreeId, path: '', folder: true } })
            return
          }
          const result = await openFolderInEditor(worktreeId, settings.environment.id)
          set({ notice: 'Requested ' + result.editor + ' to open the worktree folder' })
        } catch (error) { report(error) }
      },
      chooseFileEditor: async (editor) => {
        const pending = get().pendingOpenFile
        try {
          // A default that merely cannot open folders stays the default; the
          // choice then applies to this launch only.
          const keepDefault = !!pending?.folder && !!get().editorSettings?.preferred
          if (!keepDefault) await get().setEnvironmentEditor(editor)
          if (!pending || get().pendingOpenFile !== pending) return
          const settings = get().editorSettings!
          const result = pending.folder
            ? await openFolderInEditor(pending.worktreeId, settings.environment.id, keepDefault ? editor : undefined)
            : await openFileInEditor(pending.worktreeId, pending.path, settings.environment.id)
          set({ editorPromptOpen: false, pendingOpenFile: null, notice: 'Requested ' + result.editor + ' to open ' + (pending.folder ? 'the worktree folder' : pending.path) })
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error)
          set({ editorSettingsError: message })
          report(error)
        }
      },
      closeEditorPrompt: () => set({ editorPromptOpen: false, pendingOpenFile: null }),

      pullRequests: [],
      inspectedPullRequestId: null,
      pullRequestFocusNonce: 0,
      setInspectedPullRequestId: (inspectedPullRequestId) => set({ inspectedPullRequestId }),
      inspectPullRequest: (id) => {
        const state = get()
        const pr = state.pullRequests.find((item) => item.id === id)
        if (!pr) return
        const worktree = state.worktrees.find((item) => item.projectId === state.activeProjectId && item.prNumber === pr.number && item.branch === pr.branch)
        set({
          ...(worktree ? switchRuntimeScope(state, state.activeProjectId, worktree.id) : {}),
          inspectedPullRequestId: id,
          pullRequestFocusNonce: state.pullRequestFocusNonce + 1,
        })
      },
      setPullRequestStatus: (id, status) => { void changePullRequest(id, status) },
      addPullRequestReview: (id, body, kind) => { void reviewPullRequest(id, body, kind) },

      nodePlacements: {},
      setManualNodePlacement: (id, position) =>
        set((state) => {
          const current = state.nodePlacements[id]
          if (current?.x === position.x && current?.y === position.y && current.mode === 'manual') return state
          return { nodePlacements: { ...state.nodePlacements, [id]: { ...position, mode: 'manual' } } }
        }),
      setManualNodePlacements: (positions) =>
        set((state) => {
          const next = { ...state.nodePlacements }
          let changed = false
          Object.entries(positions).forEach(([id, position]) => {
            const current = next[id]
            if (current?.x === position.x && current?.y === position.y && current.mode === 'manual') return
            next[id] = { ...position, mode: 'manual' }
            changed = true
          })
          return changed ? { nodePlacements: next } : state
        }),
      setGeneratedNodePlacements: (positions) =>
        set((state) => {
          const next = { ...state.nodePlacements }
          let changed = false
          Object.entries(positions).forEach(([id, position]) => {
            const current = next[id]
            if (current?.x === position.x && current?.y === position.y && current.mode === 'generated') return
            next[id] = { ...position, mode: 'generated' }
            changed = true
          })
          return changed ? { nodePlacements: next } : state
        }),
      removeNodePlacement: (id) =>
        set((state) => state.nodePlacements[id] ? { nodePlacements: withoutPlacements(state.nodePlacements, [id]) } : state),
      removeNodePlacements: (ids) =>
        set((state) => ids.some((id) => state.nodePlacements[id])
          ? { nodePlacements: withoutPlacements(state.nodePlacements, ids) }
          : state),
      subtreeMoveRootId: null,
      setSubtreeMoveRoot: (subtreeMoveRootId) => set({ subtreeMoveRootId }),
      viewport: { x: 0, y: 0, zoom: 0.82 },
      setViewport: (viewport) => set({ viewport }),
      expandedHistoryWorktreeIds: [],
      toggleWorktreeHistory: (id) => set((state) => ({
        expandedHistoryWorktreeIds: state.expandedHistoryWorktreeIds.includes(id)
          ? state.expandedHistoryWorktreeIds.filter((item) => item !== id)
          : [...state.expandedHistoryWorktreeIds, id],
      })),
      canvasCommand: { type: 'fit', nonce: 0 },
      canvasReveal: { projectId: '', nodeId: '', nonce: 0 },
      requestCanvasAction: (type) => set((state) => ({
        canvasCommand: { type, nonce: state.canvasCommand.nonce + 1 },
      })),
      requestCanvasFocus: (projectId, nodeId) => set((state) => ({
        canvasReveal: { projectId, nodeId, nonce: state.canvasReveal.nonce + 1, mode: 'center' },
      })),
      canvasPreferences: DEFAULT_CANVAS_PREFERENCES,
      setCanvasPreferences: (patch) => set((state) => ({ canvasPreferences: { ...state.canvasPreferences, ...patch } })),

      paletteOpen: false,
      setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
      notice: '',
      setNotice: (notice) => set({ notice }),

      terminalSessions: [],
      activeTerminalId: '',
      terminalOutput: {},
      openTerminal: (id) => { if (id && get().agents.some(a => a.id === id && isLiveAgent(a))) { get().openRuntime(id); get().setDockState('normal') } else set({ notice: SHELL_UNAVAILABLE }) },
      setActiveTerminalId: (activeTerminalId) => set((state) => state.activeTerminalId === activeTerminalId ? state : { activeTerminalId }),
      appendTerminalCommand: (command) => {
        if (command.trim().startsWith('git ')) {
          if (command.trim() === 'git pull') void localCommand('pull')
          else if (command.trim() === 'git push') void localCommand('push')
          else if (command.trim() === 'git fetch') void localCommand('fetch')
          else set({ notice: 'Use the Git API or local Git for this operation.' })
          return
        }
        set({ notice: SHELL_UNAVAILABLE })
      },
    }),
)

export const workspacePreferences = createPreferenceBoundary(useBonsaiStore)
