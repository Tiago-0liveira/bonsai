import { useCallback } from 'react'
import { useShellRoute } from '../../app/shellRoute'
import { useBonsaiStore } from '../../stores/bonsai'

/**
 * Selection from the Branches island. Besides selecting, it follows the open
 * page: the canvas centres the row's node, and the GitHub tab shows the
 * worktree's pull request (or switches to the canvas, per Settings).
 */
export function useBranchSelect() {
  const { page, showCanvas, showPullRequest } = useShellRoute()
  return useCallback((type: 'worktree' | 'agent', id: string) => {
    const state = useBonsaiStore.getState()
    state.setSelection({ type, id })
    const preferences = state.canvasPreferences
    const focus = type === 'worktree' ? preferences.focusOnBranchSelect : preferences.focusAgentsOnBranchSelect
    if (page === 'canvas') {
      if (focus) state.requestCanvasFocus(state.activeProjectId, id)
      return
    }
    if (page !== 'github') return
    if (preferences.githubBranchSelect === 'open-canvas') {
      showCanvas()
      if (focus) state.requestCanvasFocus(state.activeProjectId, id)
      return
    }
    const worktreeId = type === 'worktree' ? id : state.agents.find(agent => agent.id === id)?.worktreeId
    const worktree = state.worktrees.find(tree => tree.id === worktreeId)
    if (!worktree) return
    if (worktree.prNumber) showPullRequest(`${worktree.projectId}:${worktree.prNumber}`)
    else state.setNotice(`No pull request for ${worktree.branch}`)
  }, [page, showCanvas, showPullRequest])
}
