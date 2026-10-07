# Process canvas nodes and terminal restoration

## Objective

Launching a process should immediately create a canvas node and open that
process's output. Reopening a project should restore the terminal views the user
left open, including an intentionally empty panel. Closing a view must never
cause another previously closed view to open.

All five implementation steps below are implemented. The findings describe the
working tree before implementation. The reported password-keeper session has
not been manually checked against a running app; automated process actions use
isolated temporary fixtures.

## Findings in the current code

1. `buildCanvasGraph.ts` accepts worktrees and agents, but no processes.
   `BonsaiCanvas.tsx` neither subscribes to project processes nor registers a
   process node renderer. Process launch already puts the returned summary in
   the store through `applyProcessSummary(summary, true)`; the graph cannot
   project it into a node.
2. `RuntimeWorkspace` in `BottomWorkspace.tsx` chooses the first available,
   non-dismissed runtime when no selected or open runtime exists, then opens it
   in an effect. Closing that runtime makes the next candidate eligible. This
   directly explains the sequence of views opening as previous ones close.
3. `workspacePersistence.ts` does not save open, selected, or dismissed runtime
   IDs. `preferenceBoundary.ts` does not observe those fields either. Dismissals
   therefore cannot survive a reload.
4. `setActiveProject` and `setActiveWorkspace` clear the open runtime IDs.
   Switching projects loses the previous view arrangement even within one
   browser session.
5. Canvas selection, inspection, ownership, subtree dragging, and shelf sizing
   currently understand agents rather than both agents and processes. The main
   worktree is omitted unless it has an unarchived agent, so simply adding a
   process renderer would still miss launches on the main worktree.
6. Existing terminal tests explicitly expect automatic opening of discovered
   processes. The intended behavior needs updated tests, not just a new guard
   around the current effect.

## Behavior contract

| Event | Canvas | Terminal panel |
| --- | --- | --- |
| Explicit successful launch | Add one node from the returned process summary | Open and focus that process once |
| Launch produces a retained failed process | Show the failed node and diagnostics | Open that process's output once |
| Validation or request failure with no created process | Do not invent a node | Keep the dialog error visible |
| Process discovered through snapshot or reconnect | Add/update its node | Do not automatically open its output |
| Close a terminal view | Keep the process node | Remove only that view; focus another already-open view in the same worktree, if any |
| Close the last view | Keep the process node | Stay empty across refreshes, project switches, and reloads |
| Explicit node/open-menu action | Keep/select the node | Open the requested output |
| Retry or manual restart | Update the same node | Keep the current open/closed preference |
| Reopen project or switch worktree | Restore its graph | Restore its saved view list, order, and active view |
| Confirmed runtime removal | Remove the missing entity | Remove its stale view reference without opening a closed replacement |

Closing a terminal view releases its browser attachment without stopping the
underlying process. Stop and restart remain separate explicit actions. Failed,
completed, and stopped process records remain inspectable while the backend
retains them.

## Implementation sequence

### 1. Replace terminal fallback opening with explicit actions

- Remove the effect that opens an arbitrary available runtime. The available
  runtime list supplies the Open menu; the saved open list supplies rendered
  terminal cards.
- Keep launch, Open-menu selection, and explicit runtime-node actions as the
  routes that open a terminal. Project/worktree selection and incoming entity
  updates must not create that intent.
- Put open, close, focus, and reorder transitions in store actions, with one
  consistent implementation for agents and processes. Separate focusing an
  already-open view from opening a new one.
- When the active card closes, select the next already-open card from the same
  project/worktree, respecting its order. When none remain, clear the active ID
  and show an empty panel with an Open control.
- Make these transitions idempotent under repeated events and React StrictMode.
  Once closed-by-default is enforced, the dismissed-ID list is unnecessary for
  choosing what to open; remove its role in fallback selection.

Primary files: `web/src/features/terminal/BottomWorkspace.tsx`,
`web/src/stores/bonsai.ts`, `web/src/api/processes.ts`, and agent launch/open
callers.

### 2. Persist terminal view preferences per project and worktree

- Add a versioned preference map keyed by project ID and worktree ID, containing
  ordered open runtime references and the active runtime reference. Store runtime
  kind and stable public ID; do not use PID, command text, or list position as
  identity. Existing process summaries use `projectID:daemonID`.
- Remember the last selected worktree per project so returning to a project
  returns to the intended panel. Treat an empty open list as a valid preference.
- Update project/workspace/worktree switches to restore these preferences,
  rather than clearing the global open list. Keep one source of truth; if legacy
  dock fields remain for compatibility, derive them from the active scope.
- Add only the view metadata to the storage allowlist and preference writer.
  Preserve the existing exclusion of runtime entities, prompts, environment
  values, terminal output, and connection/session objects.
- Increment the storage version and migrate existing records. Old records cannot
  recover already-discarded terminal preferences; default to an empty panel.
  Existing layout and other user preferences must survive migration.
- Explicit open/close/reorder actions save immediately. Snapshot, status, and
  output traffic must not repeatedly serialize preferences.

Primary files: `web/src/stores/workspacePersistence.ts`,
`web/src/stores/preferenceBoundary.ts`, `web/src/stores/bonsai.ts`.

### 3. Restore after authoritative data is available

- Hydrate preferences first but retain unresolved saved references while a
  project's initial runtime/worktree data is loading. An empty initial store is
  not proof that a runtime was removed.
- Resolve saved references against canonical entities for that project and
  worktree. Attach output only for valid saved open views; choose an active card
  only from that saved list.
- Prune stale references only after the relevant project/component has supplied
  an authoritative successful snapshot. A different project's snapshot,
  unavailable connection, or failed refresh must not erase saved preferences.
