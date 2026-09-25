import type { Project } from '../types'

export const projects: Project[] = [
  {
    id: 'bonsai',
    name: 'bonsai',
    repository: 'Tiago-0liveira/bonsai',
    description: 'A fast workspace for managing worktrees, agents, processes and review flow without leaving the terminal.',
    health: 'healthy',
    worktreeIds: ['wt-main', 'wt-web', 'wt-daemon', 'wt-release'],
    openPrCount: 3,
  },
]
