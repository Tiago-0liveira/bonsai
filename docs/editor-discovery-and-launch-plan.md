# Editor discovery and worktree launch implementation plan

Build one Go editor service that the TUI and local web API both use. Discover
supported editor launchers on PATH asynchronously, reuse a user-level cache for
15 minutes, and persist the selected default independently of that cache.

The browser sends a worktree ID and an optional editor ID to Bonsai on the
user's computer. Bonsai resolves the worktree directory and launches the editor.
This fits the current hosted-web-plus-loopback-API architecture.

## Existing integration points

- `internal/ui/model.go`: `Init()` already batches startup commands with
  `tea.Batch`; editor discovery can run alongside worktree loading and indexing.
- `internal/ui/update.go` and `internal/core/exec/exec.go`: `e` currently uses
  `tea.ExecProcess` and `EditorCmd`. The command sets its working directory but
  does not explicitly pass the worktree directory as an editor argument.
- `internal/ui/overlays.go` and `internal/core/config/state.go`: existing editor
  precedence is personal command, repository `.bonsai.yaml`, then the environment
  and platform fallback. Preserve those settings during migration.
- `internal/server/localapi/server.go`: a loopback API already authenticates
  browser sessions and resolves project/worktree ownership before handling
  worktree routes. Editor launching belongs here.
- `web/src/stores/bonsai.ts` and
  `web/src/features/terminal/BottomWorkspace.tsx`: editor selection is currently
  a browser preference with fixed choices and an unavailable-operation notice.
- `internal/core/pkgmgr/cache.go` and `internal/core/procstore/lock*.go`: reuse
  the existing user-cache, atomic-write, and cross-process locking patterns.

## 1. Shared editor registry and PATH discovery

Add `internal/core/editors` with a registry, discovery service, cache,
preference store, and launch adapters. Keep UI components free of filesystem and
process side effects.

Each registry entry defines a stable ID, display name, candidate CLI names,
supported operating systems, desktop/terminal kind, directory argument builder,
and platform launch adapter. A discovered entry additionally has a resolved
executable and supported launch modes. Expose labels and capabilities to the
browser; keep executable paths and launch arguments backend-owned.

Start with VS Code/Insiders, Cursor, Zed, Sublime Text, common JetBrains
launchers, Neovim, Vim, and Helix. Verify every candidate command and folder-open
invocation against that editor's documentation before registering it. Add
Windsurf and Antigravity desktop launchers once their adapter contracts are
verified; distinguish desktop launchers from similarly named agent CLIs.

