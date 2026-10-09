import type { PullRequest } from '../../types'

export type StackablePr = Pick<PullRequest, 'id' | 'number' | 'branch' | 'base' | 'status' | 'headRepository'>

export interface PrStackOptions {
  /** The base repository (`owner/name`). A PR whose head lives in another repository (a fork) is never a parent. */
  repository?: string
}

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
 * Branch names only identify a branch inside one repository, so a fork's head
 * (often its `main`) must not be mistaken for a base-repository branch. A head
 * equal to its own base can only come from a fork, which also covers PRs whose
 * head repository is unknown (deleted forks).
 */
const canBeParent = (pr: StackablePr, repository: string | undefined) =>
  isOpen(pr)
  && !!pr.branch
  && pr.branch !== pr.base
  && !(repository && pr.headRepository && pr.headRepository.toLowerCase() !== repository.toLowerCase())

/**
 * PR B is stacked on A when both are open, `B.base === A.branch`, and A's head
 * lives in the base repository. Group order and sibling order follow the input
 * order. Malformed input never throws: PRs without a branch or base are
 * standalone, duplicate ids are dropped, and a cycle is cut at its
 * lowest-numbered PR, which becomes the root.
 */
export function buildPrStacks<T extends StackablePr>(
  input: readonly T[] | null | undefined,
  { repository }: PrStackOptions = {},
): PrStacks<T> {
  const seen = new Set<string>()
  const prs = (input ?? []).filter((pr) => {
    if (!pr || seen.has(pr.id)) return false
    seen.add(pr.id)
    return true
  })

  // Without a repository to compare against, several open PRs may still share
  // a branch name: the lowest number wins.
  const byBranch = new Map<string, T>()
  for (const pr of prs) {
    if (!canBeParent(pr, repository)) continue
    const current = byBranch.get(pr.branch)
    if (!current || pr.number < current.number) byBranch.set(pr.branch, pr)
  }

  const parents = new Map<string, T>()
  for (const pr of prs) {
    // canBeParent rules out `branch === base`, so a PR is never its own parent.
    const parent = isOpen(pr) ? byBranch.get(pr.base) : undefined
    if (parent) parents.set(pr.id, parent)
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
