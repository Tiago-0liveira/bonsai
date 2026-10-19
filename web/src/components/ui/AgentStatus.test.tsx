import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { AgentStatusSource } from '../../lib/agentStatus'
import { AgentStatusBadge, AgentStatusDot, NeedsInputChip } from './AgentStatus'

afterEach(cleanup)
const waiting: AgentStatusSource = { state: 'running', lifecycleState: 'running', activity: { state: 'waiting', reason: 'permission', detail: 'Bash', since: '2099-01-01T00:00:00Z', source: 'hook' } }

it('names a waiting session in text and to assistive technology, not by color', () => {
  render(<AgentStatusBadge agent={waiting} />)
  const badge = screen.getByRole('img', { name: 'Needs permission: Bash' })
  expect(badge).toHaveTextContent('Needs input (permission)')
  expect(badge).toHaveAttribute('data-agent-status', 'waiting')
  expect(badge.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
})

it('shows the dot with an accessible name and a hollow dashed ring when the status is inferred', () => {
  const { rerender } = render(<AgentStatusDot agent={{ state: 'running', lifecycleState: 'running' }} />)
  expect(screen.getByRole('img', { name: 'Running' })).not.toHaveAttribute('data-inferred')
  rerender(<AgentStatusDot agent={{ ...waiting, activity: { ...waiting.activity!, inferred: true } }} />)
  const dot = screen.getByRole('img', { name: 'Needs permission: Bash (inferred)' })
  expect(dot).toHaveAttribute('data-inferred', 'true')
  expect(dot.className).toContain('border-dashed')
})

it('stops the pulse for users who prefer reduced motion and keeps lifecycle statuses plain', () => {
  render(<AgentStatusDot agent={{ state: 'finished', lifecycleState: 'failed' }} />)
  const dot = screen.getByRole('img', { name: 'Failed' })
  expect(dot.className).not.toContain('animate-pulse')
  cleanup()
  render(<AgentStatusDot agent={waiting} />)
  expect(screen.getByRole('img').className).toContain('motion-reduce:animate-none')
})

it('renders the needs-input chip with a count label only when someone waits', () => {
  const { rerender, container } = render(<NeedsInputChip count={0} />)
  expect(container).toBeEmptyDOMElement()
  rerender(<NeedsInputChip count={1} />)
  expect(screen.getByRole('img', { name: '1 agent needs input' })).toHaveTextContent('1')
  rerender(<NeedsInputChip count={3} />)
  expect(screen.getByRole('img', { name: '3 agents need input' })).toBeInTheDocument()
})
