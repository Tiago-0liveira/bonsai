export type Health = 'healthy' | 'warning' | 'error' | 'idle'
export type WorktreeKind = 'Production' | 'Feature' | 'Bug' | 'Refactor' | 'Chore'
export type WorktreeSourceType = 'existing' | 'origin' | 'new'
export type TagColor = 'purple' | 'blue' | 'green' | 'orange' | 'red' | 'cyan' | 'pink'
export type AgentState = 'running' | 'idle' | 'finished'
export type BoardStatus = 'todo' | 'progress' | 'done'
export type BoardKind = 'Idea' | 'Feature' | 'Bug' | 'Problem' | 'Task'
export type PrStatus = 'Draft' | 'Open' | 'Closed' | 'Merged'
export type CiStatus = 'running' | 'passed' | 'failed' | 'waiting'
export type DockPanelKey = 'files' | 'prs'

export interface Workspace {
  id: string
  name: string
  projectIds: string[]
}

export interface DefaultBranchInfo {
  commitSha: string
  commitMessage: string
  lastActivity: string
  releaseTag?: string
  prNumber?: number
  prTitle?: string
  ciStatus: CiStatus
}

export interface Project {
  id: string
  workspaceId: string
  name: string
  repository: string
  description: string
  health: Health
  defaultBranch: string
  defaultBranchInfo?: DefaultBranchInfo
  worktreeIds: string[]
  openPrCount: number
}

export interface WorktreeTag {
  id: string
  name: string
  color: TagColor
}

export interface Worktree {
  id: string
  projectId: string
  branch: string
  kind: WorktreeKind
  tag: string
  tagId?: string
  sourceType: WorktreeSourceType
  remoteBranch?: string
  mergeTargetBranch: string
  status: Health
  agentIds: string[]
  prNumber?: number
  prStatus?: PrStatus
  ciStatus: CiStatus
  ciFailed: number
  ahead: number
  behind: number
  dirtyFiles: number
  lastActivity: string
  gitState?: string
}

export interface CreateWorktreeInput {
  sourceType: WorktreeSourceType
  sourceRef: string
  branchName?: string
  tagId: string
  mergeTargetBranch: string
}

export interface EnvVariable {
  id: string
  key: string
  value: string
  secret: boolean
}

export interface Agent {
  id: string
  worktreeId: string
  name: string
  provider: 'Claude' | 'Codex' | 'Gemini'
  state: AgentState
  task: string
  runtime: string
  terminalId: string
}

export interface Process {
  id: string
  worktreeId: string
  name: string
  command: string
  status: Health
  port?: number
}

export interface PullRequest {
  id: string
  number: number
  title: string
  branch: string
  base: string
  status: 'Open' | 'Draft' | 'Merged' | 'Closed'
  checks: { name: string; status: 'success' | 'running' | 'failed' }[]
  commits: { sha: string; message: string; author: string }[]
  conversation: { author: string; body: string; time: string }[]
  files: { path: string; additions: number; deletions: number; diff: string[] }[]
}

export interface BoardItem {
  id: string
  title: string
  kind: BoardKind
  status: BoardStatus
  assignee: string
  priority: 'Low' | 'Medium' | 'High'
}

export interface RepoFile {
  id: string
  name: string
  path: string
  type: 'file' | 'folder'
  language?: string
  content?: string
  children?: RepoFile[]
}

export interface ActivityItem {
  id: string
  type: 'agent' | 'git' | 'pr' | 'process'
  title: string
  detail: string
  time: string
  status: Health
}

export type Selection =
  | { type: 'project'; id: string }
  | { type: 'worktree'; id: string }
  | { type: 'agent'; id: string }

export type DockState = 'collapsed' | 'normal' | 'maximized'
export type DockTab = 'agent' | 'terminal' | 'tests' | 'files' | 'pr' | 'checks' | 'logs'

export interface ViewportState {
  x: number
  y: number
  zoom: number
}
