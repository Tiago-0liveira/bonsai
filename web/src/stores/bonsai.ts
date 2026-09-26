import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agents as initialAgents } from '../mock/agents'
import {
  boardItems as initialBoardItems,
  boardLists as initialBoardLists,
  boardPriorities as initialBoardPriorities,
  boardTypes as initialBoardTypes,
} from '../mock/board'
import { projects as initialProjects, workspaces } from '../mock/projects'
import { pullRequests as initialPullRequests } from '../mock/pullRequests'
import { worktreeTags as initialWorktreeTags } from '../mock/tags'
import { worktrees as initialWorktrees } from '../mock/worktrees'
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
  PullRequest,
  Selection,
  StartAgentInput,
  ViewportState,
  Worktree,
  WorktreeKind,
  WorktreeTag,
} from '../types'

interface TerminalSession {
  id: string
  label: string
  agentId?: string
}

interface BonsaiState {
  selection: Selection
  setSelection: (selection: Selection) => void
  projectQuery: string
  setProjectQuery: (query: string) => void

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
  createMockWorktree: (input?: CreateWorktreeInput | string) => void

  startAgentDialogOpen: boolean
  startAgentTargetWorktreeId: string
  openStartAgentDialog: (worktreeId?: string) => void
  setStartAgentDialogOpen: (open: boolean) => void
  createAgent: (input: StartAgentInput) => void
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

const initialTerminalOutput: Record<string, string[]> = {
  'term-ui': ['$ pnpm dev', 'VITE v5.4.19  ready in 412 ms', '➜  Local:   http://localhost:5173/', '', '[bonsai] workspace mock state connected'],
  'term-tests': ['$ pnpm test --watch', '✓ src/stores/bonsai.test.ts', 'Watching for file changes…'],
  'term-daemon': ['$ go run . daemon', '[bonsaid] tracing process lifecycle…'],
}

function slugify(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '')
}

function kindForTag(tag: string): WorktreeKind {
  if (tag === 'bug') return 'Bug'
  if (tag === 'chore') return 'Chore'
  if (tag === 'review-code') return 'Refactor'
  if (tag === 'production') return 'Production'
  return 'Feature'
}

