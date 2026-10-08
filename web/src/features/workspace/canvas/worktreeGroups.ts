import type { WorktreeGroup } from '../../../api/git'
import type { Worktree } from '../../../types'

// Only the server's "unlinked" connection groups collapse into a stack. Every
// other worktree stays on the canvas as its own node.
export function groupCanvasWorktrees(worktrees: Worktree[], canonicalGroups: WorktreeGroup[]) {
  const membership = new Map<string, string>()
  for (const group of canonicalGroups) {
    if (group.kind === 'unlinked') for (const id of group.worktree_ids) membership.set(id, group.id)
  }
  const groups = new Map<string, { groupId?: string; label: string; items: Worktree[] }>()
  for (const worktree of worktrees) {
    const groupId = membership.get(worktree.id)
    const key = groupId ? `automatic:${groupId}` : 'ungrouped'
    const group = groups.get(key) ?? { groupId, label: groupId ? 'Local / unlinked' : '', items: [] }
    group.items.push(worktree)
    groups.set(key, group)
  }
  return [...groups.values()]
}
