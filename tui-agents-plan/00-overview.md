# TUI Agents, part 2: connect the TUI (A2 → D6)

Continuation of `~/.claude/plans/tui-agents/` (part 1). Part 1 delivered contracts and building blocks; nothing a user can launch, attach or manage yet. This plan finishes the feature.
Track files (self-contained, one Claude session each):

| File | Phases | Depends on | Touches |
| --- | --- | --- | --- |
| `A-sessions.md` | A2 record watch + web adoption, A3 real backend | merged A1 | `internal/core/agenthost`, `internal/server/localapi/agents*.go`, `internal/agentruntime`, new `internal/agentsbackend`, `docs/agents.md` |
| `B-vterm-input.md` | B2 key encoding, replies, `Input()` | merged B1 | `internal/ui/components/vterm/**` only |
| `C-accounts-ui.md` | C2 launch form + accounts modal, C3 quick usage | merged C1 | new `internal/ui/components/launchform/**`, `internal/ui/components/accounts/**` |
| `D-integration.md` | D3 wire + live read-only view, D4 terminal mode + `Ctrl+]`, D5 launch/accounts/usage/stop, D6 polish + docs | A3, B2, C2, C3 | `internal/ui/*.go`, `main.go`, `README.md`, `docs/tui-cli.md`, `docs/agents.md` |

## Where we are (verified 2026-10-16, `feat/tui-agents` @ 1beaf2b, clean tree)

Merged into `feat/tui-agents`:

| PR | Phase | Landed |
| --- | --- | --- |
| #103 | P0 | `internal/ui/agentsapi` contracts (`Sessions`, `Stream`, `Accounts`, `Backend`) + `agentsapi/fake`; `theme/agents.go` glyphs `✳ ◎ ✦`, ASCII pref |
| #106 | A1 | `agenthost.Starter` (`start.go`, flock `admit.lock`), `Summary.Origin`, `Manager.Start` delegates, `Manager.Close(false)` stops only `Origin web/""` (`manager.go:332-360`) |
| #104 | B1 | `components/vterm` (x/vt v0.1.0): `New, Write, SetViewport, ResizeEmulator, SetFocused, SetReplaying, Title, AltScreen, View, Close`; drain goroutine discards emulator replies (`vterm.go:62-73`, comment "B2 routes these bytes") |
| #105 | C1 | `internal/core/agents/usagefmt` (Bar, Percent, Duration, Headline, Compact, Rank) |
| #107 | D1 | `ui.WithAgents` option (`agents.go:22`), session index, `Changes()` subscription, worktree glyphs |
| #108 | D2 | Agents tab: strip, header, usage row, compact layouts, picker (`a`), selection memory, `placeholderArea` where the terminal goes (`agentsview.go:138`); `n m u` print "agents: coming soon" (`agentstab.go:203`, `palette.go:288`) |

Gaps that explain what you saw while testing:
- `main.go:197` still calls `ui.New(repoDir, cfg, state)` without `WithAgents`, so the committed build shows **no** Agents tab. A tab appeared only with an uncommitted, unreviewed `internal/tuiagents` backend + `main.go` wiring that was in this worktree and is no longer on disk. A3 rebuilds the backend from this plan.
- D2's tab deviates from the design in small ways (no `AGENTS · <branch>` title row, no rule between strip and header, sectioned picker without footer). D3 step 2b fixes them.

## UI source of truth

`~/bonsai-feat-tui-agents/bonsai-tui-agents-design-v2.md` (untracked, restored by the user) is authoritative for the look. Its mockups are copied into `C-accounts-ui.md` (accounts modal, quick usage) and `D-integration.md` (tab layouts, terminal mode, control strip, picker), so phase sessions can work from the track file alone. Where the design has no mockup (quick usage overlay, new-agent dialog, add-account form), those files define one in the same visual language: rounded frame with a CAPS title, account sidebar with provider groups and `>` cursor, label/value rows with `[▼]`, footer hints joined by `  ·  `. Each UI phase ends with goldens compared side by side against the design.
- Nothing consumes `vterm` outside its test. No component exists for launch form, accounts modal or quick usage. No attach, no terminal mode, no prefix.
- Web does not see TUI-started holders until API restart (`Manager.Adopt` only at startup, `localapi/agents.go:475`, `server.go:202`).

## Decisions carried over from part 1 (do not relitigate)

