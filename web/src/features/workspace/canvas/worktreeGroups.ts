import type { WorktreeGroup } from '../../../api/git'
import type { Worktree } from '../../../types'

export function groupCanvasWorktrees(worktrees: Worktree[], canonicalGroups: WorktreeGroup[]) {
  const membership = new Map<string, string>()
  for (const group of canonicalGroups) {
    if (group.kind === 'unlinked') for (const id of group.worktree_ids) membership.set(id, group.id)
  }
  const groups = new Map<string, { groupId?: string; label: string; items: Worktree[] }>()
  for (const worktree of worktrees) {
    const groupId = membership.get(worktree.id)
    const key = groupId ? `automatic:${groupId}` : `tag:${worktree.tag}`
    const group = groups.get(key) ?? { groupId, label: groupId ? 'Local / unlinked' : worktree.tag, items: [] }
    group.items.push(worktree)
    groups.set(key, group)
  }
  return [...groups.values()]
}
