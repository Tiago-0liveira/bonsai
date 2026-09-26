import type { Edge, Node } from '@xyflow/react'
import { buildBranchForest } from './graphModel'
import { getAgentShelfSize, getNodeSize, LAYOUT } from './geometry'
import type { BranchBlock, CanvasPosition, NodePlacements, Size } from './types'

interface MeasuredBlock {
  block: BranchBlock
  size: Size
  nodeSize: Size
  shelfSize: Size
  children: MeasuredBlock[]
  childrenWidth: number
  childrenHeight: number
}

function measure(block: BranchBlock): MeasuredBlock {
  const nodeSize = getNodeSize(block.node)
  const shelfSize = getAgentShelfSize(block.agentNodes.length)
  const children = block.childBlocks.map(measure)
  const childrenWidth =
    children.reduce((sum, child) => sum + child.size.width, 0) +
    Math.max(0, children.length - 1) * LAYOUT.branchGapX
  const childrenHeight = children.length ? Math.max(...children.map((child) => child.size.height)) : 0
  const localHeight = nodeSize.height + (block.agentNodes.length ? LAYOUT.agentTopGap + shelfSize.height : 0)
  const size = {
    width: Math.max(nodeSize.width, shelfSize.width, childrenWidth),
    height: localHeight + (children.length ? LAYOUT.childTopGap + childrenHeight : 0),
  }
  return { block, size, nodeSize, shelfSize, children, childrenWidth, childrenHeight }
}

function placeBlock(
  measured: MeasuredBlock,
  x: number,
  y: number,
  positions: Record<string, CanvasPosition>,
) {
  const { block, size, nodeSize, shelfSize, children } = measured
  positions[block.id] = {
    x: x + (size.width - nodeSize.width) / 2,
    y,
  }

  let cursorY = y + nodeSize.height
  if (block.agentNodes.length) {
    cursorY += LAYOUT.agentTopGap
    const columns = Math.min(3, block.agentNodes.length)
    const agentSize = getNodeSize({ type: 'agent', data: {} })
    const shelfX = x + (size.width - shelfSize.width) / 2

    block.agentNodes.forEach((agent, index) => {
      const row = Math.floor(index / columns)
      const col = index % columns
      const rowCount = Math.min(columns, block.agentNodes.length - row * columns)
      const rowWidth = rowCount * agentSize.width + Math.max(0, rowCount - 1) * LAYOUT.agentGapX
      const rowX = x + (size.width - rowWidth) / 2
      positions[agent.id] = {
        x: rowX + col * (agentSize.width + LAYOUT.agentGapX),
        y: cursorY + row * (agentSize.height + LAYOUT.agentGapY),
      }
    })
    cursorY += shelfSize.height
  }

  if (!children.length) return
  cursorY += LAYOUT.childTopGap
  let childX = x + (size.width - measured.childrenWidth) / 2
  children.forEach((child) => {
    placeBlock(child, childX, cursorY, positions)
    childX += child.size.width + LAYOUT.branchGapX
  })
}

export function computeGlobalPlacements(
  nodes: Node[],
  edges: Edge[],
  previousPlacements: NodePlacements = {},
): Record<string, CanvasPosition> {
  const project = nodes.find((node) => node.type === 'project')
  if (!project) return {}

  const forest = buildBranchForest(nodes, edges, previousPlacements).map(measure)
  const projectSize = getNodeSize(project)
  const totalForestWidth =
    forest.reduce((sum, block) => sum + block.size.width, 0) +
    Math.max(0, forest.length - 1) * LAYOUT.branchGapX
  const rootX = Math.max(360, totalForestWidth / 2 + 80 - projectSize.width / 2)
  const rootY = 34
  const positions: Record<string, CanvasPosition> = {
    [project.id]: { x: rootX, y: rootY },
  }

  const defaultNode = nodes.find((node) => node.type === 'defaultBranch')
  if (defaultNode) {
    const size = getNodeSize(defaultNode)
    positions[defaultNode.id] = {
      x: rootX - size.width - 34,
      y: rootY + Math.max(0, (projectSize.height - size.height) / 2),
    }
  }

  const envNode = nodes.find((node) => node.type === 'env')
  if (envNode) {
    positions[envNode.id] = {
      x: rootX + projectSize.width + 34,
      y: rootY + 48,
    }
  }

  if (forest.length) {
    let forestX = rootX + projectSize.width / 2 - totalForestWidth / 2
    const forestY = rootY + projectSize.height + LAYOUT.projectTopGap
    forest.forEach((block) => {
      placeBlock(block, forestX, forestY, positions)
      forestX += block.size.width + LAYOUT.branchGapX
    })
  }

  const positioned = new Set(Object.keys(positions))
  let overflow = 0
  nodes
    .filter((node) => !positioned.has(node.id))
    .sort((a, b) => a.id.localeCompare(b.id))
    .forEach((node) => {
      positions[node.id] = { x: 24 + overflow * 210, y: rootY + 620 }
      overflow += 1
    })

  return positions
}

export function layoutGraph(nodes: Node[], edges: Edge[], previousPlacements: NodePlacements = {}) {
  const positions = computeGlobalPlacements(nodes, edges, previousPlacements)
  return nodes.map((node) => ({ ...node, position: positions[node.id] ?? node.position }))
}