1. TUI is a peer client of holders, like `bonsai agents`. Never needs `bonsai web` running, never builds an `agenthost.Manager`. Starts through `agenthost.Starter{Origin:"tui"}`.
2. `internal/ui` imports only `agentsapi` (+ `core/*`, `git`). Concrete backend lives in `internal/agentsbackend`, built in `main.go`, injected via `ui.WithAgents`. UI tests use `agentsapi/fake`.
3. Viewing ≠ controlling. Showing a session attaches `read_only`; emulator sized to holder PTY, cropped. `enter` = terminal mode = re-attach without `read_only` at current offset; if another client writes, banner + `Ctrl+] t`. Only the writer resizes.
4. Terminal mode sends every key to the agent (incl. `esc`, `tab`, `ctrl+c`, `q`). Only `Ctrl+]` (`tea.KeyCtrlCloseBracket`) is intercepted, then `d a u m t s esc`, `Ctrl+]` again = literal 0x1d.
5. Account add / sign in again run the existing CLI flows with `tea.ExecProcess` (suspend, real terminal, resume).
6. TUI exit never stops holders. Last-selected session per worktree is in memory only.
7. Windows: tab says "Interactive agents need a Unix host (Linux or macOS)."; everything compiles.

New decisions for part 2:

8. Backend package name is `internal/agentsbackend` (part 1 name), not the lost `internal/tuiagents`.
9. Wire `main.go` as soon as A3 lands (D3 first step), so every later phase is testable in the real binary. Until D4, the tab is a live read-only viewer.
10. Docs ship with the phase that changes behavior (AGENTS.md rule), not in one final dump. D6 only audits.
11. Tests in `agentstab_test.go` that assert "coming soon" are replaced in D5, not deleted earlier.
12. Accounts modal, quick usage and new-agent dialog share one frame + account sidebar built in C2 (`accounts.Frame`, `accounts.Sidebar`), so they look like one system.
13. Terminal mode has its own pane layout (two-row header, bottom hint row, design section 5); the PTY is resized when the mode changes. The `Ctrl+]` control strip overlays the bottom three rows and does not resize the PTY.

## Phase graph and waves

```
A2 ─────────► A3 ──┐
C2 step 0 ────┘    │
B2 ────────────────┼──► D3 ──► D4 ──► D5 ──► D6
C2 ──► C3 ─────────┘    (A3)   (B2)   (C2,C3)
```
C2 step 0 is a small contract commit (`agentsapi.Defaults.LaunchFields` + `agents.LaunchOptionsFrom`): the original contract has no per-launch fields, which the new-agent dialog needs. Land C2 (or that commit alone, as its own PR) before starting A3.

- **Wave 1, parallel (3 sessions):** A2, B2, C2. File sets disjoint.
- **Wave 2, parallel (2 sessions):** A3 (stacked on A2), C3 (stacked on C2).
- **Wave 3, sequential, one session:** D3 → D4 → D5 → D6. All touch `internal/ui/*.go`; never two sessions there at once.
- D3 needs only A3 + merged B1 (read-only view uses `vterm.Write/View`). D4 needs B2. D5 needs C2 + C3.

## How to run it

1. Integration branch `feat/tui-agents`. Each phase: branch `feat/tui-agents-<phase>`, worktree `~/bonsai-tui-agents-<phase>`, cut from `feat/tui-agents` (stacked phases from the previous phase branch: `a3` from `a2`, `c3` from `c2`, `d4` from `d3` …). PR base = `feat/tui-agents` or the previous phase branch for stacks. One concern per commit, conventional commits, no fixup commits. PR label `release:none` for every phase except the last merge of `feat/tui-agents` into `main`.
   ```
   git -C ~/bonsai-feat-tui-agents worktree add ../bonsai-tui-agents-a2 -b feat/tui-agents-a2 feat/tui-agents
   cd ~/bonsai-tui-agents-a2 && claude
   > Execute phase A2 of ~/bonsai-feat-tui-agents/tui-agents-plan/A-sessions.md.
   ```
   Old part-1 worktrees (`~/bonsai-tui-agents-{a,b,c,d,p0}`) are clean and fully merged: remove them or reuse after `git switch -c` from `feat/tui-agents`.
2. Each phase ends with its "Done when": run checks via `test-runner`, then `reviewer` on the diff, before opening the PR.
3. Merge each PR into `feat/tui-agents`, rebase stacked branches (`git rebase feat/tui-agents`).
4. Manual acceptance (end of `D-integration.md`) runs on the real binary with `claude`/`codex` installed and `bonsai web` open in a browser.

## Orchestrate routing (unchanged from part 1)

- `scout` (antigravity gemini-3.8-flash-high, `--dangerously-skip-permissions`, one at a time): code-mapping questions inside a phase.
- `builder` (codex): steps tagged `[builder]` only (tests, mechanical edits, fully specified files). Codex cannot bind sockets: re-run holder/socket tests yourself.
- Claude session: steps tagged `[you]` (locking, protocol, attach state machine, key routing, anything security-sensitive).

## Checks every phase

Go: targeted `go test -race ./internal/<pkg>/...`, then `gofmt -l $(git ls-files '*.go')` (empty), `go vet ./...`, `go build ./...`, `GOOS=windows go build ./...`, `go test ./...`. `go.mod` changed: `go mod tidy` (no diff). Strip real `claude`/`codex`/`agy` dirs from `PATH` when running agent/CLI tests. Never touch real `$HOME` in tests (`t.TempDir`, `t.Setenv`).
