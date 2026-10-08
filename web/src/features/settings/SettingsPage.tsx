import { ProjectRootsSettings } from './ProjectRootsSettings'
import { useBonsaiStore } from '../../stores/bonsai'
export function SettingsPage() {
  const projectId = useBonsaiStore(state => state.activeProjectId)
  const project = useBonsaiStore(state => state.projects.find(p => p.id === projectId))
  const reopening = useBonsaiStore(state => state.terminalViewPreferences[projectId]?.reopening ?? 'keep_closed')
  return <div className="island flex h-full min-h-0 flex-col overflow-hidden">
    <div className="island-title shrink-0">Settings</div>
    <div className="min-h-0 flex-1 overflow-auto">
    {project && <section className="space-y-3 border-b border-border-subtle p-6">
      <h2 className="text-[13px] font-semibold">{project.name} terminals</h2>
      <label className="flex items-center gap-3 text-[12px]">When opening this project:
        <select aria-label="When opening this project" className="bonsai-focus h-8 rounded-[7px] border border-border bg-well px-2.5 text-[12px] focus:border-accent/55" value={reopening} onChange={event => {
          const value = event.target.value === 'restore' ? 'restore' : 'keep_closed'
          useBonsaiStore.setState(state => ({ terminalViewPreferences: { ...state.terminalViewPreferences,
            [projectId]: { ...(state.terminalViewPreferences[projectId] ?? { lastWorktreeId: state.dockWorktreeId, worktrees: {} }), reopening: value },
          } }))
        }}>
          <option value="keep_closed">Keep terminals closed (recommended)</option>
          <option value="restore">Restore last open views</option>
        </select>
      </label>
    </section>}
    <ProjectRootsSettings />
    </div>
  </div>
}
