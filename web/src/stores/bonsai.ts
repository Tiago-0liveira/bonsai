import { startAgent, stopAgent, mapAgent } from '../api/agents'
import type { ProjectRootsSettings } from '../api/settings'
import { create } from 'zustand'
import { SHELL_UNAVAILABLE, ENV_UNAVAILABLE } from './execution'
import { createPreferenceBoundary } from './preferenceBoundary'
import {
  boardItems as initialBoardItems,
  boardLists as initialBoardLists,
  boardPriorities as initialBoardPriorities,
  boardTypes as initialBoardTypes,
} from '../mock/board'
import { localCommand, createWorktree, changePullRequest, reviewPullRequest, updateMetadata, report, type Branch, type BranchCandidate, type RepositorySync, type WorktreeGroup } from '../api/git'
import { worktreeTags as initialWorktreeTags } from '../mock/tags'
import type {
  Agent,
  AgentState,
  BoardItem,
  BoardList,
  BoardPriority,
  BoardStatus,
  BoardType,
  CreateWorktreeInput,
  DockPanelKey,
  DockState,
  DockTab,
  EditorPreference,
  EnvVariable,
  NodePlacement,
  Project,
  Process,
  PullRequest,
  Selection,
  StartAgentInput,
  SyncFreshness,
  ViewportState,
  Worktree,
  WorktreeTag,
} from '../types'

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
  projects: Project[]
  activeWorkspaceId: string
  activeProjectId: string
  setActiveWorkspace: (id: string) => void
  setActiveProject: (id: string) => void
  createMockProject: (name?: string) => void

  sidebarCollapsed: boolean
  toggleSidebar: () => void

  worktrees: Worktree[]
  worktreeTags: WorktreeTag[]
  agents: Agent[]
  collapsedTagGroups: string[]
  detachedStackWorktreeIds: string[]
  toggleTagGroup: (projectId: string, tag: string) => void
  ejectWorktreeFromStack: (id: string) => void
  setWorktreeStackPreference: (id: string, preference: 'auto' | 'never') => void
  setWorktreeTag: (id: string, tag: string) => void
  setWorktreeMergeTarget: (id: string, branch: string) => void

  worktreeDialogOpen: boolean
  setWorktreeDialogOpen: (open: boolean) => void
  worktreeDialogTarget: { projectId: string; sourceType?: CreateWorktreeInput['sourceType']; sourceRef?: string; branchName?: string } | null
  openCreateWorktree: (projectId: string, candidate?: BranchCandidate) => void
  deleteWorktreeId: string
  setDeleteWorktreeId: (id: string) => void
  createWorktree: typeof createWorktree

  startAgentDialogOpen: boolean
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
  dismissedRuntimeIds: string[]
  openRuntimeIds: string[]
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
  editorPreference: EditorPreference | undefined
  editorPromptOpen: boolean
  pendingOpenFile: string
  requestOpenFile: (path: string) => void
  setEditorPreference: (editor: EditorPreference) => void
  closeEditorPrompt: () => void

  pullRequests: PullRequest[]
  inspectedPullRequestId: string | null
  pullRequestFocusNonce: number
  setInspectedPullRequestId: (id: string | null) => void
  inspectPullRequest: (id: string) => void
  setPullRequestStatus: (id: string, status: PullRequest['status']) => void
  addPullRequestReview: (id: string, body: string, kind: 'comment' | 'approve' | 'request-changes') => void

  boardItems: BoardItem[]
  boardLists: BoardList[]
  boardPriorities: BoardPriority[]
  boardTypes: BoardType[]
  moveBoardItem: (id: string, status: BoardStatus) => void
  addBoardList: () => void
  updateBoardList: (id: string, patch: Partial<Pick<BoardList, 'name' | 'color' | 'priority' | 'itemType'>>) => void
  removeBoardList: (id: string, moveTo: string) => void
  moveBoardList: (id: string, direction: -1 | 1) => void
  addBoardPriority: (name: string) => void
  removeBoardPriority: (id: string) => void
  addBoardType: (name: string) => void
  removeBoardType: (id: string) => void

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
  requestCanvasAction: (type: 'fit' | 'layout') => void

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

