import type { ActivityItem } from '../types'

export const activity: ActivityItem[] = [
  { id: 'a1', type: 'agent', title: 'UI builder is running', detail: 'Building the workspace canvas', time: 'now', status: 'healthy' },
  { id: 'a2', type: 'git', title: 'feat/web-workspace advanced by 2 commits', detail: '94d7bd2 → 10c8c2e', time: '7m', status: 'healthy' },
  { id: 'a3', type: 'process', title: 'Vite dev server restarted', detail: 'Listening on :5173', time: '11m', status: 'healthy' },
  { id: 'a4', type: 'pr', title: 'PR #24 checks updated', detail: '2 passed · 1 running', time: '14m', status: 'healthy' },
  { id: 'a5', type: 'agent', title: 'Log inspector finished', detail: 'Mapped stale marker cleanup path', time: '29m', status: 'idle' },
  { id: 'a6', type: 'process', title: 'Daemon test failed', detail: 'shutdown marker persisted after interrupt', time: '36m', status: 'warning' },
  { id: 'a7', type: 'pr', title: 'PR #23 moved to draft', detail: 'Waiting on deterministic Windows assertion', time: '1h', status: 'warning' },
  { id: 'a8', type: 'git', title: 'main synced', detail: 'No local changes', time: '2h', status: 'healthy' },
]
