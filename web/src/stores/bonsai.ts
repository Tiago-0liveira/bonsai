import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agents as initialAgents } from '../mock/agents'
import { boardItems as initialBoardItems } from '../mock/board'
import { projects as initialProjects, workspaces } from '../mock/projects'
import { worktrees as initialWorktrees } from '../mock/worktrees'
import type {
  Agent,
  AgentState,
  BoardItem,
  BoardStatus,
  DockState,
  DockTab,
  Project,
  Selection,
  ViewportState,
  Worktree,
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
  agents: Agent[]
  boardItems: BoardItem[]
  collapsedTagGroups: string[]
  toggleTagGroup: (projectId: string, tag: string) => void
  setWorktreeTag: (id: string, tag: string) => void

  dockState: DockState
  setDockState: (state: DockState) => void
  activeDockTab: DockTab
  setActiveDockTab: (tab: DockTab) => void

  selectedFilePath: string
  setSelectedFilePath: (path: string) => void

  nodePositions: Record<string, { x: number; y: number }>
  setNodePosition: (id: string, position: { x: number; y: number }) => void
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
  createMockWorktree: (tag?: string) => void
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

export const useBonsaiStore = create<BonsaiState>()(
  persist(
    (set, get) => ({
      selection: { type: 'project', id: 'bonsai' },
      setSelection: (selection) => set({ selection }),
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
        set({
          activeWorkspaceId,
          activeProjectId: nextProject?.id ?? state.activeProjectId,
          selection: nextProject ? { type: 'project', id: nextProject.id } : state.selection,
          nodePositions: {},
          notice: nextProject ? 'Switched workspace to ' + workspace?.name : 'Workspace selected',
        })
      },
      setActiveProject: (activeProjectId) => {
        const project = get().projects.find((item) => item.id === activeProjectId)
        if (!project) return
        set({
          activeProjectId,
          activeWorkspaceId: project.workspaceId,
          selection: { type: 'project', id: project.id },
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
            worktreeIds: [],
            openPrCount: 0,
          }
          return {
            projects: [...state.projects, project],
            activeProjectId: id,
            selection: { type: 'project', id },
            nodePositions: {},
            notice: 'Added project ' + displayName,
          }
        }),

      sidebarCollapsed: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      worktrees: initialWorktrees,
      agents: initialAgents,
      boardItems: initialBoardItems,
      collapsedTagGroups: ['bonsai:feat'],
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
      setWorktreeTag: (id, tag) =>
        set((state) => {
          const nextTag = tag.trim() || 'untagged'
          return {
            worktrees: state.worktrees.map((worktree) =>
              worktree.id === id ? { ...worktree, tag: nextTag } : worktree,
            ),
            nodePositions: {},
            notice: 'Worktree tag updated to ' + nextTag,
          }
        }),

      dockState: 'normal',
      setDockState: (dockState) => set({ dockState }),
      activeDockTab: 'terminal',
      setActiveDockTab: (activeDockTab) => set({ activeDockTab }),

      selectedFilePath: 'package.json',
      setSelectedFilePath: (selectedFilePath) => set({ selectedFilePath }),

      nodePositions: {},
      setNodePosition: (id, position) =>
        set((state) => ({ nodePositions: { ...state.nodePositions, [id]: position } })),
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
      createMockWorktree: (tag = 'feat') =>
        set((state) => {
          const projectId = state.activeProjectId
          const project = state.projects.find((item) => item.id === projectId)
          const projectWorktrees = state.worktrees.filter((item) => item.projectId === projectId)
          const id = 'wt-' + projectId + '-' + (projectWorktrees.length + 1)
          const branch = tag + '/prototype-' + (projectWorktrees.length + 1)
          const worktree: Worktree = {
            id,
            projectId,
            branch,
            kind: 'Feature',
            tag,
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
              item.id === project?.id ? { ...item, worktreeIds: [...item.worktreeIds, id] } : item,
            ),
            selection: { type: 'worktree', id },
            nodePositions: {},
            notice: 'Created a mock ' + tag + ' worktree',
          }
        }),
      startMockAgent: () => {
        const state = get()
        const selectedWorktree =
          state.selection.type === 'worktree'
            ? state.selection.id
            : state.selection.type === 'agent'
              ? state.agents.find((agent) => agent.id === state.selection.id)?.worktreeId
              : state.worktrees.find((worktree) => worktree.projectId === state.activeProjectId)?.id
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
      name: 'bonsai-web-workspace-v2',
      partialize: (state) => ({
        selection: state.selection,
        projects: state.projects,
        activeWorkspaceId: state.activeWorkspaceId,
        activeProjectId: state.activeProjectId,
        sidebarCollapsed: state.sidebarCollapsed,
        dockState: state.dockState,
        activeDockTab: state.activeDockTab,
        selectedFilePath: state.selectedFilePath,
        nodePositions: state.nodePositions,
        viewport: state.viewport,
        boardItems: state.boardItems,
        worktrees: state.worktrees,
        agents: state.agents,
        collapsedTagGroups: state.collapsedTagGroups,
      }),
    },
  ),
)