- Reconcile worktree removal and project removal explicitly. If the saved
  worktree no longer exists, choose a valid worktree without opening new views.
- Preserve process revision ordering and the existing pending-snapshot launch
  protection. An older snapshot must not erase a just-launched process or
  overwrite a newer lifecycle status.
- Keep stream cleanup on close/unmount and replay on explicit reopen. Avoid
  attaching output streams merely because a process node exists.

Primary files: `web/src/api/git.ts`,
`web/src/api/snapshotReconciliation.ts`, terminal preference reconciliation, and
`web/src/features/terminal/ProcessTerminal.tsx`.

### 4. Add process nodes throughout the canvas

- Feed `useProjectProcesses` into `CanvasGraphInput`, register `ProcessNode`, and
  add process identity/status fields to graph data. Add `process` to selection,
  canvas selection reconciliation, node-click handling, and the inspector.
- Render one node per retained process with command/name and lifecycle status.
  Provide explicit Open output, Stop, and Restart actions with the existing API
  pending/error handling. Status changes update the existing node.
- Include the main worktree when it owns a process, even without an agent. Attach
  processes to their worktree; when that worktree is represented by a collapsed
  stack, attach to the visible stack while retaining the real worktree ID.
- For legacy records with no resolvable worktree, use a project-owned process
  shelf with an association label. Do not silently omit the record or assign it
  to an unrelated branch.
- Include process IDs and ownership in topology detection. Add process counts to
  relevant worktree/stack summaries so grouped runtimes remain discoverable.
- Generalize agent shelf placement and ownership to runtime children where
  needed: geometry, local placement, global layout, collisions, and subtree
  dragging must include processes. Preserve existing manually placed nodes and
  avoid global relayout on lifecycle/status changes.
- Persist process node placements independently of whether their terminal views
  are open. Adapt the sanitizer so legitimate runtime placement references are
  not discarded just because a view is saved as open.

Primary files: `web/src/features/workspace/canvas/buildCanvasGraph.ts`,
`BonsaiCanvas.tsx`, `graphReconciliation.ts`, `canvas/layout/*`,
`web/src/features/workspace/nodes/BonsaiNode.tsx`,
`web/src/features/inspector/Inspector.tsx`, `web/src/types/index.ts`.

### 5. Make launch integration consistent

- Use the returned process summary to upsert the process and open its output in
  one store transition. Select the new process node. Do not wait for a periodic
  snapshot to create its graph representation.
- Ensure launch into a collapsed group makes the new node discoverable without
  rearranging unrelated branches. Reveal the new node with a targeted viewport
  adjustment only when necessary.
- Do not let a delayed launch response switch the user back to a project they
  left while the request was pending. Save the created process and its open-view
  intent under the original scope, and surface completion without stealing the
  current scope.
- Treat terminal attachment failure separately from process launch failure: keep
  the created node and show a reconnectable output error. Stream preparation
  must not make an already-created process appear unlaunched.
- Manual restart uses the same stable process ID and node. It must not reopen a
  closed terminal or reset a manually positioned node.

Primary files: `web/src/features/workspace/StartProcessDialog.tsx`,
`web/src/api/processes.ts`, `web/src/api/processStream.ts`, and store actions.

## Verification and completion criteria

1. Add store/component regressions for three available runtimes with zero saved
   open views; closing the only open view while two others are available; explicit
   reopening; and scoped active-card fallback. All must remain stable under
   StrictMode, repeated snapshots, and lifecycle updates.
2. Add persistence tests for empty/open lists, ordering, active view, project and
   worktree switching, delayed hydration, authoritative stale-ID cleanup, legacy
   migration, and storage failure. Retain tests that prove sensitive/runtime data
   never reaches browser storage.
3. Add graph/layout regressions for launch on feature/main worktrees, mixed
   agent/process children, collapsed stacks, unresolved worktree association,
   process selection, no overlaps, subtree dragging, and stable manual placement
   through retries and status updates.
4. Extend the native full-stack process test to assert a node immediately after
   POST, exactly one node after snapshot/reconnect/restart, output replay, and
   independent behavior for multiple processes. Verify closing a view leaves its
   process running and its node present.
5. Add a full-stack restoration scenario using an isolated fixture: open several
   views, close all, switch projects, reload/reconnect, and confirm none reopen.
   Repeat with a saved subset across multiple worktrees and verify only that
   subset and its order return. Use the fixture for automation; validate the
   reported password-keeper project manually without launching/stopping its
   processes as part of routine automated tests.
6. Run focused Vitest tests, web typecheck/lint/build, and relevant Playwright
   full-stack suites. Run Go checks if implementation changes a server contract.

Deliver steps 1–3 first to stop the reopening loop and protect view preferences,
then steps 4–5 for complete canvas/launch behavior. Both parts are required to
finish the reported task. No new daemon or terminal protocol is expected unless
the restoration tests expose an authoritative-data or identity gap.

## Implementation verification

- All 220 Vitest tests pass, including scoped view actions, hydration, migration,
  unavailable worktrees, storage failures, process selection, and mixed layouts.
- Web typecheck, lint, and production build pass.
- Twenty-four relevant Playwright scenarios pass across process/agent native
  fixtures, restoration, execution controls, layout, storage migration,
  worktree management, workspace lifecycle, state sync, smoke, and render
  isolation. PID-only updates produce zero graph computations, layout
  computations, main-workspace commits, preference writes, and API requests.
- Focused Go tests pass for local API, daemon server, package manager, and
  process store; repository-wide `go vet ./...` passes.
- Restoration uses the existing snapshot freshness and process identity
  contracts. No daemon or terminal protocol changes were needed.
- Manual validation of the reported password-keeper session remains outstanding.
