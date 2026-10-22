# Plan 04b — Garden setup wiring

Status: not started. Prerequisite: 04 — Garden foundations
(backend packages landed; UI/composition wiring outstanding).
Next: 05 — Garden board.

## Deliverable

Connect the existing Garden backend to the application and implement
**Garden → Setup**: the Garden workspace body for every state before a usable
Garden exists, plus the matching Settings → Garden status. Board, Table and
Drafts stay out of scope (plans 05–07).

Current state: `internal/core/garden`, `internal/core/garden/connect`,
`internal/core/garden/sync` and `internal/git/github/ghcli/garden` exist and are
tested, but nothing outside those packages imports them. The Garden tab is an
empty shell body and Settings → Garden → Sync is still `unavailable(...)`.

References: [Garden setup mockups](../../design/tui-redesign/garden-setup/README.md)
(GS-01 to GS-08, the visual source of truth for this plan),
[garden contract](../../design/tui-redesign/garden-contract.md) (connection
section 04.2), [original brief, Journey A](../../design/tui-redesign/original-design-brief.md).

## Rules

1. **No I/O in the TUI.** `Update`/`View` never block. Every GitHub, store or
   Git call is a `tea.Cmd` into an injected backend. `internal/ui` never imports
   `ghcli`, `connect.Store` or `os/exec`.
2. **One backend boundary.** Mirror `agentsapi`: an `internal/ui/gardenapi.Backend`
   interface plus a `ui.WithGarden(b)` option. The real implementation lives
   outside `internal/ui` (proposed `internal/gardenbackend`) and is wired in
   `main.go` next to `ui.New`. Settings and Garden → Setup consume the same
   backend; there is no second setup flow.
3. **Access is not identity.** The backend exposes Reader always and Setup only
   for Write/Admin. A read-only connection renders no setup actions (GS-07).
   A Project step without Project scope surfaces `ErrScopeMissing`; label steps
   still run (GS-06). The GitHub identity is shown separately from coding-agent
   accounts.
4. **Opt-in, zero import.** Setup offers Create / Connect existing / Skip
   (GS-01). The plan is previewed step by step before anything runs (GS-04);
   `ImportCount == 0`. Apply runs only from the confirmation screen, where
   Cancel has initial focus (GS-05).
5. **Resume, never roll back.** A partial apply keeps completed steps. Resume is
   the primary action for `partial-setup` and re-observes GitHub before any write
   (GS-06). Concurrent setup for one clone shows `operation-in-progress`, never a
   second apply.
6. **Never adopt silently.** A same-name Project without verified ownership is
   flagged and only usable through an explicit Connect existing choice (GS-03).
   A remote ambiguity shows a target choice keyed by numeric repository ID (GS-02).
7. **Safe presentation only.** The UI shows safe codes and messages from
   `connect`/`sync`. Never raw errors, responses, tokens or unsanitized remote
   URLs. Bonsai never refreshes a token; GS-06 tells the user which `gh` command
   to run themselves.
8. **Ordering lives in the backend.** The backend owns the request generation
   counter and the `sync.Store` epoch. The UI discards replies whose
   `(GardenID, LocalRepoID, generation)` is not its latest request. Skip and
   reassociation increment the epoch.
9. **Shell conventions.** Breadcrumb `Garden › Setup › <step>`; submenu shows
   `✚ Setup` active while Board, Table and Drafts are muted until a Garden exists.
   Header right shows `owner/repo · <state>`. Esc returns to the parent step and
   restores its selection.
10. **Cell-accurate layout.** 80×24, 120×35 and 160×48 as in GS-01/GS-04: at 80
    columns the rail collapses into a choice list and the plan drops its note
    column. Truncate by terminal cell width. Every theme keeps an ASCII fallback.

## Decisions

- Setup lives in the Garden body. Settings → Garden shows the same state and
  links to it.
- `ApprovalPolicy` defaults to the authenticated viewer's numeric GitHub ID until
  plan 08 adds a setting.
- Create uses the canonical label set from the garden contract and only the
  Stage/Type Project fields.
- Skip is stored per local clone and is reversible from GS-08 or Settings.

## 04b.1 — Backend composition

- Add a `RemoteLister` implementation in `internal/core/git` (no remote-listing
  seam exists yet). Garden packages do not shell out.
- Add `internal/gardenbackend`: `ghcli.NewShared` → `garden.FromService`,
  `connect.ResolveTarget`, `connect.OpenStore`, and one `sync.Store` per local
  repository for connection status.
- Expose: `Status()`, `Candidates()`, `Projects(owner)`, `PreviewPlan(mode, target, project)`,
  `Apply(opID, plan)`, `Resume(opID)`, `Skip()` and `Unskip()`.
- Tests use `connect/fakegh` and the in-memory fakes with `t.TempDir()` homes.
  Cover ambiguity, same-ID remotes with aliases, skip/unskip, partial apply then
  resume, scope-missing, read-only access, and two clones sharing one Garden with
  separate locks.

## 04b.2 — Garden → Setup screens

| State or step | Screen | Primary action |
|---|---|---|
| `none` | GS-01: mode rail, repository, identity, access, scope, zero import | Preview setup plan |
| Target ambiguous | GS-02: candidates by numeric ID, remotes as aliases | Use selected repository |
| Connect existing | GS-03: owner's Projects, field reuse, same-name warning | Preview setup plan |
| Plan preview | GS-04: every step with change/kind/target/note, zero-import summary | Review and apply |
| Confirmation | GS-05: exact consequence, Cancel focused | Apply setup |
| `partial-setup`, `awaiting-manual` | GS-06: step statuses, failed safe code, user-run fix, operation ID | Resume setup |
| `operation-in-progress` | GS-06 layout without actions, progress only | none |
| `read-only` | GS-07: access explained, setup actions absent | Open Board (empty until plan 05) |
| `skipped` | GS-08: off for this clone, reversible | Set up Garden |
| `connected` | Leaves Setup for the Board (placeholder until plan 05) | — |

## 04b.3 — Settings → Garden

- Repository link section: target repository, GitHub identity, access level and
  association state from `sync.Status`, with an "Open Garden setup" link.
- Replace the `sync` placeholder with a real status row: state, operation
  progress and last sync.
- Approval gates and Issue import remain `unavailable(...)` with plan numbers.

## Exit criteria and verification

- [ ] `main.go` wires a real Garden backend; the UI reaches it only through
      `gardenapi.Backend`.
- [ ] Every GS screen is reachable from its backend state against fakes, and its
      rendering matches the mockup's structure at the listed sizes.
- [ ] Setup imports zero issues; create, connect existing, skip/unskip, read-only
      and partial-resume work against fakes.
- [ ] No GitHub write happens before GS-05 Apply; Esc and Cancel never write.
- [ ] Settings → Garden and Garden → Setup show identical state from one backend.
- [ ] Late replies and other-repository replies never land.
- [ ] Targeted tests, `go test -race` for `internal/gardenbackend/...` and
      `internal/core/garden/...`, then `gofmt`, `go vet`, `go build`, `go test ./...`.
- [ ] `docs/garden.md` added (setup section) and linked from `README.md`;
      `docs/tui-cli.md` updated.
- [ ] Completion records of plans 04 and 04b filled in.

Completion record: pending implementation and validation evidence.
