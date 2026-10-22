# 02 / A focused Garden ticket overlay

Status: planned.

## Deliverable

Board cards and table rows open one consistent item overlay without losing their
origin, selection or scroll. Actions reuse existing services.

## Read only

Source: `internal/ui/gardenboard.go`, `internal/ui/gardenboard_view.go`,
`internal/ui/gardentable.go`, `internal/ui/gardenapi/gardenapi.go`,
`internal/ui/update.go`, `internal/ui/agentterm.go`.
Images: [planning](../design/png/planning-120x35.png),
[table origin](../design/png/human-review-120x35-table.png).

Entry: the redesign shell can compose an opaque panel over its parent frame using
ANSI/cell-safe rendering. Existing Issue documents and action commands are available.

## Specification

- Add a Garden-scoped overlay state keyed by repository and stable Issue ID.
  Capture Board/Table origin and selection; Escape restores that exact context.
  Refreshing/deleting the item shows a bounded notice rather than another ticket.
- Give the overlay a fixed identity/state header, Overview/Specification/Plan/
  Activity navigation, scrollable body and pinned actions. A side summary rail is
  optional at wide sizes; compact sizes drop the rail, not essential actions.
- Route Enter and cell-based mouse activation to the same open action. While open,
  consume its keys and clicks before global shortcuts or the board. Tab cycles
  enabled controls; provide explicit back/close and preserve disabled-action reasons.
- Populate content from existing document, planner and launch projections. Show
  only actions whose backend capability and approval/freshness checks are available.
  Reading a tab never starts a job. Destructive/launch confirmations stay explicit.
- Opening an agent terminal is a separate action with explicit input control.
  Navigation or closing the overlay never stops a holder; keep `Ctrl+]` protection.

## Checks and exit

Run `go test ./internal/ui/...`. Cover keyboard/mouse equivalence, focus trapping,
return selection, resize/scroll, item removal, stale async replies and terminal
escape behavior. Demonstrate the same Issue opened from Board and Table in the real
TUI, including 80×24. Stop with the shared host and existing actions working; no new
planning, review or publication backend is included.
