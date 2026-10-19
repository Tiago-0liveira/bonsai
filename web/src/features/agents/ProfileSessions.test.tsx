import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { Agent } from '../../types'
import type { ProfileSession } from './agentsModel'
import { ProfileSessions } from './ProfileSessions'

const session = (id: string, patch: Partial<Agent>): ProfileSession => ({
  agent: { id, name: id, worktreeId: 'w', provider: 'Claude', providerId: 'claude', model: '', reasoningEffort: '', workType: '', prompt: '', archived: false, presentation: 'canvas', state: 'running', lifecycleState: 'running', task: 'running', runtime: '', terminalId: id, createdAt: '2099-01-01T00:00:00Z', ...patch },
  projectId: 'bonsai', project: 'bonsai', branch: 'main', active: patch.lifecycleState !== 'exited',
})

afterEach(cleanup)

it('shows each session\'s status and keeps stop available for a session that waits or idles', () => {
  const waiting = { state: 'waiting' as const, reason: 'permission' as const, detail: 'Bash', since: '2099-01-01T00:00:00Z', source: 'hook' as const }
  render(<ProfileSessions fields={[]} now={Date.now()} onShowOnCanvas={() => undefined} sessions={[
    session('blocked', { activity: waiting }),
    session('resting', { activity: { ...waiting, state: 'idle', reason: undefined, detail: undefined } }),
    session('plain', {}),
    session('done', { lifecycleState: 'exited', state: 'finished', activity: waiting }),
  ]} />)
  expect(screen.getByRole('img', { name: 'Needs permission: Bash' })).toHaveTextContent('Needs input (permission)')
  expect(screen.getByRole('img', { name: 'Idle' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Running' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Exited' })).toBeInTheDocument()
  for (const name of ['blocked', 'resting', 'plain']) expect(screen.getByRole('button', { name: `Stop ${name}` })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Stop done' })).toBeDisabled()
})
