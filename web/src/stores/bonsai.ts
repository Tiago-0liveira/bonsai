import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agents as initialAgents } from '../mock/agents'
import { boardItems as initialBoardItems } from '../mock/board'
import { projects as initialProjects, workspaces } from '../mock/projects'
import { worktreeTags as initialWorktreeTags } from '../mock/tags'
import { worktrees as initialWorktrees } from '../mock/worktrees'
import type {
  Agent,
  AgentState,
  BoardItem,
  BoardStatus,
  CreateWorktreeInput,
  DockPanelKey,
  DockState,
  DockTab,
  EnvVariable,
  Project,
  Selection,
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
  boardItems: BoardItem[]
  collapsedTagGroups: string[]
  stackExcludedWorktreeIds: string[]
  toggleTagGroup: (projectId: string, tag: string) => void
  ejectWorktreeFromStack: (id: string) => void
  setWorktreeTag: (id: string, tag: string) => void
  setWorktreeMergeTarget: (id: string, branch: string) => void

  worktreeDialogOpen: boolean
  setWorktreeDialogOpen: (open: boolean) => void
  createMockWorktree: (input?: CreateWorktreeInput | string) => void

  envEditorOpen: boolean
  setEnvEditorOpen: (open: boolean) => void
  envVariables: Record<string, EnvVariable[]>
  addEnvVariable: (projectId: string) => void
  updateEnvVariable: (projectId: string, id: string, patch: Partial<Pick<EnvVariable, 'key' | 'value' | 'secret'>>) => void
  removeEnvVariable: (projectId: string, id: string) => void

  dockState: DockState
  setDockState: (state: DockState) => void
  activeDockTab: DockTab
  setActiveDockTab: (tab: DockTab) => void
  dockWorktreeId: string
  setDockWorktreeId: (id: string) => void
  rightPanels: Record<DockPanelKey, boolean>
  toggleRightPanel: (panel: DockPanelKey) => void
  setRightPanel: (panel: DockPanelKey, open: boolean) => void

  selectedFilePath: string
  setSelectedFilePath: (path: string) => void

  nodePositions: Record<string, { x: number; y: number }>
  setNodePosition: (id: string, position: { x: number; y: number }) => void
  setNodePositionsBatch: (positions: Record<string, { x: number; y: number }>) => void
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

  moveBoardItem: (id: string, status: BoardStatus) => void
  setAgentState: (id: string, state: AgentState) => void
  startMockAgent: () => void
}

const initialTerminalOutput: Record<string, string[]> = {
  'term-ui': [
    '$ pnpm dev',
    'VITE v5.4.19  ready in 412 ms',
    '➜  Local:   http://localhost:5173/',
    '',
    '[bonsai] workspace mock state connected',
  ],
  'term-tests': [
    '$ pnpm test --watch',
    '✓ src/stores/bonsai.test.ts (3 tests)',
    'Test Files  1 passed (1)',
    'Watching for file changes…',
  ],
}

function slugify(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/(^-|-$)/g, '')
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

