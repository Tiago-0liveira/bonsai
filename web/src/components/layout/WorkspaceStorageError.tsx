import { workspacePreferences, useBonsaiStore } from '../../stores/bonsai'
import { retryWorkspaceStorage, useWorkspaceStorageStatus } from '../../stores/workspacePersistence'

export function WorkspaceStorageError() {
  const error = useWorkspaceStorageStatus(state => state.error)
  if (!error) return null
  return (
    <div role="alert" className="flex shrink-0 items-center gap-3 border-b border-warn-solid/22 bg-warn-solid/8 px-3 py-2 text-[12px] text-warn">
      <span>{error}</span>
      <button type="button" onClick={() => { retryWorkspaceStorage(useBonsaiStore.getState()); workspacePreferences.flush() }} className="bonsai-focus shrink-0 underline">Retry saving preferences</button>
    </div>
  )
}
