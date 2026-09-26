import { useEffect, useMemo, useRef } from 'react'
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
import type { Health, Worktree } from '../../../types'
import { getTagPresentation } from '../tagStyles'
import { getDescendantIds, getStructuralParentMap } from './layout/graphModel'
import { computeGlobalPlacements } from './layout/globalLayout'
import {
  placeAddedNodesLocally,
  placeExpandedStackLocally,
  placeMissingNodes,
  refreshGeneratedAgentShelves,
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
  defaultBranch: DefaultBranchNode,
  env: EnvNode,
}

const edgeTypes = {
  prMerge: PullRequestMergeEdge,
}

function groupHealth(items: Worktree[]): Health {
  if (items.some((item) => item.status === 'error')) return 'error'
  if (items.some((item) => item.status === 'warning')) return 'warning'
  if (items.some((item) => item.status === 'healthy')) return 'healthy'
  return 'idle'
}

interface DragSnapshot {
  rootId: string
  rootStart: { x: number; y: number }
  companionIds: string[]
  positions: Record<string, { x: number; y: number }>
}

export function BonsaiCanvas({ focus }: { focus?: 'worktrees' | 'agents' }) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const tags = useBonsaiStore((state) => state.worktreeTags)
  const agents = useBonsaiStore((state) => state.agents)
  const collapsedTagGroups = useBonsaiStore((state) => state.collapsedTagGroups)
  const detachedStackWorktreeIds = useBonsaiStore((state) => state.detachedStackWorktreeIds)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const nodePlacements = useBonsaiStore((state) => state.nodePlacements)
  const setManualNodePlacement = useBonsaiStore((state) => state.setManualNodePlacement)
  const setManualNodePlacements = useBonsaiStore((state) => state.setManualNodePlacements)
  const setGeneratedNodePlacements = useBonsaiStore((state) => state.setGeneratedNodePlacements)
  const subtreeMoveRootId = useBonsaiStore((state) => state.subtreeMoveRootId)
  const setSubtreeMoveRoot = useBonsaiStore((state) => state.setSubtreeMoveRoot)
  const viewport = useBonsaiStore((state) => state.viewport)
  const setViewport = useBonsaiStore((state) => state.setViewport)
  const canvasCommand = useBonsaiStore((state) => state.canvasCommand)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const envVariables = useBonsaiStore((state) => state.envVariables)
  const lastCommand = useRef(0)
  const lastFocusFit = useRef<string | undefined>(undefined)
  const dragSnapshot = useRef<DragSnapshot | null>(null)
  const initializedProjects = useRef<Set<string>>(new Set())
  const previousTopology = useRef<{
    projectId: string
    ids: Set<string>
    parents: Map<string, string>
    stacks: Map<string, string[]>
  } | null>(null)
  const { fitView } = useReactFlow()
  const nodesInitialized = useNodesInitialized()
  const fitViewRef = useRef(fitView)

  useEffect(() => {
    fitViewRef.current = fitView
  }, [fitView])

  const graph = useMemo(() => {
    const project = projects.find((item) => item.id === activeProjectId) ?? projects[0]
    if (!project) return { nodes: [] as Node[], edges: [] as Edge[], projectId: '', topologyKey: 'empty' }

    const allProjectWorktrees = worktrees.filter((worktree) => worktree.projectId === project.id)
    const projectWorktrees = allProjectWorktrees.filter((worktree) => worktree.branch !== project.defaultBranch)
    const projectWorktreeIds = new Set(projectWorktrees.map((worktree) => worktree.id))
    const projectAgents = agents.filter((agent) => projectWorktreeIds.has(agent.worktreeId) && (agent.presentation ?? (agent.archived ? 'archived' : 'canvas')) !== 'archived')
    const runningAgents = projectAgents.filter((agent) => agent.state === 'running').length
    const activePrs = projectWorktrees.filter((worktree) => worktree.prStatus === 'Open' || worktree.prStatus === 'Draft').length
    const failedChecks = projectWorktrees.reduce((total, worktree) => total + worktree.ciFailed, 0)
    const runningCi = projectWorktrees.filter((worktree) => worktree.ciStatus === 'running').length

    const groups = new Map<string, Worktree[]>()
    projectWorktrees.forEach((worktree) => {
      const items = groups.get(worktree.tag) ?? []
      items.push(worktree)
      groups.set(worktree.tag, items)
    })

    const visibleEntries: Array<
      | { type: 'stack'; id: string; tag: string; items: Worktree[] }
      | { type: 'worktree'; id: string; worktree: Worktree; items: Worktree[] }
    > = []
    const visibleNodeForWorktree = new Map<string, string>()

    groups.forEach((items, tag) => {
      const stackable = items.filter(
        (item) => item.stackPreference !== 'never' && !detachedStackWorktreeIds.includes(item.id),
      )
      const separate = items.filter(
        (item) => item.stackPreference === 'never' || detachedStackWorktreeIds.includes(item.id),
      )
      const collapsed = stackable.length > 1 && collapsedTagGroups.includes(project.id + ':' + tag)

      if (collapsed) {
        const stackId = 'stack:' + project.id + ':' + tag
        visibleEntries.push({ type: 'stack', id: stackId, tag, items: stackable })
        stackable.forEach((item) => visibleNodeForWorktree.set(item.id, stackId))
      } else {
        stackable.forEach((worktree) => {
          visibleEntries.push({ type: 'worktree', id: worktree.id, worktree, items })
          visibleNodeForWorktree.set(worktree.id, worktree.id)
        })
      }

      separate.forEach((worktree) => {
        visibleEntries.push({ type: 'worktree', id: worktree.id, worktree, items })
        visibleNodeForWorktree.set(worktree.id, worktree.id)
      })
    })

    const rootDefault = nodePlacements[project.id] ?? { x: 420, y: 34 }
    const defaultBranchId = 'default:' + project.id
    const envId = 'env:' + project.id
    const nodes: Node[] = [
      {
        id: project.id,
        type: 'project',
        position: rootDefault,
        data: {
          entityId: project.id,
          kind: 'project',
          title: project.name,
          subtitle: project.repository,
          health: project.health,
          defaultBranch: project.defaultBranch,
          ciSummary: failedChecks ? failedChecks + ' failed checks' : runningCi ? runningCi + ' CI running' : 'CI healthy',
          stats: [
            { label: 'worktrees', value: projectWorktrees.length },
            { label: 'running', value: runningAgents },
            { label: 'open prs', value: activePrs },
            { label: 'ci failed', value: failedChecks },
          ],
        } satisfies BonsaiGraphData,
      },
      {
        id: defaultBranchId,
        type: 'defaultBranch',
        draggable: false,
        selectable: false,
        position: { x: rootDefault.x - 266, y: rootDefault.y },
        data: {
          entityId: defaultBranchId,
          kind: 'default-branch',
          title: project.defaultBranch,
          defaultBranchInfo: project.defaultBranchInfo,
        } satisfies BonsaiGraphData,
      },
      {
        id: envId,
        type: 'env',
        draggable: false,
        selectable: false,
        position: { x: rootDefault.x + 334, y: rootDefault.y + 42 },
        data: {
          entityId: envId,
          kind: 'env',
          title: '.env',
          envCount: (envVariables[project.id] ?? []).length,
        } satisfies BonsaiGraphData,
      },
    ]

    const edges: Edge[] = [
      {
        id: defaultBranchId + '-' + project.id,
        source: defaultBranchId,
        target: project.id,
        type: 'straight',
        data: { relationship: 'default' },
        style: { stroke: 'rgb(75 214 140 / .45)', strokeWidth: 1.3 },
      },
    ]

    visibleEntries.forEach((entry, index) => {
      const fallbackPosition = { x: 40 + index * 250, y: 220 }
      if (entry.type === 'stack') {
        const stackAgents = projectAgents.filter((agent) => entry.items.some((worktree) => worktree.id === agent.worktreeId))
        const stackPrs = entry.items.filter((worktree) => worktree.prStatus && worktree.prStatus !== 'Closed').length
        const tagDefinition = tags.find((tag) => tag.id === entry.items[0]?.tagId || tag.name === entry.tag)
        const presentation = getTagPresentation(tagDefinition)
        nodes.push({
          id: entry.id,
          type: 'stack',
          position: nodePlacements[entry.id] ?? fallbackPosition,
          data: {
            entityId: entry.id,
            kind: 'stack',
            title: entry.tag,
            tag: entry.tag,
            tagColor: presentation.foreground,
            tagBackground: presentation.background,
            tagBorder: presentation.border,
            stackCount: entry.items.length,
            health: groupHealth(entry.items),
            subtitle: stackAgents.length + ' agents · ' + stackPrs + ' PRs',
            stackItems: entry.items.map((worktree) => ({
              id: worktree.id,
              branch: worktree.branch,
              prNumber: worktree.prNumber,
              prStatus: worktree.prStatus,
              ciStatus: worktree.ciStatus,
              hasRunningAgent: projectAgents.some((agent) => agent.worktreeId === worktree.id && agent.state === 'running'),
            })),
          } satisfies BonsaiGraphData,
        })
        return
      }

      const worktree = entry.worktree
      const worktreeAgents = projectAgents.filter((agent) => agent.worktreeId === worktree.id)
      const canvasAgents = worktreeAgents.filter(
        (agent) => (agent.presentation ?? (agent.archived ? 'archived' : 'canvas')) === 'canvas',
      )
      const historyAgents = worktreeAgents.filter(
        (agent) => (agent.presentation ?? (agent.archived ? 'archived' : 'canvas')) === 'history',
      )
      const tagDefinition = tags.find((tag) => tag.id === worktree.tagId || tag.name === worktree.tag)
      const presentation = getTagPresentation(tagDefinition)
      nodes.push({
        id: worktree.id,
        type: 'worktree',
        position: nodePlacements[worktree.id] ?? fallbackPosition,
        data: {
          entityId: worktree.id,
          kind: 'worktree',
          title: worktree.branch,
          subtitle: worktree.ahead + '↑ ' + worktree.behind + '↓',
          health: worktree.status,
          tag: worktree.tag,
          tagColor: presentation.foreground,
          tagBackground: presentation.background,
          tagBorder: presentation.border,
          tagCount: entry.items.length,
          mergeTargetBranch: worktree.mergeTargetBranch,
          prNumber: worktree.prNumber,
          prStatus: worktree.prStatus,
          ciStatus: worktree.ciStatus,
          ciFailed: worktree.ciFailed,
          gitState: worktree.gitState,
          stats: [{ label: 'agents', value: canvasAgents.length }],
          historyItems: historyAgents.map((agent) => ({
            id: agent.id,
            name: agent.name,
            provider: agent.provider,
            finishedAt: agent.finishedAt,
          })),
        } satisfies BonsaiGraphData,
      })

      canvasAgents.forEach((agent, agentIndex) => {
        nodes.push({
          id: agent.id,
          type: 'agent',
          position: nodePlacements[agent.id] ?? { x: 58 + agentIndex * 192, y: 420 },
          data: {
            entityId: agent.id,
            kind: 'agent',
            title: agent.name,
            agentState: agent.state,
            provider: agent.provider,
            model: agent.model,
            reasoningEffort: agent.reasoningEffort + (agent.fastMode ? ' · Fast' : ''),
            task: agent.task,
            runtime: agent.runtime,
            worktreeId: worktree.id,
          } satisfies BonsaiGraphData,
        })
        edges.push({
          id: worktree.id + '-' + agent.id,
          source: worktree.id,
          target: agent.id,
          type: 'smoothstep',
          data: { relationship: 'agent' },
          style: {
            stroke: 'rgb(50 53 62)',
            strokeWidth: 1,
            strokeDasharray: agent.state === 'finished' ? '3 4' : undefined,
          },
        })
      })
    })

    const branchToWorktree = new Map(projectWorktrees.map((worktree) => [worktree.branch, worktree]))
    const structuralKeys = new Set<string>()

    projectWorktrees.forEach((worktree) => {
      const target = visibleNodeForWorktree.get(worktree.id)
      if (!target) return
      const parentWorktree = branchToWorktree.get(worktree.mergeTargetBranch)
      const source = parentWorktree ? visibleNodeForWorktree.get(parentWorktree.id) ?? project.id : project.id
      if (source === target) return
      const edgeKey = source + '>' + target
      if (!structuralKeys.has(edgeKey)) {
        structuralKeys.add(edgeKey)
        const nested = worktree.mergeTargetBranch !== project.defaultBranch && Boolean(parentWorktree)
        edges.push({
          id: 'structure:' + edgeKey,
          source,
          target,
          type: 'smoothstep',
          data: { relationship: 'hierarchy' },
          style: nested
            ? { stroke: 'transparent', strokeWidth: 0.1 }
            : { stroke: 'rgb(62 65 75)', strokeWidth: 1 },
        })
      }

      if (
        parentWorktree &&
        worktree.mergeTargetBranch !== project.defaultBranch &&
        visibleNodeForWorktree.get(parentWorktree.id) === parentWorktree.id &&
        target === worktree.id
      ) {
        edges.push({
          id: 'pr:' + worktree.id + '>' + parentWorktree.id,
          source: worktree.id,
          target: parentWorktree.id,
          sourceHandle: 'pr-source',
          targetHandle: 'pr-target',
          type: 'prMerge',
          data: {
            relationship: 'merge-pr',
            prNumber: worktree.prNumber,
            targetBranch: parentWorktree.branch,
          },
        })
      }
    })

    return {
      nodes,
      edges,
      projectId: project.id,
      topologyKey:
        project.id +
        '|' +
        visibleEntries.map((entry) => entry.id).join('|') +
        '|' +
        projectWorktrees.map((worktree) => worktree.id + '>' + worktree.mergeTargetBranch).join('|') +
        '|' +
        nodes.filter((node) => node.type === 'agent').map((node) => node.id).join('|'),
    }
  }, [
    activeProjectId,
    agents,
    collapsedTagGroups,
    detachedStackWorktreeIds,
    envVariables,
    nodePlacements,
    projects,
    tags,
    worktrees,
  ])

  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(graph.edges)

  useEffect(() => setNodes(graph.nodes), [graph.nodes, setNodes])
  useEffect(() => setEdges(graph.edges), [graph.edges, setEdges])

  useEffect(() => {
    setNodes((current) => {
      let changed = false
      const next = current.map((node) => {
        const data = node.data as BonsaiGraphData
        const selected =
          (data.kind === 'project' || data.kind === 'worktree' || data.kind === 'agent') &&
          selection.type === data.kind &&
          selection.id === data.entityId
        if (node.selected === selected) return node
        changed = true
        return { ...node, selected }
      })
      return changed ? next : current
    })
  }, [selection, setNodes])

  useEffect(() => {
    if (!graph.projectId) return

    const placeableNodes = graph.nodes.filter(
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
        const allPositions = computeGlobalPlacements(graph.nodes, graph.edges, nodePlacements)
        const generated = Object.fromEntries(
          placeableNodes
            .map((node) => [node.id, allPositions[node.id]] as const)
            .filter((entry): entry is [string, { x: number; y: number }] => Boolean(entry[1])),
        )
        setGeneratedNodePlacements(generated)
        previousTopology.current = {
          projectId: graph.projectId,
          ids: currentIds,
          parents: currentParents,
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
      placeMissingNodes(graph.nodes, graph.edges, nodePlacements, missingIds),
    )

    if (previous) {
      const addedIds = [...currentIds].filter((id) => !previous.ids.has(id))
      Object.assign(generated, placeAddedNodesLocally(graph.nodes, nodePlacements, addedIds))

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
          placeExpandedStackLocally(graph.nodes, workingPlacements, missingMembers, anchor),
        )
      })

      const changedParents = [...currentParents.entries()]
        .filter(([id, parent]) => previous.parents.has(id) && previous.parents.get(id) !== parent)
        .map(([id]) => id)
      Object.assign(
        generated,
        relocateGeneratedBranches(graph.nodes, graph.edges, nodePlacements, changedParents),
      )
    }

    const workingPlacements = {
      ...nodePlacements,
      ...Object.fromEntries(
        Object.entries(generated).map(([id, position]) => [id, { ...position, mode: 'generated' as const }]),
      ),
    }
    Object.assign(
      generated,
      refreshGeneratedAgentShelves(graph.nodes, graph.edges, workingPlacements),
    )

    if (Object.keys(generated).length) setGeneratedNodePlacements(generated)
    previousTopology.current = {
      projectId: graph.projectId,
      ids: currentIds,
      parents: currentParents,
      stacks: currentStacks,
    }
  }, [
    graph.projectId,
    graph.topologyKey,
    nodePlacements,
    setGeneratedNodePlacements,
  ])

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
    const positions = computeGlobalPlacements(nodes, edges, nodePlacements)
    setNodes((current) =>
      current.map((node) => ({ ...node, position: positions[node.id] ?? node.position })),
    )
    const generated = Object.fromEntries(
      nodes
        .filter((node) => node.type !== 'defaultBranch' && node.type !== 'env')
        .map((node) => [node.id, positions[node.id]] as const)
        .filter((entry): entry is [string, { x: number; y: number }] => Boolean(entry[1])),
    )
    setGeneratedNodePlacements(generated)
    requestAnimationFrame(() => void fitViewRef.current({ padding: 0.14, duration: 300 }))
  }, [
    canvasCommand,
    edges,
    nodePlacements,
    nodes,
    setGeneratedNodePlacements,
    setNodes,
  ])

  const onNodeClick: NodeMouseHandler = (_, node) => {
    const data = node.data as BonsaiGraphData
    if (data.kind === 'stack') {
      if (data.tag) toggleTagGroup(activeProjectId, data.tag)
      return
    }
    if (data.kind === 'project' || data.kind === 'worktree' || data.kind === 'agent') {
      setSelection({ type: data.kind, id: data.entityId })
    }
  }

  const autoLayout = () => {
    const positions = computeGlobalPlacements(nodes, edges, nodePlacements)
    setNodes((current) =>
      current.map((node) => ({ ...node, position: positions[node.id] ?? node.position })),
    )
    const generated = Object.fromEntries(
      nodes
        .filter((node) => node.type !== 'defaultBranch' && node.type !== 'env')
        .map((node) => [node.id, positions[node.id]] as const)
        .filter((entry): entry is [string, { x: number; y: number }] => Boolean(entry[1])),
    )
    setGeneratedNodePlacements(generated)
    requestAnimationFrame(() => void fitViewRef.current({ padding: 0.14, duration: 300 }))
  }

  const beginDrag: OnNodeDrag<Node> = (_, node) => {
    const companionIds = new Set<string>()
    if (node.type === 'project') {
      companionIds.add('default:' + activeProjectId)
      companionIds.add('env:' + activeProjectId)
    }
    if (subtreeMoveRootId === node.id) getDescendantIds(node.id, edges).forEach((id) => companionIds.add(id))
    const positions: Record<string, { x: number; y: number }> = {}
    nodes.forEach((candidate) => {
      if (companionIds.has(candidate.id)) positions[candidate.id] = { ...candidate.position }
    })
    dragSnapshot.current = { rootId: node.id, rootStart: { ...node.position }, companionIds: [...companionIds], positions }
  }

  const dragNode: OnNodeDrag<Node> = (_, node) => {
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
  }

  const finishDrag: OnNodeDrag<Node> = (_, node) => {
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
        const moved = nodes.find((candidate) => candidate.id === id)
        return moved?.type !== 'defaultBranch' && moved?.type !== 'env'
      }),
    )
    setManualNodePlacements(persisted)
    dragSnapshot.current = null
    setSubtreeMoveRoot(null)
  }

  return (
    <div className="relative h-full min-h-0 w-full bg-[rgb(var(--bg))]">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onNodeDragStart={beginDrag}
        onNodeDrag={dragNode}
        onNodeDragStop={finishDrag}
        onMoveEnd={(_, nextViewport) => setViewport(nextViewport)}
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
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="rgb(44 47 55)" />
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
