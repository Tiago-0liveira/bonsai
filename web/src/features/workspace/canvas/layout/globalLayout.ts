import type { Edge, Node } from '@xyflow/react'
import { buildBranchForest } from './graphModel'
import { getNodeSize as layoutSize, LAYOUT } from './geometry'
import type { BranchBlock, CanvasPosition, NodePlacements, Size } from './types'

interface ShelfRow {
  nodes: Node[]
  sizes: Size[]
  width: number
  height: number
}

interface MeasuredBlock {
  block: BranchBlock
  size: Size
  nodeSize: Size
  shelfSize: Size
  children: MeasuredBlock[]
  childrenWidth: number
  shelfRows: ShelfRow[]
}

function measure(block: BranchBlock): MeasuredBlock {
  const nodeSize = layoutSize(block.node)
  const shelfRows: ShelfRow[] = []
  for (let index = 0; index < block.agentNodes.length; index += 3) {
    const nodes = block.agentNodes.slice(index, index + 3)
    const sizes = nodes.map(layoutSize)
    shelfRows.push({
      nodes,
      sizes,
      width: sizes.reduce((sum, size) => sum + size.width, 0) + (nodes.length - 1) * LAYOUT.agentGapX,
      height: Math.max(...sizes.map((size) => size.height)),
    })
  }
  const shelfSize = {
    width: Math.max(0, ...shelfRows.map((row) => row.width)),
    height: shelfRows.reduce((sum, row) => sum + row.height, 0) +
      Math.max(0, shelfRows.length - 1) * LAYOUT.agentGapY,
  }
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
  return { block, size, nodeSize, shelfSize, children, childrenWidth, shelfRows }
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
    let rowY = cursorY
    measured.shelfRows.forEach((row) => {
      let rowX = x + (size.width - row.width) / 2
      row.nodes.forEach((agent, index) => {
        positions[agent.id] = { x: rowX, y: rowY }
        rowX += row.sizes[index].width + LAYOUT.agentGapX
      })
      rowY += row.height + LAYOUT.agentGapY
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
  const projectSize = layoutSize(project)
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
    const size = layoutSize(defaultNode)
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

  const headerBottom = Math.max(...nodes
    .filter((node) => positions[node.id])
    .map((node) => positions[node.id].y + layoutSize(node).height))

  if (forest.length) {
    let forestX = rootX + projectSize.width / 2 - totalForestWidth / 2
    const forestY = headerBottom + LAYOUT.projectTopGap
    forest.forEach((block) => {
      placeBlock(block, forestX, forestY, positions)
      forestX += block.size.width + LAYOUT.branchGapX
    })
  }

  const positioned = new Set(Object.keys(positions))
  const overflowY = Math.max(...nodes
    .filter((node) => positioned.has(node.id))
    .map((node) => positions[node.id].y + layoutSize(node).height)) + LAYOUT.childTopGap
  let overflowX = 80
  nodes
    .filter((node) => !positioned.has(node.id))
    .sort((a, b) => a.id.localeCompare(b.id))
    .forEach((node) => {
      positions[node.id] = { x: overflowX, y: overflowY }
      overflowX += layoutSize(node).width + LAYOUT.branchGapX
    })

  return positions
}

export function layoutGraph(nodes: Node[], edges: Edge[], previousPlacements: NodePlacements = {}) {
  const positions = computeGlobalPlacements(nodes, edges, previousPlacements)
  return nodes.map((node) => ({ ...node, position: positions[node.id] ?? node.position }))
}
