import type { BoardItem, BoardList, BoardPriority, BoardType } from '../types'

export const boardLists: BoardList[] = [
  { id: 'feat', name: 'feat', color: 'purple', priority: 'High', itemType: 'Feature', order: 0 },
  { id: 'bug', name: 'bug', color: 'red', priority: 'High', itemType: 'Bug', order: 1 },
  { id: 'chore', name: 'chore', color: 'blue', priority: 'Medium', itemType: 'Task', order: 2 },
  { id: 'review-code', name: 'review-code', color: 'orange', priority: 'Medium', itemType: 'Review', order: 3 },
  { id: 'docs', name: 'docs', color: 'cyan', priority: 'Low', itemType: 'Documentation', order: 4 },
]

export const boardPriorities: BoardPriority[] = [
  { id: 'high', name: 'High', rank: 0 },
  { id: 'medium', name: 'Medium', rank: 1 },
  { id: 'low', name: 'Low', rank: 2 },
]

export const boardTypes: BoardType[] = [
  { id: 'feature', name: 'Feature' },
  { id: 'bug', name: 'Bug' },
  { id: 'task', name: 'Task' },
  { id: 'review', name: 'Review' },
  { id: 'docs', name: 'Documentation' },
  { id: 'idea', name: 'Idea' },
]

export const boardItems: BoardItem[] = [
  { id: 'b1', title: 'Canvas keyboard navigation', kind: 'Feature', status: 'feat', assignee: 'UI builder', priority: 'High' },
  { id: 'b2', title: 'Reduce node chrome at 75% zoom', kind: 'Review', status: 'review-code', assignee: 'Review pass', priority: 'Medium' },
  { id: 'b3', title: 'Agent restart confirmation', kind: 'Task', status: 'chore', assignee: 'Main watcher', priority: 'Low' },
  { id: 'b4', title: 'Terminal resize flicker', kind: 'Bug', status: 'bug', assignee: 'Test runner', priority: 'High' },
  { id: 'b5', title: 'Document branch hierarchy', kind: 'Documentation', status: 'docs', assignee: 'UI builder', priority: 'Medium' },
]
