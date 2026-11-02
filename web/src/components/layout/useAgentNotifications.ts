import { useEffect } from 'react'
import { detectAgentAlerts, seenActivity } from '../../lib/agentTransitions'
import { showAgentAlert } from '../../lib/browserNotifications'
import { useBonsaiStore } from '../../stores/bonsai'

const REMEMBERED_ALERTS = 200

/**
 * Raises a browser notification when an agent starts waiting for input or ends a turn while this tab is
 * hidden. It reads the store the snapshots already feed (no listener of its own), and it never alerts for
 * the state present when the shell mounts.
 */
export function useAgentNotifications() {
  useEffect(() => {
    let seen = seenActivity(useBonsaiStore.getState().agents)
    const raised = new Set<string>()
    return useBonsaiStore.subscribe((state, before) => {
      if (state.agents === before.agents) return
      const result = detectAgentAlerts(seen, state.agents)
      seen = result.seen
      if (document.visibilityState !== 'hidden') return
      for (const alert of result.alerts) {
        const key = `${alert.agentId}|${alert.kind}|${alert.since}`
        if (raised.has(key)) continue
        if (!showAgentAlert(alert, state.worktrees.find(tree => tree.id === alert.worktreeId)?.branch ?? '')) continue
        raised.add(key)
        if (raised.size > REMEMBERED_ALERTS) raised.delete(raised.values().next().value as string)
      }
    })
  }, [])
}
