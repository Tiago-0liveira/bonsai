import { useCallback, useEffect, useMemo, useRef } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  ReactFlow,
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { LocateFixed, Network, Plus } from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import { useProjectWorktrees, useProjectAgents, useProjectCanvasProcesses, useProjectCanvasPreferences } from '../../../stores/projectSelectors'
import { ProcessNode, ProcessShelfNode } from '../nodes/ProcessNode'
import { getNodeSize } from './layout/geometry'
import type { ViewportState } from '../../../types'
import { report, syncProject } from '../../../api/git'
import { getDescendantIds, getStructuralParentMap } from './layout/graphModel'
import { computeGlobalPlacements } from './layout/globalLayout'
import { buildCanvasGraph } from './buildCanvasGraph'
import { applyCanvasSelection, createPrLabelSelector, reconcileCanvasNodes } from './graphReconciliation'
import { reconcileById, shareEqual } from '../../../stores/reconciliation'
import {
  placeAddedNodesLocally,
  placeExpandedStackLocally,
  placeMissingNodes,
  refreshGeneratedRuntimeShelves,
  relocateGeneratedBranches,
} from './layout/localPlacement'
import { PullRequestMergeEdge } from './PullRequestMergeEdge'
import {
  AgentNode,
  DefaultBranchNode,
  EnvNode,
  ProjectNode,
  StackNode,
  WorktreeNode,
  type BonsaiGraphData,
} from '../nodes/BonsaiNode'

const nodeTypes = {
  project: ProjectNode,
  worktree: WorktreeNode,
  stack: StackNode,
  agent: AgentNode,
  process: ProcessNode,
  runtimeShelf: ProcessShelfNode,
  defaultBranch: DefaultBranchNode,
  env: EnvNode,
}

const edgeTypes = {
  prMerge: PullRequestMergeEdge,
}

interface DragSnapshot {
  rootId: string
  rootStart: { x: number; y: number }
  companionIds: string[]
  positions: Record<string, { x: number; y: number }>
}

function canonicalLayoutNodes(nodes: Node[]) {
  return nodes.map((node) => {
    if (node.type !== 'worktree') return node
    return {
      ...node,
      // Ignore transient expanded/collapsed DOM height, but keep History
      // metadata so geometry can reserve the full expansion deterministically.
      measured: node.measured ? { ...node.measured, height: undefined } : undefined,
    }
  })
}

function storablePositions(
  nodes: Node[],
  positions: Record<string, { x: number; y: number }>,
) {
  const result: Record<string, { x: number; y: number }> = {}
  nodes.forEach((node) => {
    if (node.type === 'defaultBranch' || node.type === 'env') return
    const position = positions[node.id]
    if (position) result[node.id] = position
  })
  return result
}

