import { describe, expect, it } from 'vitest'
import { buildCanvasGraph, type CanvasGraphInput } from './buildCanvasGraph'
import { applyCanvasSelection, reconcileCanvasNodes } from './graphReconciliation'
import { computeGlobalPlacements } from './layout/globalLayout'
import { getDescendantIds } from './layout/graphModel'
import { getNodeRect, rectsOverlap } from './layout/geometry'
import { placeMissingNodes, refreshGeneratedRuntimeShelves } from './layout/localPlacement'
import { projectCanvasProcessesSelector, processNodeSelector } from '../../../stores/projectSelectors'
import { useBonsaiStore } from '../../../stores/bonsai'
import { projects } from '../../../test/fixtures/projects'
import { worktrees } from '../../../test/fixtures/worktrees'
import { agents } from '../../../mock/agents'
import type { Process } from '../../../types'

const process = (id: number, worktreeId = 'wt-web'): Process => ({ id: `bonsai:${id}`, projectId: 'bonsai', daemonId: id, worktreeId, name: 'Same command', command: 'pnpm dev', status: 'healthy', lifecycleStatus: 'running' })
const input = (): CanvasGraphInput => ({ project: projects[0], worktrees, agents: [], processes: [], tags: [], collapsedTagGroups: [], detachedStackWorktreeIds: [], expandedAutomaticGroups: [], nodePlacements: {}, envCount: 0 })

