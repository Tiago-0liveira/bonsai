import type { Process } from '../types'

export const processes: Process[] = [
  { id: 'proc-web', worktreeId: 'wt-web', name: 'Vite', command: 'pnpm dev', status: 'healthy', port: 5173 },
  { id: 'proc-tests', worktreeId: 'wt-web', name: 'Vitest', command: 'pnpm test --watch', status: 'healthy' },
  { id: 'proc-daemon', worktreeId: 'wt-daemon', name: 'bonsaid', command: 'go run . daemon', status: 'warning' },
  { id: 'proc-release', worktreeId: 'wt-release', name: 'release dry-run', command: 'goreleaser release --snapshot', status: 'idle' },
]
