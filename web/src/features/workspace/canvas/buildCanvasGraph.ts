import type { Edge, Node } from '@xyflow/react'
import type { Agent, Health, NodePlacement, Project, Worktree, WorktreeTag } from '../../../types'
import type { WorktreeGroup } from '../../../api/git'
import type { BonsaiGraphData } from '../nodes/BonsaiNode'
import { connectionLabel } from '../connectionLabel'
import { getTagPresentation } from '../tagStyles'
import { groupCanvasWorktrees } from './worktreeGroups'

export interface CanvasGraphInput {
  project?: Project
  worktrees: Worktree[]
  agents: Agent[]
  tags: WorktreeTag[]
  worktreeGroups?: WorktreeGroup[]
  collapsedTagGroups: string[]
  detachedStackWorktreeIds: string[]
  expandedAutomaticGroups: string[]
  nodePlacements: Record<string, NodePlacement>
  envCount: number
}

function groupHealth(items: Worktree[]): Health {
  if (items.some((item) => item.status === 'error')) return 'error'
  if (items.some((item) => item.status === 'warning')) return 'warning'
  if (items.some((item) => item.status === 'healthy')) return 'healthy'
  return 'idle'
}

// Topology and status projection are pure; rendered geometry and selection are
// reconciled separately and never written back by a status-only update.
export function buildCanvasGraph(input: CanvasGraphInput) {
  const { project, worktrees, agents, tags, worktreeGroups, collapsedTagGroups, detachedStackWorktreeIds, expandedAutomaticGroups, nodePlacements, envCount } = input
  const positionFor = (id: string, fallback: { x: number; y: number }) => {
    const placement = nodePlacements[id]
    return placement ? { x: placement.x, y: placement.y } : fallback
  }
  if (!project) return { nodes: [] as Node[], edges: [] as Edge[], projectId: '', topologyKey: 'empty' }

  const allProjectWorktrees = worktrees.filter((worktree) => worktree.projectId === project.id)
  const projectWorktrees = allProjectWorktrees.filter(worktree => (!worktree.main && (worktree.main !== undefined || worktree.branch !== project.defaultBranch)) || agents.some(a => a.worktreeId === worktree.id && !a.archived))
  const projectWorktreeIds = new Set(projectWorktrees.map((worktree) => worktree.id))
  const projectAgents = agents.filter((agent) => projectWorktreeIds.has(agent.worktreeId) && (agent.presentation ?? (agent.archived ? 'archived' : 'canvas')) !== 'archived')
  const runningAgents = projectAgents.filter((agent) => agent.state === 'running').length
  const activePrs = projectWorktrees.filter((worktree) => worktree.prStatus === 'Open' || worktree.prStatus === 'Draft').length
  const failedChecks = projectWorktrees.reduce((total, worktree) => total + worktree.ciFailed, 0)
  const runningCi = projectWorktrees.filter((worktree) => worktree.ciStatus === 'running').length

  const groups = groupCanvasWorktrees(projectWorktrees, worktreeGroups ?? [])

  const visibleEntries: Array<
    | { type: 'stack'; id: string; tag: string; groupId?: string; items: Worktree[] }
    | { type: 'worktree'; id: string; worktree: Worktree; groupId?: string; items: Worktree[] }
  > = []
  const visibleNodeForWorktree = new Map<string, string>()

  groups.forEach(({ items, label: tag, groupId }) => {
    const stackable = items.filter(
      (item) => item.stackPreference !== 'never' && !detachedStackWorktreeIds.includes(item.id),
    )
    const separate = items.filter(
      (item) => item.stackPreference === 'never' || detachedStackWorktreeIds.includes(item.id),
    )
    const collapsed = stackable.length > 1 && (groupId ? !expandedAutomaticGroups.includes(groupId) : collapsedTagGroups.includes(project.id + ':' + tag))

    if (collapsed) {
      const stackId = groupId ? 'stack:' + groupId : 'stack:' + project.id + ':' + tag
      visibleEntries.push({ type: 'stack', id: stackId, tag, groupId, items: stackable })
      stackable.forEach((item) => visibleNodeForWorktree.set(item.id, stackId))
    } else {
      stackable.forEach((worktree) => {
        visibleEntries.push({ type: 'worktree', id: worktree.id, worktree, groupId, items })
        visibleNodeForWorktree.set(worktree.id, worktree.id)
      })
    }

    separate.forEach((worktree) => {
      visibleEntries.push({ type: 'worktree', id: worktree.id, worktree, groupId, items })
      visibleNodeForWorktree.set(worktree.id, worktree.id)
    })
  })

  const rootDefault = positionFor(project.id, { x: 420, y: 34 })
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
        envCount: envCount,
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
        position: positionFor(entry.id, fallbackPosition),
        data: {
          entityId: entry.id,
          kind: 'stack',
          title: entry.tag,
          groupId: entry.groupId,
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
            connectionLabel: connectionLabel(worktree.connection?.reason, worktree.headSha, worktree.connection?.statusUnknown),
            dirtyFiles: worktree.dirtyFiles,
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
      position: positionFor(worktree.id, fallbackPosition),
      data: {
        entityId: worktree.id,
        kind: 'worktree',
        title: worktree.branch,
        groupId: entry.groupId,
        connectionLabel: connectionLabel(worktree.connection?.reason, worktree.headSha, worktree.connection?.statusUnknown),
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
        position: positionFor(agent.id, { x: 58 + agentIndex * 192, y: 420 }),
        data: {
          entityId: agent.id,
          kind: 'agent',
          title: agent.name,
          agentState: agent.state,
          provider: agent.provider,
          model: agent.profileName ?? agent.model,
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
          projectId: project.id,
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
}
