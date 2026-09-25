import type { Project, Workspace } from '../types'

export const workspaces: Workspace[] = [
  {
    id: 'personal',
    name: 'personal',
    projectIds: ['bonsai', 'sprout-lab'],
  },
  {
    id: 'client',
    name: 'client-work',
    projectIds: ['storefront', 'mobile-backend'],
  },
]

export const projects: Project[] = [
  {
    id: 'bonsai',
    workspaceId: 'personal',
    name: 'bonsai',
    repository: 'Tiago-0liveira/bonsai',
    description: 'A fast workspace for managing worktrees, agents, processes and review flow without leaving the terminal.',
    health: 'healthy',
    defaultBranch: 'main',
    worktreeIds: ['wt-main', 'wt-web', 'wt-docs', 'wt-daemon', 'wt-release', 'wt-review'],
    openPrCount: 4,
  },
  {
    id: 'sprout-lab',
    workspaceId: 'personal',
    name: 'sprout-lab',
    repository: 'Tiago-0liveira/sprout-lab',
    description: 'A compact sandbox project used to exercise workspace switching.',
    health: 'idle',
    defaultBranch: 'main',
    worktreeIds: [],
    openPrCount: 0,
  },
  {
    id: 'storefront',
    workspaceId: 'client',
    name: 'storefront',
    repository: 'acme/storefront',
    description: 'Client storefront workspace with mocked project state.',
    health: 'warning',
    defaultBranch: 'main',
    worktreeIds: [],
    openPrCount: 1,
  },
  {
    id: 'mobile-backend',
    workspaceId: 'client',
    name: 'mobile-backend',
    repository: 'acme/mobile-backend',
    description: 'Mock mobile service project.',
    health: 'healthy',
    defaultBranch: 'main',
    worktreeIds: [],
    openPrCount: 0,
  },
]
