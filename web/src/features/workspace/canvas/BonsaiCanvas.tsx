import { useEffect, useMemo, useRef } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
} from '@xyflow/react'
import { LocateFixed, Network } from 'lucide-react'
import { projects } from '../../../mock/projects'
import { useBonsaiStore } from '../../../stores/bonsai'
import { layoutGraph } from './layout'
import {
  AgentNode,
  ProjectNode,
  WorktreeNode,
  type BonsaiGraphData,
} from '../nodes/BonsaiNode'

const nodeTypes = {
  project: ProjectNode,
  worktree: WorktreeNode,
  agent: AgentNode,
}

function defaultPosition(id: string, kind: BonsaiGraphData['kind'], index: number) {
  if (kind === 'project') return { x: 40, y: 260 }
  if (kind === 'worktree') return { x: 390, y: 55 + index * 175 }
  return { x: 770 + (index % 2) * 270, y: 25 + Math.floor(index / 2) * 158 }
}

export function BonsaiCanvas({ focus }: { focus?: 'worktrees' | 'agents' }) {
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const agents = useBonsaiStore((state) => state.agents)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const nodePositions = useBonsaiStore((state) => state.nodePositions)
  const setNodePosition = useBonsaiStore((state) => state.setNodePosition)
  const viewport = useBonsaiStore((state) => state.viewport)
  const setViewport = useBonsaiStore((state) => state.setViewport)
  const canvasCommand = useBonsaiStore((state) => state.canvasCommand)
  const lastCommand = useRef(0)
  const { fitView } = useReactFlow()

  const graph = useMemo(() => {
    const project = projects[0]
    const projectAgents = agents.length
    const nodes: Node[] = [
      {
        id: project.id,
        type: 'project',
        position: nodePositions[project.id] ?? defaultPosition(project.id, 'project', 0),
        selected: selection.type === 'project' && selection.id === project.id,
        data: {
          entityId: project.id,
          kind: 'project',
          title: project.name,
          subtitle: project.repository,
          health: project.health,
          stats: [
            { label: 'trees', value: worktrees.length },
            { label: 'agents', value: projectAgents },
            { label: 'open prs', value: project.openPrCount },
          ],
        } satisfies BonsaiGraphData,
      },
    ]

    worktrees.forEach((worktree, index) => {
      nodes.push({
        id: worktree.id,
        type: 'worktree',
        position: nodePositions[worktree.id] ?? defaultPosition(worktree.id, 'worktree', index),
        selected: selection.type === 'worktree' && selection.id === worktree.id,
        data: {
          entityId: worktree.id,
          kind: 'worktree',
          title: worktree.branch,
          subtitle: `${worktree.ahead} ahead · ${worktree.behind} behind`,
          health: worktree.status,
          worktreeKind: worktree.kind,
          prNumber: worktree.prNumber,
          gitState: worktree.gitState,
          stats: [
            { label: 'agents', value: worktree.agentIds.length },
            { label: 'ahead', value: worktree.ahead },
            { label: 'behind', value: worktree.behind },
          ],
        } satisfies BonsaiGraphData,
      })
    })

    agents.forEach((agent, index) => {
      nodes.push({
        id: agent.id,
        type: 'agent',
        position: nodePositions[agent.id] ?? defaultPosition(agent.id, 'agent', index),
        selected: selection.type === 'agent' && selection.id === agent.id,
        data: {
          entityId: agent.id,
          kind: 'agent',
          title: agent.name,
          subtitle: agent.state,
          agentState: agent.state,
          provider: agent.provider,
          task: agent.task,
          runtime: agent.runtime,
        } satisfies BonsaiGraphData,
      })
    })

    const edges: Edge[] = worktrees
      .map((worktree) => ({
        id: `project-${worktree.id}`,
        source: project.id,
        target: worktree.id,
        type: 'smoothstep',
        style: { stroke: 'rgb(62 65 75)', strokeWidth: 1 },
      }))
      .concat(
        agents.map((agent) => ({
          id: `${agent.worktreeId}-${agent.id}`,
          source: agent.worktreeId,
          target: agent.id,
          type: 'smoothstep',
          style: { stroke: 'rgb(50 53 62)', strokeWidth: 1 },
        })),
      )

    return { nodes, edges }
  }, [agents, nodePositions, selection, worktrees])

  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(graph.edges)

  useEffect(() => setNodes(graph.nodes), [graph.nodes, setNodes])
  useEffect(() => setEdges(graph.edges), [graph.edges, setEdges])

  useEffect(() => {
    if (!focus) return
    const ids =
      focus === 'agents' ? agents.map((agent) => agent.id) : worktrees.map((worktree) => worktree.id)
    const visible = nodes.filter((node) => ids.includes(node.id))
    if (visible.length) {
      void fitView({ nodes: visible, padding: 0.25, duration: 350 })
    }
  }, [focus])

  useEffect(() => {
    if (!canvasCommand.nonce || lastCommand.current === canvasCommand.nonce) return
    lastCommand.current = canvasCommand.nonce
    if (canvasCommand.type === 'fit') {
      void fitView({ padding: 0.18, duration: 350 })
      return
    }

    void layoutGraph(nodes, edges).then((laidOut) => {
      setNodes(laidOut)
      laidOut.forEach((node) => setNodePosition(node.id, node.position))
      requestAnimationFrame(() => void fitView({ padding: 0.18, duration: 350 }))
    })
  }, [canvasCommand, edges, fitView, nodes, setNodePosition, setNodes])

  const onNodeClick: NodeMouseHandler = (_, node) => {
    const data = node.data as BonsaiGraphData
    setSelection({ type: data.kind, id: data.entityId })
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
        minZoom={0.25}
        maxZoom={1.8}
        selectionOnDrag
        panOnScroll
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={18}
          size={1}
          color="rgb(44 47 55)"
        />
        <Controls position="bottom-left" showInteractive={false} />
        <MiniMap
          position="bottom-right"
          nodeColor={(node) =>
            node.type === 'project'
              ? 'rgb(151 109 255)'
              : node.type === 'worktree'
                ? 'rgb(92 157 255)'
                : 'rgb(75 214 140)'
          }
          maskColor="rgb(8 9 11 / .76)"
        />
      </ReactFlow>

      <div className="pointer-events-none absolute left-3 top-3 flex items-center gap-2">
        <div className="pointer-events-auto flex items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel)/.94)] p-0.5 shadow-lg backdrop-blur">
          <button
            onClick={() => void fitView({ padding: 0.18, duration: 300 })}
            className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <LocateFixed size={12} /> Fit
          </button>
          <button
            onClick={() => {
              void layoutGraph(nodes, edges).then((laidOut) => {
                setNodes(laidOut)
                laidOut.forEach((node) => setNodePosition(node.id, node.position))
                requestAnimationFrame(() => void fitView({ padding: 0.18, duration: 300 }))
              })
            }}
            className="bonsai-focus flex items-center gap-1.5 rounded px-2 py-1.5 text-[11px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <Network size={12} /> Auto-layout
          </button>
        </div>
      </div>
    </div>
  )
}
