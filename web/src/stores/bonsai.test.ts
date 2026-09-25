import { beforeEach, describe, expect, it } from 'vitest'
import { agents } from '../mock/agents'
import { boardItems, boardLists, boardPriorities, boardTypes } from '../mock/board'
import { projects } from '../mock/projects'
import { pullRequests } from '../mock/pullRequests'
import { worktreeTags } from '../mock/tags'
import { worktrees } from '../mock/worktrees'
import { useBonsaiStore } from './bonsai'

describe('bonsai mock store', () => {
  beforeEach(() => {
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
      dockWorktreeId: 'wt-web',
      dockRuntimeId: 'agent-ui',
      openRuntimeIds: ['agent-ui'],
      rightPanels: { files: true, prs: true },
      envVariables: {
        bonsai: [{ id: 'env-test', key: 'NODE_ENV', value: 'test', secret: false }],
      },
      notice: '',
    })
  })

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

  it('updates an agent state without a backend', () => {
    useBonsaiStore.getState().setAgentState('agent-ui', 'finished')
    expect(useBonsaiStore.getState().agents.find((agent) => agent.id === 'agent-ui')?.state).toBe('finished')
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
    expect(state.agents.find((agent) => agent.id === 'agent-ui')?.state).toBe('finished')
    expect(state.openRuntimeIds).not.toContain('agent-ui')
  })

  it('creates a worktree from a selected source, tag, and merge target', () => {
    const previousCount = useBonsaiStore.getState().worktrees.length
    useBonsaiStore.getState().createMockWorktree({
      sourceType: 'existing',
      sourceRef: 'feat/local-experiment',
      tagId: 'review-code',
      mergeTargetBranch: 'feat/web-workspace',
    })
    expect(useBonsaiStore.getState().worktrees).toHaveLength(previousCount + 1)
    const selection = useBonsaiStore.getState().selection
    expect(selection.type).toBe('worktree')
    if (selection.type !== 'worktree') return
    const created = useBonsaiStore.getState().worktrees.find((item) => item.id === selection.id)
    expect(created?.tag).toBe('review-code')
    expect(created?.branch).toBe('feat/local-experiment')
    expect(created?.mergeTargetBranch).toBe('feat/web-workspace')
    expect(created?.sourceType).toBe('existing')
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

  it('supports a persistent never-stack preference', () => {
    useBonsaiStore.getState().setWorktreeStackPreference('wt-web', 'never')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.stackPreference).toBe('never')
  })

  it('changes PR state and adds reviews', () => {
    useBonsaiStore.getState().setPullRequestStatus('pr-24', 'Merged')
    expect(useBonsaiStore.getState().pullRequests.find((pr) => pr.id === 'pr-24')?.status).toBe('Merged')
    useBonsaiStore.getState().addPullRequestReview('pr-23', 'Looks good after the fix.', 'approve')
    expect(useBonsaiStore.getState().pullRequests.find((pr) => pr.id === 'pr-23')?.conversation.at(-1)?.author).toBe('You')
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
})
