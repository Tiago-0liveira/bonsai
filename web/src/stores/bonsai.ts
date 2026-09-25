import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agents as initialAgents } from '../mock/agents'
import { boardItems as initialBoardItems } from '../mock/board'
import { worktrees as initialWorktrees } from '../mock/worktrees'
import type {
  Agent,
  AgentState,
  BoardItem,
  BoardStatus,
  DockState,
  DockTab,
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

  worktrees: Worktree[]
  agents: Agent[]
  boardItems: BoardItem[]

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
  createMockWorktree: () => void
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

export const useBonsaiStore = create<BonsaiState>()(
  persist(
    (set, get) => ({
      selection: { type: 'project', id: 'bonsai' },
      setSelection: (selection) => set({ selection }),
      projectQuery: '',
      setProjectQuery: (projectQuery) => set({ projectQuery }),

      worktrees: initialWorktrees,
      agents: initialAgents,
      boardItems: initialBoardItems,

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
        const id = agent?.terminalId ?? `term-${Date.now()}`
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
                  `# ${agent?.name ?? 'Bonsai shell'}`,
                  agent ? `# ${agent.task}` : '# Frontend-only mock terminal',
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
            [activeTerminalId]: [...lines, `$ ${command}`, ...result],
          },
          notice: `Ran “${command}” in the mock terminal`,
        })
      },

      moveBoardItem: (id, status) =>
        set((state) => ({
          boardItems: state.boardItems.map((item) => (item.id === id ? { ...item, status } : item)),
        })),
      setAgentState: (id, agentState) =>
        set((state) => ({
          agents: state.agents.map((agent) => (agent.id === id ? { ...agent, state: agentState } : agent)),
          notice: `Agent moved to ${agentState}`,
        })),
      createMockWorktree: () =>
        set((state) => {
          const id = `wt-prototype-${state.worktrees.length + 1}`
          return {
            worktrees: [
              ...state.worktrees,
              {
                id,
                projectId: 'bonsai',
                branch: `feat/prototype-${state.worktrees.length + 1}`,
                kind: 'Feature',
                status: 'idle',
                agentIds: [],
                ahead: 0,
                behind: 0,
                gitState: 'clean',
              },
            ],
            selection: { type: 'worktree', id },
            notice: 'Created a mock worktree',
          }
        }),
      startMockAgent: () =>
        set((state) => {
          const selectedWorktree =
            state.selection.type === 'worktree'
              ? state.selection.id
              : state.selection.type === 'agent'
                ? state.agents.find((agent) => agent.id === state.selection.id)?.worktreeId
                : 'wt-web'
          const worktreeId = selectedWorktree ?? 'wt-web'
          const id = `agent-prototype-${state.agents.length + 1}`
          const terminalId = `term-prototype-${state.agents.length + 1}`
          const agent: Agent = {
            id,
            worktreeId,
            name: `Prototype agent ${state.agents.length + 1}`,
            provider: 'Codex',
            state: 'running',
            task: 'Exploring the next frontend interaction',
            runtime: 'just now',
            terminalId,
          }
          return {
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
          }
        }),
    }),
    {
      name: 'bonsai-web-workspace',
      partialize: (state) => ({
        selection: state.selection,
        dockState: state.dockState,
        activeDockTab: state.activeDockTab,
        selectedFilePath: state.selectedFilePath,
        nodePositions: state.nodePositions,
        viewport: state.viewport,
        boardItems: state.boardItems,
      }),
    },
  ),
)
