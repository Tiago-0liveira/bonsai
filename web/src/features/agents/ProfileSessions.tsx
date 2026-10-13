import { LocateFixed, Square, TerminalSquare } from 'lucide-react'
import { useState } from 'react'
import type { LaunchField } from '../../api/agents'
import { launchDetails } from '../../api/launchDetails'
import { useBonsaiStore } from '../../stores/bonsai'
import { formatAge, type ProfileSession } from './agentsModel'

const PAGE = 25
const STATE_LABEL: Record<string, string> = { starting: 'Starting', running: 'Running', stopping: 'Stopping', exited: 'Exited', failed: 'Failed' }

export function ProfileSessions({ sessions, fields, now, onShowOnCanvas }: {
  sessions: ProfileSession[]
  /** The provider's launch fields, to label each session's launch details. */
  fields: LaunchField[]
  now: number
  /** Called after the agent is selected in its project, to bring the canvas into view. */
  onShowOnCanvas: () => void
}) {
  const [limit, setLimit] = useState(PAGE)
  const openTerminal = useBonsaiStore(state => state.openTerminal)
  const setAgentState = useBonsaiStore(state => state.setAgentState)
  const setSelection = useBonsaiStore(state => state.setSelection)
  const setActiveProject = useBonsaiStore(state => state.setActiveProject)
  if (sessions.length === 0) return <p className="p-4 text-[12px] text-muted">No sessions yet. Start an agent with this profile to see it here.</p>

  // Runtimes open in the active project's dock, so enter the session's project first.
  const enterProject = ({ projectId }: ProfileSession) => {
    if (projectId && projectId !== useBonsaiStore.getState().activeProjectId) setActiveProject(projectId)
  }
  const openSessionTerminal = (session: ProfileSession) => {
    enterProject(session)
    openTerminal(session.agent.id)
  }
  const showOnCanvas = (session: ProfileSession) => {
    const { agent } = session
    enterProject(session)
    setSelection({ type: 'agent', id: agent.id })
    useBonsaiStore.setState(state => ({ canvasReveal: { projectId: state.activeProjectId, nodeId: agent.id, nonce: state.canvasReveal.nonce + 1 } }))
    onShowOnCanvas()
  }

  return (
    <div className="p-2">
      <ul aria-label="Sessions" className="divide-y divide-border-subtle">
        {sessions.slice(0, limit).map(session => {
          const { agent } = session
          const state = agent.lifecycleState ?? 'running'
          // The mapped task holds the session's error, or its state when there is none.
          const failure = agent.task && agent.task !== state ? agent.task : ''
          const details = launchDetails(fields, agent.model, agent.launchOptions).join(' · ')
          return (
            <li key={agent.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-2.5">
              <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + (session.active ? 'bg-accent-solid' : state === 'failed' ? 'bg-danger-solid' : 'bg-muted-2')} aria-hidden="true" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-[12px]">
                  <span className="min-w-0 truncate font-medium text-text">{agent.name}</span>
                  <span className="shrink-0 rounded-[5px] bg-panel-4 px-1.5 font-mono text-[9.5px] text-muted">{STATE_LABEL[state] ?? state}</span>
                </div>
                <div className="truncate font-mono text-[10px] text-muted-2">
                  {[session.project, session.branch].filter(Boolean).join(' · ')}
                  {agent.createdAt && ` · started ${formatAge(agent.createdAt, now)}`}
                  {agent.finishedAt && ` · ended ${formatAge(agent.finishedAt, now)}`}
                  {agent.exitCode !== undefined && !session.active && ` · exit ${agent.exitCode}`}
                </div>
                {details && <div className="truncate font-mono text-[10px] text-muted" title={details}>{details}</div>}
                {failure && <div className="mt-0.5 truncate text-[11px] text-danger" title={failure}>{failure}</div>}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <button type="button" disabled={!session.active} aria-label={`Open terminal for ${agent.name}`} title="Open terminal" onClick={() => openSessionTerminal(session)} className="bonsai-focus btn-ghost h-7 w-7 justify-center px-0 disabled:opacity-40"><TerminalSquare size={13} /></button>
                <button type="button" disabled={!session.active || state === 'stopping'} aria-label={`Stop ${agent.name}`} title="Stop agent" onClick={() => setAgentState(agent.id, 'finished')} className="bonsai-focus btn-ghost h-7 w-7 justify-center px-0 disabled:opacity-40"><Square size={13} /></button>
                <button type="button" aria-label={`Show ${agent.name} on canvas`} title="Show on canvas" onClick={() => showOnCanvas(session)} className="bonsai-focus btn-ghost h-7 w-7 justify-center px-0"><LocateFixed size={13} /></button>
              </div>
            </li>
          )
        })}
      </ul>
      {sessions.length > limit && (
        <button type="button" onClick={() => setLimit(value => value + PAGE)} className="bonsai-focus btn-ghost mx-2 mt-2 h-7 text-[11px]">Show {Math.min(PAGE, sessions.length - limit)} more</button>
      )}
    </div>
  )
}