- Probe candidates with Go's `exec.LookPath`, without executing them. It follows
  host platform conventions, including Windows `PATHEXT`. Retain its rejection
  of current-directory lookup results. [Go executable lookup documentation](https://pkg.go.dev/os/exec#LookPath).
- Use a small bounded worker pool, initially four workers, to probe independent
  editors concurrently. Test aliases in deterministic order within each entry.
- Deduplicate aliases of the same editor; retain distinct products such as
  VS Code and VS Code Insiders. Sort results deterministically.
- Capture the discovery environment once per scan. Do not invoke a login shell,
  enumerate every application, run version commands, or walk the filesystem.
- Report an empty successful catalog separately from loading or a failed scan.
  Retain a previous successful catalog on unexpected discovery failures.

PATH discovery identifies registered launchers visible to Bonsai's environment.
Editors without a PATH launcher need setup instructions. For example, VS Code
on macOS requires installing its shell command. [VS Code CLI setup](https://code.visualstudio.com/docs/configure/command-line).

## 2. Fast startup and a 15-minute cache

Use two layers: an immutable in-memory catalog per service instance, backed by
a small JSON file shared by Bonsai processes belonging to the same OS user.
Use `os.UserCacheDir()` rather than putting discovered machine state in a
repository or browser storage. [Go user cache directory documentation](https://pkg.go.dev/os#UserCacheDir).

Proposed path:

```text
<UserCacheDir>/bonsai/editors/v1/<environment-fingerprint>.json
```

Store schema/registry version, environment fingerprint, successful check time,
and discovered entries. The fingerprint includes the ordered PATH, Windows
PATHEXT, operating system, and architecture. Different process environments
must not overwrite or reuse one another's catalogs. Store preferences elsewhere.

| Cache condition | Behavior |
| --- | --- |
| Valid and younger than 15 minutes | Publish it; skip scanning |
| Valid but expired | Publish it as stale; refresh in the background |
| Missing, corrupt, incompatible, or wrong environment | Show discovery pending; start a background scan |
| Cache write fails | Keep fresh results in memory; discovery still works |
| Explicit Refresh editors | Bypass TTL and queue one refresh |
| Cached executable disappears at launch | Reject that launch and request a refresh |

Cache empty successful results too. Consider future timestamps invalid so clock
changes cannot keep entries fresh indefinitely. TTL expiry should replace a
catalog only after a successful complete scan; a partial scan is not a fresh
negative result.

Coalesce refreshes within each process. Coordinate across processes with
`procstore.TryLock` per cache fingerprint, recheck freshness after acquiring the
lock, and write through a unique temporary file plus atomic replacement. A
contending process keeps serving its current catalog and retries cache loading
asynchronously; it never blocks UI startup on the other process's scan. If cache
storage/locking is unavailable, perform an uncached scan instead.

For the TUI, add a discovery command to `Model.Init()` and deliver catalog
updates as Tea messages. For web use, start the service alongside the existing
local API background services in `Run()`; API construction and listener startup
must not await discovery. This covers a new `bonsai serve` API process. When
`bonsai serve` reuses an existing healthy API, its running editor service already
owns freshness. Avoid separately scanning in every repository daemon.

Long-lived TUI/API services schedule the next refresh when the catalog expires;
stop timers/workers with their owning lifecycle. Ordinary renders and worktree
switches read memory, with no repeated PATH or cache-file checks. Version/help
commands do not need discovery.

Performance targets, to measure rather than assume: no discovery work on the
startup critical path, memory reads without filesystem I/O, warm-cache load
around 10 ms or less, and a typical local cold scan around 100 ms or less.
Filesystem calls may stall on network-mounted PATH entries; bound active
workers and keep the interface usable instead of promising a hard scan timeout.

## 3. Persist one default and define launch behavior

Add a dedicated user configuration file, for example:

```text
<UserConfigDir>/bonsai/editor-settings.json
{ "version": 1, "revision": 1, "default_editor_id": "cursor" }
```

Use a cross-process locked read-modify-write operation and atomic replacement.
Include a revision so competing web tabs/TUI saves can report a conflict rather
than overwrite a newer selection. Keeping this file separate avoids writing
the TUI's entire `state.json` from the web API and losing unrelated preferences.

Define default resolution explicitly:

1. An editor ID supplied for this launch only.
2. The shared saved default editor ID.
3. Existing TUI personal command, repository command, `$VISUAL`, `$EDITOR`, then
   the existing platform fallback.

The last step is **Automatic** in both clients. A missing saved editor remains
the saved choice with an unavailable state; prompt for another editor rather
than silently choosing a different application. Opening with a dropdown choice
does not change the default. **Set as default** is a separate action.

Automatic is resolved for the owning project because `.bonsai.yaml` can differ
between repositories. Keep the saved selection user-scoped and expose the
effective project default separately. A fallback that cannot open directories,
such as a file-only editor, is unavailable for this worktree action.

Leave existing personal commands intact as the Automatic fallback. Map known
legacy commands to a registry entry where possible while retaining their TUI
argument behavior. Unknown custom commands continue to work through the TUI's
existing path. Expose them to web launch only after a local, structured adapter
is configured; the web request cannot supply a command string or executable.

For the existing browser preference, import `vscode`, `cursor`, or `zed` once
only if no shared setting or legacy personal setting exists and the editor is
available. Resolve this conditional import under the preference-file lock.
Map legacy `system` to Automatic. A later browser reconnect must never overwrite
a saved shared choice. After migration, remove editor preference from workspace
persistence and keep the server result as live client state.

Build folder invocations with explicit executable and argv, setting the working
directory and passing the absolute worktree directory using each adapter's
folder-open syntax. VS Code and Zed document explicit folder arguments.
[VS Code CLI](https://code.visualstudio.com/docs/configure/command-line),
[Zed project opening](https://zed.dev/docs/getting-started).

| Editor/client | Launch behavior |
| --- | --- |
| Desktop editor from TUI | Start independently and keep the TUI responsive |
| Terminal editor from TUI | Use `tea.ExecProcess`; restore the TUI after exit |
| Desktop editor from web | Local API starts the native editor independently |
| Terminal editor from web | Local API starts it in an available native terminal |

Native terminal support requires its own small adapter registry. Probe supported
terminal launchers with the same background discovery; begin with verified
Linux terminal adapters, Windows Terminal, and macOS Terminal. Adapters own
working-directory and argv handling. When a platform requires a shell or
AppleScript boundary, use a fixed adapter and tested escaping, without evaluating
browser-supplied command text. Without a supported terminal, retain the detected
terminal editor in the dropdown as disabled with a useful reason.

Windows PATH entries may resolve to `.cmd`/`.bat` launchers. Add explicit
Windows adapters for registered products, preferring their native executable
when the shim identifies it reliably. Do not assume a batch launcher behaves
like an executable or route all launches through generic shell concatenation.
Go documents different argument parsing for batch files. [Go command argument handling](https://pkg.go.dev/os/exec#Command).

Detached launches use `Start()` and reap children asynchronously. Do not attach
their lifetime to the HTTP request context, wait for the editor window to close,
apply web `--wait` flags, or supervise editors as Bonsai project processes.
Success means the launch was started; a desktop window appearing is not
guaranteed by process creation. Preserve useful launch errors and refresh stale
availability without automatically launching a replacement editor.

## 4. Local API contract and synchronization

Add `internal/server/localapi/editors.go` and register these routes:

| Route | Purpose |
| --- | --- |
| `GET /api/editors` | Catalog, freshness, selected default, settings revision, and launch capabilities |
| `GET /api/projects/{projectId}/editor` | Effective project default, its source, and availability |
| `POST /api/editors/refresh` | Coalesced explicit discovery; return `202` |
| `PATCH /api/settings/editor` | Save or clear the shared default using the expected revision |
| `POST /api/worktrees/{id}/editor` | Open this worktree with the supplied editor ID or effective default |

Proposed launch body:

```json
{ "editor_id": "cursor" }
```

Omitting `editor_id` resolves the effective default. Resolve worktree ownership
through the existing request scoping and authoritative Git inventory, including
the main working copy. Recheck that the directory exists and is available before
starting the editor. Accept stable IDs rather than arbitrary paths or arguments.

Reuse existing host, origin, session, strict JSON, and request-size handling.
Revalidate the selected executable before launch. Return structured failures
such as `editor_unavailable`, `terminal_unavailable`, `worktree_unavailable`,
`settings_conflict`, and `editor_launch_failed`.

Use an `Idempotency-Key` for launch requests. A bounded, expiring request journal
coalesces concurrent identical requests and replays the result without opening
another window. Include worktree/editor selection in request identity, reject
key reuse with different input, and capture the resolved editor for the first
request. A retry that omitted the editor ID replays that original choice even
if the default has since changed. Document that an API restart or a crash at
the process-spawn boundary prevents an exactly-once guarantee. Do not retry an
ambiguous launch automatically with a new key.

Publish a user-scoped `editors_changed` event through the existing authenticated
event socket when discovery or preferences change. Add its payload to
`events_hub.go` and `LocalEvent`, and send current editor state during bootstrap.
Keep this independent of Git snapshots and canvas topology revisions.

The TUI/API also reread the small preference file on editor actions and on a
bounded background preference check so a default changed in either client
becomes visible in the other. Use a catalog/settings revision and backend epoch
to reject obsolete browser responses after reconnect or refresh. Keep a
monotonic editor-state sequence per API epoch distinct from the persisted
settings revision, which changes only when a preference is saved.

## 5. TUI integration

- Keep `e` as **Open in default editor** for the selected worktree.
- Add **Open in editor…** to the command palette, with a searchable detected
  editor picker for a one-time launch. Capture the selected worktree when the
  action starts.
- Replace the default-editor preference's free-text-only flow with detected
  choices, Automatic, Refresh editors, and access to the existing custom-command
  editor. Display the effective default and unavailable/loading states.
- Apply scan and preference results through Tea messages. Terminal-editor
  execution suspends the TUI; desktop launches do not.

Primary files: `internal/ui/model.go`, `cmds.go`, `update.go`, `overlays.go`,
`palette.go`, and `components/prefs/prefs.go`.

## 6. Web integration

Add `web/src/api/editors.ts`, a small editor state slice, and a reusable
`WorktreeEditorActions` component.

- Each worktree gets a split action: **Open in <default>** plus a dropdown of
  detected editors. If there is no launchable effective default, the main action
  opens the chooser. Selecting a first default saves it and resumes the captured
  worktree launch after successful persistence.
- Reuse it in the worktree inspector and node menus, expanded stack rows, and
  the main/default-branch worktree surface. Stacked worktrees must retain their
  individual actions.
- Show which entry is the default and offer **Set as default**. A one-time
  selection opens that editor without changing the saved preference.
- Add default selection and Refresh editors to Settings. Use detected results
  rather than the existing hard-coded editor list.
- Load catalog/settings on connection and reconnect, independently of Git
  bootstrap; then apply `editors_changed` events. Discovery and launch pending
  states should not block the workspace. Load the project's effective default
  when that project becomes active and invalidate it on relevant settings or
  repository-config changes.
- Capture worktree/project IDs for each operation, prevent duplicate clicks,
  and show launch errors next to the action. Disconnect disables launch controls.
- Keep editor discovery, defaults, and pending operations out of browser
  workspace persistence. Changes must not move nodes or reset the viewport.
- Replace the misleading fixed-choice dialog with shared editor selection.
  Keep file opening as a separate operation: this plan launches worktree folders;
  it must not turn an existing file action into a folder action silently.

Primary files: `web/src/types/index.ts`, `stores/bonsai.ts`,
`stores/workspacePersistence.ts`, `stores/preferenceBoundary.ts`,
`features/settings/SettingsPage.tsx`, `features/inspector/Inspector.tsx`,
`features/workspace/nodes/BonsaiNode.tsx`, and
`features/terminal/BottomWorkspace.tsx`.

## Delivery order and verification

1. Registry, concurrent PATH discovery, cache, and injected clock/lookup/storage
   dependencies. Verify fresh/stale/empty caches, environment differences,
   corruption, permission failures, concurrent refreshes, and cross-process locks.
2. Shared preference store, precedence, migration, and desktop launch adapters.
   Verify revision conflicts and preservation of existing TUI settings.
3. Terminal launch adapters and TUI discovery/picker/default integration. Verify
   TUI suspension/restoration and detached desktop behavior.
4. Local API routes, launch deduplication, editor events, and reconnect bootstrap.
   Verify auth failures, unknown IDs, deleted worktrees, disappearing launchers,
   settings changes from another process, and launch errors.
5. Web split actions, Settings, persistence migration, and browser flows. Verify
   every worktree surface, shared defaults, one-time choices, unavailable editors,
   project switching during requests, disconnect/reconnect, and canvas stability.
6. Document supported launchers, PATH setup, cache behavior, Automatic precedence,
   native-terminal requirements, and Windows/WSL/headless limitations.

Use temporary PATH directories, fake executables/launchers, an injected clock,
and an injected process starter. Include argv tests for spaces, Unicode, quotes,
and shell metacharacters. Exercise Windows shims and native-terminal boundaries
on their actual platforms; do not claim platform support from a Linux-only test.
Real-editor smoke tests should confirm the requested worktree opens in a desktop
session. Benchmarks measure warm-cache and cold-scan latency without brittle
timing assertions in unit tests.

Implementation checks: `go test ./...`, `go test -race ./...`, `go vet ./...`,
`pnpm --dir web typecheck`, `pnpm --dir web test`, `pnpm --dir web build`, and
the relevant Playwright settings/worktree suites plus a new editor-launch suite.
No product tests need to run for this planning-only change.

Acceptance: editor discovery never holds up startup; a successful catalog is
reused for 15 minutes and can be refreshed explicitly; both clients use the same
saved default; each web worktree opens through its own default/dropdown action;
TUI terminal editors remain interactive; unavailable launch modes explain what
is needed; discovery and editor actions do not alter worktree/canvas state.
