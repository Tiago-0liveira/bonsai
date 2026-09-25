import { beforeEach, describe, expect, it } from 'vitest'
import { agents } from '../mock/agents'
import { boardItems } from '../mock/board'
import { projects } from '../mock/projects'
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
      agents,
      worktrees,
      worktreeTags,
      collapsedTagGroups: ['bonsai:feat'],
      stackExcludedWorktreeIds: [],
      dockWorktreeId: 'wt-web',
      rightPanels: { files: true, prs: true },
      envVariables: {
        bonsai: [{ id: 'env-test', key: 'NODE_ENV', value: 'test', secret: false }],
      },
      notice: '',
    })
  })

  it('moves board items between columns', () => {
    useBonsaiStore.getState().moveBoardItem('b1', 'done')
    expect(useBonsaiStore.getState().boardItems.find((item) => item.id === 'b1')?.status).toBe('done')
  })

  it('updates an agent state without a backend', () => {
    useBonsaiStore.getState().setAgentState('agent-ui', 'finished')
    expect(useBonsaiStore.getState().agents.find((agent) => agent.id === 'agent-ui')?.state).toBe('finished')
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

  it('rejects duplicate worktree branches', () => {
    const previousCount = useBonsaiStore.getState().worktrees.length
    useBonsaiStore.getState().createMockWorktree({
      sourceType: 'existing',
      sourceRef: 'feat/web-workspace',
      tagId: 'feat',
      mergeTargetBranch: 'main',
    })
    expect(useBonsaiStore.getState().worktrees).toHaveLength(previousCount)
    expect(useBonsaiStore.getState().notice).toContain('already has a worktree')
  })

  it('prevents merge-target cycles', () => {
    useBonsaiStore.getState().setWorktreeMergeTarget('wt-web', 'feat/workspace-docs')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.mergeTargetBranch).toBe('main')
    expect(useBonsaiStore.getState().notice).toContain('cycle')
  })

  it('ejects one worktree from a collapsed stack without changing its tag', () => {
    useBonsaiStore.getState().ejectWorktreeFromStack('wt-web')
    expect(useBonsaiStore.getState().stackExcludedWorktreeIds).toContain('wt-web')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.tag).toBe('feat')
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

  it('creates and switches to a project in the current workspace', () => {
    useBonsaiStore.getState().createMockProject('demo app')
    const state = useBonsaiStore.getState()
    expect(state.activeProjectId).toBe('demo-app')
    expect(state.projects.find((item) => item.id === 'demo-app')?.workspaceId).toBe('personal')
  })

  it('updates a user-defined worktree tag', () => {
    useBonsaiStore.getState().setWorktreeTag('wt-web', 'review-code')
    expect(useBonsaiStore.getState().worktrees.find((agent) => agent.id === 'wt-web')?.tag).toBe('review-code')
  })

  it('toggles collapsed tag groups', () => {
    useBonsaiStore.getState().toggleTagGroup('bonsai', 'feat')
    expect(useBonsaiStore.getState().collapsedTagGroups).not.toContain('bonsai:feat')
  })
})
