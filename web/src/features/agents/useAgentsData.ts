import { useCallback, useEffect, useRef, useState } from 'react'
import { agentProviders, type AgentAccount, type AgentCapability, type AgentUsage } from '../../api/agents'
import { agentAccountsIfChanged, agentUsage, refreshAgentUsage } from '../../api/agentsOverview'
import { subscribeAgentsChanged } from '../../api/localClient'

/** Profiles are revalidated this often while the page is visible. */
export const ACCOUNTS_INTERVAL_MS = 30_000
/** Usage is re-read this often while the page is visible; the API answers from its cache. */
export const USAGE_INTERVAL_MS = 60_000

const pageVisible = () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
const messageOf = (error: unknown) => error instanceof Error ? error.message : String(error)

export interface AgentsData {
  /** Undefined until the first answer. */
  providers?: AgentCapability[]
  accounts?: AgentAccount[]
  /** Last failure of the provider or profile list; cleared by the next success. */
  error: string
  /** Cached usage by profile ID. */
  usage: Record<string, AgentUsage>
  usageLoaded: boolean
  usageError: string
  /** Profile IDs with a refresh in flight; '' stands for all profiles. */
  refreshing: string[]
  refreshUsage: (accountId?: string) => Promise<void>
  /** Shows a profile the page itself changed, then revalidates the list without waiting for an event. */
  applyAccount: (account: AgentAccount) => void
  /** Drops a profile the page itself removed, then revalidates. */
  dropAccount: (accountId: string) => void
  /** Fetches everything again, for the retry buttons. */
  reload: () => void
}

/**
 * Providers, profiles and usage for the Agents page. Profiles revalidate on
 * focus, visibility, `agents_changed` and every 30 s while visible, with the
 * ETag of the last answer; usage loads after the first paint and polls every
 * 60 s while visible. Providers (which may run `--version`) revalidate only on
 * focus and visibility.
 */
