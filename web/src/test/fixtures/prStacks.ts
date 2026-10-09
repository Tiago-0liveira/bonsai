import type { PullRequest } from '../../types'

export const stackRepository = 'acme/widgets'

let sequence = 0

/** A minimal pull request for stacking scenarios; ids stay unique across calls. */
export function stackPr(
  number: number,
  branch: string,
  base: string,
  overrides: Partial<PullRequest> = {},
): PullRequest {
  return {
    id: `pr-${number}-${sequence++}`,
    number,
    title: `PR #${number}`,
    description: '',
    branch,
    base,
    status: 'Open',
    headRepository: stackRepository,
    createdAt: '',
    updatedAt: '',
    checks: [],
    commits: [],
    conversation: [],
    files: [],
    ...overrides,
  }
}

/** main ← a ← b ← c, listed out of order. */
export function threeDeepStack() {
  const a = stackPr(1, 'feat/a', 'main')
  const b = stackPr(2, 'feat/b', 'feat/a')
  const c = stackPr(3, 'feat/c', 'feat/b')
  return { a, b, c, prs: [c, a, b] }
}

/** a and b are based on each other; tail hangs off a. */
export function cyclicStack() {
  const a = stackPr(7, 'feat/a', 'feat/b')
  const b = stackPr(3, 'feat/b', 'feat/a')
  const tail = stackPr(9, 'feat/tail', 'feat/a')
  return { a, b, tail, prs: [a, b, tail] }
}

/** Based on a branch that no open PR provides (merged and deleted). */
export function orphanBase() {
  const orphan = stackPr(2, 'feat/b', 'feat/deleted')
  return { orphan, prs: [orphan] }
}

/** A contributor's fork PR from their own `main`, next to ordinary PRs on main. */
export function forkFromMain() {
  const fork = stackPr(50, 'main', 'main', { headRepository: 'contributor/widgets' })
  const first = stackPr(1, 'feat/a', 'main')
  const second = stackPr(2, 'feat/b', 'main')
  return { fork, first, second, prs: [fork, first, second] }
}
