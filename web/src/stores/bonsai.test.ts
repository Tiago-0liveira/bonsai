import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { agents } from '../mock/agents'
import { boardItems, boardLists, boardPriorities, boardTypes } from '../mock/board'
import { projects } from '../test/fixtures/projects'
import { pullRequests } from '../test/fixtures/pullRequests'
import { worktreeTags } from '../mock/tags'
import { worktrees } from '../test/fixtures/worktrees'
import { useBonsaiStore } from './bonsai'

describe('bonsai store', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('daemon offline')))
    localStorage.clear()
    useBonsaiStore.setState({
      selection: { type: 'project', id: 'bonsai' },
      projects,
      activeWorkspaceId: 'personal',
      activeProjectId: 'bonsai',
      sidebarCollapsed: false,
      boardItems,
      boardLists,
      boardPriorities,
      boardTypes,
      pullRequests,
      agents,
      worktrees,
      worktreeTags,
      collapsedTagGroups: ['bonsai:feat'],
      detachedStackWorktreeIds: [],
      nodePlacements: {},
      dockWorktreeId: 'wt-web',
      dockRuntimeId: 'agent-ui',
      openRuntimeIds: ['agent-ui'],
      rightPanels: { files: true, prs: true },
      dockHeight: 30,
      envVariables: {
        bonsai: [{ id: 'env-test', key: 'NODE_ENV', value: 'test', secret: false }],
      },
      notice: '',
    })
  })

  afterEach(() => vi.unstubAllGlobals())

  it('moves board items between user-defined lists', () => {
    useBonsaiStore.getState().moveBoardItem('b1', 'bug')
    expect(useBonsaiStore.getState().boardItems.find((item) => item.id === 'b1')?.status).toBe('bug')
  })

  it('adds and removes dynamic table lists while moving cards', () => {
    useBonsaiStore.getState().addBoardList()
    const list = useBonsaiStore.getState().boardLists.at(-1)
    expect(list).toBeDefined()
    if (!list) return
    useBonsaiStore.getState().moveBoardItem('b1', list.id)
    useBonsaiStore.getState().removeBoardList(list.id, 'feat')
    expect(useBonsaiStore.getState().boardItems.find((item) => item.id === 'b1')?.status).toBe('feat')
  })

  it('stopping an agent keeps it on the canvas until history is explicitly requested', () => {
    useBonsaiStore.getState().setAgentState('agent-ui', 'finished')
    const stopped = useBonsaiStore.getState().agents.find((agent) => agent.id === 'agent-ui')
    expect(stopped?.state).toBe('finished')
    expect(stopped?.presentation).toBe('canvas')

    useBonsaiStore.getState().moveAgentToHistory('agent-ui')
    expect(useBonsaiStore.getState().agents.find((agent) => agent.id === 'agent-ui')?.presentation).toBe('history')

    useBonsaiStore.getState().restoreAgentFromHistory('agent-ui')
    expect(useBonsaiStore.getState().agents.find((agent) => agent.id === 'agent-ui')?.presentation).toBe('canvas')
  })

  it('selects the exact child agent runtime', () => {
    useBonsaiStore.getState().setSelection({ type: 'agent', id: 'agent-tests' })
    const state = useBonsaiStore.getState()
    expect(state.dockWorktreeId).toBe('wt-web')
    expect(state.dockRuntimeId).toBe('agent-tests')
    expect(state.openRuntimeIds).toContain('agent-tests')
    expect(state.activeTerminalId).toBe('term-tests')
  })

  it('creates an agent only on the explicitly selected worktree', () => {
    const count = useBonsaiStore.getState().agents.length
    useBonsaiStore.getState().createAgent({
      worktreeId: 'wt-daemon',
      name: 'Focused debugger',
      provider: 'Codex',
      model: 'gpt-5.6-codex',
      reasoningEffort: 'High',
      fastMode: true,
      workType: 'Debugging',
      prompt: 'Trace the daemon lifecycle failure.',
    })
    const state = useBonsaiStore.getState()
    expect(state.agents).toHaveLength(count + 1)
    expect(state.agents.at(-1)?.worktreeId).toBe('wt-daemon')
    expect(state.dockWorktreeId).toBe('wt-daemon')
  })

  it('archives a running agent and removes it from open runtimes', () => {
    useBonsaiStore.setState({ openRuntimeIds: ['agent-ui'], dockRuntimeId: 'agent-ui' })
    useBonsaiStore.getState().archiveAgent('agent-ui')
    const state = useBonsaiStore.getState()
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.archived).toBe(true)
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.presentation).toBe('archived')
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.state).toBe('finished')
    expect(state.openRuntimeIds).not.toContain('agent-ui')
  })

  it('sends worktree creation to the daemon API and preserves state on failure', async () => {
    const previous = useBonsaiStore.getState().worktrees
    useBonsaiStore.getState().createMockWorktree({ sourceType: 'existing', sourceRef: 'feat/local-experiment', tagId: 'review-code', mergeTargetBranch: 'main' })
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(useBonsaiStore.getState().worktrees).toBe(previous)
    expect(fetch).toHaveBeenCalledWith('/api/projects/bonsai/worktrees', expect.objectContaining({ method: 'POST', body: JSON.stringify({ mode: 'existing', branch: 'feat/local-experiment', base: 'feat/local-experiment' }) }))
  })

  it('prevents merge-target cycles', () => {
    useBonsaiStore.getState().setWorktreeMergeTarget('wt-web', 'feat/workspace-docs')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.mergeTargetBranch).toBe('main')
    expect(useBonsaiStore.getState().notice).toContain('cycle')
  })

  it('temporarily detaches a worktree from a stack and restores it when the group toggles', () => {
    useBonsaiStore.getState().ejectWorktreeFromStack('wt-web')
    expect(useBonsaiStore.getState().detachedStackWorktreeIds).toContain('wt-web')
    useBonsaiStore.getState().toggleTagGroup('bonsai', 'feat')
    expect(useBonsaiStore.getState().detachedStackWorktreeIds).not.toContain('wt-web')
  })

  it('persists stack preference through the metadata API before changing state', async () => {
    useBonsaiStore.getState().setWorktreeStackPreference('wt-web', 'never')
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(useBonsaiStore.getState().worktrees.find(item => item.id === 'wt-web')?.stackPreference).not.toBe('never')
    expect(fetch).toHaveBeenCalledWith('/api/worktrees/wt-web/metadata', expect.objectContaining({ method: 'PATCH' }))
  })

  it('does not claim a PR merged when GitHub fails', async () => {
    const pr = { ...pullRequests[0], id: 'bonsai:24' }
    useBonsaiStore.setState({ pullRequests: [pr] })
    useBonsaiStore.getState().setPullRequestStatus(pr.id, 'Merged')
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(useBonsaiStore.getState().pullRequests[0].status).toBe(pr.status)
    expect(fetch).toHaveBeenCalledWith('/api/projects/bonsai/pull-requests/' + pr.number + '/merge', expect.objectContaining({ method: 'POST' }))
  })

  it('toggles independent right-side dock panels', () => {
    useBonsaiStore.getState().toggleRightPanel('files')
    expect(useBonsaiStore.getState().rightPanels.files).toBe(false)
    expect(useBonsaiStore.getState().rightPanels.prs).toBe(true)
  })

  it('adds, updates, and removes project environment variables', () => {
    useBonsaiStore.getState().addEnvVariable('bonsai')
    const added = useBonsaiStore.getState().envVariables.bonsai.at(-1)
    expect(added).toBeDefined()
    if (!added) return
    useBonsaiStore.getState().updateEnvVariable('bonsai', added.id, { key: 'API_KEY', value: 'secret' })
    expect(useBonsaiStore.getState().envVariables.bonsai.find((item) => item.id === added.id)?.key).toBe('API_KEY')
    useBonsaiStore.getState().removeEnvVariable('bonsai', added.id)
    expect(useBonsaiStore.getState().envVariables.bonsai.some((item) => item.id === added.id)).toBe(false)
  })

  it('preserves placements when stacks are toggled or detached', () => {
    useBonsaiStore.getState().setManualNodePlacement('wt-web', { x: 120, y: 240 })
    useBonsaiStore.getState().setGeneratedNodePlacements({
      'stack:bonsai:feat': { x: 400, y: 260 },
    })

    useBonsaiStore.getState().toggleTagGroup('bonsai', 'feat')
    useBonsaiStore.getState().ejectWorktreeFromStack('wt-web')

    const placements = useBonsaiStore.getState().nodePlacements
    expect(placements['wt-web']).toEqual({ x: 120, y: 240, mode: 'manual' })
    expect(placements['stack:bonsai:feat']).toEqual({ x: 400, y: 260, mode: 'generated' })
  })

  it('preserves manual placement across merge-target and metadata changes', () => {
    useBonsaiStore.getState().setManualNodePlacement('wt-daemon', { x: 620, y: 310 })
    const before = useBonsaiStore.getState().nodePlacements['wt-daemon']

    useBonsaiStore.getState().setWorktreeMergeTarget('wt-daemon', 'feat/web-workspace')
    useBonsaiStore.getState().setAgentState('agent-ui', 'finished')
    useBonsaiStore.getState().setWorktreeStackPreference('wt-web', 'never')

    expect(useBonsaiStore.getState().nodePlacements['wt-daemon']).toEqual(before)
  })

  it('removes only an agent placement when moving it to history', () => {
    useBonsaiStore.getState().setGeneratedNodePlacements({
      'agent-ui': { x: 300, y: 500 },
      'wt-web': { x: 260, y: 240 },
    })

    useBonsaiStore.getState().moveAgentToHistory('agent-ui')

    const placements = useBonsaiStore.getState().nodePlacements
    expect(placements['agent-ui']).toBeUndefined()
    expect(placements['wt-web']).toEqual({ x: 260, y: 240, mode: 'generated' })
  })

  it('marks drag-style placement updates as manual', () => {
    useBonsaiStore.getState().setGeneratedNodePlacements({
      'wt-web': { x: 10, y: 20 },
    })
    useBonsaiStore.getState().setManualNodePlacement('wt-web', { x: 44, y: 88 })

    expect(useBonsaiStore.getState().nodePlacements['wt-web']).toEqual({
      x: 44,
      y: 88,
      mode: 'manual',
    })
  })

})


describe('dock sizing', () => {
  it('clamps the remembered open dock height', () => {
    useBonsaiStore.getState().setDockHeight(4)
    expect(useBonsaiStore.getState().dockHeight).toBe(14)
    useBonsaiStore.getState().setDockHeight(90)
    expect(useBonsaiStore.getState().dockHeight).toBe(72)
  })
})
