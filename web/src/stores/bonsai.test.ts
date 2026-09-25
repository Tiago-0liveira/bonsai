import { beforeEach, describe, expect, it } from 'vitest'
import { agents } from '../mock/agents'
import { boardItems } from '../mock/board'
import { worktrees } from '../mock/worktrees'
import { useBonsaiStore } from './bonsai'

describe('bonsai mock store', () => {
  beforeEach(() => {
    localStorage.clear()
    useBonsaiStore.setState({
      selection: { type: 'project', id: 'bonsai' },
      boardItems,
      agents,
      worktrees,
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

  it('creates a mock worktree and selects it', () => {
    const previousCount = useBonsaiStore.getState().worktrees.length
    useBonsaiStore.getState().createMockWorktree()
    expect(useBonsaiStore.getState().worktrees).toHaveLength(previousCount + 1)
    expect(useBonsaiStore.getState().selection.type).toBe('worktree')
  })
})
