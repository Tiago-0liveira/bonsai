import type { BoardItem } from '../types'

export const boardItems: BoardItem[] = [
  { id: 'b1', title: 'Canvas keyboard navigation', kind: 'Feature', status: 'todo', assignee: 'UI builder', priority: 'High' },
  { id: 'b2', title: 'Reduce node chrome at 75% zoom', kind: 'Problem', status: 'todo', assignee: 'Review pass', priority: 'Medium' },
  { id: 'b3', title: 'Agent restart confirmation', kind: 'Task', status: 'todo', assignee: 'Main watcher', priority: 'Low' },
  { id: 'b4', title: 'Persist inspector width', kind: 'Idea', status: 'todo', assignee: 'UI builder', priority: 'Low' },
  { id: 'b5', title: 'Responsive compact sidebar', kind: 'Feature', status: 'progress', assignee: 'UI builder', priority: 'High' },
  { id: 'b6', title: 'Terminal resize flicker', kind: 'Bug', status: 'progress', assignee: 'Test runner', priority: 'High' },
  { id: 'b7', title: 'Command palette action feedback', kind: 'Task', status: 'progress', assignee: 'Review pass', priority: 'Medium' },
  { id: 'b8', title: 'PR diff density pass', kind: 'Problem', status: 'progress', assignee: 'Review pass', priority: 'Medium' },
  { id: 'b9', title: 'Project node summary', kind: 'Feature', status: 'done', assignee: 'UI builder', priority: 'High' },
  { id: 'b10', title: 'ELK hierarchy layout', kind: 'Feature', status: 'done', assignee: 'UI builder', priority: 'High' },
  { id: 'b11', title: 'Mock activity timeline', kind: 'Task', status: 'done', assignee: 'Main watcher', priority: 'Low' },
  { id: 'b12', title: 'Fake terminal streams', kind: 'Feature', status: 'done', assignee: 'Test runner', priority: 'Medium' },
  { id: 'b13', title: 'Status token palette', kind: 'Idea', status: 'done', assignee: 'Review pass', priority: 'Low' },
  { id: 'b14', title: 'Node context menu', kind: 'Feature', status: 'progress', assignee: 'UI builder', priority: 'Medium' },
]
