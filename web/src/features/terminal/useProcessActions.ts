import { useRef, useState } from 'react'
import type { Process } from '../../types'
import { restartProcess, stopProcess } from '../../api/processes'
import { useBonsaiStore } from '../../stores/bonsai'

export type ProcessActionTarget = Pick<Process, 'id' | 'projectId' | 'daemonId' | 'lifecycleStatus'>

export function useProcessActions(process: ProcessActionTarget | undefined) {
  const [pending, setPending] = useState<'stop' | 'restart' | ''>('')
  const [error, setError] = useState('')
  const busy = useRef(false)
  const setSelection = useBonsaiStore(state => state.setSelection)
  const canStop = !!process && !pending && !['done', 'failed', 'stopped', 'lost', 'stopping'].includes(process.lifecycleStatus)
  const canRestart = !!process && !pending

  const action = async (name: 'stop' | 'restart') => {
    if (!process || busy.current || (name === 'stop' && !canStop)) return
    busy.current = true; setPending(name); setError('')
    try {
      if (name === 'stop') await stopProcess(process.projectId, process.daemonId)
      else await restartProcess(process.projectId, process.daemonId)
    } catch (error) { setError(error instanceof Error ? error.message : String(error)) }
    finally { busy.current = false; setPending('') }
  }

  return {
    pending, error, canStop, canRestart,
    stop: () => { void action('stop') },
    restart: () => { void action('restart') },
    openOutput: () => { if (process) setSelection({ type: 'process', id: process.id }) },
    inspect: () => {
      if (process) useBonsaiStore.setState(state => state.selection.type === 'process' && state.selection.id === process.id
        ? state : { selection: { type: 'process' as const, id: process.id } })
    },
  }
}