describe('process graph and runtime layout', () => {
  it('projects a process immediately on a feature worktree and selects its stable node', () => {
    const data = input(); data.processes = [process(1)]
    const graph = buildCanvasGraph(data)
    expect(graph.nodes.filter(node => node.type === 'process')).toHaveLength(1)
    expect(graph.edges).toContainEqual(expect.objectContaining({ source: 'wt-web', target: 'bonsai:1', data: { relationship: 'process' } }))
    expect(applyCanvasSelection(graph.nodes, { type: 'process', id: 'bonsai:1' }).find(node => node.id === 'bonsai:1')?.selected).toBe(true)
    expect(getDescendantIds('wt-web', graph.edges)).toContain('bonsai:1')
  })

  it('includes main when it owns a process without any agents', () => {
    const data = input()
    const main = { ...worktrees[0], id: 'main', main: true, branch: projects[0].defaultBranch, projectId: projects[0].id }
    data.worktrees = [main]; data.processes = [process(1, 'main')]
    const graph = buildCanvasGraph(data)
    expect(graph.nodes.find(node => node.id === 'main')?.type).toBe('worktree')
    expect(graph.edges).toContainEqual(expect.objectContaining({ source: 'main', target: 'bonsai:1' }))
    expect(getDescendantIds(projects[0].id, graph.edges)).toEqual(expect.arrayContaining(['main', 'bonsai:1']))
  })

  it('keeps agent and process nodes attached to collapsed stacks with real ownership and counts', () => {
    const data = input()
    data.worktrees = worktrees.map(tree => ({ ...tree, tag: 'together', stackPreference: 'auto' }))
    data.agents = [{ ...agents[0], worktreeId: 'wt-web', presentation: 'canvas' }]
    data.processes = [process(1), process(2, 'wt-daemon')]
    data.collapsedTagGroups = ['bonsai:together']
    const graph = buildCanvasGraph(data), stack = graph.nodes.find(node => node.type === 'stack')!
    expect(stack.data.subtitle).toContain('2 processes')
    expect(graph.nodes.find(node => node.id === 'bonsai:1')?.data.worktreeId).toBe('wt-web')
    expect(graph.edges.filter(edge => edge.data?.relationship === 'process').every(edge => edge.source === stack.id)).toBe(true)
    expect(getDescendantIds(stack.id, graph.edges)).toEqual(expect.arrayContaining([agents[0].id, 'bonsai:1', 'bonsai:2']))
    expect((stack.data.stackItems as { id: string; processCount: number }[]).find(tree => tree.id === 'wt-web')?.processCount).toBe(1)
  })

  it('shows legacy processes on a labeled project shelf rather than another worktree', () => {
    const data = input(); data.processes = [process(1, ''), process(2, 'missing-worktree')]
    const graph = buildCanvasGraph(data)
    expect(graph.nodes.find(node => node.id === 'process-shelf:bonsai')?.data.subtitle).toBe('Worktree association unavailable')
    expect(graph.edges.filter(edge => edge.data?.relationship === 'process').every(edge => edge.source === 'process-shelf:bonsai')).toBe(true)
    expect(getDescendantIds('bonsai', graph.edges)).toEqual(expect.arrayContaining(['process-shelf:bonsai', 'bonsai:1', 'bonsai:2']))
  })

  it('places mixed agent/process shelves, collapsed stack children and unresolved records without overlaps', () => {
    const data = input()
    data.agents = agents.filter(agent => agent.worktreeId === 'wt-web').map(agent => ({ ...agent, presentation: 'canvas' }))
    data.processes = [process(1), process(2), process(3), process(4, ''), process(5, 'wt-daemon')]
    for (const collapsed of [false, true]) {
      data.collapsedTagGroups = collapsed ? ['bonsai:feat'] : []
      const graph = buildCanvasGraph(data), positions = computeGlobalPlacements(graph.nodes, graph.edges)
      for (let i = 0; i < graph.nodes.length; i++) {
        const node = graph.nodes[i]
        expect(positions[node.id]).toBeDefined()
        for (const other of graph.nodes.slice(i + 1)) {
          expect(rectsOverlap(getNodeRect(node, positions[node.id]), getNodeRect(other, positions[other.id])), `${node.id} overlaps ${other.id}`).toBe(false)
        }
      }
    }
  })

  it('locally adds a process without moving manual runtimes or unrelated branches', () => {
    const data = input(); data.processes = [process(1)]
    let graph = buildCanvasGraph(data)
    const positions = computeGlobalPlacements(graph.nodes, graph.edges)
    data.nodePlacements = Object.fromEntries(Object.entries(positions).map(([id, position]) => [id, { ...position, mode: 'manual' }]))
    data.processes.push(process(2))
    graph = buildCanvasGraph(data)
    const missing = placeMissingNodes(graph.nodes, graph.edges, data.nodePlacements, ['bonsai:2'])
    const shelves = refreshGeneratedRuntimeShelves(graph.nodes, graph.edges, data.nodePlacements, new Set(['wt-web']))
    expect(missing).toEqual({})
    expect(Object.keys(shelves)).toEqual(['bonsai:2'])
    const newNode = graph.nodes.find(node => node.id === 'bonsai:2')!
    for (const node of graph.nodes.filter(node => node.id !== newNode.id)) {
      expect(rectsOverlap(getNodeRect(newNode, shelves[newNode.id]), getNodeRect(node, data.nodePlacements[node.id]))).toBe(false)
    }
  })

  it('keeps topology and manual placement through retries, status changes and stable-ID restarts', () => {
    const data = input(); data.processes = [process(1)]; data.nodePlacements['bonsai:1'] = { x: 1000, y: 900, mode: 'manual' }
    const graph = buildCanvasGraph(data)
    const retryProcess: Process = { ...process(1), lifecycleStatus: 'backoff', revision: 2, retryCount: 1 }
    data.processes = [retryProcess]
    const retry = buildCanvasGraph(data)
    expect(retry.topologyKey).toBe(graph.topologyKey)
    const nodes = reconcileCanvasNodes(graph.nodes, retry.nodes, graph.nodes)
    expect(nodes.find(node => node.id === 'bonsai:1')?.position).toBe(graph.nodes.find(node => node.id === 'bonsai:1')?.position)
    for (const node of graph.nodes.filter(node => node.type !== 'process' && node.type !== 'project')) expect(nodes.find(next => next.id === node.id)).toBe(node)
    const restartedProcess: Process = { ...process(1), lifecycleStatus: 'starting', revision: 3 }
    data.processes = [restartedProcess]
    const restart = buildCanvasGraph(data)
    expect(restart.topologyKey).toBe(graph.topologyKey)
    expect(restart.nodes.filter(node => node.type === 'process')).toHaveLength(1)
    expect(restart.nodes.find(node => node.id === 'bonsai:1')?.position).toEqual({ x: 1000, y: 900 })
  })

  it('ignores transport metadata while observing lifecycle, association and diagnostic changes', () => {
    const state = { ...useBonsaiStore.getInitialState(), processes: [process(1)] }
    const canvas = projectCanvasProcessesSelector('bonsai'), node = processNodeSelector('bonsai:1')
    const graph = canvas(state), presentation = node(state)
    state.processes = [{ ...state.processes[0], pid: 42, revision: 3, retryCount: 2, attempt: 3 }]
    expect(canvas(state)).toBe(graph)
    expect(node(state)).toBe(presentation)
    state.processes = [{ ...state.processes[0], exitError: 'Unable to connect' }]
    expect(canvas(state)).toBe(graph)
    expect(node(state)?.exitError).toBe('Unable to connect')
    state.processes = [{ ...state.processes[0], lifecycleStatus: 'failed' }]
    const failed = canvas(state)
    expect(failed).not.toBe(graph)
    state.processes = [{ ...state.processes[0], worktreeId: 'wt-daemon' }]
    expect(canvas(state)).not.toBe(failed)
  })
})
