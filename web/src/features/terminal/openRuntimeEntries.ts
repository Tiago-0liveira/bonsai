import { useBonsaiStore } from '../../stores/bonsai'
import { useProjectAgents, useProjectProcesses, useProjectWorktrees } from '../../stores/projectSelectors'
import type { Agent, Process } from '../../types'

export type RuntimeEntry =
  | { id: string; type: 'agent'; agent: Agent }
  | { id: string; type: 'process'; process: Process }

export function agentPresentation(agent: Agent) {
  return agent.presentation ?? (agent.archived ? 'archived' : 'canvas')
}

/** Runtimes that belong to the docked worktree, and the subset currently open as terminal cards. */
export function useOpenRuntimeEntries() {
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useProjectWorktrees(activeProjectId)
  const agents = useProjectAgents(activeProjectId)
  const processes = useProjectProcesses(activeProjectId)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const openRuntimeIds = useBonsaiStore((state) => state.openRuntimeIds)

  const projectWorktrees = worktrees.filter((item) => item.projectId === activeProjectId)
  const worktree = projectWorktrees.find((item) => item.id === dockWorktreeId)
  const worktreeAgents = agents.filter((item) => item.worktreeId === worktree?.id && agentPresentation(item) !== 'archived')
  const worktreeProcesses = processes.filter(item => worktree ? item.worktreeId === worktree.id
    : !dockWorktreeId && !projectWorktrees.some(tree => tree.id === item.worktreeId))
  const available: RuntimeEntry[] = [
    ...worktreeAgents.map((agent) => ({ id: agent.id, type: 'agent' as const, agent })),
    ...worktreeProcesses.map((process) => ({ id: process.id, type: 'process' as const, process })),
  ]
  const availableMap = new Map(available.map((runtime) => [runtime.id, runtime]))
  const openEntries = openRuntimeIds.map((id) => availableMap.get(id)).filter((item): item is RuntimeEntry => Boolean(item))
  return { worktree, available, openEntries }
}
