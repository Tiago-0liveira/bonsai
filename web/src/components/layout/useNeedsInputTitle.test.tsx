import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { useBonsaiStore } from '../../stores/bonsai'
import type { Agent } from '../../types'
import { useNeedsInputTitle } from './useNeedsInputTitle'

function Probe() { useNeedsInputTitle(); return null }
const agent = (id: string, waiting: boolean) => ({ id, worktreeId: 'w', state: 'running', lifecycleState: 'running', activity: { state: waiting ? 'waiting' : 'idle', since: '2099-01-01T00:00:00Z', source: 'hook' } }) as Agent

beforeEach(() => { document.title = 'Bonsai'; useBonsaiStore.setState({ agents: [] }) })
afterEach(() => { cleanup(); useBonsaiStore.setState({ agents: [] }) })

it('prefixes the title with the number of waiting agents and restores it at zero', () => {
  render(<Probe />)
  expect(document.title).toBe('Bonsai')
  act(() => useBonsaiStore.setState({ agents: [agent('a', true), agent('b', false)] }))
  expect(document.title).toBe('(1) Bonsai')
  act(() => useBonsaiStore.setState({ agents: [agent('a', true), agent('b', true)] }))
  expect(document.title).toBe('(2) Bonsai')
  act(() => useBonsaiStore.setState({ agents: [agent('a', false), agent('b', false)] }))
  expect(document.title).toBe('Bonsai')
})

it('restores the title when the shell unmounts', () => {
  const { unmount } = render(<Probe />)
  act(() => useBonsaiStore.setState({ agents: [agent('a', true)] }))
  expect(document.title).toBe('(1) Bonsai')
  unmount()
  expect(document.title).toBe('Bonsai')
})