export function useAgentsData(): AgentsData {
  const [providers, setProviders] = useState<AgentCapability[]>()
  const [accounts, setAccounts] = useState<AgentAccount[]>()
  const [providersError, setProvidersError] = useState('')
  const [accountsError, setAccountsError] = useState('')
  const [usage, setUsage] = useState<Record<string, AgentUsage>>({})
  const [usageLoaded, setUsageLoaded] = useState(false)
  const [usageError, setUsageError] = useState('')
  const [refreshing, setRefreshing] = useState<string[]>([])
  const alive = useRef(true)
  const etag = useRef<string | undefined>(undefined)
  const bodies = useRef({ accounts: '', providers: '' })
  const flight = useRef({ accounts: false, providers: false, usage: false, accountsAgain: false })
  const usageStarted = useRef(false)
  // A slower, older usage answer must not replace a newer one.
  const usageSeq = useRef({ issued: 0, applied: 0 })

  const loadProviders = useCallback(async () => {
    if (flight.current.providers) return
    flight.current.providers = true
    try {
      const next = await agentProviders()
      if (!alive.current) return
      const body = JSON.stringify(next)
      if (body !== bodies.current.providers) { bodies.current.providers = body; setProviders(next) }
      setProvidersError('')
    } catch (failure) {
      if (alive.current) setProvidersError(messageOf(failure))
    } finally {
      flight.current.providers = false
    }
  }, [])

  const loadUsage = useCallback(async () => {
    if (flight.current.usage) return
    flight.current.usage = true
    const seq = ++usageSeq.current.issued
    try {
      const items = await agentUsage()
      if (!alive.current) return
      if (seq > usageSeq.current.applied) {
        usageSeq.current.applied = seq
        setUsage(Object.fromEntries(items.map(item => [item.account_id, item])))
      }
      setUsageError('')
    } catch (failure) {
      if (alive.current) setUsageError(messageOf(failure))
    } finally {
      flight.current.usage = false
      if (alive.current) setUsageLoaded(true)
    }
  }, [])

  const loadAccounts = useCallback(async (): Promise<void> => {
    if (flight.current.accounts) return
    flight.current.accounts = true
    try {
      const next = await agentAccountsIfChanged(etag.current)
      if (!alive.current) return
      setAccountsError('')
      if (!next) return
      etag.current = next.etag
      const body = JSON.stringify(next.accounts)
      if (body === bodies.current.accounts) return
      bodies.current.accounts = body
      setAccounts(next.accounts)
      // A new or renamed profile needs its usage entry.
      if (usageStarted.current) void loadUsage()
    } catch (failure) {
      if (alive.current) setAccountsError(messageOf(failure))
    } finally {
      flight.current.accounts = false
      // A change made while a read was in flight may not be in that answer.
      if (flight.current.accountsAgain && alive.current) { flight.current.accountsAgain = false; void loadAccounts() }
    }
  }, [loadUsage])

  const revalidateAccounts = useCallback(() => {
    etag.current = undefined
    if (flight.current.accounts) flight.current.accountsAgain = true
    else void loadAccounts()
  }, [loadAccounts])

  const applyAccount = useCallback((account: AgentAccount) => {
    setAccounts(current => current?.map(item => item.id === account.id ? account : item))
    revalidateAccounts()
  }, [revalidateAccounts])

  const dropAccount = useCallback((accountId: string) => {
    setAccounts(current => current?.filter(item => item.id !== accountId))
    revalidateAccounts()
  }, [revalidateAccounts])

  const refreshUsage = useCallback(async (accountId?: string) => {
    const key = accountId ?? ''
    const seq = ++usageSeq.current.issued
    setRefreshing(current => [...current, key])
    try {
      const items = await refreshAgentUsage(accountId)
      if (!alive.current) return
      if (seq > usageSeq.current.applied) {
        usageSeq.current.applied = seq
        setUsage(current => ({ ...current, ...Object.fromEntries(items.map(item => [item.account_id, item])) }))
      }
      setUsageError('')
    } catch (failure) {
      if (alive.current) setUsageError(messageOf(failure))
    } finally {
      if (alive.current) setRefreshing(current => { const index = current.indexOf(key); return index < 0 ? current : current.filter((_, at) => at !== index) })
    }
  }, [])

  // Providers and profiles: first load, then the revalidation triggers.
  useEffect(() => {
    alive.current = true
    void loadProviders()
    void loadAccounts()
    const revalidate = () => {
      if (!pageVisible()) return
      void loadProviders()
      void loadAccounts()
      if (usageStarted.current) void loadUsage()
    }
    window.addEventListener('focus', revalidate)
    document.addEventListener('visibilitychange', revalidate)
    const unsubscribe = subscribeAgentsChanged(() => {
      void loadAccounts()
      if (usageStarted.current) void loadUsage()
    })
    const timer = setInterval(() => { if (pageVisible()) void loadAccounts() }, ACCOUNTS_INTERVAL_MS)
    return () => {
      alive.current = false
      window.removeEventListener('focus', revalidate)
      document.removeEventListener('visibilitychange', revalidate)
      unsubscribe()
      clearInterval(timer)
    }
  }, [loadAccounts, loadProviders, loadUsage])

  // Usage waits for the first paint of the list, then polls while visible.
  const ready = providers !== undefined && accounts !== undefined
  useEffect(() => {
    if (!ready) return
    usageStarted.current = true
    const first = setTimeout(() => void loadUsage(), 0)
    const timer = setInterval(() => { if (pageVisible()) void loadUsage() }, USAGE_INTERVAL_MS)
    return () => { clearTimeout(first); clearInterval(timer) }
  }, [ready, loadUsage])

  const reload = useCallback(() => {
    etag.current = undefined
    void loadProviders()
    void loadAccounts()
    void loadUsage()
  }, [loadAccounts, loadProviders, loadUsage])

  return { providers, accounts, error: providersError || accountsError, usage, usageLoaded, usageError, refreshing, refreshUsage, applyAccount, dropAccount, reload }
}
