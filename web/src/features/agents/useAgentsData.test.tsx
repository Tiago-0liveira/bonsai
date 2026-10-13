import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentProviders, type AgentAccount, type AgentCapability, type AgentUsage } from '../../api/agents'
import { agentAccountsIfChanged, agentUsage, refreshAgentUsage } from '../../api/agentsOverview'
import { ACCOUNTS_INTERVAL_MS, USAGE_INTERVAL_MS, useAgentsData } from './useAgentsData'

const events = vi.hoisted(() => ({ emit: undefined as undefined | ((revision: number) => void), unsubscribed: 0 }))
vi.mock('../../api/localClient', async importOriginal => ({
  ...await importOriginal<object>(),
  subscribeAgentsChanged: vi.fn((listener: (revision: number) => void) => { events.emit = listener; return () => { events.unsubscribed++ } }),
}))
vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentProviders: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({
  ...await importOriginal<object>(),
  agentAccountsIfChanged: vi.fn(), agentUsage: vi.fn(), refreshAgentUsage: vi.fn(),
}))

const providers: AgentCapability[] = [{ id: 'codex', label: 'Codex', available: true }]
const accounts: AgentAccount[] = [{ id: 'a', name: 'A', provider: 'codex' }]
const usageOf = (id: string, score: number, rest: Partial<AgentUsage> = {}): AgentUsage => ({ account_id: id, provider: 'codex', headline: { score }, ...rest })
// Each hop (providers, profiles, then the first-paint usage timer) needs its own turn: React
// flushes effects when act exits, and a zero-delay timer set during a tick fires 1 ms later.
const flush = async (ms = 0) => {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
  for (let turn = 0; turn < 3; turn++) await act(async () => { await vi.advanceTimersByTimeAsync(1) })
}
const setVisibility = (state: 'visible' | 'hidden') => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
  document.dispatchEvent(new Event('visibilitychange'))
}

beforeEach(() => {
  vi.useFakeTimers()
  events.emit = undefined
  events.unsubscribed = 0
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  vi.mocked(agentProviders).mockResolvedValue(providers)
  vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts, etag: '"v1"' })
  vi.mocked(agentUsage).mockResolvedValue([usageOf('a', 0.7)])
})
afterEach(() => { cleanup(); vi.useRealTimers(); vi.clearAllMocks() })

