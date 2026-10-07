import { useRef, useState } from 'react'
import type { Process } from '../../types'
import { removeProcess, restartProcess, stopProcess } from '../../api/processes'
import { useBonsaiStore } from '../../stores/bonsai'

export type ProcessActionTarget = Pick<Process, 'id' | 'projectId' | 'daemonId' | 'lifecycleStatus'>

export function useProcessActions(process: ProcessActionTarget | undefined) {
  const [pending, setPending] = useState<'stop' | 'restart' | 'remove' | ''>('')
  const [error, setError] = useState('')
  const busy = useRef(false)
  const setSelection = useBonsaiStore(state => state.setSelection)
  const canStop = !!process && !pending && !['done', 'failed', 'stopped', 'lost', 'stopping'].includes(process.lifecycleStatus)
  const canRestart = !!process && !pending

  const action = async (name: 'stop' | 'restart' | 'remove') => {
    if (!process || busy.current || (name === 'stop' && !canStop)) return
    busy.current = true; setPending(name); setError('')
    try {
      if (name === 'stop') await stopProcess(process.projectId, process.daemonId)
      else if (name === 'restart') await restartProcess(process.projectId, process.daemonId)
      else await removeProcess(process.projectId, process.daemonId, !['done', 'failed', 'stopped', 'lost'].includes(process.lifecycleStatus))
    } catch (error) { setError(error instanceof Error ? error.message : String(error)) }
    finally { busy.current = false; setPending('') }
  }

  return {
    pending, error, canStop, canRestart,
    deleteLabel: process && !['done', 'failed', 'stopped', 'lost'].includes(process.lifecycleStatus) ? 'Stop and delete process' : 'Delete process',
    remove: () => {
      if (!process || busy.current) return
      const state = useBonsaiStore.getState()
      const record = state.processes.find(p => p.id === process.id)
      const tree = state.worktrees.find(t => t.id === record?.worktreeId)
      const active = !['done', 'failed', 'stopped', 'lost'].includes(process.lifecycleStatus)
      if (window.confirm(`${active ? 'Stop and delete' : 'Delete'} process \"${record?.command || record?.name || '#' + process.daemonId}\" in ${tree?.branch || 'unassigned worktree'}?\nThis permanently deletes this execution and its retained logs.`)) void action('remove')
    },
    stop: () => { void action('stop') },
    restart: () => { void action('restart') },
    openOutput: () => { if (process) setSelection({ type: 'process', id: process.id }) },
    inspect: () => {
      if (process) useBonsaiStore.setState(state => state.selection.type === 'process' && state.selection.id === process.id
        ? state : { selection: { type: 'process' as const, id: process.id } })
    },
  }
}
