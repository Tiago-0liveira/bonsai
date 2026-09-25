import { useEffect, useMemo, useRef } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  ReactFlow,
  useEdgesState,
  useNodesState,
  useNodesInitialized,
  useReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { LocateFixed, Network, Plus } from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import type { Health, Worktree } from '../../../types'
import { CreateWorktreeDialog } from '../CreateWorktreeDialog'
import { EnvEditor } from '../EnvEditor'
import { getTagPresentation } from '../tagStyles'
import { getDescendantIds, layoutGraph } from './layout'
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
  const stackExcludedWorktreeIds = useBonsaiStore((state) => state.stackExcludedWorktreeIds)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const nodePositions = useBonsaiStore((state) => state.nodePositions)
  const setNodePosition = useBonsaiStore((state) => state.setNodePosition)
  const setNodePositionsBatch = useBonsaiStore((state) => state.setNodePositionsBatch)
  const subtreeMoveRootId = useBonsaiStore((state) => state.subtreeMoveRootId)
  const setSubtreeMoveRoot = useBonsaiStore((state) => state.setSubtreeMoveRoot)
  const viewport = useBonsaiStore((state) => state.viewport)
  const setViewport = useBonsaiStore((state) => state.setViewport)
  const canvasCommand = useBonsaiStore((state) => state.canvasCommand)
  const setWorktreeDialogOpen = useBonsaiStore((state) => state.setWorktreeDialogOpen)
  const envVariables = useBonsaiStore((state) => state.envVariables)
  const lastCommand = useRef(0)
  const dragSnapshot = useRef<DragSnapshot | null>(null)
  const { fitView } = useReactFlow()
  const nodesInitialized = useNodesInitialized()
  const fitViewRef = useRef(fitView)

  useEffect(() => {
    fitViewRef.current = fitView
  }, [fitView])

  const graph = useMemo(() => {
    const project = projects.find((item) => item.id === activeProjectId) ?? projects[0]
    if (!project) return { nodes: [] as Node[], edges: [] as Edge[], shapeKey: 'empty' }

    const allProjectWorktrees = worktrees.filter((worktree) => worktree.projectId === project.id)
    const projectWorktrees = allProjectWorktrees.filter((worktree) => worktree.branch !== project.defaultBranch)
    const projectWorktreeIds = new Set(projectWorktrees.map((worktree) => worktree.id))
    const projectAgents = agents.filter((agent) => projectWorktreeIds.has(agent.worktreeId))
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
      const excluded = items.filter((item) => stackExcludedWorktreeIds.includes(item.id))
      const stackable = items.filter((item) => !stackExcludedWorktreeIds.includes(item.id))
      const collapsed =
        stackable.length > 1 &&
        collapsedTagGroups.includes(project.id + ':' + tag)

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

      excluded.forEach((worktree) => {
        visibleEntries.push({ type: 'worktree', id: worktree.id, worktree, items })
        visibleNodeForWorktree.set(worktree.id, worktree.id)
      })
    })

    const rootDefault = nodePositions[project.id] ?? { x: 420, y: 34 }
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
        position: { x: rootDefault.x - 278, y: rootDefault.y },
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
        position: { x: rootDefault.x + 334, y: rootDefault.y + 48 },
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
        const stackAgents = projectAgents.filter((agent) =>
          entry.items.some((worktree) => worktree.id === agent.worktreeId),
        )
        const stackPrs = entry.items.filter((worktree) => worktree.prStatus && worktree.prStatus !== 'Closed').length
        const tagDefinition = tags.find((tag) => tag.id === entry.items[0]?.tagId || tag.name === entry.tag)
        const presentation = getTagPresentation(tagDefinition)
        nodes.push({
          id: entry.id,
          type: 'stack',
          position: nodePositions[entry.id] ?? fallbackPosition,
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
              prStatus: worktree.prStatus,
              ciStatus: worktree.ciStatus,
            })),
          } satisfies BonsaiGraphData,
        })
        return
      }

      const worktree = entry.worktree
      const worktreeAgents = projectAgents.filter((agent) => agent.worktreeId === worktree.id)
      const tagDefinition = tags.find((tag) => tag.id === worktree.tagId || tag.name === worktree.tag)
      const presentation = getTagPresentation(tagDefinition)
      nodes.push({
        id: worktree.id,
        type: 'worktree',
        position: nodePositions[worktree.id] ?? fallbackPosition,
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
          stats: [{ label: 'agents', value: worktreeAgents.length }],
        } satisfies BonsaiGraphData,
      })
    })

    const branchToWorktree = new Map(projectWorktrees.map((worktree) => [worktree.branch, worktree]))
    const edgeKeys = new Set<string>()

    projectWorktrees.forEach((worktree) => {
      const target = visibleNodeForWorktree.get(worktree.id)
      if (!target) return
      const parentWorktree = branchToWorktree.get(worktree.mergeTargetBranch)
      const source = parentWorktree
        ? visibleNodeForWorktree.get(parentWorktree.id) ?? project.id
        : project.id
      if (source === target) return
      const edgeKey = source + '>' + target
      if (edgeKeys.has(edgeKey)) return
      edgeKeys.add(edgeKey)
      const nested = worktree.mergeTargetBranch !== project.defaultBranch && Boolean(parentWorktree)
      edges.push({
        id: 'merge:' + edgeKey,
        source,
        target,
        type: 'smoothstep',
        data: { relationship: 'hierarchy', mergeTargetBranch: worktree.mergeTargetBranch },
        label: nested ? 'merge → ' + worktree.mergeTargetBranch : undefined,
        labelStyle: nested ? { fill: 'rgb(151 109 255)', fontSize: 8 } : undefined,
        labelBgStyle: nested ? { fill: 'rgb(12 14 17)', fillOpacity: 0.94 } : undefined,
        labelBgPadding: nested ? [4, 2] : undefined,
        style: nested
          ? { stroke: 'rgb(151 109 255 / .62)', strokeWidth: 1.25, strokeDasharray: '5 4' }
          : { stroke: 'rgb(62 65 75)', strokeWidth: 1 },
      })
    })

    projectWorktrees.forEach((worktree) => {
      const visibleParent = visibleNodeForWorktree.get(worktree.id)
      if (!visibleParent || visibleParent !== worktree.id) return
      const worktreeAgents = projectAgents.filter((agent) => agent.worktreeId === worktree.id)
      worktreeAgents.forEach((agent, agentIndex) => {
        const id = agent.id
        nodes.push({
          id,
          type: 'agent',
          position: nodePositions[id] ?? { x: 58, y: 420 + agentIndex * 110 },
          data: {
            entityId: id,
            kind: 'agent',
            title: agent.name,
            agentState: agent.state,
            provider: agent.provider,
            task: agent.task,
            runtime: agent.runtime,
          } satisfies BonsaiGraphData,
        })
        edges.push({
          id: worktree.id + '-' + id,
          source: worktree.id,
          target: id,
          type: 'smoothstep',
          data: { relationship: 'agent' },
          style: { stroke: 'rgb(50 53 62)', strokeWidth: 1 },
        })
      })
    })

    return {
      nodes,
      edges,
      shapeKey:
        project.id +
        '|' +
        visibleEntries.map((entry) => entry.id).join('|') +
        '|' +
        projectWorktrees.map((worktree) => worktree.id + '>' + worktree.mergeTargetBranch).join('|'),
    }
  }, [
    activeProjectId,
    agents,
    collapsedTagGroups,
    envVariables,
    nodePositions,
    projects,
    stackExcludedWorktreeIds,
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
    let cancelled = false
    void layoutGraph(graph.nodes, graph.edges).then((laidOut) => {
      if (cancelled) return
      setNodes(laidOut)
      const positions: Record<string, { x: number; y: number }> = {}
      laidOut.forEach((node) => {
        if (node.type === 'defaultBranch' || node.type === 'env') return
        positions[node.id] = node.position
      })
      setNodePositionsBatch(positions)
    })
    return () => {
      cancelled = true
    }
  }, [graph.shapeKey, setNodePositionsBatch, setNodes])

  useEffect(() => {
    if (!nodesInitialized) return
    const frame = requestAnimationFrame(() => {
      void fitViewRef.current({ padding: 0.14, duration: 280 })
    })
    return () => cancelAnimationFrame(frame)
  }, [graph.shapeKey, nodesInitialized])

  useEffect(() => {
    if (!focus || !nodesInitialized) return
    const ids =
      focus === 'agents'
        ? nodes.filter((node) => node.type === 'agent').map((node) => node.id)
        : nodes.filter((node) => node.type === 'worktree' || node.type === 'stack').map((node) => node.id)
    const visible = nodes.filter((node) => ids.includes(node.id))
    if (!visible.length) return
    const frame = requestAnimationFrame(() => {
      void fitViewRef.current({ nodes: visible, padding: 0.2, duration: 300 })
    })
    return () => cancelAnimationFrame(frame)
  }, [focus, graph.shapeKey, nodesInitialized])

  useEffect(() => {
    if (!canvasCommand.nonce || lastCommand.current === canvasCommand.nonce) return
    lastCommand.current = canvasCommand.nonce
    if (canvasCommand.type === 'fit') {
      void fitViewRef.current({ padding: 0.14, duration: 300 })
      return
    }

    void layoutGraph(nodes, edges).then((laidOut) => {
      setNodes(laidOut)
      const positions: Record<string, { x: number; y: number }> = {}
      laidOut.forEach((node) => {
        if (node.type === 'defaultBranch' || node.type === 'env') return
        positions[node.id] = node.position
      })
      setNodePositionsBatch(positions)
      requestAnimationFrame(() => void fitViewRef.current({ padding: 0.14, duration: 300 }))
    })
  }, [canvasCommand, edges, nodes, setNodePositionsBatch, setNodes])

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
    void layoutGraph(nodes, edges).then((laidOut) => {
      setNodes(laidOut)
      const positions: Record<string, { x: number; y: number }> = {}
      laidOut.forEach((node) => {
        if (node.type === 'defaultBranch' || node.type === 'env') return
        positions[node.id] = node.position
      })
      setNodePositionsBatch(positions)
      requestAnimationFrame(() => void fitView({ padding: 0.14, duration: 300 }))
    })
  }

  const beginDrag: OnNodeDrag<Node> = (_, node) => {
    const companionIds = new Set<string>()
    if (node.type === 'project') {
      companionIds.add('default:' + activeProjectId)
      companionIds.add('env:' + activeProjectId)
    }
    if (subtreeMoveRootId === node.id) {
      getDescendantIds(node.id, edges).forEach((id) => companionIds.add(id))
    }
    const positions: Record<string, { x: number; y: number }> = {}
    nodes.forEach((candidate) => {
      if (companionIds.has(candidate.id)) positions[candidate.id] = { ...candidate.position }
    })
    dragSnapshot.current = {
      rootId: node.id,
      rootStart: { ...node.position },
      companionIds: [...companionIds],
      positions,
    }
  }

  const dragNode: OnNodeDrag<Node> = (_, node) => {
    const snapshot = dragSnapshot.current
    if (!snapshot || snapshot.rootId !== node.id || !snapshot.companionIds.length) return
    const dx = node.position.x - snapshot.rootStart.x
    const dy = node.position.y - snapshot.rootStart.y
    setNodes((current) =>
      current.map((candidate) => {
        const initial = snapshot.positions[candidate.id]
        return initial
          ? { ...candidate, position: { x: initial.x + dx, y: initial.y + dy } }
          : candidate
      }),
    )
  }

  const finishDrag: OnNodeDrag<Node> = (_, node) => {
    const snapshot = dragSnapshot.current
    if (!snapshot || snapshot.rootId !== node.id) {
      setNodePosition(node.id, node.position)
      setSubtreeMoveRoot(null)
      return
    }
    const dx = node.position.x - snapshot.rootStart.x
    const dy = node.position.y - snapshot.rootStart.y
    const positions: Record<string, { x: number; y: number }> = {
      [node.id]: node.position,
    }
    snapshot.companionIds.forEach((id) => {
      const initial = snapshot.positions[id]
      if (initial) positions[id] = { x: initial.x + dx, y: initial.y + dy }
    })
    setNodePositionsBatch(positions)
    dragSnapshot.current = null
    setSubtreeMoveRoot(null)
  }

  return (
    <div className="relative h-full min-h-0 w-full bg-[rgb(var(--bg))]">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
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
        panOnScroll
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="rgb(44 47 55)" />
        <Controls position="bottom-left" showInteractive={false} />
      </ReactFlow>

      <div className="pointer-events-none absolute left-3 top-3 flex items-center gap-2">
        <div className="pointer-events-auto flex items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] p-0.5 shadow-lg backdrop-blur">
          <button
            onClick={() => void fitView({ padding: 0.14, duration: 300 })}
            className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <LocateFixed size={12} /> Fit
          </button>
          <button
            onClick={autoLayout}
            className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <Network size={12} /> Auto-layout
          </button>
        </div>
        <span className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.88)] px-2 py-1.5 text-[9px] text-[rgb(var(--muted-2))]">
          {nodes.length} visible nodes
        </span>
      </div>

      <button
        onClick={() => setWorktreeDialogOpen(true)}
        className="bonsai-focus absolute right-3 top-3 flex items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] px-2.5 py-2 text-[10px] text-[rgb(var(--muted))] shadow-lg backdrop-blur hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
      >
        <Plus size={12} /> New worktree
      </button>

      <CreateWorktreeDialog />
      <EnvEditor />
    </div>
  )
}