describe('useAgentsData', () => {
  it('loads providers and profiles, then usage after them', async () => {
    const { result } = renderHook(() => useAgentsData())
    expect(result.current.providers).toBeUndefined()
    expect(agentUsage).not.toHaveBeenCalled()
    await flush()
    expect(result.current.providers).toEqual(providers)
    expect(result.current.accounts).toEqual(accounts)
    expect(result.current.usage.a.headline?.score).toBe(0.7)
    expect(result.current.usageLoaded).toBe(true)
    expect(vi.mocked(agentUsage).mock.invocationCallOrder[0]).toBeGreaterThan(vi.mocked(agentAccountsIfChanged).mock.invocationCallOrder[0])
  })

  it('revalidates profiles every 30 s with the last ETag and keeps the list when nothing changed', async () => {
    const { result } = renderHook(() => useAgentsData())
    await flush()
    const first = result.current.accounts
    vi.mocked(agentAccountsIfChanged).mockResolvedValue(undefined)
    await flush(ACCOUNTS_INTERVAL_MS)
    expect(agentAccountsIfChanged).toHaveBeenLastCalledWith('"v1"')
    expect(result.current.accounts).toBe(first)
    // An identical body without an ETag (cross-origin) is not a change either.
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: structuredClone(accounts) })
    await flush(ACCOUNTS_INTERVAL_MS)
    expect(result.current.accounts).toBe(first)
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [...accounts, { id: 'b', name: 'B', provider: 'codex' }], etag: '"v2"' })
    await flush(ACCOUNTS_INTERVAL_MS)
    expect(result.current.accounts).toHaveLength(2)
  })

  it('polls usage every 60 s while visible and nothing while hidden', async () => {
    renderHook(() => useAgentsData())
    await flush()
    const usageCalls = vi.mocked(agentUsage).mock.calls.length
    const accountCalls = vi.mocked(agentAccountsIfChanged).mock.calls.length
    await flush(USAGE_INTERVAL_MS)
    expect(vi.mocked(agentUsage).mock.calls.length).toBeGreaterThan(usageCalls)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    const hiddenUsage = vi.mocked(agentUsage).mock.calls.length
    const hiddenAccounts = vi.mocked(agentAccountsIfChanged).mock.calls.length
    await flush(USAGE_INTERVAL_MS * 2)
    expect(agentUsage).toHaveBeenCalledTimes(hiddenUsage)
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(hiddenAccounts)
    expect(accountCalls).toBeLessThanOrEqual(hiddenAccounts)
  })

  it('revalidates everything when the page becomes visible or focused', async () => {
    renderHook(() => useAgentsData())
    await flush()
    const before = { providers: vi.mocked(agentProviders).mock.calls.length, accounts: vi.mocked(agentAccountsIfChanged).mock.calls.length, usage: vi.mocked(agentUsage).mock.calls.length }
    act(() => setVisibility('hidden'))
    await flush()
    expect(agentProviders).toHaveBeenCalledTimes(before.providers)
    act(() => setVisibility('visible'))
    await flush()
    expect(agentProviders).toHaveBeenCalledTimes(before.providers + 1)
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(before.accounts + 1)
    expect(agentUsage).toHaveBeenCalledTimes(before.usage + 1)
    act(() => { window.dispatchEvent(new Event('focus')) })
    await flush()
    expect(agentProviders).toHaveBeenCalledTimes(before.providers + 2)
  })

  it('revalidates profiles and usage on agents_changed, but not providers', async () => {
    renderHook(() => useAgentsData())
    await flush()
    const before = { providers: vi.mocked(agentProviders).mock.calls.length, accounts: vi.mocked(agentAccountsIfChanged).mock.calls.length, usage: vi.mocked(agentUsage).mock.calls.length }
    act(() => events.emit?.(4))
    await flush()
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(before.accounts + 1)
    expect(agentUsage).toHaveBeenCalledTimes(before.usage + 1)
    expect(agentProviders).toHaveBeenCalledTimes(before.providers)
  })

  it('shares one request between triggers that arrive while it is pending', async () => {
    let release!: (value: { accounts: AgentAccount[] }) => void
    vi.mocked(agentAccountsIfChanged).mockReturnValue(new Promise(resolve => { release = resolve }))
    renderHook(() => useAgentsData())
    await flush()
    act(() => { events.emit?.(1); events.emit?.(2); window.dispatchEvent(new Event('focus')) })
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(1)
    await act(async () => release({ accounts }))
  })

  it('merges a refreshed profile into the cached usage and tracks it while pending', async () => {
    vi.mocked(agentUsage).mockResolvedValue([usageOf('a', 0.7), usageOf('b', 0.5)])
    const { result } = renderHook(() => useAgentsData())
    await flush()
    let release!: (items: AgentUsage[]) => void
    vi.mocked(refreshAgentUsage).mockReturnValue(new Promise(resolve => { release = resolve }))
    let done!: Promise<void>
    act(() => { done = result.current.refreshUsage('a') })
    expect(result.current.refreshing).toEqual(['a'])
    await act(async () => { release([usageOf('a', 0.2, { throttled: true })]); await done })
    expect(refreshAgentUsage).toHaveBeenCalledWith('a')
    expect(result.current.refreshing).toEqual([])
    expect(result.current.usage.a).toMatchObject({ throttled: true, headline: { score: 0.2 } })
    expect(result.current.usage.b.headline?.score).toBe(0.5)
  })

  it('does not let an older cached read replace a newer refresh', async () => {
    const { result } = renderHook(() => useAgentsData())
    await flush()
    let slow!: (items: AgentUsage[]) => void
    vi.mocked(agentUsage).mockReturnValueOnce(new Promise(resolve => { slow = resolve }))
    act(() => { events.emit?.(1) })
    vi.mocked(refreshAgentUsage).mockResolvedValue([usageOf('a', 0.9)])
    await act(async () => { await result.current.refreshUsage('a') })
    expect(result.current.usage.a.headline?.score).toBe(0.9)
    await act(async () => { slow([usageOf('a', 0.1)]) })
    expect(result.current.usage.a.headline?.score).toBe(0.9)
  })

  it('reports a failed first load and recovers on reload', async () => {
    vi.mocked(agentProviders).mockRejectedValueOnce(new Error('Update Bonsai to enable agent profiles.'))
    const { result } = renderHook(() => useAgentsData())
    await flush()
    expect(result.current.error).toBe('Update Bonsai to enable agent profiles.')
    act(() => result.current.reload())
    await flush()
    expect(result.current.error).toBe('')
    expect(result.current.providers).toEqual(providers)
  })

  it('keeps the loaded data and reports a usage failure separately', async () => {
    vi.mocked(agentUsage).mockRejectedValue(new Error('Usage service down'))
    const { result } = renderHook(() => useAgentsData())
    await flush()
    expect(result.current.accounts).toEqual(accounts)
    expect(result.current.usageError).toBe('Usage service down')
    expect(result.current.usageLoaded).toBe(true)
  })

  it('stops listening and polling when unmounted', async () => {
    const { unmount } = renderHook(() => useAgentsData())
    await flush()
    unmount()
    expect(events.unsubscribed).toBe(1)
    const calls = vi.mocked(agentAccountsIfChanged).mock.calls.length
    await flush(ACCOUNTS_INTERVAL_MS * 3)
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(calls)
  })

  it('shows a profile the page changed at once, then revalidates without the old ETag', async () => {
    const { result } = renderHook(() => useAgentsData())
    await flush()
    vi.mocked(agentAccountsIfChanged).mockClear()
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [{ id: 'a', name: 'Renamed', provider: 'codex' }], etag: '"v2"' })
    act(() => result.current.applyAccount({ id: 'a', name: 'Renamed', provider: 'codex', defaults: { model: 'm' } }))
    expect(result.current.accounts?.[0]).toMatchObject({ name: 'Renamed', defaults: { model: 'm' } })
    await flush()
    expect(agentAccountsIfChanged).toHaveBeenCalledWith(undefined)
    expect(result.current.accounts).toEqual([{ id: 'a', name: 'Renamed', provider: 'codex' }])
  })

  it('drops a removed profile and reads again when a read was already in flight', async () => {
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [...accounts, { id: 'b', name: 'B', provider: 'codex' }], etag: '"v1"' })
    const { result } = renderHook(() => useAgentsData())
    await flush()
    let release: (value: { accounts: AgentAccount[]; etag: string }) => void = () => {}
    vi.mocked(agentAccountsIfChanged).mockClear()
    vi.mocked(agentAccountsIfChanged).mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
    // An interval read starts and is still waiting when the page removes a profile.
    await flush(ACCOUNTS_INTERVAL_MS)
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(1)
    act(() => result.current.dropAccount('b'))
    expect(result.current.accounts?.map(item => item.id)).toEqual(['a'])
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts, etag: '"v3"' })
    // The in-flight answer predates the removal; the follow-up read replaces it.
    await act(async () => { release({ accounts: [...accounts, { id: 'b', name: 'B', provider: 'codex' }], etag: '"v2"' }) })
    await flush()
    expect(agentAccountsIfChanged).toHaveBeenCalledTimes(2)
    expect(result.current.accounts?.map(item => item.id)).toEqual(['a'])
  })
})
