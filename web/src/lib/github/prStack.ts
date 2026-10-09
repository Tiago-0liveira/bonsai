import type { PullRequest } from '../../types'

export type StackablePr = Pick<PullRequest, 'id' | 'number' | 'branch' | 'base' | 'status'>

export interface PrStack<T extends StackablePr> {
  /** Bottom of the stack: the PR whose base is not another open PR's branch. */
  root: T
  /** Every PR in the stack, root first, each parent before its children. */
  prs: T[]
}

export interface PrStacks<T extends StackablePr> {
  stacks: PrStack<T>[]
  standalone: T[]
  /** The PR this one is stacked on. */
  parentOf(pr: StackablePr): T | undefined
  /** Root first, ending with `pr` itself. A standalone PR yields just itself. */
  chainOf(pr: StackablePr): T[]
  /** True when the PR is stacked on another open PR. */
  isStacked(pr: StackablePr): boolean
  /** The stack the PR belongs to, root included. */
  stackOf(pr: StackablePr): PrStack<T> | undefined
}

const isOpen = (pr: StackablePr) => pr.status === 'Open' || pr.status === 'Draft'

/**
 * PR B is stacked on A when `B.base === A.branch` and A is open. Group order
 * and sibling order follow the input order. Malformed input never throws: PRs
 * without a branch or base are standalone, duplicate ids are dropped, and a
 * cycle is cut at its lowest-numbered PR, which becomes the root.
 *
 * Callers choose which PRs participate (e.g. only the Open tab's list).
 */
export function buildPrStacks<T extends StackablePr>(input: readonly T[] | null | undefined): PrStacks<T> {
  const seen = new Set<string>()
  const prs = (input ?? []).filter((pr) => {
    if (!pr || seen.has(pr.id)) return false
    seen.add(pr.id)
    return true
  })

  // Several open PRs may share a branch name (forks): the lowest number wins.
  const byBranch = new Map<string, T>()
  for (const pr of prs) {
    if (!pr.branch || !isOpen(pr)) continue
    const current = byBranch.get(pr.branch)
    if (!current || pr.number < current.number) byBranch.set(pr.branch, pr)
  }

  const parents = new Map<string, T>()
  for (const pr of prs) {
    const parent = pr.base ? byBranch.get(pr.base) : undefined
    if (parent && parent.id !== pr.id) parents.set(pr.id, parent)
  }

  breakCycles(prs, parents)

  const children = new Map<string, T[]>()
  for (const pr of prs) {
    const parent = parents.get(pr.id)
    if (parent) children.set(parent.id, [...(children.get(parent.id) ?? []), pr])
  }

  const stacks: PrStack<T>[] = []
  const stackById = new Map<string, PrStack<T>>()
  const standalone: T[] = []
  for (const pr of prs) {
    if (parents.has(pr.id)) continue
    if (!children.has(pr.id)) {
      standalone.push(pr)
      continue
    }
    const stack: PrStack<T> = { root: pr, prs: [] }
    const visit = (node: T) => {
      stack.prs.push(node)
      stackById.set(node.id, stack)
      for (const child of children.get(node.id) ?? []) visit(child)
    }
    visit(pr)
    stacks.push(stack)
  }

  const chainOf = (pr: StackablePr): T[] => {
    const chain: T[] = []
    for (let node = prs.find((p) => p.id === pr.id); node && !chain.includes(node); node = parents.get(node.id)) {
      chain.unshift(node)
    }
    return chain
  }

  return {
    stacks,
    standalone,
    parentOf: (pr) => parents.get(pr.id),
    chainOf,
    isStacked: (pr) => parents.has(pr.id),
    stackOf: (pr) => stackById.get(pr.id),
  }
}

function breakCycles<T extends StackablePr>(prs: readonly T[], parents: Map<string, T>) {
  const done = new Set<string>()
  for (const start of prs) {
    const path: T[] = []
    const onPath = new Set<string>()
    let node: T | undefined = start
    while (node && !done.has(node.id) && !onPath.has(node.id)) {
      path.push(node)
      onPath.add(node.id)
      node = parents.get(node.id)
    }
    if (node && onPath.has(node.id)) {
      const cycle = path.slice(path.indexOf(node))
      const root = cycle.reduce((lowest, pr) => (pr.number < lowest.number ? pr : lowest))
      parents.delete(root.id)
    }
    for (const pr of path) done.add(pr.id)
  }
}
