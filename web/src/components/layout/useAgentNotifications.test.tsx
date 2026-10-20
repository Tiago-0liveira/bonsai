import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { reloadBrowserNotificationPrefs, setBrowserNotificationPrefs } from '../../lib/browserNotifications'
import { useBonsaiStore } from '../../stores/bonsai'
import type { ActivityState, Agent, Worktree } from '../../types'
import { useAgentNotifications } from './useAgentNotifications'

function Probe() { useAgentNotifications(); return null }
const agent = (id: string, state: ActivityState, since = '2099-01-01T00:00:00Z') => ({
  id, name: `session ${id}`, worktreeId: 'w1', state: 'running', lifecycleState: 'running', prompt: 'secret prompt', task: 'rm -rf /',
  activity: { state, since, source: 'hook', detail: 'Bash' },
}) as Agent
const shown: { title: string; options?: NotificationOptions }[] = []
let visibility: DocumentVisibilityState

beforeEach(() => {
  shown.length = 0
  visibility = 'hidden'
  localStorage.clear()
  reloadBrowserNotificationPrefs()
  setBrowserNotificationPrefs({ enabled: true })
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility)
  vi.stubGlobal('Notification', Object.assign(function (this: object, title: string, options?: NotificationOptions) { shown.push({ title, options }) }, { permission: 'granted' }))
  useBonsaiStore.setState({ agents: [], worktrees: [{ id: 'w1', branch: 'feature/login' } as Worktree] })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); useBonsaiStore.setState({ agents: [], worktrees: [] }) })

it('stays quiet for the state present at mount and for agents that appear later', () => {
  useBonsaiStore.setState({ agents: [agent('a', 'waiting')] })
  render(<Probe />)
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting'), agent('b', 'waiting')] }))
  expect(shown).toEqual([])
})

it('notifies a hidden tab once per observed transition, with names and status only', () => {
  useBonsaiStore.setState({ agents: [agent('a', 'working')] })
  render(<Probe />)
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'working', '2099-01-01T00:01:00Z')] }))
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'idle', '2099-01-01T00:02:00Z')] }))
  expect(shown.map(item => item.title)).toEqual(['Needs input', 'Turn complete'])
  expect(shown[0].options?.body).toBe('session a · feature/login')
  expect(JSON.stringify(shown)).not.toMatch(/secret prompt|rm -rf|Bash/)
})

it('does not notify while the tab is visible, nor later for that transition', () => {
  visibility = 'visible'
  useBonsaiStore.setState({ agents: [agent('a', 'working')] })
  render(<Probe />)
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  visibility = 'hidden'
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  expect(shown).toEqual([])
})

it('does not notify when the viewer has not enabled browser notifications', () => {
  setBrowserNotificationPrefs({ enabled: false })
  useBonsaiStore.setState({ agents: [agent('a', 'working')] })
  render(<Probe />)
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  expect(shown).toEqual([])
})

it('stops listening when the shell unmounts', () => {
  useBonsaiStore.setState({ agents: [agent('a', 'working')] })
  const { unmount } = render(<Probe />)
  unmount()
  act(() => useBonsaiStore.setState({ agents: [agent('a', 'waiting')] }))
  expect(shown).toEqual([])
})
