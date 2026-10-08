import { describe, expect, it } from 'vitest'
import { worktrees } from '../../../test/fixtures/worktrees'
import { groupCanvasWorktrees } from './worktreeGroups'

describe('canonical canvas groups', () => {
  it('stacks only unlinked members, once each, and leaves every other worktree ungrouped', () => {
    const trees = worktrees.slice(0, 3).map((tree, index) => ({ ...tree, id: `tree-${index}` }))
    const before = structuredClone(trees)
    const groups = groupCanvasWorktrees(trees, [{ id: 'unlinked:repo', kind: 'unlinked', worktree_ids: ['tree-0', 'tree-1', 'stale'] }])
    expect(groups.map(group => [group.groupId, group.items.map(item => item.id)])).toEqual([['unlinked:repo', ['tree-0', 'tree-1']], [undefined, ['tree-2']]])
    expect(trees).toEqual(before)
  })
})
