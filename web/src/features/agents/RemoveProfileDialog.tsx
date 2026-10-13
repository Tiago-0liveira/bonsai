import { useRef, useState } from 'react'
import { stopAgent, type AgentAccount } from '../../api/agents'
import { AgentProfileError, removeAgentProfile } from '../../api/agentsOverview'
import { useBonsaiStore } from '../../stores/bonsai'
import { ProfileDialog } from './ProfileDialog'

/** After stopping sessions, the removal is retried this often while they wind down. */
const RETRY_DELAY_MS = 400
const RETRIES = 12
const wait = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms))

type Step =
  | { name: 'confirm' }
  | { name: 'blocked'; sessionIds: string[] }
  | { name: 'done'; warnings: string[] }

/**
 * Confirms removing a profile. The API refuses while any of its sessions is
 * active in any project; the dialog then lists them and offers to stop them
 * and remove the profile in one step.
 */
export function RemoveProfileDialog({ account, providerLabel, onClose, onRemoved }: {
  account: AgentAccount
  providerLabel: string
  onClose: () => void
  /** The profile is gone; its provider-side cleanup warnings, if any, were shown already. */
  onRemoved: () => void
}) {
  const [step, setStep] = useState<Step>({ name: 'confirm' })
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const agents = useBonsaiStore(state => state.agents)
  const worktrees = useBonsaiStore(state => state.worktrees)

  const describe = (id: string) => {
    const agent = agents.find(item => item.id === id)
    if (!agent) return { id, label: `Session ${id}`, projectId: '' }
    const projectId = agent.projectId ?? worktrees.find(item => item.id === agent.worktreeId)?.projectId ?? ''
    return { id, label: agent.name, projectId }
  }

  const finish = (warnings: string[]) => {
    onRemoved()
    if (warnings.length === 0) {
      useBonsaiStore.getState().setNotice(`Profile ${account.name} removed`)
      onClose()
      return
    }
    setStep({ name: 'done', warnings })
  }

  const remove = async (stopFirst: string[] = []) => {
    if (busy.current) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      if (stopFirst.length) {
        const targets = stopFirst.map(describe)
        const missing = targets.find(target => !target.projectId)
        if (missing) throw new Error(`Stop ${missing.label} from its project first.`)
        await Promise.all(targets.map(target => stopAgent(target.projectId, target.id, `stop-${target.id}`)))
      }
      // A stopped session leaves the active list a moment later.
      for (let attempt = 0; ; attempt++) {
        try {
          const { warnings } = await removeAgentProfile(account.id)
          finish(warnings ?? [])
          return
        } catch (failure) {
          const stillRunning = failure instanceof AgentProfileError && failure.code === 'profile_in_use'
          if (stillRunning && stopFirst.length && attempt < RETRIES) { await wait(RETRY_DELAY_MS); continue }
          if (stillRunning) {
            setStep({ name: 'blocked', sessionIds: failure.sessionIds })
            if (stopFirst.length) setError('The sessions are still stopping. Try again in a moment.')
            return
          }
          throw failure
        }
      }
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  if (step.name === 'done') {
    return (
      <ProfileDialog label="Profile removed" onClose={onClose}>
        <h2 className="text-[13px] font-semibold">Profile removed</h2>
        <p className="text-xs text-muted">{account.name} was removed, but its {providerLabel} cleanup reported:</p>
        <ul aria-label="Cleanup warnings" className="space-y-1.5 rounded-[10px] border border-warn-solid/30 bg-warn-solid/10 p-3">
          {step.warnings.map(warning => <li key={warning} className="break-words text-[11px] text-warn">{warning}</li>)}
        </ul>
        <div className="flex justify-end"><button type="button" autoFocus onClick={onClose} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px]">Close</button></div>
      </ProfileDialog>
    )
  }

  const blocked = step.name === 'blocked' ? step.sessionIds.map(describe) : []
  return (
    <ProfileDialog label="Remove profile" locked={pending} onClose={onClose}>
      <h2 className="text-[13px] font-semibold">Remove {account.name}?</h2>
      {step.name === 'confirm'
        ? (
          <p className="text-xs text-muted">
            This deletes the profile's {providerLabel} home folder: its conversation history, session data and settings, and the sign-in where it is stored there. Your repositories and worktrees are not touched. It cannot be undone.
          </p>
        )
        : (
          <>
            <p role="alert" className="text-xs text-warn">{blocked.length === 1 ? 'A session is' : `${blocked.length} sessions are`} still using this profile. Stop {blocked.length === 1 ? 'it' : 'them'} to remove the profile.</p>
            <ul aria-label="Running sessions" className="divide-y divide-border-subtle rounded-[10px] border border-border bg-well">
              {blocked.map(session => <li key={session.id} className="truncate px-3 py-2 text-[12px]">{session.label}</li>)}
            </ul>
          </>
        )}
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      <div className="flex justify-end gap-3">
        <button type="button" disabled={pending} autoFocus onClick={onClose} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">Cancel</button>
        {step.name === 'confirm'
          ? <button type="button" disabled={pending} onClick={() => void remove()} className="bonsai-focus btn-danger-tint h-8 rounded-[7px] px-3 text-[12px] disabled:opacity-40">{pending ? 'Removing…' : 'Remove profile'}</button>
          : <button type="button" disabled={pending || blocked.length === 0} onClick={() => void remove(step.sessionIds)} className="bonsai-focus btn-danger-tint h-8 rounded-[7px] px-3 text-[12px] disabled:opacity-40">{pending ? 'Stopping…' : `Stop ${blocked.length === 1 ? 'session' : `${blocked.length} sessions`} and remove`}</button>}
      </div>
    </ProfileDialog>
  )
}
