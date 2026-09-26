import type { Edge, Node } from '@xyflow/react'
import type { BranchBlock, NodePlacements } from './types'

function relationship(edge: Edge) {
  return edge.data?.relationship
}

export function isStructuralEdge(edge: Edge) {
  return relationship(edge) === 'hierarchy'
}

export function isAgentEdge(edge: Edge) {
  return relationship(edge) === 'agent'
}

export function getStructuralParentMap(edges: Edge[]) {
  const parents = new Map<string, string>()
  edges.filter(isStructuralEdge).forEach((edge) => parents.set(edge.target, edge.source))
  return parents
}

export function getDescendantIds(rootId: string, edges: Edge[]) {
  const descendants = new Set<string>()
  const queue = [rootId]
  while (queue.length) {
    const source = queue.shift()
    if (!source) continue
    edges
      .filter((edge) => edge.source === source && (isStructuralEdge(edge) || isAgentEdge(edge)))
      .forEach((edge) => {
        if (descendants.has(edge.target)) return
        descendants.add(edge.target)
        queue.push(edge.target)
      })
  }
  return [...descendants]
}

function sortBranches(nodes: Node[], placements: NodePlacements) {
  return [...nodes].sort((a, b) => {
    const ax = placements[a.id]?.x
    const bx = placements[b.id]?.x
    if (ax !== undefined && bx !== undefined && ax !== bx) return ax - bx
    if (ax !== undefined && bx === undefined) return -1
    if (ax === undefined && bx !== undefined) return 1
    const aTag = String(a.data?.tag ?? '')
    const bTag = String(b.data?.tag ?? '')
    const byTag = aTag.localeCompare(bTag)
    if (byTag) return byTag
    const byTitle = String(a.data?.title ?? '').localeCompare(String(b.data?.title ?? ''))
    if (byTitle) return byTitle
    return a.id.localeCompare(b.id)
  })
}

export function buildBranchForest(nodes: Node[], edges: Edge[], placements: NodePlacements): BranchBlock[] {
  const project = nodes.find((node) => node.type === 'project')
  if (!project) return []

  const branchNodes = nodes.filter((node) => node.type === 'worktree' || node.type === 'stack')
  const branchMap = new Map(branchNodes.map((node) => [node.id, node]))
  const agents = new Map<string, Node[]>()
  edges.filter(isAgentEdge).forEach((edge) => {
    const agent = nodes.find((node) => node.id === edge.target && node.type === 'agent')
    if (!agent) return
    agents.set(edge.source, [...(agents.get(edge.source) ?? []), agent])
  })

  const children = new Map<string, Node[]>()
  edges.filter(isStructuralEdge).forEach((edge) => {
    const target = branchMap.get(edge.target)
    if (!target) return
    children.set(edge.source, [...(children.get(edge.source) ?? []), target])
  })

  const visiting = new Set<string>()
  const build = (node: Node): BranchBlock => {
    if (visiting.has(node.id)) return { id: node.id, node, agentNodes: [], childBlocks: [] }
    visiting.add(node.id)
    const block: BranchBlock = {
      id: node.id,
      node,
      agentNodes: [...(agents.get(node.id) ?? [])].sort((a, b) => a.id.localeCompare(b.id)),
      childBlocks: sortBranches(children.get(node.id) ?? [], placements).map(build),
    }
    visiting.delete(node.id)
    return block
  }

  const explicitRoots = sortBranches(children.get(project.id) ?? [], placements)
  const parented = new Set(edges.filter(isStructuralEdge).map((edge) => edge.target))
  const orphans = sortBranches(
    branchNodes.filter((node) => !parented.has(node.id) && !explicitRoots.some((root) => root.id === node.id)),
    placements,
  )

  return [...explicitRoots, ...orphans].map(build)
}
