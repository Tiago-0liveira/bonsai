import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { agents } from '../mock/agents'
import { projects } from '../test/fixtures/projects'
import { pullRequests } from '../test/fixtures/pullRequests'
import { worktrees } from '../test/fixtures/worktrees'
import { __resetLocalClientForTests, connectLocalBonsai } from '../api/localClient'
import { useBonsaiStore } from './bonsai'

describe('bonsai store', () => {
  beforeEach(async () => {
    __resetLocalClientForTests()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ version: 'test', api_version: 3 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'test-session', expires_at: new Date(Date.now() + 60_000).toISOString() }), { status: 201, headers: { 'Content-Type': 'application/json' } }))
      .mockRejectedValue(new Error('daemon offline')))
    await connectLocalBonsai()
    localStorage.clear()
    useBonsaiStore.setState({
      selection: { type: 'project', id: 'bonsai' },
      projects,
      activeWorkspaceId: 'personal',
      activeProjectId: 'bonsai',
      sidebarCollapsed: false,
      pullRequests,
      agents,
      worktrees,
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
      terminalSessions: [{ id: 'fixture-terminal', label: 'Recorded fixture' }],
      activeTerminalId: 'fixture-terminal',
      terminalOutput: { 'fixture-terminal': ['Recorded presentation fixture'] },
      notice: '',
    })
  })

  afterEach(() => vi.unstubAllGlobals())

  it('rejects agent stop and restart without changing runtime state', () => {
    const previous = useBonsaiStore.getState().agents
    for (const state of ['finished', 'running', 'idle'] as const) {
      useBonsaiStore.getState().setAgentState('agent-ui', state)
      expect(useBonsaiStore.getState().agents).toBe(previous)
      expect(useBonsaiStore.getState().notice).toContain('unavailable')
    }
  })

  it('changes history presentation without manufacturing a finished state', () => {
    useBonsaiStore.getState().moveAgentToHistory('agent-ui')
    expect(useBonsaiStore.getState().agents.find(agent => agent.id === 'agent-ui')).toMatchObject({ state: 'running', presentation: 'history' })
    useBonsaiStore.getState().restoreAgentFromHistory('agent-ui')
    expect(useBonsaiStore.getState().agents.find(agent => agent.id === 'agent-ui')?.presentation).toBe('canvas')
  })

  it('selects the exact child agent runtime', () => {
    useBonsaiStore.getState().setSelection({ type: 'agent', id: 'agent-tests' })
    const state = useBonsaiStore.getState()
    expect(state.dockWorktreeId).toBe('wt-web')
    expect(state.dockRuntimeId).toBe('agent-tests')
    expect(state.openRuntimeIds).toContain('agent-tests')
    expect(state.activeTerminalId).toBe('term-tests')
  })

  it('rejects unsupported provider creation without changing runtime state', async () => {
    const previous = useBonsaiStore.getState()
    await expect(useBonsaiStore.getState().createAgent({
      worktreeId: 'wt-daemon',
      name: 'Focused debugger',
      provider: 'Codex',
      model: 'gpt-5.6-codex',
      reasoningEffort: 'High',
      fastMode: true,
      workType: 'Debugging',
      prompt: 'Trace the daemon lifecycle failure.',
    })).rejects.toThrow('Antigravity')
    const state = useBonsaiStore.getState()
    for (const key of ['agents', 'worktrees', 'selection', 'terminalSessions', 'terminalOutput', 'openRuntimeIds', 'dockRuntimeId'] as const) expect(state[key]).toBe(previous[key])
  })

  it('archives presentation without claiming to stop a running agent', () => {
    useBonsaiStore.setState({ openRuntimeIds: ['agent-ui'], dockRuntimeId: 'agent-ui' })
    useBonsaiStore.getState().archiveAgent('agent-ui')
    const state = useBonsaiStore.getState()
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.archived).toBe(true)
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.presentation).toBe('archived')
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.state).toBe('running')
    expect(state.openRuntimeIds).not.toContain('agent-ui')
  })

  it('starts with empty simulated runtime collections', () => {
    const initial = useBonsaiStore.getInitialState()
    expect(initial.agents).toEqual([])
    expect(initial.terminalSessions).toEqual([])
    expect(initial.terminalOutput).toEqual({})
    expect(initial.activeTerminalId).toBe('')
  })

  it.each([undefined, 'agent-ui'])('rejects shell creation for %s', (id) => {
    const previous = useBonsaiStore.getState()
    useBonsaiStore.getState().openTerminal(id)
    const state = useBonsaiStore.getState()
    expect(state.terminalSessions).toBe(previous.terminalSessions)
    expect(state.terminalOutput).toBe(previous.terminalOutput)
    expect(state.activeTerminalId).toBe(previous.activeTerminalId)
    expect(state.notice).toContain('unavailable')
  })

  it.each(['pnpm test', 'go test ./...', 'cargo test', 'make test', 'pnpm dev', 'echo hello', 'git pull && pnpm dev'])('never manufactures output for %s', (command) => {
    const previous = useBonsaiStore.getState()
    const fetchCount = vi.mocked(fetch).mock.calls.length
    useBonsaiStore.getState().appendTerminalCommand(command)
    expect(useBonsaiStore.getState().terminalOutput).toBe(previous.terminalOutput)
    expect(useBonsaiStore.getState().terminalSessions).toBe(previous.terminalSessions)
    expect(useBonsaiStore.getState().notice).not.toMatch(/passed|completed|ready|Ran/)
    expect(vi.mocked(fetch).mock.calls).toHaveLength(fetchCount)
  })

  it.each(['pull', 'push', 'fetch'])('keeps structured Git %s usable without shell output', async (action) => {
    const output = useBonsaiStore.getState().terminalOutput
    useBonsaiStore.getState().appendTerminalCommand('git ' + action)
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:7001/api/worktrees/wt-web/' + action, expect.objectContaining({ method: 'POST' }))
    expect(useBonsaiStore.getState().terminalOutput).toBe(output)
  })

  it('reports external editor unavailability while saving its preference', () => {
    useBonsaiStore.setState({ editorPreference: 'vscode', pendingOpenFile: '' })
    useBonsaiStore.getState().requestOpenFile('src/main.ts')
    expect(useBonsaiStore.getState().selectedFilePath).toBe('src/main.ts')
    expect(useBonsaiStore.getState().notice).toContain('unavailable')
    useBonsaiStore.setState({ pendingOpenFile: 'src/main.ts' })
    useBonsaiStore.getState().setEditorPreference('cursor')
    expect(useBonsaiStore.getState().editorPreference).toBe('cursor')
    expect(useBonsaiStore.getState().notice).toContain('unavailable')
  })

  it('legacy agent launch callers open profile selection without creating a session', () => {
    const agents = useBonsaiStore.getState().agents
    useBonsaiStore.getState().startMockAgent()
    expect(useBonsaiStore.getState().agents).toBe(agents)
    expect(useBonsaiStore.getState().startAgentDialogOpen).toBe(true)
  })

  it('sends worktree creation to the daemon API and preserves state on failure', async () => {
    const previous = useBonsaiStore.getState().worktrees
    await expect(useBonsaiStore.getState().createWorktree({ sourceType: 'existing', sourceRef: 'feat/local-experiment', mergeTargetBranch: 'main' })).rejects.toThrow('daemon offline')
    expect(useBonsaiStore.getState().worktrees).toBe(previous)
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:7001/api/projects/bonsai/worktrees', expect.objectContaining({ method: 'POST', credentials: 'omit', body: JSON.stringify({ mode: 'existing', branch: 'feat/local-experiment', base: 'feat/local-experiment' }) }))
  })

  it('prevents merge-target cycles', () => {
    useBonsaiStore.getState().setWorktreeMergeTarget('wt-web', 'feat/workspace-docs')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.mergeTargetBranch).toBe('main')
    expect(useBonsaiStore.getState().notice).toContain('cycle')
  })

  it('temporarily detaches a worktree from a stack and restores it when the group toggles', () => {
    useBonsaiStore.getState().ejectWorktreeFromStack('wt-web')
    expect(useBonsaiStore.getState().detachedStackWorktreeIds).toContain('wt-web')
    useBonsaiStore.setState({ worktreeGroups: { bonsai: [{ id: 'unlinked:bonsai', kind: 'unlinked', worktree_ids: ['wt-web', 'wt-docs'] }] } })
    useBonsaiStore.getState().toggleAutomaticGroup('unlinked:bonsai')
    expect(useBonsaiStore.getState().detachedStackWorktreeIds).not.toContain('wt-web')
  })

  it('persists stack preference through the metadata API before changing state', async () => {
    useBonsaiStore.getState().setWorktreeStackPreference('wt-web', 'never')
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(useBonsaiStore.getState().worktrees.find(item => item.id === 'wt-web')?.stackPreference).not.toBe('never')
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:7001/api/worktrees/wt-web/metadata', expect.objectContaining({ method: 'PATCH', credentials: 'omit' }))
  })

  it('does not claim a PR merged when GitHub fails', async () => {
    const pr = { ...pullRequests[0], id: 'bonsai:24' }
    useBonsaiStore.setState({ pullRequests: [pr] })
    useBonsaiStore.getState().setPullRequestStatus(pr.id, 'Merged')
    await vi.waitFor(() => expect(useBonsaiStore.getState().notice).toBe('daemon offline'))
    expect(useBonsaiStore.getState().pullRequests[0].status).toBe(pr.status)
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:7001/api/projects/bonsai/pull-requests/' + pr.number + '/merge', expect.objectContaining({ method: 'POST', credentials: 'omit' }))
  })

  it('toggles independent right-side dock panels', () => {
    useBonsaiStore.getState().toggleRightPanel('files')
    expect(useBonsaiStore.getState().rightPanels.files).toBe(false)
    expect(useBonsaiStore.getState().rightPanels.prs).toBe(true)
  })

  it('rejects environment edits through the action layer', () => {
    const variables = useBonsaiStore.getState().envVariables
    useBonsaiStore.getState().addEnvVariable('bonsai')
    expect(useBonsaiStore.getState().envVariables).toBe(variables)
    useBonsaiStore.getState().updateEnvVariable('bonsai', 'env-test', { key: 'API_KEY', value: 'synthetic-marker', secret: false })
    expect(useBonsaiStore.getState().envVariables).toBe(variables)
    useBonsaiStore.getState().removeEnvVariable('bonsai', 'env-test')
    expect(useBonsaiStore.getState().envVariables).toBe(variables)
    expect(useBonsaiStore.getState().notice).toContain('unavailable')
  })

  it('preserves placements when stacks are toggled or detached', () => {
    useBonsaiStore.getState().setManualNodePlacement('wt-web', { x: 120, y: 240 })
    useBonsaiStore.getState().setGeneratedNodePlacements({
      'stack:unlinked:bonsai': { x: 400, y: 260 },
    })

    useBonsaiStore.getState().toggleAutomaticGroup('unlinked:bonsai')
    useBonsaiStore.getState().ejectWorktreeFromStack('wt-web')

    const placements = useBonsaiStore.getState().nodePlacements
    expect(placements['wt-web']).toEqual({ x: 120, y: 240, mode: 'manual' })
    expect(placements['stack:unlinked:bonsai']).toEqual({ x: 400, y: 260, mode: 'generated' })
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