function branchForInput(input: CreateWorktreeInput, fallback: string) {
  if (input.sourceType === 'new') return input.branchName?.trim() || fallback
  if (input.sourceType === 'origin') return input.sourceRef.replace(/^origin\//, '')
  return input.sourceRef
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
  persist(
    (set, get) => ({
      selection: { type: 'project', id: 'bonsai' },
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

        set({ selection, dockWorktreeId, dockRuntimeId, openRuntimeIds, activeTerminalId, dockState: 'normal' })
      },
      projectQuery: '',
      setProjectQuery: (projectQuery) => set({ projectQuery }),

      projects: initialProjects,
      activeWorkspaceId: 'personal',
      activeProjectId: 'bonsai',
      setActiveWorkspace: (activeWorkspaceId) => {
        const state = get()
        const workspace = workspaces.find((item) => item.id === activeWorkspaceId)
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
      createMockProject: (name) =>
        set((state) => {
          const fallback = 'new-project-' + (state.projects.length + 1)
          const displayName = (name?.trim() || fallback).slice(0, 42)
          const baseId = slugify(displayName) || fallback
          let id = baseId
          let suffix = 2
          while (state.projects.some((project) => project.id === id)) {
            id = baseId + '-' + suffix
            suffix += 1
          }
          const project: Project = {
            id,
            workspaceId: state.activeWorkspaceId,
            name: displayName,
            repository: 'local/' + id,
            description: 'Frontend-only mock project. Connect repository details later.',
            health: 'idle',
            defaultBranch: 'main',
            defaultBranchInfo: {
              commitSha: 'local',
              commitMessage: 'No commits loaded yet',
              lastActivity: 'just now',
              ciStatus: 'waiting',
            },
            worktreeIds: [],
            openPrCount: 0,
          }
          return {
            projects: [...state.projects, project],
            activeProjectId: id,
            selection: { type: 'project', id },
            dockWorktreeId: '',
            dockRuntimeId: '',
            openRuntimeIds: [],
            notice: 'Added project ' + displayName,
          }
        }),

      sidebarCollapsed: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      worktrees: initialWorktrees,
      worktreeTags: initialWorktreeTags,
      agents: initialAgents,
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
      setWorktreeStackPreference: (id, stackPreference) =>
        set((state) => ({
          worktrees: state.worktrees.map((item) => item.id === id ? { ...item, stackPreference } : item),
          detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter((item) => item !== id),
          notice: stackPreference === 'never' ? 'This worktree will stay separate from stacks' : 'Automatic stacking restored',
        })),
      setWorktreeTag: (id, tag) =>
        set((state) => {
          const nextTag = tag.trim() || 'untagged'
          const tagDefinition = state.worktreeTags.find((item) => item.name === nextTag)
          return {
            worktrees: state.worktrees.map((worktree) => worktree.id === id ? { ...worktree, tag: nextTag, tagId: tagDefinition?.id } : worktree),
            detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter((item) => item !== id),
            notice: 'Worktree tag updated to ' + nextTag,
          }
        }),
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
        set({
          worktrees: state.worktrees.map((item) => item.id === id ? { ...item, mergeTargetBranch: branch } : item),
          notice: worktree.branch + ' now merges into ' + branch,
        })
      },

      worktreeDialogOpen: false,
      setWorktreeDialogOpen: (worktreeDialogOpen) => set({ worktreeDialogOpen }),
      createMockWorktree: (input) =>
        set((state) => {
          const project = state.projects.find((item) => item.id === state.activeProjectId)
          if (!project) return {}
          const projectWorktrees = state.worktrees.filter((item) => item.projectId === project.id)
          const fallbackTag = typeof input === 'string' ? input : 'feat'
          const fallbackTagDef = state.worktreeTags.find((item) => item.name === fallbackTag) ?? state.worktreeTags[0]
          const normalized: CreateWorktreeInput =
            typeof input === 'object'
              ? input
              : {
                  sourceType: 'new',
                  sourceRef: project.defaultBranch,
                  branchName: fallbackTag + '/prototype-' + (projectWorktrees.length + 1),
                  tagId: fallbackTagDef?.id ?? 'feat',
                  mergeTargetBranch: project.defaultBranch,
                }
          const tagDefinition = state.worktreeTags.find((item) => item.id === normalized.tagId) ?? fallbackTagDef
          const fallbackBranch = (tagDefinition?.name ?? 'feat') + '/prototype-' + (projectWorktrees.length + 1)
          const branch = branchForInput(normalized, fallbackBranch)
          if (!branch.trim()) return { notice: 'Choose or enter a branch before creating the worktree' }
          if (projectWorktrees.some((item) => item.branch === branch)) return { notice: branch + ' already has a worktree' }
          const id = 'wt-' + project.id + '-' + (projectWorktrees.length + 1) + '-' + Date.now().toString(36)
          const tag = tagDefinition?.name ?? 'feat'
          const mergeTargetExists =
            normalized.mergeTargetBranch === project.defaultBranch ||
            projectWorktrees.some((item) => item.branch === normalized.mergeTargetBranch)
          const worktree: Worktree = {
            id,
            projectId: project.id,
            branch,
            kind: kindForTag(tag),
            tag,
            tagId: tagDefinition?.id,
            sourceType: normalized.sourceType,
            remoteBranch: normalized.sourceType === 'origin' ? normalized.sourceRef : undefined,
            mergeTargetBranch: mergeTargetExists ? normalized.mergeTargetBranch : project.defaultBranch,
            stackPreference: 'auto',
            status: 'idle',
            agentIds: [],
            ciStatus: 'waiting',
            ciFailed: 0,
            ahead: 0,
            behind: 0,
            dirtyFiles: 0,
            lastActivity: 'just now',
            gitState: 'clean',
          }
          return {
            worktrees: [...state.worktrees, worktree],
            projects: state.projects.map((item) => item.id === project.id ? { ...item, worktreeIds: [...item.worktreeIds, id] } : item),
            selection: { type: 'worktree', id },
            dockWorktreeId: id,
            dockRuntimeId: '',
            worktreeDialogOpen: false,
            notice: 'Created worktree ' + branch,
          }
        }),

      startAgentDialogOpen: false,
      startAgentTargetWorktreeId: '',
      openStartAgentDialog: (worktreeId = '') => set({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktreeId }),
      setStartAgentDialogOpen: (startAgentDialogOpen) => set({ startAgentDialogOpen }),
      createAgent: (input) =>
        set((state) => {
          const worktree = state.worktrees.find((item) => item.id === input.worktreeId)
          if (!worktree) return { notice: 'Choose a worktree before starting an agent' }
          const id = 'agent-' + Date.now().toString(36)
          const terminalId = 'term-' + id
          const agent: Agent = {
            id,
            worktreeId: input.worktreeId,
            name: input.name.trim() || input.provider + ' agent',
            provider: input.provider,
            model: input.model,
            reasoningEffort: input.reasoningEffort,
            fastMode: input.fastMode,
            workType: input.workType,
            prompt: input.prompt,
            archived: false,
            presentation: 'canvas',
            state: 'running',
            task: input.prompt.slice(0, 90) || input.workType,
            runtime: 'just now',
            terminalId,
            createdAt: 'just now',
          }
          return {
            agents: [...state.agents, agent],
            worktrees: state.worktrees.map((item) => item.id === input.worktreeId ? { ...item, agentIds: [...item.agentIds, id] } : item),
            selection: { type: 'agent', id },
            dockWorktreeId: input.worktreeId,
            dockRuntimeId: id,
            openRuntimeIds: uniqueAdd(state.openRuntimeIds, id),
            activeTerminalId: terminalId,
            terminalSessions: [...state.terminalSessions, { id: terminalId, label: agent.name, agentId: id }],
            terminalOutput: {
              ...state.terminalOutput,
              [terminalId]: [
                '# ' + agent.name,
                '# ' + input.provider + ' · ' + input.model + ' · ' + input.reasoningEffort + (input.fastMode ? ' · Fast' : ''),
                '# ' + input.workType,
                '$ ' + input.prompt,
              ],
            },
            startAgentDialogOpen: false,
            startAgentTargetWorktreeId: '',
            dockState: 'normal',
            notice: 'Started ' + agent.name + ' on ' + worktree.branch,
          }
        }),
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
              ? { ...agent, presentation: 'history', archived: false, state: agent.state === 'running' ? 'finished' : agent.state, finishedAt: agent.finishedAt ?? 'just now' }
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
              ? { ...agent, presentation: 'archived', archived: true, state: agent.state === 'running' ? 'finished' : agent.state, finishedAt: agent.finishedAt ?? 'just now' }
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
      setAgentState: (id, agentState) =>
        set((state) => ({
          agents: state.agents.map((agent) => agent.id === id ? { ...agent, state: agentState, finishedAt: agentState === 'finished' ? 'just now' : agent.finishedAt } : agent),
          notice: 'Agent moved to ' + agentState,
        })),

      envEditorOpen: false,
      setEnvEditorOpen: (envEditorOpen) => set({ envEditorOpen }),
      envVariables: {
        bonsai: [
          { id: 'env-1', key: 'BONSAI_API_URL', value: 'http://localhost:8080', secret: false },
          { id: 'env-2', key: 'GITHUB_TOKEN', value: 'ghp_example_token', secret: true },
          { id: 'env-3', key: 'NODE_ENV', value: 'development', secret: false },
        ],
      },
      addEnvVariable: (projectId) =>
        set((state) => ({
          envVariables: {
            ...state.envVariables,
            [projectId]: [...(state.envVariables[projectId] ?? []), { id: 'env-' + Date.now().toString(36), key: '', value: '', secret: true }],
          },
        })),
      updateEnvVariable: (projectId, id, patch) =>
        set((state) => ({
          envVariables: {
            ...state.envVariables,
            [projectId]: (state.envVariables[projectId] ?? []).map((item) => item.id === id ? { ...item, ...patch } : item),
          },
        })),
      removeEnvVariable: (projectId, id) =>
        set((state) => ({
          envVariables: {
            ...state.envVariables,
            [projectId]: (state.envVariables[projectId] ?? []).filter((item) => item.id !== id),
          },
        })),

      dockState: 'normal',
      setDockState: (dockState) => set({ dockState }),
      dockHeight: 30,
      setDockHeight: (dockHeight) => set({ dockHeight: Math.min(72, Math.max(14, dockHeight)) }),
      activeDockTab: 'terminal',
      setActiveDockTab: (activeDockTab) => set({ activeDockTab }),
      dockWorktreeId: 'wt-web',
      setDockWorktreeId: (dockWorktreeId) =>
        set((state) => state.dockWorktreeId === dockWorktreeId ? state : { dockWorktreeId, dockRuntimeId: '' }),
      dockRuntimeId: 'agent-ui',
      setDockRuntimeId: (dockRuntimeId) =>
        set((state) => {
          if (state.dockRuntimeId === dockRuntimeId) return state
          const agent = state.agents.find((item) => item.id === dockRuntimeId)
          return {
            dockRuntimeId,
            dockWorktreeId: agent?.worktreeId ?? state.dockWorktreeId,
            activeTerminalId: agent?.terminalId ?? state.activeTerminalId,
            openRuntimeIds: dockRuntimeId ? uniqueAdd(state.openRuntimeIds, dockRuntimeId) : state.openRuntimeIds,
          }
        }),
      openRuntimeIds: ['agent-ui', 'agent-tests'],
      openRuntime: (id) =>
        set((state) => {
          const agent = state.agents.find((item) => item.id === id)
          return {
            openRuntimeIds: uniqueAdd(state.openRuntimeIds, id),
            dockRuntimeId: id,
            dockWorktreeId: agent?.worktreeId ?? state.dockWorktreeId,
            activeTerminalId: agent?.terminalId ?? state.activeTerminalId,
          }
        }),
      closeRuntime: (id) =>
        set((state) => {
          const next = state.openRuntimeIds.filter((item) => item !== id)
          return {
            openRuntimeIds: next,
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
        set({ selectedFilePath: path, notice: 'Opening ' + path + ' in ' + state.editorPreference + ' (mock)' })
      },
      setEditorPreference: (editorPreference) => {
        const state = get()
        set({
          editorPreference,
          editorPromptOpen: false,
          notice: state.pendingOpenFile ? 'Opening ' + state.pendingOpenFile + ' in ' + editorPreference + ' (mock)' : 'Editor preference saved',
          pendingOpenFile: '',
        })
      },
      closeEditorPrompt: () => set({ editorPromptOpen: false, pendingOpenFile: '' }),

      pullRequests: initialPullRequests,
      setPullRequestStatus: (id, status) =>
        set((state) => ({
          pullRequests: state.pullRequests.map((pr) => pr.id === id ? { ...pr, status, updatedAt: 'just now' } : pr),
          notice: status === 'Merged' ? 'Pull request merged' : status === 'Closed' ? 'Pull request closed' : 'Pull request opened for review',
        })),
      addPullRequestReview: (id, body, kind) =>
        set((state) => {
          const message = body.trim() || (kind === 'approve' ? 'Approved' : kind === 'request-changes' ? 'Changes requested' : '')
          if (!message) return state
          return {
            pullRequests: state.pullRequests.map((pr) =>
              pr.id === id
                ? {
                    ...pr,
                    updatedAt: 'just now',
                    conversation: [
                      ...pr.conversation,
                      {
                        author: 'You',
                        body: message,
                        time: 'just now',
                        kind: kind === 'comment' ? 'comment' : 'review',
                      },
                    ],
                  }
                : pr,
            ),
            notice: kind === 'approve' ? 'Review approved' : kind === 'request-changes' ? 'Changes requested' : 'Review comment added',
          }
        }),

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
      canvasCommand: { type: 'fit', nonce: 0 },
      requestCanvasAction: (type) => set((state) => ({ canvasCommand: { type, nonce: state.canvasCommand.nonce + 1 } })),

      paletteOpen: false,
      setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
      notice: '',
      setNotice: (notice) => set({ notice }),

      terminalSessions: [
        { id: 'term-ui', label: 'UI builder', agentId: 'agent-ui' },
        { id: 'term-tests', label: 'Tests', agentId: 'agent-tests' },
      ],
      activeTerminalId: 'term-ui',
      terminalOutput: initialTerminalOutput,
      openTerminal: (agentId) => {
        const state = get()
        const agent = agentId ? state.agents.find((item) => item.id === agentId) : undefined
        const id = agent?.terminalId ?? 'term-' + Date.now()
        const exists = state.terminalSessions.some((session) => session.id === id)
        set({
          activeDockTab: 'terminal',
          dockState: 'normal',
          dockWorktreeId: agent?.worktreeId ?? state.dockWorktreeId,
          dockRuntimeId: agent?.id ?? state.dockRuntimeId,
          openRuntimeIds: agent ? uniqueAdd(state.openRuntimeIds, agent.id) : state.openRuntimeIds,
          activeTerminalId: id,
          terminalSessions: exists ? state.terminalSessions : [...state.terminalSessions, { id, label: agent?.name ?? 'Shell', agentId }],
          terminalOutput: state.terminalOutput[id]
            ? state.terminalOutput
            : { ...state.terminalOutput, [id]: ['# ' + (agent?.name ?? 'Bonsai shell'), agent ? '# ' + agent.task : '# Frontend-only mock terminal', '$ '] },
        })
      },
      setActiveTerminalId: (activeTerminalId) => set((state) => state.activeTerminalId === activeTerminalId ? state : { activeTerminalId }),
      appendTerminalCommand: (command) => {
        const { activeTerminalId, terminalOutput } = get()
        const lines = terminalOutput[activeTerminalId] ?? []
        const result =
          command.includes('test') || command.includes('go test')
            ? ['running tests…', '✓ all mock checks passed']
            : command.includes('dev')
              ? ['starting development server…', 'ready on http://localhost:5173']
              : command.includes('push')
                ? ['pushing branch…', 'Everything up-to-date (mock)']
                : ['command completed (mock)']
        set({
          activeDockTab: 'terminal',
          dockState: 'normal',
          terminalOutput: { ...terminalOutput, [activeTerminalId]: [...lines, '$ ' + command, ...result] },
          notice: 'Ran “' + command + '” in the mock terminal',
        })
      },
    }),
    {
      name: 'bonsai-web-workspace-v5',
      version: 6,
      migrate: (persisted) => {
        const state = persisted as BonsaiState & {
          nodePositions?: Record<string, { x: number; y: number }>
        }
        if (!state.nodePlacements && state.nodePositions) {
          state.nodePlacements = Object.fromEntries(
            Object.entries(state.nodePositions).map(([id, position]) => [id, { ...position, mode: 'manual' as const }]),
          )
        }
        delete state.nodePositions
        return state
      },
      partialize: (state) => ({
        selection: state.selection,
        projects: state.projects,
        activeWorkspaceId: state.activeWorkspaceId,
        activeProjectId: state.activeProjectId,
        sidebarCollapsed: state.sidebarCollapsed,
        dockState: state.dockState,
        dockHeight: state.dockHeight,
        activeDockTab: state.activeDockTab,
        dockWorktreeId: state.dockWorktreeId,
        dockRuntimeId: state.dockRuntimeId,
        openRuntimeIds: state.openRuntimeIds,
        collapsedBranchIds: state.collapsedBranchIds,
        rightPanels: state.rightPanels,
        selectedFilePath: state.selectedFilePath,
        editorPreference: state.editorPreference,
        nodePlacements: state.nodePlacements,
        viewport: state.viewport,
        boardItems: state.boardItems,
        boardLists: state.boardLists,
        boardPriorities: state.boardPriorities,
        boardTypes: state.boardTypes,
        worktrees: state.worktrees,
        agents: state.agents,
        collapsedTagGroups: state.collapsedTagGroups,
        envVariables: state.envVariables,
        pullRequests: state.pullRequests,
      }),
    },
  ),
)
