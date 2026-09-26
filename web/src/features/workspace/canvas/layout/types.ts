import type { Node } from '@xyflow/react'

export interface CanvasPosition {
  x: number
  y: number
}

export type PlacementMode = 'manual' | 'generated'

export interface NodePlacement extends CanvasPosition {
  mode: PlacementMode
}

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
