# FIXES — review fixes roadmap

Condensed from the approved review-fixes plan. Design rule throughout: **tea.Cmd closures
capture only immutable values (strings, ints, maps built inside the cmd); never model state.**
Model-dependent logic runs in the handler when the msg arrives (pattern:
`commitPreviewMsg` / `onCommitPreview`).

## Context

A full code review found three classes of problems:

1. The UI event loop is blocked by synchronous `git`/`gh` subprocesses in `Update` handlers
   (bulk-prune scan runs a network `gh` call — the TUI freezes for seconds).
2. `git worktree remove --force` is unconditional, so uncommitted work can be silently
   destroyed when the cached dirty-state is stale.
3. No subprocess has a timeout, so a hung `gh`/`git fetch` blocks a tea.Cmd goroutine forever.

Plus a real bug (TUI alias resolution contradicts the CLI and the documented shadowing
behavior) and four cheap correctness fixes.

## Steps (each compiles and tests green on its own)

1. **`gh.Checks` parses JSON once** — `parseChecks` returns `([]Check, error)`; drop the
   second unmarshal.
2. **Alias shadowing fix** — shared `config.ResolveAlias` (config then state, last match
   wins); CLI and TUI both delegate to it; TUI uses a locked alias copy.
3. **`help`/`shell-init` work outside a repo** — handled before the `git.MainRoot` gate in
   `cli.Run`.
4. **Subprocess timeouts** — git 120s (`runRaw` via `exec.CommandContext`), gh 30s (new
   `runGHOutput`/`runGHCombined` helpers); interactive commands stay untimed.
5. **Safe worktree removal** — `RemoveWorktree` drops `--force`, returns `ErrWorktreeDirty`
   when git refuses; explicit `ForceRemoveWorktree` for the opt-in path. Bulk prune records
   the first error, skips failed targets, continues with the rest.
6. **Async UI** — branch lists (rebase + create-existing) load via `branchesMsg`; bulk-prune
   scan runs in a tea.Cmd and arrives as `pruneCandidatesMsg`; filtering happens in handlers
   against live model state.
7. ~~**Parallelize `loadInspector`**~~ — N/A: no inspector feature in this
   codebase version.
8. **Force toggle in prune modal** — `f` toggles `force (discard uncommitted changes)`;
   bulk prune gets no force path (dirty targets stay skipped).

Step 4 lands before 5 so the new removal code already runs under the timeout.

## Verification

After each step: `go build ./... && go vet ./... && go test ./...`.

End-to-end manual smoke (run `./bonsai` in a repo with worktrees):

1. Startup renders; no freeze when pressing `X` (bulk prune) — status shows "scanning…" then
   the confirm modal appears.
2. `r` (rebase) and `n` → Existing branch open their modals without freezing.
3. Prune a **dirty** worktree → clear error in the status bar, worktree intact; with the
   force toggle it removes.
4. Bulk prune with one dirty target: others removed, dirty skipped, `bulk prune (n/total)`.
5. Add a shadowing alias in-app (`p` → new alias same name as config alias) → running it
   executes the user command.
6. Outside a repo: `bonsai help` and `bonsai shell-init` print normally.
7. Dead remote (optional): fetch/PR loads fail with timeout errors instead of hanging.
