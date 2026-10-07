import type { Process } from '../types'

export const processes: Process[] = [
  { id: 'proc-web', projectId: 'bonsai', daemonId: 1, worktreeId: 'wt-web', name: 'Vite', command: 'pnpm dev', status: 'healthy', lifecycleStatus: 'running', port: 5173 },
  { id: 'proc-tests', projectId: 'bonsai', daemonId: 2, worktreeId: 'wt-web', name: 'Vitest', command: 'pnpm test --watch', status: 'healthy', lifecycleStatus: 'running' },
  { id: 'proc-daemon', projectId: 'bonsai', daemonId: 3, worktreeId: 'wt-daemon', name: 'bonsaid', command: 'go run . daemon', status: 'warning', lifecycleStatus: 'backoff' },
  { id: 'proc-release', projectId: 'bonsai', daemonId: 4, worktreeId: 'wt-release', name: 'release dry-run', command: 'goreleaser release --snapshot', status: 'idle', lifecycleStatus: 'done' },
]
