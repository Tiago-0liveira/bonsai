import type { Edge, Node } from '@xyflow/react'

function sizeOf(node: Node) {
  const type = node.type ?? 'agent'
  if (type === 'project') return { width: 300, height: 154 }
  if (type === 'defaultBranch') return { width: 244, height: 156 }
  if (type === 'env') return { width: 150, height: 56 }
  if (type === 'worktree') return { width: 230, height: 154 }
  if (type === 'stack') {
    const count = Number(node.data?.stackCount ?? 1)
    return { width: 272, height: 58 + Math.min(6, count) * 31 }
  }
  return { width: 172, height: 92 }
}

function isHierarchyEdge(edge: Edge) {
  const relationship = edge.data?.relationship
  return relationship === 'hierarchy' || relationship === 'agent'
}

export function getDescendantIds(rootId: string, edges: Edge[]) {
  const descendants = new Set<string>()
  const queue = [rootId]
  while (queue.length) {
    const source = queue.shift()
    if (!source) continue
    edges
      .filter((edge) => edge.source === source && isHierarchyEdge(edge))
      .forEach((edge) => {
        if (descendants.has(edge.target)) return
        descendants.add(edge.target)
        queue.push(edge.target)
      })
  }
  return [...descendants]
}

export async function layoutGraph(nodes: Node[], edges: Edge[]) {
  const project = nodes.find((node) => node.type === 'project')
  if (!project) return nodes

  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const childMap = new Map<string, string[]>()
  edges.filter(isHierarchyEdge).forEach((edge) => {
    const items = childMap.get(edge.source) ?? []
    items.push(edge.target)
    childMap.set(edge.source, items)
  })

  const gap = 34
  const widthMemo = new Map<string, number>()
  const subtreeWidth = (id: string, visiting = new Set<string>()): number => {
    if (widthMemo.has(id)) return widthMemo.get(id) as number
    const node = nodeMap.get(id)
    if (!node) return 0
    if (visiting.has(id)) return sizeOf(node).width
    const nextVisiting = new Set(visiting)
    nextVisiting.add(id)
    const children = (childMap.get(id) ?? []).filter((childId) => nodeMap.has(childId))
    const ownWidth = sizeOf(node).width
    if (!children.length) {
      widthMemo.set(id, ownWidth)
      return ownWidth
    }
    const childWidth =
      children.reduce((total, childId) => total + subtreeWidth(childId, nextVisiting), 0) +
      gap * Math.max(0, children.length - 1)
    const result = Math.max(ownWidth, childWidth)
    widthMemo.set(id, result)
    return result
  }

  const directChildren = childMap.get(project.id) ?? []
  const totalChildrenWidth =
    directChildren.reduce((total, id) => total + subtreeWidth(id), 0) +
    gap * Math.max(0, directChildren.length - 1)
  const rootX = Math.max(360, totalChildrenWidth / 2 + 80 - sizeOf(project).width / 2)
  const rootY = 34
  const positions = new Map<string, { x: number; y: number }>()
  positions.set(project.id, { x: rootX, y: rootY })

  const defaultNode = nodes.find((node) => node.type === 'defaultBranch')
  if (defaultNode) {
    positions.set(defaultNode.id, {
      x: rootX - sizeOf(defaultNode).width - 34,
      y: rootY + Math.max(0, (sizeOf(project).height - sizeOf(defaultNode).height) / 2),
    })
  }

  const envNode = nodes.find((node) => node.type === 'env')
  if (envNode) {
    positions.set(envNode.id, {
      x: rootX + sizeOf(project).width + 34,
      y: rootY + 48,
    })
  }

  const placeChildren = (sourceId: string, centerX: number, depth: number, visiting = new Set<string>()) => {
    if (visiting.has(sourceId)) return
    const nextVisiting = new Set(visiting)
    nextVisiting.add(sourceId)
    const children = (childMap.get(sourceId) ?? []).filter((id) => nodeMap.has(id))
    if (!children.length) return
    const widths = children.map((id) => subtreeWidth(id))
    const total = widths.reduce((sum, width) => sum + width, 0) + gap * Math.max(0, children.length - 1)
    let cursor = centerX - total / 2

    children.forEach((id, index) => {
      const node = nodeMap.get(id)
      if (!node) return
      const allocated = widths[index]
      const nodeSize = sizeOf(node)
      const x = cursor + allocated / 2 - nodeSize.width / 2
      const y = rootY + 188 * depth
      positions.set(id, { x, y })
      placeChildren(id, cursor + allocated / 2, depth + 1, nextVisiting)
      cursor += allocated + gap
    })
  }

  placeChildren(project.id, rootX + sizeOf(project).width / 2, 1)

  let overflow = 0
  nodes.forEach((node) => {
    if (positions.has(node.id)) return
    positions.set(node.id, { x: 24 + overflow * 210, y: rootY + 560 })
    overflow += 1
  })

  return nodes.map((node) => ({
    ...node,
    position: positions.get(node.id) ?? node.position,
  }))
}
