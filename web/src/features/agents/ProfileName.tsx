import { Pencil } from 'lucide-react'
import { useRef, useState } from 'react'
import type { AgentAccount } from '../../api/agents'
import { updateAgentProfile } from '../../api/agentsOverview'
import { profileNameProblem } from './profileForm'

/**
 * The profile's name in the detail header, renamed in place: Enter saves,
 * Escape (or leaving the field) cancels. The API's own message is shown when it
 * refuses a name.
 */
export function ProfileName({ account, siblings, onRenamed }: {
  account: AgentAccount
  /** Other profiles of the same provider, whose names are taken. */
  siblings: AgentAccount[]
  onRenamed: (account: AgentAccount) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const busy = useRef(false)
  const taken = siblings.filter(item => item.id !== account.id).map(item => item.name)
  const problem = editing && draft !== account.name ? profileNameProblem(draft, taken) : ''

  const begin = () => { setDraft(account.name); setError(''); setEditing(true) }
  const cancel = () => { if (!busy.current) { setEditing(false); setError('') } }
  const commit = async () => {
    if (busy.current) return
    if (draft === account.name) { setEditing(false); return }
    if (profileNameProblem(draft, taken)) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      const updated = await updateAgentProfile(account.id, { name: draft })
      setEditing(false)
      onRenamed(updated)
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  if (!editing) {
    return (
      <span className="flex min-w-0 items-center gap-1">
        <h2 className="min-w-0 truncate text-[14px] font-semibold text-text">{account.name}</h2>
        <button type="button" aria-label="Rename profile" title="Rename profile" onClick={begin} className="bonsai-focus btn-ghost h-6 w-6 shrink-0 justify-center px-0"><Pencil size={12} /></button>
      </span>
    )
  }
  const shown = error || problem
  return (
    <span className="flex min-w-0 flex-col">
      <input
        autoFocus
        aria-label="Profile name"
        aria-invalid={shown ? true : undefined}
        maxLength={64}
        spellCheck={false}
        disabled={pending}
        value={draft}
        onChange={event => { setDraft(event.target.value); setError('') }}
        onKeyDown={event => {
          if (event.key === 'Enter') { event.preventDefault(); void commit() }
          else if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); cancel() }
        }}
        onBlur={cancel}
        className="bonsai-focus h-7 w-[220px] max-w-full rounded-[7px] border border-border bg-well px-2 text-[13px] font-semibold outline-none focus:border-accent/55 disabled:opacity-50"
      />
      {shown && <span role="alert" className="mt-1 max-w-[320px] text-[10px] text-danger">{shown}</span>}
    </span>
  )
}
