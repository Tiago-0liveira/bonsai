import { useEffect, useMemo, useRef } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  ReactFlow,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
} from '@xyflow/react'
import { LocateFixed, Network, Plus } from 'lucide-react'
import { useBonsaiStore } from '../../../stores/bonsai'
import type { Health, Worktree } from '../../../types'
import { layoutGraph } from './layout'
import {
  AgentNode,
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
}

function groupHealth(items: Worktree[]): Health {
  if (items.some((item) => item.status === 'error')) return 'error'
  if (items.some((item) => item.status === 'warning')) return 'warning'
  if (items.some((item) => item.status === 'healthy')) return 'healthy'
  return 'idle'
}

export function BonsaiCanvas({ focus }: { focus?: 'worktrees' | 'agents' }) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const collapsedTagGroups = useBonsaiStore((state) => state.collapsedTagGroups)
  const toggleTagGroup = useBonsaiStore((state) => state.toggleTagGroup)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const nodePositions = useBonsaiStore((state) => state.nodePositions)
  const setNodePosition = useBonsaiStore((state) => state.setNodePosition)
  const viewport = useBonsaiStore((state) => state.viewport)
  const setViewport = useBonsaiStore((state) => state.setViewport)
  const canvasCommand = useBonsaiStore((state) => state.canvasCommand)
  const createMockWorktree = useBonsaiStore((state) => state.createMockWorktree)
  const lastCommand = useRef(0)
  const { fitView } = useReactFlow()

  const graph = useMemo(() => {
    const project = projects.find((item) => item.id === activeProjectId) ?? projects[0]
    if (!project) return { nodes: [] as Node[], edges: [] as Edge[], shapeKey: 'empty' }

    const projectWorktrees = worktrees.filter((worktree) => worktree.projectId === project.id)
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

    const visibleParents: Array<
      | { type: 'stack'; tag: string; items: Worktree[] }
      | { type: 'worktree'; worktree: Worktree; items: Worktree[] }
    > = []

    groups.forEach((items, tag) => {
      const collapsed = items.length > 1 && collapsedTagGroups.includes(project.id + ':' + tag)
      if (collapsed) {
        visibleParents.push({ type: 'stack', tag, items })
      } else {
        items.forEach((worktree) => visibleParents.push({ type: 'worktree', worktree, items }))
      }
    })

    const width = Math.max(300, visibleParents.length * 242)
    const rootDefault = { x: Math.max(24, width / 2 - 150), y: 24 }

    const nodes: Node[] = [
      {
        id: project.id,
        type: 'project',
        position: nodePositions[project.id] ?? rootDefault,
        selected: selection.type === 'project' && selection.id === project.id,
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
    ]
    const edges: Edge[] = []

    visibleParents.forEach((entry, parentIndex) => {
      const parentX = 24 + parentIndex * 242
      if (entry.type === 'stack') {
        const stackId = 'stack:' + project.id + ':' + entry.tag
        const stackAgents = projectAgents.filter((agent) =>
          entry.items.some((worktree) => worktree.id === agent.worktreeId),
        )
        const stackPrs = entry.items.filter((worktree) => worktree.prStatus && worktree.prStatus !== 'Closed').length
        nodes.push({
          id: stackId,
          type: 'stack',
          position: nodePositions[stackId] ?? { x: parentX, y: 205 },
          data: {
            entityId: stackId,
            kind: 'stack',
            title: entry.tag,
            tag: entry.tag,
            stackCount: entry.items.length,
            health: groupHealth(entry.items),
            subtitle: stackAgents.length + ' agents · ' + stackPrs + ' PRs',
          } satisfies BonsaiGraphData,
        })
        edges.push({
          id: project.id + '-' + stackId,
          source: project.id,
          target: stackId,
          type: 'smoothstep',
          style: { stroke: 'rgb(62 65 75)', strokeWidth: 1 },
        })
        return
      }

      const worktree = entry.worktree
      const worktreeAgents = projectAgents.filter((agent) => agent.worktreeId === worktree.id)
      nodes.push({
        id: worktree.id,
        type: 'worktree',
        position: nodePositions[worktree.id] ?? { x: parentX, y: 205 },
        selected: selection.type === 'worktree' && selection.id === worktree.id,
        data: {
          entityId: worktree.id,
          kind: 'worktree',
          title: worktree.branch,
          subtitle: worktree.ahead + '↑ ' + worktree.behind + '↓',
          health: worktree.status,
          tag: worktree.tag,
          tagCount: entry.items.length,
          prNumber: worktree.prNumber,
          prStatus: worktree.prStatus,
          ciStatus: worktree.ciStatus,
          ciFailed: worktree.ciFailed,
          gitState: worktree.gitState,
          stats: [{ label: 'agents', value: worktreeAgents.length }],
        } satisfies BonsaiGraphData,
      })
      edges.push({
        id: project.id + '-' + worktree.id,
        source: project.id,
        target: worktree.id,
        type: 'smoothstep',
        style: { stroke: 'rgb(62 65 75)', strokeWidth: 1 },
      })

      worktreeAgents.forEach((agent, agentIndex) => {
        const id = agent.id
        nodes.push({
          id,
          type: 'agent',
          position: nodePositions[id] ?? { x: parentX + 19, y: 352 + agentIndex * 106 },
          selected: selection.type === 'agent' && selection.id === id,
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
          style: { stroke: 'rgb(50 53 62)', strokeWidth: 1 },
        })
      })
    })

    return {
      nodes,
      edges,
      shapeKey: project.id + '|' + visibleParents.map((entry) => entry.type === 'stack' ? 'stack:' + entry.tag : entry.worktree.id).join('|'),
    }
  }, [activeProjectId, agents, collapsedTagGroups, nodePositions, projects, selection, worktrees])

  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(graph.edges)

  useEffect(() => setNodes(graph.nodes), [graph.nodes, setNodes])
  useEffect(() => setEdges(graph.edges), [graph.edges, setEdges])

  useEffect(() => {
    const frame = requestAnimationFrame(() => void fitView({ padding: 0.14, duration: 280 }))
    return () => cancelAnimationFrame(frame)
  }, [graph.shapeKey, fitView])

  useEffect(() => {
    if (!focus) return
    const ids =
      focus === 'agents'
        ? nodes.filter((node) => node.type === 'agent').map((node) => node.id)
        : nodes.filter((node) => node.type === 'worktree' || node.type === 'stack').map((node) => node.id)
    const visible = nodes.filter((node) => ids.includes(node.id))
    if (visible.length) void fitView({ nodes: visible, padding: 0.2, duration: 300 })
  }, [focus, fitView])

  useEffect(() => {
    if (!canvasCommand.nonce || lastCommand.current === canvasCommand.nonce) return
    lastCommand.current = canvasCommand.nonce
    if (canvasCommand.type === 'fit') {
      void fitView({ padding: 0.14, duration: 300 })
      return
    }

    void layoutGraph(nodes, edges).then((laidOut) => {
      setNodes(laidOut)
      laidOut.forEach((node) => setNodePosition(node.id, node.position))
      requestAnimationFrame(() => void fitView({ padding: 0.14, duration: 300 }))
    })
  }, [canvasCommand, edges, fitView, nodes, setNodePosition, setNodes])

  const onNodeClick: NodeMouseHandler = (_, node) => {
    const data = node.data as BonsaiGraphData
    if (data.kind === 'stack') {
      if (data.tag) toggleTagGroup(activeProjectId, data.tag)
      return
    }
    setSelection({ type: data.kind, id: data.entityId })
  }

  const autoLayout = () => {
    void layoutGraph(nodes, edges).then((laidOut) => {
      setNodes(laidOut)
      laidOut.forEach((node) => setNodePosition(node.id, node.position))
      requestAnimationFrame(() => void fitView({ padding: 0.14, duration: 300 }))
    })
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
        onNodeDragStop={(_, node) => setNodePosition(node.id, node.position)}
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
        onClick={() => createMockWorktree('feat')}
        className="bonsai-focus absolute right-3 top-3 flex items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] px-2.5 py-2 text-[10px] text-[rgb(var(--muted))] shadow-lg backdrop-blur hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
      >
        <Plus size={12} /> New worktree
      </button>
    </div>
  )
}
