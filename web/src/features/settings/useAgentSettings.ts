import { useCallback, useEffect, useMemo, useState } from 'react'
import { isLiveAgent } from '../../api/agents'
import { isActiveSession } from '../../api/agentsOverview'
import { loadAgentSettings, saveAgentSettings, type AgentSettings, type AgentSettingsChange } from '../../api/settings'
import { useBonsaiStore } from '../../stores/bonsai'

// useAgentSettings reads GET /api/settings/agents on mount and when the window
// regains focus (the setting is also changed from other windows). An API
// without the endpoint leaves it undefined. save writes with the read
// revision; on a conflict it reloads and reports the error.
export function useAgentSettings() {
  const [settings, setSettings] = useState<AgentSettings>()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const load = useCallback(() => loadAgentSettings().then(setSettings, () => undefined), [])
  useEffect(() => {
    void load()
    window.addEventListener('focus', load)
    return () => window.removeEventListener('focus', load)
  }, [load])
  const save = useCallback(async (change: AgentSettingsChange) => {
    if (!settings) return
    setSaving(true)
    setError('')
    try {
      setSettings(await saveAgentSettings(settings, change))
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure))
      await load()
    } finally {
      setSaving(false)
    }
  }, [settings, load])
  return { settings, saving, error, save }
}

/** Agent sessions running now in every project, from the live snapshots. */
export function useRunningAgents() {
  const agents = useBonsaiStore(state => state.agents)
  return useMemo(() => agents.filter(agent => isLiveAgent(agent) && isActiveSession(agent.lifecycleState)).length, [agents])
}

export function willStopNotice(running: number) {
  return `${running} ${running === 1 ? 'agent is' : 'agents are'} running — ${running === 1 ? 'it' : 'they'} will be stopped when Bonsai web stops.`
}
