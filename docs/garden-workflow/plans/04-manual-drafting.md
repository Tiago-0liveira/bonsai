# 04 / Complete drafting without AI

Status: planned.

## Deliverable

Capture, shape, save and reopen an idea entirely by hand in a private drafting
workspace. AI is an optional invitation, never a required step.

## Read only

Source: `internal/ui/gardendrafts.go`, `internal/ui/gardendrafts_view.go`,
`internal/ui/gardenedit.go`, `internal/ui/gardenedit_view.go`,
`internal/ui/gardenidea.go`, `internal/ui/gardenapi/gardenapi.go`.
Images: [capture](../design/png/capture-120x35.png),
[manual editor](../design/png/manual-120x35.png),
[compact](../design/png/manual-80x24.png).

Entry: an injected private-draft backend supports stable IDs, scoped listing and
revision-checked saves; existing GitHub draft documents remain usable.

## Specification

- New draft starts with one description field. Continue writing opens the same
  draft's structured specification; title and type can be edited while shaping it.
  Neither action creates an Issue, validates work or launches a provider.
- Use a compact section rail for Problem, Outcome, Requirements, Criteria and
  Questions. Preserve extra sections and free text. AI-off mode allocates space to
  writing, with a secondary Invite assistant action. Compact layouts use tabs.
- Save through commands with ordered, debounced updates and explicit Ctrl+S flush.
  Show Saving/Saved locally/Unsaved changes truthfully. Failed saves retain text;
  offer recovery before quitting with unwritten edits. Restore saved selection/text
  after restart and keep cross-process conflicts visible.
- Distinguish local Draft IDs and existing GitHub draft Issues in the inbox.
  Both remain selectable; old documents keep existing edit/validation behavior.
  Archive is explicit and reversible where retained data permits it.
- Show concrete readiness reasons: required sections and unresolved decisions.
  Review & create issue may open a read-only local preview; if no publication
  service is available, explain why confirmation is unavailable.

## Checks and exit

Run `go test ./internal/ui/... ./internal/gardenbackend/...`. Demonstrate the full
no-provider journey, restart recovery, save failure/conflict, text-field shortcut
ownership and all reference sizes. Stop when manual drafting works end to end
locally; no GitHub creation or AI behavior changes are included.