export function BonsaiCanvas({ focus }: { focus?: 'worktrees' | 'agents' }) {
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const activeProject = useBonsaiStore(state => state.projects.find(project => project.id === activeProjectId) ?? state.projects[0])
  const worktrees = useProjectWorktrees(activeProject?.id ?? '')
  const { nodePlacements, collapsedTagGroups, detachedStackWorktreeIds, expandedAutomaticGroups } = useProjectCanvasPreferences(activeProject?.id ?? '')
  const tags = useBonsaiStore((state) => state.worktreeTags)
  const agents = useProjectAgents(activeProject?.id ?? '')
  const processes = useProjectCanvasProcesses(activeProject?.id ?? '')
  const worktreeGroups = useBonsaiStore(state => state.worktreeGroups[activeProject?.id ?? ''])
  const toggleAutomaticGroup = useBonsaiStore(state => state.toggleAutomaticGroup)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const setManualNodePlacement = useBonsaiStore((state) => state.setManualNodePlacement)
  const setManualNodePlacements = useBonsaiStore((state) => state.setManualNodePlacements)
  const setGeneratedNodePlacements = useBonsaiStore((state) => state.setGeneratedNodePlacements)
  const subtreeMoveRootId = useBonsaiStore((state) => state.subtreeMoveRootId)
  const setSubtreeMoveRoot = useBonsaiStore((state) => state.setSubtreeMoveRoot)
  const viewport = useBonsaiStore((state) => state.viewport)
  const setViewport = useBonsaiStore((state) => state.setViewport)
  const canvasCommand = useBonsaiStore((state) => state.canvasCommand)
  const canvasReveal = useBonsaiStore(state => state.canvasReveal)
  const requestCanvasAction = useBonsaiStore((state) => state.requestCanvasAction)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const envVariables = useBonsaiStore(state => state.envVariables[activeProject?.id ?? ''])
  const lastCommand = useRef(0)
  const lastReveal = useRef(0)
  const hostRef = useRef<HTMLDivElement>(null)
  const lastFocusFit = useRef<string | undefined>(undefined)
  const dragSnapshot = useRef<DragSnapshot | null>(null)
  const initializedProjects = useRef<Set<string>>(new Set())
  const previousTopology = useRef<{
    projectId: string
    ids: Set<string>
    parents: Map<string, string>
    runtimeOwners: Map<string, string>
    stacks: Map<string, string[]>
  } | null>(null)
  const { fitView, getEdges, getNodes, getViewport, setCenter } = useReactFlow()
  const nodesInitialized = useNodesInitialized()
  const fitViewRef = useRef(fitView)

  useEffect(() => {
    fitViewRef.current = fitView
  }, [fitView])

  const activeAvailable = !!activeProject && activeProject.available !== false
  useEffect(() => {
    if (!activeProjectId || !activeAvailable) return
    const activate = () => { void syncProject(activeProjectId, true).catch(report) }
    activate()
    const timer = window.setInterval(activate, 5 * 60 * 1000)
    return () => window.clearInterval(timer)
  }, [activeProjectId, activeAvailable])

  const graph = useMemo(() => buildCanvasGraph({
    project: activeProject, worktrees, agents, processes, tags, worktreeGroups, collapsedTagGroups,
    detachedStackWorktreeIds, expandedAutomaticGroups, nodePlacements, envCount: envVariables?.length ?? 0,
  }), [activeProject, worktrees, agents, processes, tags, worktreeGroups, collapsedTagGroups, detachedStackWorktreeIds, expandedAutomaticGroups, nodePlacements, envVariables?.length])

  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(graph.edges)
  const labelSelector = useMemo(() => createPrLabelSelector(), [])
  const displayEdges = useMemo(() => labelSelector(nodes, edges), [nodes, edges, labelSelector])
  const previousProjection = useRef(graph.nodes)

  useEffect(() => {
    const previous = previousProjection.current
    previousProjection.current = graph.nodes
    setNodes(current => reconcileCanvasNodes(current, graph.nodes, previous))
  }, [graph.nodes, setNodes])
  useEffect(() => setEdges(current => reconcileById(current, graph.edges)), [graph.edges, setEdges])

  useEffect(() => {
    setNodes(current => applyCanvasSelection(current, selection))
  }, [selection, graph.nodes, setNodes])

  useEffect(() => {
    if (!graph.projectId) return

    const rendered = new Map(getNodes().map((node) => [node.id, node]))
    const layoutNodes = graph.nodes.map((node) => ({ ...node, measured: rendered.get(node.id)?.measured }))
    const currentRuntimeOwners = new Map(graph.edges
      .filter((edge) => edge.data?.relationship === 'agent' || edge.data?.relationship === 'process')
      .map((edge) => [edge.target, edge.source]))
    const placeableNodes = layoutNodes.filter(
      (node) => node.type !== 'defaultBranch' && node.type !== 'env',
    )
    const currentIds = new Set(placeableNodes.map((node) => node.id))
    const currentParents = getStructuralParentMap(graph.edges)
    const currentStacks = new Map(
      graph.nodes
        .filter((node) => node.type === 'stack')
        .map((node) => [
          node.id,
          ((node.data?.stackItems ?? []) as Array<{ id: string }>).map((item) => item.id),
        ] as const),
    )
    const previous =
      previousTopology.current?.projectId === graph.projectId
        ? previousTopology.current
        : null
    const firstForProject = !initializedProjects.current.has(graph.projectId)
    const hasUsablePlacements = placeableNodes.some((node) => Boolean(nodePlacements[node.id]))

    if (firstForProject) {
      initializedProjects.current.add(graph.projectId)
      if (!hasUsablePlacements) {
        const allPositions = computeGlobalPlacements(layoutNodes, graph.edges, nodePlacements)
        const generated = storablePositions(placeableNodes, allPositions)
        setGeneratedNodePlacements(generated)
        previousTopology.current = {
          projectId: graph.projectId,
          ids: currentIds,
          parents: currentParents,
          runtimeOwners: currentRuntimeOwners,
          stacks: currentStacks,
        }
        requestAnimationFrame(() => {
          requestAnimationFrame(() => void fitViewRef.current({ padding: 0.14, duration: 300 }))
        })
        return
      }
    }

    const generated: Record<string, { x: number; y: number }> = {}
    const missingIds = placeableNodes
      .filter((node) => !nodePlacements[node.id])
      .map((node) => node.id)
    Object.assign(
      generated,
      placeMissingNodes(layoutNodes, graph.edges, nodePlacements, missingIds),
    )

    if (previous) {
      const addedIds = [...currentIds].filter((id) => !previous.ids.has(id))
      Object.assign(generated, placeAddedNodesLocally(layoutNodes, nodePlacements, addedIds))

      const addedSet = new Set(addedIds)
      previous.stacks.forEach((memberIds, stackId) => {
        if (currentStacks.has(stackId)) return
        const anchor = nodePlacements[stackId]
        if (!anchor) return
        const missingMembers = memberIds.filter((id) => addedSet.has(id) && !nodePlacements[id])
        if (!missingMembers.length) return
        const workingPlacements = {
          ...nodePlacements,
          ...Object.fromEntries(
            Object.entries(generated).map(([id, position]) => [
              id,
              { ...position, mode: 'generated' as const },
            ]),
          ),
        }
        Object.assign(
          generated,
          placeExpandedStackLocally(layoutNodes, workingPlacements, missingMembers, anchor),
        )
      })

      const changedParents = [...currentParents.entries()]
        .filter(([id, parent]) => previous.parents.has(id) && previous.parents.get(id) !== parent)
        .map(([id]) => id)
      Object.assign(
        generated,
        relocateGeneratedBranches(layoutNodes, graph.edges, nodePlacements, changedParents),
      )
    }

    const workingPlacements = {
      ...nodePlacements,
      ...Object.fromEntries(
        Object.entries(generated).map(([id, position]) => [id, { ...position, mode: 'generated' as const }]),
      ),
    }
    // Refresh only shelves whose membership or owner placement changed. A
    // sibling's add/remove operation must not undo existing Auto-layout results.
    const affectedOwners = new Set<string>()
    currentRuntimeOwners.forEach((ownerId, runtimeId) => {
      if (!nodePlacements[runtimeId] || generated[ownerId] ||
          (previous && previous.runtimeOwners.get(runtimeId) !== ownerId)) {
        affectedOwners.add(ownerId)
      }
    })
    previous?.runtimeOwners.forEach((ownerId, runtimeId) => {
      if (currentRuntimeOwners.get(runtimeId) !== ownerId) affectedOwners.add(ownerId)
    })
    if (affectedOwners.size) {
      Object.assign(generated,
        refreshGeneratedRuntimeShelves(layoutNodes, graph.edges, workingPlacements, affectedOwners))
    }

    if (Object.keys(generated).length) setGeneratedNodePlacements(generated)
    previousTopology.current = {
      projectId: graph.projectId,
      ids: currentIds,
      parents: currentParents,
      runtimeOwners: currentRuntimeOwners,
      stacks: currentStacks,
    }
  // Relayout is triggered by topology/placement changes. Status and label updates
  // replace graph arrays without changing topology and must preserve positions.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    getNodes,
    graph.projectId,
    graph.topologyKey,
    nodePlacements,
    setGeneratedNodePlacements,
  ])

  useEffect(() => {
    if (!nodesInitialized || !canvasReveal.nonce || lastReveal.current === canvasReveal.nonce || canvasReveal.projectId !== activeProjectId) return
    const node = nodes.find(node => node.id === canvasReveal.nodeId)
    if (!node || !nodePlacements[node.id] || !hostRef.current) return
    lastReveal.current = canvasReveal.nonce
    const viewport = getViewport(), size = getNodeSize(node), host = hostRef.current
    const x = node.position.x * viewport.zoom + viewport.x, y = node.position.y * viewport.zoom + viewport.y
    if (x >= 0 && y >= 0 && x + size.width * viewport.zoom <= host.clientWidth && y + size.height * viewport.zoom <= host.clientHeight) return
    void setCenter(node.position.x + size.width / 2, node.position.y + size.height / 2, { zoom: viewport.zoom, duration: 250 })
  }, [activeProjectId, canvasReveal, nodesInitialized, nodes, nodePlacements, getViewport, setCenter])

  useEffect(() => {
    if (!focus) {
      lastFocusFit.current = undefined
      return
    }
    if (!nodesInitialized || lastFocusFit.current === focus) return
    const ids =
      focus === 'agents'
        ? nodes.filter((node) => node.type === 'agent').map((node) => node.id)
        : nodes.filter((node) => node.type === 'worktree' || node.type === 'stack').map((node) => node.id)
    const visible = nodes.filter((node) => ids.includes(node.id))
    if (!visible.length) return
    lastFocusFit.current = focus
    const frame = requestAnimationFrame(() => void fitViewRef.current({ nodes: visible, padding: 0.2, duration: 300 }))
    return () => cancelAnimationFrame(frame)
  }, [focus, nodesInitialized, nodes])

  useEffect(() => {
    if (!canvasCommand.nonce || lastCommand.current === canvasCommand.nonce) return
    lastCommand.current = canvasCommand.nonce
    if (canvasCommand.type === 'fit') {
      void fitViewRef.current({ padding: 0.14, duration: 300 })
      return
    }

    // History expansion changes measured worktree height but is presentation
    // state, not graph topology. Compute against canonical collapsed worktree
    // dimensions, commit placements first, then collapse History. Consumers that
    // wait for History to disappear therefore observe the completed layout.
    let secondFrame = 0
    const firstFrame = requestAnimationFrame(() => {
      secondFrame = requestAnimationFrame(() => {
        const currentNodes = getNodes()
        const layoutNodes = canonicalLayoutNodes(currentNodes)
        const currentEdges = getEdges()
        const currentPlacements = useBonsaiStore.getState().nodePlacements
        const positions = computeGlobalPlacements(layoutNodes, currentEdges, currentPlacements)
        setNodes((current) =>
          current.map((node) => shareEqual(node, { ...node, position: positions[node.id] ?? node.position })),
        )
        const generated = storablePositions(currentNodes, positions)
        setGeneratedNodePlacements(generated)
        useBonsaiStore.setState({ expandedHistoryWorktreeIds: [] })
        requestAnimationFrame(() => void fitViewRef.current({ padding: 0.14, duration: 300 }))
      })
    })
    return () => {
      cancelAnimationFrame(firstFrame)
      if (secondFrame) cancelAnimationFrame(secondFrame)
    }
  }, [
    canvasCommand.nonce,
    canvasCommand.type,
    getEdges,
    getNodes,
    setGeneratedNodePlacements,
    setNodes,
  ])

  const onNodeClick = useCallback<NodeMouseHandler>((_, node) => {
    const data = node.data as BonsaiGraphData
    if (data.kind === 'stack') {
      if (data.groupId) toggleAutomaticGroup(data.groupId)
      else if (data.tag) toggleTagGroup(activeProjectId, data.tag)
      return
    }
    if (data.kind === 'project' || data.kind === 'worktree' || data.kind === 'agent' || data.kind === 'process') {
      setSelection({ type: data.kind, id: data.entityId })
    }
  }, [activeProjectId, setSelection, toggleAutomaticGroup, toggleTagGroup])

  const autoLayout = useCallback(() => requestCanvasAction('layout'), [requestCanvasAction])
  const onMoveEnd = useCallback((_: unknown, nextViewport: ViewportState) => setViewport(nextViewport), [setViewport])

  const beginDrag = useCallback<OnNodeDrag<Node>>((_, node) => {
    const companionIds = new Set<string>()
    if (node.type === 'project') {
      companionIds.add('default:' + activeProjectId)
      companionIds.add('env:' + activeProjectId)
    }
    if (subtreeMoveRootId === node.id) getDescendantIds(node.id, getEdges()).forEach((id) => companionIds.add(id))
    const positions: Record<string, { x: number; y: number }> = {}
    getNodes().forEach((candidate) => {
      if (companionIds.has(candidate.id)) positions[candidate.id] = { ...candidate.position }
    })
    dragSnapshot.current = { rootId: node.id, rootStart: { ...node.position }, companionIds: [...companionIds], positions }
  }, [activeProjectId, getEdges, getNodes, subtreeMoveRootId])

  const dragNode = useCallback<OnNodeDrag<Node>>((_, node) => {
    const snapshot = dragSnapshot.current
    if (!snapshot || snapshot.rootId !== node.id || !snapshot.companionIds.length) return
    const dx = node.position.x - snapshot.rootStart.x
    const dy = node.position.y - snapshot.rootStart.y
    setNodes((current) =>
      current.map((candidate) => {
        const initial = snapshot.positions[candidate.id]
        return initial ? { ...candidate, position: { x: initial.x + dx, y: initial.y + dy } } : candidate
      }),
    )
  }, [setNodes])

  const finishDrag = useCallback<OnNodeDrag<Node>>((_, node) => {
    const snapshot = dragSnapshot.current
    if (!snapshot || snapshot.rootId !== node.id) {
      setManualNodePlacement(node.id, node.position)
      setSubtreeMoveRoot(null)
      return
    }
    const dx = node.position.x - snapshot.rootStart.x
    const dy = node.position.y - snapshot.rootStart.y
    const positions: Record<string, { x: number; y: number }> = { [node.id]: node.position }
    snapshot.companionIds.forEach((id) => {
      const initial = snapshot.positions[id]
      if (initial) positions[id] = { x: initial.x + dx, y: initial.y + dy }
    })
    const persisted = Object.fromEntries(
      Object.entries(positions).filter(([id]) => {
        const moved = getNodes().find((candidate) => candidate.id === id)
        return moved?.type !== 'defaultBranch' && moved?.type !== 'env'
      }),
    )
    setManualNodePlacements(persisted)
    dragSnapshot.current = null
    setSubtreeMoveRoot(null)
  }, [getNodes, setManualNodePlacement, setManualNodePlacements, setSubtreeMoveRoot])

  return (
    <div ref={hostRef} className="relative h-full min-h-0 w-full bg-[rgb(var(--bg))]">
      <ReactFlow
        nodes={nodes}
        edges={displayEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onNodeDragStart={beginDrag}
        onNodeDrag={dragNode}
        onNodeDragStop={finishDrag}
        onMoveEnd={onMoveEnd}
        defaultViewport={viewport}
        minZoom={0.28}
        maxZoom={1.8}
        selectionOnDrag
        panOnScroll={false}
        zoomOnScroll
        zoomOnPinch
        panOnDrag
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={22} size={1} color="rgb(var(--text) / .07)" />
        <Controls position="bottom-left" showInteractive={false} />
      </ReactFlow>

      <div className="pointer-events-none absolute left-3 top-3 flex items-center gap-2">
        <div className="pointer-events-auto flex items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] p-0.5 shadow-lg backdrop-blur">
          <button onClick={() => void fitViewRef.current({ padding: 0.14, duration: 300 })} className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
            <LocateFixed size={12} /> Fit
          </button>
          <button onClick={autoLayout} className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
            <Network size={12} /> Auto-layout
          </button>
        </div>
      </div>

      <button
        onClick={() => setWorktreeDialogOpen(true)}
        className="bonsai-focus absolute right-3 top-3 flex items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] px-2.5 py-2 text-[10px] text-[rgb(var(--muted))] shadow-lg backdrop-blur hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
      >
        <Plus size={12} /> New worktree
      </button>
    </div>
  )
}
