# Sequential TUI redesign plans

Status: planning only; no phase has been implemented by this change.

The user approved [revision 02 of the design](../../design/tui-redesign/README.md)
and requested this implementation order: **base shell → Settings → Accounts →
Garden → Playground → Worktrees**. These are separate, cumulative work plans,
not competing design alternatives. Finish each plan's exit criteria before
starting the next numbered plan. Do not implement later phases opportunistically.

## Baseline and scope

- Integration branch: `feat/tui-redesign`.
- Worktree: `/home/tiagoliv/bonsai-tui-redesign`.
- Created from `feat/agent-status` at
  `513186bf9ff8517a06a5cf064907de1dc0d01797`.
- That baseline includes provider activity reporting and the Claude question/plan
  reason fix. Preserve those changes and the existing terminal input protections.
- Go, Bubble Tea and Lip Gloss remain the application platform. The HTML review
  gallery is a design artifact, not the implementation technology.
- No `web/` redesign, public API expansion, provider authentication rewrite,
  dependency change, or real GitHub backlog migration is implicit in these plans.
- The original brief mentioned a separate Garden implementation plan, which was
  unavailable. Plans 04 and 08 explicitly resolve those missing contracts before
  depending on them; mockups are not evidence that the backend already exists.

## Required order

| Order | Plan | Reviewable result | Dependency | Status |
|---|---|---|---|---|
| 01 | [Base shell](01-base-shell.md) | Background, five-tab selector, footer, empty body | Baseline | Implemented |
| 02 | [Settings](02-settings.md) | Real preferences in clear nested sections | 01 | Not started |
| 03 | [Accounts](03-accounts.md) | Profiles, usage, defaults and existing account flows | 02 | Not started |
| 04 | [Garden foundations](04-garden-foundations.md) | Canonical Issue contract, connection/setup and safe reads | 03 | Not started |
| 05 | [Garden board](05-garden-board.md) | Selectable state columns for approved work | 04 | Not started |
| 06 | [Garden table](06-garden-table.md) | Selectable rows over the same approved-work collection | 05 | Not started |
| 07 | [Garden drafts](07-garden-drafts.md) | Separate idea inbox, capture, edits and explicit import | 06 | Not started |
| 08 | [Specifications and plans](08-garden-approval-and-planning.md) | Both revision-bound approvals and local read-only planning | 07 | Not started |
| 09 | [Local implementation launch](09-garden-local-launch.md) | Explicit, isolated, recoverable task execution | 08 | Not started |
| 10 | [Playground](10-playground.md) | Repository session cockpit, terminal, changes and review | 09 | Not started |
| 11 | [Worktrees](11-worktrees.md) | Existing Git/process functionality in the redesigned workspace | 10 | Not started |
| 12 | [Cutover and hardening](12-cutover-and-hardening.md) | Default new UI with parity, migration and operational checks | 11 | Not started |

Each plan has smaller phases that are also sequential. A phase can be a focused
commit or review unit; a plan is not a requirement to land a giant PR. Follow the
repository's conventional commit and release-label rules. This planning change
does not authorize remote publishing, merging, or execution of the implementation.

## Transitional application behavior

Plan 01 introduces an explicit opt-in redesign entry while the current TUI stays
usable. The proposed new root flag is `--tui-layout`, accepting `legacy` and
`redesign`; this is a future interface, not a currently supported command. Keep
legacy as the default until plan 12. Validate and document flag parsing in plan
01, including coexistence with the existing config option and CLI subcommands.

The redesign preview initially contains only its shell and empty workspace
bodies. Add working content in exactly the sequence above. Do not port Worktrees
into plan 01 to make the preview appear complete. Retain the legacy path until
its functions have real replacements; avoid implementing a second process,
session, config or Git backend in either path.

Settings can display a future Garden/automation setting as unavailable with a
reason, but must not persist or imply behavior that has no backend. Garden Board
and Table can correctly be empty before creation/approval is implemented; only
tests and design artifacts may use the fabricated screenshot data.

## Shared implementation boundaries

1. The shell owns outer geometry, workspace routing, breadcrumbs, focus and the
   footer. Workspaces own their bodies and scoped actions. Avoid a generic
   framework or an eager extraction of every old root-model field.
2. `Update` and `View` perform no blocking I/O. UI commands invoke injected core
   services; provider/runtime wiring stays outside the UI. Preserve the existing
   `agentsapi` boundary and reuse the holder-backed terminal client.
3. Account capabilities and terminal capabilities are independent. Accounts must
   work even where hosted PTYs are unsupported.
4. GitHub Issues is the Garden's canonical record. Project views are projections;
   local cache and local execution records are not a competing issue database.
5. Draft ideas are a separate view and workflow lane. Type is independent of
   stage. Board/Table never silently admit unvalidated ideas.
6. Approve idea, request local plan, approve exact plan, and start local
   implementation are four separate actions. Remote edits never launch local
   processes. Stale approvals block launch.
7. Keep last good data after errors. Order async results by repository scope and
   request generation; preserve selected IDs and document anchors on refresh.
8. Selection is not keyboard capture. Only explicit terminal control sends keys;
   `Ctrl+]` escapes it. Respect writer ownership, replay generation and late-frame
   guards. No shortcut remapping may bypass this boundary.
9. Add gray surface roles and semantic accents through the installed theme
   system. Provide all preset fallbacks and ASCII equivalents. Usage meters must
   label used versus remaining and must not imply task progress.
10. Preserve agent-profile isolation, environment scrubbing, stored-token-only
    reads, ownership/locks, loopback boundaries and the plain Go build.

## How to execute a plan

Read root and area `AGENTS.md`, then this index, the numbered plan and its image
references. Recheck source seams because the baseline may evolve. Record actual
commit, changed files, validation, demo results and any unresolved constraint in
the plan's completion record. Check every earlier plan is complete before moving
forward. Tests passing and a truthful demo are engineering completion criteria,
not an invented requirement to ask for permission after each edit.

All numbered plans currently have an empty completion record. Approval of the
visual design does not mean the mockup's fabricated numbers or placeholder field
values are production defaults.

## Verification for future code changes

Run focused tests for changed existing packages first. Existing examples:

```sh
go test ./internal/ui/...
go test ./internal/core/config/...
go test ./internal/agentsbackend/... ./internal/core/agents/...
go test ./internal/git/... ./internal/daemon/...
```

When a later plan adds a package, run its targeted tests after it exists. Agent,
daemon, server or concurrency work also needs race tests for the affected
packages. Before finishing a Go implementation change, follow the repository's
full checks:

```sh
gofmt -l $(git ls-files '*.go')
go vet ./...
go build ./...
go test ./...
```

Formatting must print nothing. Use fake GitHub/provider services and temporary
homes; no test may log into a real account or mutate a real backlog. Add meaningful
state-machine, focus, ordering and selected rendering checks, not snapshot-only
tests that repeat the implementation. Update `README.md` and relevant user docs
when each real behavior ships. There are no code checks to run for this docs-only
planning change; validate links, assets, sequence and baseline instead.
