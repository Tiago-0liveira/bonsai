# 01 / Render a real terminal overlay

Status: planned.

## Deliverable

Render the existing Garden board behind an opaque centered panel, with a dimmed
parent and correct terminal-cell geometry. This is rendering infrastructure only.

## Read only

Source: `internal/ui/shell_view.go`, `internal/ui/view.go`,
`internal/ui/components/modals/modals.go`, `internal/ui/update.go`,
`internal/ui/theme/theme.go`, `internal/ui/shell_test.go`, `go.mod`.
Images: [wide](../design/png/planning-120x35.png),
[compact sheet](../design/png/plan-review-80x24.png).

Entry: the redesign shell and existing centered modal component work. Today the
shell returns the modal instead of the underlying board; preserve legacy behavior.

## Specification

- Add a small compositor under `internal/ui/components/overlay/` (new). Its
  contract accepts a bounded parent frame, panel frame and cell coordinates and
  returns one complete frame. Lip Gloss `Place` alone is not composition.
- Measure/cut by terminal cells, preserving ANSI state and grapheme clusters.
  Reuse available ANSI/cell helpers; do not split by bytes/runes or implement a
  new terminal emulator. Wide glyphs at clipping boundaries must not leak halves.
- Fill every panel cell, including padding, with its opaque theme background.
  Render the parent with an explicit dim palette; no transparency, blur or global
  theme mutation. Preserve foreground/background resets outside the panel.
- Compose the panel over the board in the redesign shell. Use a plain border and
  integer geometry; center within available bounds. At 80×24 use a near-full sheet;
  clamp/scroll content on resize, keeping its header and actions reachable.
- Prove the result using one fixture panel populated from an existing Issue's
  read-only summary. Do not add ticket workflows or replace every legacy modal.

## Checks and exit

Run `go test ./internal/ui/components/overlay/... ./internal/ui/...` after the new
package exists. Cover ANSI bleed, background coverage, combining/wide glyphs,
clipped edges and resize at all three reference sizes. Render through the real
Bubble Tea `View` path in a terminal: the board remains visible around the panel,
closing restores it, and no stale cells remain. Record the demo command and result.

Stop when composition works. No dependency upgrade, new backend or workflow writes.