function slugify(value: string) { return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') }

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
        let dockWorktreeId = state.dockWorktreeId
        let dockRuntimeId = state.dockRuntimeId
        let openRuntimeIds = state.openRuntimeIds
        let activeTerminalId = state.activeTerminalId

        if (selection.type === 'worktree') {
          dockWorktreeId = selection.id
          dockRuntimeId = ''
        }
        if (selection.type === 'agent') {
          const agent = state.agents.find((item) => item.id === selection.id)
          if (agent) {
            dockWorktreeId = agent.worktreeId
            dockRuntimeId = agent.id
            openRuntimeIds = uniqueAdd(openRuntimeIds, agent.id)
            activeTerminalId = agent.terminalId
          }
        }

        if (
          state.selection.type === selection.type &&
          state.selection.id === selection.id &&
          state.dockWorktreeId === dockWorktreeId &&
          state.dockRuntimeId === dockRuntimeId
        ) return

        set({ selection, dockWorktreeId, dockRuntimeId, openRuntimeIds, activeTerminalId, dismissedRuntimeIds: selection.type === 'agent' ? state.dismissedRuntimeIds.filter(id => id !== selection.id) : state.dismissedRuntimeIds, dockState: 'normal' })
      },
      projectQuery: '',
      setProjectQuery: (projectQuery) => set({ projectQuery }),

      branchCandidates: {},
      worktreeGroups: {},
      repositorySync: {},
      expandedAutomaticGroups: [],
      toggleAutomaticGroup: (id) => set(state => ({ expandedAutomaticGroups: state.expandedAutomaticGroups.includes(id) ? state.expandedAutomaticGroups.filter(value => value !== id) : [...state.expandedAutomaticGroups, id] })),
      gitBranches: {},
      gitOnline: {},
      gitRevision: 0,
      gitError: '',
      syncFreshness: {},
      processes: [],
      rootSettings: null,
      rootsLoading: false,
      rootsSaving: false,
      rootsError: '',
      projects: [],
      activeWorkspaceId: '',
      activeProjectId: '',
      setActiveWorkspace: (activeWorkspaceId) => {
        const state = get()
        const workspace = { name: activeWorkspaceId, projectIds: state.projects.filter(p => p.workspaceId === activeWorkspaceId).map(p => p.id) }
        const nextProject =
          state.projects.find((project) => workspace?.projectIds.includes(project.id)) ??
          state.projects.find((project) => project.workspaceId === activeWorkspaceId)
        const nextWorktree = state.worktrees.find((worktree) => worktree.projectId === nextProject?.id && worktree.branch !== nextProject?.defaultBranch)
        set({
          activeWorkspaceId,
          activeProjectId: nextProject?.id ?? state.activeProjectId,
          selection: nextProject ? { type: 'project', id: nextProject.id } : state.selection,
          dockWorktreeId: nextWorktree?.id ?? '',
          dockRuntimeId: '',
          openRuntimeIds: [],
          notice: nextProject ? 'Switched workspace to ' + workspace?.name : 'Workspace selected',
        })
      },
      setActiveProject: (activeProjectId) => {
        const state = get()
        const project = state.projects.find((item) => item.id === activeProjectId)
        if (!project) return
        const nextWorktree = state.worktrees.find((worktree) => worktree.projectId === project.id && worktree.branch !== project.defaultBranch)
        set({
          activeProjectId,
          activeWorkspaceId: project.workspaceId,
          selection: { type: 'project', id: project.id },
          dockWorktreeId: nextWorktree?.id ?? '',
          dockRuntimeId: '',
          openRuntimeIds: [],
          notice: 'Opened project ' + project.name,
        })
      },
      createMockProject: () => set({ notice: 'Add repositories in the server configuration, then enroll a device.' }),

      sidebarCollapsed: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      worktrees: [],
      worktreeTags: initialWorktreeTags,
      agents: [],
      collapsedTagGroups: ['bonsai:feat'],
      detachedStackWorktreeIds: [],
      toggleTagGroup: (projectId, tag) =>
        set((state) => {
          const key = projectId + ':' + tag
          const groupIds = state.worktrees.filter((item) => item.projectId === projectId && item.tag === tag).map((item) => item.id)
          return {
            collapsedTagGroups: state.collapsedTagGroups.includes(key)
              ? state.collapsedTagGroups.filter((item) => item !== key)
              : [...state.collapsedTagGroups, key],
            detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter((id) => !groupIds.includes(id)),
          }
        }),
      ejectWorktreeFromStack: (id) =>
        set((state) => ({
          detachedStackWorktreeIds: uniqueAdd(state.detachedStackWorktreeIds, id),
          notice: 'Detached worktree from this stack until the group is toggled',
        })),
      setWorktreeStackPreference: (id, stackPreference) => { void updateMetadata(id, { stack_preference: stackPreference }).catch(report) },
      setWorktreeTag: (id, tag) => { void updateMetadata(id, { tag }).catch(report) },
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
      startAgentTargetWorktreeId: '',
      openStartAgentDialog: (worktreeId = '') => set({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktreeId }),
      setStartAgentDialogOpen: (startAgentDialogOpen) => set({ startAgentDialogOpen }),
      createAgent: async (input) => {
        const tree = get().worktrees.find(w => w.id === input.worktreeId)
        if (!tree || input.provider !== 'Antigravity' || !input.accountId) throw new Error('Select an Antigravity profile and worktree.')
        const result = await startAgent(tree.projectId, { worktree_id: tree.id, account_id: input.accountId, name: input.name, model: input.model, prompt: input.prompt, full_access: input.fullAccess, cols: 80, rows: 24 }, input.requestKey ?? crypto.randomUUID())
        if (!result.id) throw new Error(result.error || 'Start was interrupted. Close this dialog and start again.')
        set(state => ({ agents: state.agents.some(a => a.id === result.id) ? state.agents : [...state.agents, mapAgent(result)], worktrees: state.worktrees.map(w => w.id === tree.id ? { ...w, agentIds: uniqueAdd(w.agentIds, result.id) } : w), startAgentDialogOpen: false }))
        get().setSelection({ type: 'agent', id: result.id })
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
          openRuntimeIds: state.openRuntimeIds.filter((runtimeId) => runtimeId !== id),
          dockRuntimeId: state.dockRuntimeId === id ? '' : state.dockRuntimeId,
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
          openRuntimeIds: state.openRuntimeIds.filter((runtimeId) => runtimeId !== id),
          dockRuntimeId: state.dockRuntimeId === id ? '' : state.dockRuntimeId,
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
        if (project && agent?.providerId === 'antigravity' && next === 'finished') void stopAgent(project, id, `stop-${id}`).catch(report)
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
        set((state) => state.dockWorktreeId === dockWorktreeId ? state : { dockWorktreeId, dockRuntimeId: '' }),
      dockRuntimeId: '',
      setDockRuntimeId: (dockRuntimeId) =>
        set((state) => {
          if (state.dockRuntimeId === dockRuntimeId) return state
          const agent = state.agents.find((item) => item.id === dockRuntimeId)
          const process = state.processes.find((item) => item.id === dockRuntimeId)
          return {
            dockRuntimeId,
            dockWorktreeId: agent?.worktreeId ?? process?.worktreeId ?? state.dockWorktreeId,
            activeTerminalId: agent?.terminalId ?? state.activeTerminalId,
            openRuntimeIds: dockRuntimeId ? uniqueAdd(state.openRuntimeIds, dockRuntimeId) : state.openRuntimeIds,
          }
        }),
      dismissedRuntimeIds: [],
      openRuntimeIds: [],
      openRuntime: (id) =>
        set((state) => {
          const agent = state.agents.find((item) => item.id === id)
          const process = state.processes.find((item) => item.id === id)
          if (!agent && !process) return state
          const dockWorktreeId = agent?.worktreeId ?? process!.worktreeId
          if (state.dockRuntimeId === id && state.dockWorktreeId === dockWorktreeId && state.openRuntimeIds.includes(id)) return state
          return {
            openRuntimeIds: uniqueAdd(state.openRuntimeIds, id),
            dismissedRuntimeIds: state.dismissedRuntimeIds.filter(value => value !== id),
            dockRuntimeId: id,
            dockWorktreeId,
            activeTerminalId: agent?.terminalId ?? state.activeTerminalId,
          }
        }),
      closeRuntime: (id) =>
        set((state) => {
          const next = state.openRuntimeIds.filter((item) => item !== id)
          return {
            openRuntimeIds: next,
            dismissedRuntimeIds: uniqueAdd(state.dismissedRuntimeIds, id),
            dockRuntimeId: state.dockRuntimeId === id ? (next[0] ?? '') : state.dockRuntimeId,
          }
        }),
      reorderOpenRuntime: (activeId, overId) =>
        set((state) => {
          const from = state.openRuntimeIds.indexOf(activeId)
          const to = state.openRuntimeIds.indexOf(overId)
          if (from < 0 || to < 0 || from === to) return state
          const next = [...state.openRuntimeIds]
          next.splice(from, 1)
          next.splice(to, 0, activeId)
          return { openRuntimeIds: next }
        }),
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
      editorPreference: undefined,
      editorPromptOpen: false,
      pendingOpenFile: '',
      requestOpenFile: (path) => {
        const state = get()
        if (!state.editorPreference) {
          set({ editorPromptOpen: true, pendingOpenFile: path, selectedFilePath: path })
          return
        }
        set({ selectedFilePath: path, notice: 'Opening files in an external editor is unavailable in the connected app.' })
      },
      setEditorPreference: (editorPreference) => {
        const state = get()
        set({
          editorPreference,
          editorPromptOpen: false,
          notice: state.pendingOpenFile ? 'Editor preference saved. Opening files in an external editor is unavailable in the connected app.' : 'Editor preference saved',
          pendingOpenFile: '',
        })
      },
      closeEditorPrompt: () => set({ editorPromptOpen: false, pendingOpenFile: '' }),

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
          inspectedPullRequestId: id,
          pullRequestFocusNonce: state.pullRequestFocusNonce + 1,
          dockState: state.dockState === 'collapsed' ? 'normal' : state.dockState,
          dockWorktreeId: worktree?.id ?? state.dockWorktreeId,
          rightPanels: { ...state.rightPanels, prs: true },
        })
      },
      setPullRequestStatus: (id, status) => { void changePullRequest(id, status) },
      addPullRequestReview: (id, body, kind) => { void reviewPullRequest(id, body, kind) },

      boardItems: initialBoardItems,
      boardLists: initialBoardLists,
      boardPriorities: initialBoardPriorities,
      boardTypes: initialBoardTypes,
      moveBoardItem: (id, status) => set((state) => ({ boardItems: state.boardItems.map((item) => item.id === id ? { ...item, status } : item) })),
      addBoardList: () =>
        set((state) => {
          const id = 'list-' + Date.now().toString(36)
          return {
            boardLists: [...state.boardLists, { id, name: 'new-list', color: 'purple', priority: state.boardPriorities[0]?.name ?? 'High', itemType: state.boardTypes[0]?.name ?? 'Task', order: state.boardLists.length }],
          }
        }),
      updateBoardList: (id, patch) => set((state) => ({ boardLists: state.boardLists.map((list) => list.id === id ? { ...list, ...patch } : list) })),
      removeBoardList: (id, moveTo) =>
        set((state) => ({
          boardItems: state.boardItems.map((item) => item.status === id ? { ...item, status: moveTo } : item),
          boardLists: state.boardLists.filter((list) => list.id !== id).map((list, index) => ({ ...list, order: index })),
          notice: 'List removed',
        })),
      moveBoardList: (id, direction) =>
        set((state) => {
          const sorted = [...state.boardLists].sort((a, b) => a.order - b.order)
          const index = sorted.findIndex((item) => item.id === id)
          const nextIndex = index + direction
          if (index < 0 || nextIndex < 0 || nextIndex >= sorted.length) return state
          const swap = sorted[nextIndex]
          sorted[nextIndex] = sorted[index]
          sorted[index] = swap
          return { boardLists: sorted.map((list, order) => ({ ...list, order })) }
        }),
      addBoardPriority: (name) =>
        set((state) => {
          const trimmed = name.trim()
          if (!trimmed || state.boardPriorities.some((item) => item.name.toLowerCase() === trimmed.toLowerCase())) return state
          return { boardPriorities: [...state.boardPriorities, { id: slugify(trimmed) || Date.now().toString(36), name: trimmed, rank: state.boardPriorities.length }] }
        }),
      removeBoardPriority: (id) => set((state) => ({ boardPriorities: state.boardPriorities.filter((item) => item.id !== id) })),
      addBoardType: (name) =>
        set((state) => {
          const trimmed = name.trim()
          if (!trimmed || state.boardTypes.some((item) => item.name.toLowerCase() === trimmed.toLowerCase())) return state
          return { boardTypes: [...state.boardTypes, { id: slugify(trimmed) || Date.now().toString(36), name: trimmed }] }
        }),
      removeBoardType: (id) => set((state) => ({ boardTypes: state.boardTypes.filter((item) => item.id !== id) })),

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
      requestCanvasAction: (type) => set((state) => ({
        canvasCommand: { type, nonce: state.canvasCommand.nonce + 1 },
      })),

      paletteOpen: false,
      setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
      notice: '',
      setNotice: (notice) => set({ notice }),

      terminalSessions: [],
      activeTerminalId: '',
      terminalOutput: {},
      openTerminal: (id) => { if (id && get().agents.some(a => a.id === id && a.providerId === 'antigravity')) { get().openRuntime(id); get().setDockState('normal') } else set({ notice: SHELL_UNAVAILABLE }) },
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
