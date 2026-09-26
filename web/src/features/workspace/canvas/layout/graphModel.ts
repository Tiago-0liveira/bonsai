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
  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const agents = new Map<string, Node[]>()
  const assignedAgents = new Set<string>()
  const sortedEdges = [...edges].sort((a, b) =>
    a.source.localeCompare(b.source) || a.target.localeCompare(b.target))
  sortedEdges.filter(isAgentEdge).forEach((edge) => {
    const agent = nodeMap.get(edge.target)
    if (agent?.type !== 'agent' || !branchMap.has(edge.source) || assignedAgents.has(agent.id)) return
    assignedAgents.add(agent.id)
    agents.set(edge.source, [...(agents.get(edge.source) ?? []), agent])
  })

  // A visible stack can have multiple incoming relationships. Give each block
  // one deterministic owner, preferring a branch parent over the project.
  const parents = new Map<string, string>()
  sortedEdges.filter(isStructuralEdge).forEach((edge) => {
    if (!branchMap.has(edge.target) || edge.source === edge.target) return
    if (edge.source !== project.id && !branchMap.has(edge.source)) return
    if (!parents.has(edge.target) || parents.get(edge.target) === project.id) {
      parents.set(edge.target, edge.source)
    }
  })

  // Break each cycle at its smallest ID, independently of prior coordinates.
  const checked = new Set<string>()
  for (const node of branchNodes) {
    const path = new Set<string>()
    let id: string | undefined = node.id
    while (id && branchMap.has(id) && !checked.has(id)) {
      if (path.has(id)) {
        const cycle = [id]
        let next = parents.get(id)
        while (next && next !== id) {
          cycle.push(next)
          next = parents.get(next)
        }
        parents.delete(cycle.sort((a, b) => a.localeCompare(b))[0])
        break
      }
      path.add(id)
      id = parents.get(id)
    }
    path.forEach((item) => checked.add(item))
  }

  const children = new Map<string, Node[]>()
  branchNodes.forEach((node) => {
    const parent = parents.get(node.id) ?? project.id
    children.set(parent, [...(children.get(parent) ?? []), node])
  })
  const build = (node: Node): BranchBlock => ({
    id: node.id,
    node,
    agentNodes: [...(agents.get(node.id) ?? [])].sort((a, b) => a.id.localeCompare(b.id)),
    childBlocks: sortBranches(children.get(node.id) ?? [], placements).map(build),
  })

  return sortBranches(children.get(project.id) ?? [], placements).map(build)
}
