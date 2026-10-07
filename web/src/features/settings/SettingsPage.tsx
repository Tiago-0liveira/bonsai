import { ProjectRootsSettings } from './ProjectRootsSettings'
import { useBonsaiStore } from '../../stores/bonsai'
export function SettingsPage() {
  const projectId = useBonsaiStore(state => state.activeProjectId)
  const project = useBonsaiStore(state => state.projects.find(p => p.id === projectId))
  const reopening = useBonsaiStore(state => state.terminalViewPreferences[projectId]?.reopening ?? 'keep_closed')
  return <div className="h-full overflow-auto">
    {project && <section className="space-y-3 border-b border-[rgb(var(--border))] p-6">
      <h2 className="text-sm font-semibold">{project.name} terminals</h2>
      <label className="flex items-center gap-3 text-xs">When opening this project:
        <select aria-label="When opening this project" className="bonsai-focus rounded bg-[rgb(var(--panel-2))] p-2" value={reopening} onChange={event => {
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
}
