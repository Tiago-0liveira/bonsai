import ELK from 'elkjs/lib/elk.bundled.js'
import type { Edge, Node } from '@xyflow/react'

const elk = new ELK()

export async function layoutGraph(nodes: Node[], edges: Edge[]) {
  const graph = await elk.layout({
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.spacing.nodeNode': '42',
      'elk.layered.spacing.nodeNodeBetweenLayers': '86',
      'elk.padding': '[top=32,left=32,bottom=32,right=32]',
    },
    children: nodes.map((node) => ({
      id: node.id,
      width: node.type === 'project' ? 236 : node.type === 'worktree' ? 250 : 224,
      height: node.type === 'agent' ? 126 : 142,
    })),
    edges: edges.map((edge) => ({
      id: edge.id,
      sources: [edge.source],
      targets: [edge.target],
    })),
  })

  const positions = new Map(
    (graph.children ?? []).map((child) => [child.id, { x: child.x ?? 0, y: child.y ?? 0 }]),
  )

  return nodes.map((node) => ({
    ...node,
    position: positions.get(node.id) ?? node.position,
  }))
}
