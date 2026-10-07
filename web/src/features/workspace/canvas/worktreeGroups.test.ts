import { describe, expect, it } from 'vitest'
import { worktrees } from '../../../test/fixtures/worktrees'
import { groupCanvasWorktrees } from './worktreeGroups'

describe('canonical canvas groups', () => {
  it('gives unlinked membership precedence without changing user tags or duplicating members', () => {
    const trees = worktrees.slice(0, 3).map((tree, index) => ({ ...tree, id: `tree-${index}`, tag: index === 1 ? 'other' : 'feat' }))
    const before = structuredClone(trees)
    const groups = groupCanvasWorktrees(trees, [{ id: 'unlinked:repo', kind: 'unlinked', worktree_ids: ['tree-0', 'tree-1', 'stale'] }])
    expect(groups.map(group => [group.label, group.items.map(item => item.id)])).toEqual([['Local / unlinked', ['tree-0', 'tree-1']], ['feat', ['tree-2']]])
    expect(trees).toEqual(before)
  })
})
