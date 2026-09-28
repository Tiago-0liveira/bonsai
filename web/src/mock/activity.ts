import type { ActivityItem } from '../types'

export const activity: ActivityItem[] = [
  { id: 'a1', type: 'agent', title: 'UI builder is running', detail: 'Building the workspace canvas', time: 'now', status: 'healthy' },
  { id: 'a3', type: 'process', title: 'Vite dev server restarted', detail: 'Listening on :5173', time: '11m', status: 'healthy' },
  { id: 'a5', type: 'agent', title: 'Log inspector finished', detail: 'Mapped stale marker cleanup path', time: '29m', status: 'idle' },
  { id: 'a6', type: 'process', title: 'Daemon test failed', detail: 'shutdown marker persisted after interrupt', time: '36m', status: 'warning' },
]