export const useBonsaiStore = create<BonsaiState>()(
  persist(
    (set, get) => ({
      selection: { type: 'project', id: 'bonsai' },
      setSelection: (selection) => {
        const state = get()
        let dockWorktreeId = state.dockWorktreeId
        if (selection.type === 'worktree') dockWorktreeId = selection.id
        if (selection.type === 'agent') {
          dockWorktreeId = state.agents.find((agent) => agent.id === selection.id)?.worktreeId ?? dockWorktreeId
        }
        if (
          state.selection.type === selection.type &&
          state.selection.id === selection.id &&
          state.dockWorktreeId === dockWorktreeId
        ) {
          return
        }
        set({ selection, dockWorktreeId })
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
        const nextWorktree = state.worktrees.find(
          (worktree) => worktree.projectId === nextProject?.id && worktree.branch !== nextProject?.defaultBranch,
        )
        set({
          activeWorkspaceId,
          activeProjectId: nextProject?.id ?? state.activeProjectId,
          selection: nextProject ? { type: 'project', id: nextProject.id } : state.selection,
          dockWorktreeId: nextWorktree?.id ?? '',
          nodePositions: {},
          notice: nextProject ? 'Switched workspace to ' + workspace?.name : 'Workspace selected',
        })
      },
      setActiveProject: (activeProjectId) => {
        const state = get()
        const project = state.projects.find((item) => item.id === activeProjectId)
        if (!project) return
        const nextWorktree = state.worktrees.find(
          (worktree) => worktree.projectId === project.id && worktree.branch !== project.defaultBranch,
        )
        set({
          activeProjectId,
          activeWorkspaceId: project.workspaceId,
          selection: { type: 'project', id: project.id },
          dockWorktreeId: nextWorktree?.id ?? '',
          nodePositions: {},
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
            nodePositions: {},
            notice: 'Added project ' + displayName,
          }
        }),

      sidebarCollapsed: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      worktrees: initialWorktrees,
      worktreeTags: initialWorktreeTags,
      agents: initialAgents,
      boardItems: initialBoardItems,
      collapsedTagGroups: ['bonsai:feat'],
      stackExcludedWorktreeIds: [],
      toggleTagGroup: (projectId, tag) =>
        set((state) => {
          const key = projectId + ':' + tag
          return {
            collapsedTagGroups: state.collapsedTagGroups.includes(key)
              ? state.collapsedTagGroups.filter((item) => item !== key)
              : [...state.collapsedTagGroups, key],
            nodePositions: {},
          }
        }),
      ejectWorktreeFromStack: (id) =>
        set((state) => ({
          stackExcludedWorktreeIds: state.stackExcludedWorktreeIds.includes(id)
            ? state.stackExcludedWorktreeIds
            : [...state.stackExcludedWorktreeIds, id],
          nodePositions: {},
          notice: 'Removed worktree from the collapsed stack',
        })),
      setWorktreeTag: (id, tag) =>
        set((state) => {
          const nextTag = tag.trim() || 'untagged'
          const tagDefinition = state.worktreeTags.find((item) => item.name === nextTag)
          return {
            worktrees: state.worktrees.map((worktree) =>
              worktree.id === id ? { ...worktree, tag: nextTag, tagId: tagDefinition?.id } : worktree,
            ),
            stackExcludedWorktreeIds: state.stackExcludedWorktreeIds.filter((item) => item !== id),
            nodePositions: {},
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
          const parent = state.worktrees.find(
            (item) => item.projectId === project.id && item.branch === cursor,
          )
          if (!parent) break
          cursor = parent.mergeTargetBranch
        }
        set({
          worktrees: state.worktrees.map((item) =>
            item.id === id ? { ...item, mergeTargetBranch: branch } : item,
          ),
          nodePositions: {},
          notice: worktree.branch + ' now merges into ' + branch,
        })
      },

      worktreeDialogOpen: false,
      setWorktreeDialogOpen: (worktreeDialogOpen) => set({ worktreeDialogOpen }),
      createMockWorktree: (input) =>
        set((state) => {
          const projectId = state.activeProjectId
          const project = state.projects.find((item) => item.id === projectId)
          if (!project) return {}
          const projectWorktrees = state.worktrees.filter((item) => item.projectId === projectId)
          const fallbackTag = typeof input === 'string' ? input : 'feat'
          const fallbackTagDef =
            state.worktreeTags.find((item) => item.name === fallbackTag) ?? state.worktreeTags[0]
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
          const tagDefinition =
            state.worktreeTags.find((item) => item.id === normalized.tagId) ?? fallbackTagDef
          const fallbackBranch = (tagDefinition?.name ?? 'feat') + '/prototype-' + (projectWorktrees.length + 1)
          const branch = branchForInput(normalized, fallbackBranch)
          if (!branch.trim()) {
            return { notice: 'Choose or enter a branch before creating the worktree' }
          }
          if (projectWorktrees.some((item) => item.branch === branch)) {
            return { notice: branch + ' already has a worktree' }
          }
          const id = 'wt-' + projectId + '-' + (projectWorktrees.length + 1) + '-' + Date.now().toString(36)
          const tag = tagDefinition?.name ?? 'feat'
          const mergeTargetExists =
            normalized.mergeTargetBranch === project.defaultBranch ||
            projectWorktrees.some((item) => item.branch === normalized.mergeTargetBranch)
          const mergeTargetBranch = mergeTargetExists ? normalized.mergeTargetBranch : project.defaultBranch
          const worktree: Worktree = {
            id,
            projectId,
            branch,
            kind: kindForTag(tag),
            tag,
            tagId: tagDefinition?.id,
            sourceType: normalized.sourceType,
            remoteBranch: normalized.sourceType === 'origin' ? normalized.sourceRef : undefined,
            mergeTargetBranch,
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
            projects: state.projects.map((item) =>
              item.id === project.id ? { ...item, worktreeIds: [...item.worktreeIds, id] } : item,
            ),
            selection: { type: 'worktree', id },
            dockWorktreeId: id,
            worktreeDialogOpen: false,
            nodePositions: {},
            notice: 'Created worktree ' + branch,
          }
        }),

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
            [projectId]: [
              ...(state.envVariables[projectId] ?? []),
              { id: 'env-' + Date.now().toString(36), key: '', value: '', secret: true },
            ],
          },
        })),
      updateEnvVariable: (projectId, id, patch) =>
        set((state) => ({
          envVariables: {
            ...state.envVariables,
            [projectId]: (state.envVariables[projectId] ?? []).map((item) =>
              item.id === id ? { ...item, ...patch } : item,
            ),
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
      activeDockTab: 'terminal',
      setActiveDockTab: (activeDockTab) => set({ activeDockTab }),
      dockWorktreeId: 'wt-web',
      setDockWorktreeId: (dockWorktreeId) => set({ dockWorktreeId }),
      rightPanels: { files: true, prs: true },
      toggleRightPanel: (panel) =>
        set((state) => ({ rightPanels: { ...state.rightPanels, [panel]: !state.rightPanels[panel] } })),
      setRightPanel: (panel, open) =>
        set((state) => ({ rightPanels: { ...state.rightPanels, [panel]: open } })),

      selectedFilePath: 'package.json',
      setSelectedFilePath: (selectedFilePath) => set({ selectedFilePath }),

      nodePositions: {},
      setNodePosition: (id, position) =>
        set((state) => {
          const current = state.nodePositions[id]
          if (current?.x === position.x && current?.y === position.y) return state
          return { nodePositions: { ...state.nodePositions, [id]: position } }
        }),
      setNodePositionsBatch: (positions) =>
        set((state) => {
          const entries = Object.entries(positions)
          const unchanged = entries.every(([id, position]) => {
            const current = state.nodePositions[id]
            return current?.x === position.x && current?.y === position.y
          })
          if (unchanged) return state
          return { nodePositions: { ...state.nodePositions, ...positions } }
        }),
      subtreeMoveRootId: null,
      setSubtreeMoveRoot: (subtreeMoveRootId) => set({ subtreeMoveRootId }),
      viewport: { x: 0, y: 0, zoom: 0.82 },
      setViewport: (viewport) => set({ viewport }),
      canvasCommand: { type: 'fit', nonce: 0 },
      requestCanvasAction: (type) =>
        set((state) => ({ canvasCommand: { type, nonce: state.canvasCommand.nonce + 1 } })),

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
          activeTerminalId: id,
          terminalSessions: exists
            ? state.terminalSessions
            : [...state.terminalSessions, { id, label: agent?.name ?? 'Shell', agentId }],
          terminalOutput: state.terminalOutput[id]
            ? state.terminalOutput
            : {
                ...state.terminalOutput,
                [id]: [
                  '# ' + (agent?.name ?? 'Bonsai shell'),
                  agent ? '# ' + agent.task : '# Frontend-only mock terminal',
                  '$ ',
                ],
              },
        })
      },
      setActiveTerminalId: (activeTerminalId) => set({ activeTerminalId }),
      appendTerminalCommand: (command) => {
        const { activeTerminalId, terminalOutput } = get()
        const lines = terminalOutput[activeTerminalId] ?? []
        const result =
          command.includes('test') || command.includes('go test')
            ? ['running tests…', '✓ all mock checks passed']
            : command.includes('dev')
              ? ['starting development server…', 'ready on http://localhost:5173']
              : command.includes('push')
                ? ['pushing feat/web-workspace…', 'Everything up-to-date (mock)']
                : ['command completed (mock)']
        set({
          activeDockTab: 'terminal',
          dockState: 'normal',
          terminalOutput: {
            ...terminalOutput,
            [activeTerminalId]: [...lines, '$ ' + command, ...result],
          },
          notice: 'Ran “' + command + '” in the mock terminal',
        })
      },

      moveBoardItem: (id, status) =>
        set((state) => ({
          boardItems: state.boardItems.map((item) => (item.id === id ? { ...item, status } : item)),
        })),
      setAgentState: (id, agentState) =>
        set((state) => ({
          agents: state.agents.map((agent) => (agent.id === id ? { ...agent, state: agentState } : agent)),
          notice: 'Agent moved to ' + agentState,
        })),
      startMockAgent: () => {
        const state = get()
        const selectedWorktree =
          state.selection.type === 'worktree'
            ? state.selection.id
            : state.selection.type === 'agent'
              ? state.agents.find((agent) => agent.id === state.selection.id)?.worktreeId
              : state.dockWorktreeId || state.worktrees.find(
                  (worktree) =>
                    worktree.projectId === state.activeProjectId &&
                    worktree.branch !== state.projects.find((project) => project.id === state.activeProjectId)?.defaultBranch,
                )?.id
        if (!selectedWorktree) {
          set({ notice: 'Create a worktree before starting an agent' })
          return
        }
        const worktreeId = selectedWorktree
        const id = 'agent-prototype-' + (state.agents.length + 1)
        const terminalId = 'term-prototype-' + (state.agents.length + 1)
        const agent: Agent = {
          id,
          worktreeId,
          name: 'Prototype agent ' + (state.agents.length + 1),
          provider: 'Codex',
          state: 'running',
          task: 'Exploring the next frontend interaction',
          runtime: 'just now',
          terminalId,
        }
        set({
          agents: [...state.agents, agent],
          worktrees: state.worktrees.map((worktree) =>
            worktree.id === worktreeId
              ? { ...worktree, agentIds: [...worktree.agentIds, id] }
              : worktree,
          ),
          selection: { type: 'agent', id },
          dockWorktreeId: worktreeId,
          terminalSessions: [...state.terminalSessions, { id: terminalId, label: agent.name, agentId: id }],
          terminalOutput: {
            ...state.terminalOutput,
            [terminalId]: ['# Prototype agent session', '# No backend process is running.', '$ '],
          },
          notice: 'Started a mock agent',
        })
      },
    }),
    {
      name: 'bonsai-web-workspace-v3',
      partialize: (state) => ({
        selection: state.selection,
        projects: state.projects,
        activeWorkspaceId: state.activeWorkspaceId,
        activeProjectId: state.activeProjectId,
        sidebarCollapsed: state.sidebarCollapsed,
        dockState: state.dockState,
        activeDockTab: state.activeDockTab,
        dockWorktreeId: state.dockWorktreeId,
        rightPanels: state.rightPanels,
        selectedFilePath: state.selectedFilePath,
        nodePositions: state.nodePositions,
        viewport: state.viewport,
        boardItems: state.boardItems,
        worktrees: state.worktrees,
        agents: state.agents,
        collapsedTagGroups: state.collapsedTagGroups,
        stackExcludedWorktreeIds: state.stackExcludedWorktreeIds,
        envVariables: state.envVariables,
      }),
    },
  ),
)
