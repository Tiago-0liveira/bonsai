import { useBonsaiStore } from '../../stores/bonsai'

export function CanvasSettings() {
  const preferences = useBonsaiStore(state => state.canvasPreferences)
  const update = useBonsaiStore(state => state.setCanvasPreferences)

  return <section className="mx-auto w-full max-w-2xl space-y-3 border-b border-border-subtle p-6" aria-labelledby="canvas-settings-title">
    <h2 id="canvas-settings-title" className="text-[13px] font-semibold">Canvas</h2>
    <p className="text-sm text-muted">Choose what happens when you select a row in the Branches list. Focusing centres the node in view and keeps the zoom.</p>
    <label className="flex items-center gap-2 text-[12px]">
      <input type="checkbox" checked={preferences.focusOnBranchSelect} onChange={event => update({ focusOnBranchSelect: event.target.checked })} />
      Focus a worktree on the canvas when it is selected in Branches
    </label>
    <label className="flex items-center gap-2 text-[12px]">
      <input type="checkbox" checked={preferences.focusAgentsOnBranchSelect} onChange={event => update({ focusAgentsOnBranchSelect: event.target.checked })} />
      Focus an agent on the canvas when it is selected in Branches
    </label>
    <label className="flex items-center gap-3 text-[12px]">On the GitHub tab, selecting a branch
      <select aria-label="On the GitHub tab, selecting a branch" className="bonsai-focus h-8 rounded-[7px] border border-border bg-well px-2.5 text-[12px] focus:border-accent/55"
        value={preferences.githubBranchSelect}
        onChange={event => update({ githubBranchSelect: event.target.value === 'open-canvas' ? 'open-canvas' : 'show-pr' })}>
        <option value="show-pr">Shows its pull request</option>
        <option value="open-canvas">Switches to the canvas</option>
      </select>
    </label>
  </section>
}
