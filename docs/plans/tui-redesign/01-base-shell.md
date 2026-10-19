# Plan 01 — Base shell only

Status: not started. Prerequisite: [baseline and shared rules](README.md).
Next: [02 — Settings](02-settings.md).

## Deliverable

A runnable opt-in TUI containing **only background, header/top selector, footer,
and an empty body**. The top selector has Garden, Playground, Worktrees, Accounts
and Settings in that order. No workspace screen is implemented in this plan.

References: [shell crop from Settings](../../design/tui-redesign/revision-02/png/s-02-120x35-appearance.png),
[80-column shell](../../design/tui-redesign/revision-02/png/g-03-80x24-142.png),
[160-column shell](../../design/tui-redesign/revision-02/png/g-01-160x48-161.png).
Use only the outer chrome of these images. Their body content is later work.

## 01.1 — Isolate the preview entry

- Add the proposed `--tui-layout` opt-in described in the index through `main.go`.
  Preserve config selection, subcommand dispatch, existing startup errors and the
  legacy default. Invalid layout values fail clearly before starting a TUI.
- Introduce a small root layout/workspace choice and construction option in
  `internal/ui/model.go`; keep the old body reachable through legacy mode.
- Do not extract Accounts, terminal or Worktrees merely to populate new tabs.
- New empty workspace bodies do not start GitHub fetches, agent watchers, usage
  refreshes or setup jobs. Existing legacy initialization remains functional.

## 01.2 — Draw the shell

- Implement named outer regions and one geometry calculation in `view.go` or a
  focused new shell file. Reserve exact rows before sizing the body.
- Extend `theme.Palette` with the smallest necessary background/surface/raised/
  secondary-text roles. Resolve them for every preset and allowed override path;
  mockup hex values guide the Bonsai theme, not a global palette replacement.
- Top tabs show icon plus text, a filled selected state and a restrained inactive
  state. Keep all five visible at 80 columns. ASCII mode retains all labels.
- Footer shows current workspace/focus, actual available navigation and status.
  Body stays empty. Submenu/detail rows are geometry hooks, not fake content.

## 01.3 — Make selection and sizing reliable

- Root navigation and the existing command palette can select each workspace.
  Palette selection changes the tab only; no business action executes.
- Keep scoped keymap overrides and effective hints. Do not globally assign `1–5`
  over the existing agent session digits or take `g` away from Worktrees.
- Track selection in memory in this phase. Persistent last-workspace preference
  belongs to plan 02. Establish a focus enum for navigation/body/dialog/input;
  the empty body cannot pretend to own terminal input.
- Support 80×24, 120×35 and 160×48 without wrapping chrome or writing beyond the
  alternate-screen bounds. A smaller terminal gets a bounded resize notice.

## Source seams

`main.go`, `internal/ui/model.go`, `internal/ui/view.go`,
`internal/ui/update.go`, `internal/ui/keys.go`, `internal/ui/palette.go`,
`internal/ui/theme/theme.go`, `internal/ui/overlays.go` and corresponding tests.
These are inspected baseline paths, not a mandate to touch every file.

## Exit criteria and verification

- [ ] The preview contains exactly the requested shell; all five bodies are empty.
- [ ] All tabs can be selected without triggering domain work.
- [ ] Resize, ASCII and every installed theme keep chrome inside the viewport.
- [ ] Focus and key-routing tests cover tab changes and modal precedence.
- [ ] Legacy startup and existing CLI subcommands still behave as before.
- [ ] Existing UI tests plus relevant root-entry tests pass; full Go checks pass.
- [ ] `README.md` and `docs/tui-cli.md` explain the opt-in preview truthfully.

Completion record: implementation commit, focused/full checks and three-size demo
evidence to be filled in after implementation.
