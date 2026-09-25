import type { Edge, Node } from '@xyflow/react'

const dimensions: Record<string, { width: number; height: number }> = {
  project: { width: 300, height: 154 },
  worktree: { width: 210, height: 132 },
  stack: { width: 210, height: 112 },
  agent: { width: 172, height: 92 },
}

function sizeOf(node: Node) {
  return dimensions[node.type ?? 'agent'] ?? dimensions.agent
}

export async function layoutGraph(nodes: Node[], edges: Edge[]) {
  const project = nodes.find((node) => node.type === 'project')
  if (!project) return nodes

  const directIds = edges.filter((edge) => edge.source === project.id).map((edge) => edge.target)
  const directNodes = directIds
    .map((id) => nodes.find((node) => node.id === id))
    .filter((node): node is Node => Boolean(node))

  const gap = 24
  const columnWidth = 218
  const totalWidth = Math.max(300, directNodes.length * columnWidth + Math.max(0, directNodes.length - 1) * gap)
  const positions = new Map<string, { x: number; y: number }>()

  positions.set(project.id, { x: Math.max(24, totalWidth / 2 - sizeOf(project).width / 2), y: 24 })

  directNodes.forEach((node, index) => {
    const x = index * (columnWidth + gap) + 24
    positions.set(node.id, { x, y: 205 })

    const childIds = edges.filter((edge) => edge.source === node.id).map((edge) => edge.target)
    const childNodes = childIds
      .map((id) => nodes.find((candidate) => candidate.id === id))
      .filter((candidate): candidate is Node => Boolean(candidate))

    childNodes.forEach((child, childIndex) => {
      const childSize = sizeOf(child)
      const parentSize = sizeOf(node)
      positions.set(child.id, {
        x: x + Math.max(0, (parentSize.width - childSize.width) / 2),
        y: 352 + childIndex * 106,
      })
    })
  })

  const placed = new Set(positions.keys())
  let overflow = 0
  nodes.forEach((node) => {
    if (placed.has(node.id)) return
    positions.set(node.id, { x: 24 + overflow * (columnWidth + gap), y: 520 })
    overflow += 1
  })

  return nodes.map((node) => ({
    ...node,
    position: positions.get(node.id) ?? node.position,
  }))
}
