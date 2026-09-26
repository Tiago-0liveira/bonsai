import type { Node } from '@xyflow/react'
import type { NodePlacement, NodePlacementMode } from '../../../../types'

export type { NodePlacement, NodePlacementMode } from '../../../../types'

export interface CanvasPosition {
  x: number
  y: number
}

export type PlacementMode = NodePlacementMode
export type NodePlacements = Record<string, NodePlacement>

export interface Size {
  width: number
  height: number
}

export interface Rect extends CanvasPosition, Size {}

export interface BranchBlock {
  id: string
  node: Node
  agentNodes: Node[]
  childBlocks: BranchBlock[]
}
