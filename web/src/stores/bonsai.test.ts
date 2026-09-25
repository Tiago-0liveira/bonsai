import { beforeEach, describe, expect, it } from 'vitest'
import { agents } from '../mock/agents'
import { boardItems } from '../mock/board'
import { projects } from '../mock/projects'
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
      collapsedTagGroups: ['bonsai:feat'],
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

  it('creates a mock worktree on the active project', () => {
    const previousCount = useBonsaiStore.getState().worktrees.length
    useBonsaiStore.getState().createMockWorktree('review-code')
    expect(useBonsaiStore.getState().worktrees).toHaveLength(previousCount + 1)
    const selection = useBonsaiStore.getState().selection
    expect(selection.type).toBe('worktree')
    const created = useBonsaiStore.getState().worktrees.find((item) => item.id === selection.id)
    expect(created?.tag).toBe('review-code')
    expect(created?.projectId).toBe('bonsai')
  })

  it('creates and switches to a project in the current workspace', () => {
    useBonsaiStore.getState().createMockProject('demo app')
    const state = useBonsaiStore.getState()
    expect(state.activeProjectId).toBe('demo-app')
    expect(state.projects.find((item) => item.id === 'demo-app')?.workspaceId).toBe('personal')
  })

  it('updates a user-defined worktree tag', () => {
    useBonsaiStore.getState().setWorktreeTag('wt-web', 'design-review')
    expect(useBonsaiStore.getState().worktrees.find((item) => item.id === 'wt-web')?.tag).toBe('design-review')
  })

  it('toggles collapsed tag groups', () => {
    useBonsaiStore.getState().toggleTagGroup('bonsai', 'feat')
    expect(useBonsaiStore.getState().collapsedTagGroups).not.toContain('bonsai:feat')
  })
})
