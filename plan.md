# TUI Configurable Pane Layout Plan

## Branch strategy

This work must not be implemented directly on refactor/local-api-security.

Use the dedicated branch:

~~~text
feat/tui-configurable-layout
~~~

This branch was created from the current head of refactor/local-api-security:

~~~text
83cdefffc20a135bfff0d90427516e219cfaa06e
test(web): use numeric GitHub repository identity
~~~

All work described by this plan must stay on feat/tui-configurable-layout until it is complete and validated.

The source branch refactor/local-api-security must remain untouched by this feature work.

---

## Goal

Make the existing Bubble Tea terminal UI layout configurable per user without changing Bonsai's Git behavior, daemon/server behavior, local API behavior, CLI behavior, or web application.

A user must be able to choose:

- where the existing top-level TUI panes appear;
- whether those panes are arranged horizontally or vertically;
- the relative size allocated to each existing pane;
- and reset the layout to the legacy default.

The layout preference must be personal to the user. It must not be a repository-level setting and must not be written to .bonsai.yaml.

The default experience must remain visually and behaviorally equivalent to the current TUI:

~~~text
horizontal
worktrees first
worktrees 35%
workspace 65%
~~~

The feature is a layout/presentation change only.

---

# Non-negotiable scope boundary

This plan is intentionally strict.

## Allowed product behavior changes

Only these behaviors may change:

1. top-level TUI pane placement;
2. top-level TUI pane relative size;
3. TUI preference controls required to configure that layout;
4. mouse hit-testing required because panes may move;
5. layout calculations required for terminal resize;
6. user-level persistence of the TUI layout preference.

Nothing else is part of this work.

## Forbidden behavior changes

Do not change:

- any Git command;
- any Git command arguments;
- any Git operation sequencing;
- any worktree creation/deletion behavior;
- any PR, checks, workflow, fetch, pull, push, commit, rebase, prune, or update behavior;
- any gh invocation;
- any local Git service implementation;
- any daemon protocol;
- any daemon process supervision;
- any local API endpoint;
- any local API security rule;
- any relay or server code;
- any browser/web code;
- any CLI command or CLI flag;
- any process lifecycle behavior;
- any hook behavior;
- any project config semantics;
- any theme semantics;
- any keybinding semantics;
- any tab visibility rule;
- any tab action;
- any worktree list sorting/filtering behavior.

If implementing the layout requires changing one of those areas, stop. That means the design has escaped scope and the plan must be reconsidered before continuing.

---

# Current TUI inventory

The current terminal UI has exactly two top-level panes.

## Pane 1 — worktrees

Existing component:

~~~text
internal/ui/components/worktreelist
~~~

Current behavior:

- rendered on the left;
- owns worktree navigation/filtering;
- receives list focus;
- uses the worktree list Bubble Tea model.

Stable pane ID for the layout system:

~~~text
worktrees
~~~

## Pane 2 — workspace

The current code calls this the right pane.

It is one existing cyclic/tabbed pane, not several separate panes.

It contains the existing rightTab cycle:

- Git Log;
- Processes;
- Inspect;
- Diff;
- Checks;
- PR.

Stable pane ID for the layout system:

~~~text
workspace
~~~

The implementation may use the neutral term workspace pane in new code even though existing variables such as rightTab can remain for minimal churn.

## Important clarification about cyclic panes

There is currently one cyclic pane: workspace.

Do not create another cyclic pane.

Do not turn Git Log, Processes, Inspect, Diff, Checks, or PR into independent top-level panes.

Do not split the workspace pane.

Do not duplicate the workspace pane.

Do not create configurable pane instances.

The architecture may use stable pane IDs and generic layout data so a future change can add panes deliberately, but this branch must register exactly the two existing panes above.

## Elements that are not panes

The following must remain outside the configurable pane layout:

- status/help bar;
- preferences overlay;
- modals;
- update prompt;
- command palette;
- process footer inside the Processes tab;
- tab strip inside the workspace pane.

The status bar remains a full-width row at the bottom.

Overlays remain centered over the complete terminal surface.

---

# Current implementation facts that must be preserved

The current TUI layout is hard-coded around:

~~~text
leftPaneRatio = 35
~~~

The current geometry path is primarily:

~~~text
Model.dims()
Model.layout()
Model.View()
~~~

The current mouse router assumes a left/right split using an X-coordinate comparison.

The current workspace viewport height is tab-sensitive. In particular, the Processes tab reserves rows for its footer, and the terminal viewport must be sized to the actual rendered space so scrolling continues to work.

The current render tests enforce that the TUI never exceeds terminal height and that the tab strip never wraps.

Those invariants remain mandatory after this feature.

---

# Persistence boundary

The requested layout is a per-user preference.

Bonsai already persists personal UI preferences through state.json under the user's config directory. Reuse that mechanism.

Do not introduce:

- a second TUI-specific config file;
- environment variables for layout;
- CLI flags for layout;
- .bonsai.yaml layout fields;
- repository-local layout files.

The only permitted code outside internal/ui is the existing user-preference storage seam in:

~~~text
internal/core/config/state.go
~~~

That exception is allowed only to add serializable data fields representing the personal TUI layout preference.

No business logic, Git logic, server logic, or command execution logic may be added there.

If persistence can be completed without modifying any other non-UI file, no other non-UI file may be touched.

---

# Proposed persisted layout model

Use a small, versionable layout preference representing only the current top-level pane arrangement.

Recommended conceptual shape:

~~~go
type TUILayoutPrefs struct {
    Version int            json:"version,omitempty"
    Axis    string         json:"axis,omitempty"
    Order   []string       json:"order,omitempty"
    Sizes   map[string]int json:"sizes,omitempty"
}
~~~

And inside the existing personal Prefs:

~~~go
Layout TUILayoutPrefs json:"layout,omitempty"
~~~

The exact Go names may differ, but the semantics must not.

## Supported values in this branch

Axis:

~~~text
horizontal
vertical
~~~

Order must contain exactly these IDs once each:

~~~text
worktrees
workspace
~~~

Sizes are relative percentages for the same two IDs and must normalize to 100.

Default:

~~~text
axis: horizontal
order:
  - worktrees
  - workspace
sizes:
  worktrees: 35
  workspace: 65
~~~

## Why order plus sizes

This gives the user all four meaningful placements without absolute coordinates:

Horizontal + worktrees first:

~~~text
[ worktrees ][ workspace ]
~~~

Horizontal + workspace first:

~~~text
[ workspace ][ worktrees ]
~~~

Vertical + worktrees first:

~~~text
[ worktrees  ]
[ workspace  ]
~~~

Vertical + workspace first:

~~~text
[ workspace  ]
[ worktrees  ]
~~~

It also avoids binding persisted preferences to the words left and right, which would become incorrect in vertical layouts.

## No absolute terminal coordinates

Do not persist X/Y positions or cell dimensions.

Terminal windows resize. Persisting absolute cell coordinates would be fragile and would create invalid layouts whenever the terminal geometry changes.

Persist logical order and relative size only.

---

# Validation and normalization rules

Create one TUI-owned normalization path and use it everywhere.

A malformed personal preference must never make the TUI fail to start.

## Valid preference rules

A persisted layout is valid only when:

1. Axis is horizontal or vertical.
2. Order contains exactly two entries.
3. Order contains worktrees exactly once.
4. Order contains workspace exactly once.
5. Order contains no unknown pane ID.
6. Both known pane IDs have a positive size.
7. The normalized split keeps both panes within the supported preference range.
8. No persisted value is trusted without normalization.

## Supported preference range

The Preferences UI should expose pane percentages in 5-point increments.

For this branch, persist user-selected pane size in the inclusive range:

~~~text
20% through 80%
~~~

Because there are only two panes, changing one changes the other to the complement.

Examples:

~~~text
20 / 80
35 / 65
50 / 50
65 / 35
80 / 20
~~~

Do not allow 0/100 or hidden panes.

This feature is pane placement and sizing, not pane visibility.

## Invalid preference behavior

If persisted layout data is malformed, incomplete, duplicated, unknown, or impossible:

- do not panic;
- do not partially hide a pane;
- do not silently invent a third layout state;
- fall back atomically to the legacy default layout;
- continue launching Bonsai normally.

Do not rewrite state.json merely because a temporary terminal size forced a render-time clamp.

---

# Runtime geometry model

Introduce a small, pure layout layer under internal/ui.

Recommended new file:

~~~text
internal/ui/layout.go
~~~

The purpose is to centralize all top-level pane geometry.

Recommended internal concepts:

~~~go
type paneID string

const (
    paneWorktrees paneID = "worktrees"
    paneWorkspace paneID = "workspace"
)

type axis int

const (
    axisHorizontal axis = iota
    axisVertical
)

type paneRect struct {
    X int
    Y int
    W int
    H int
}

type resolvedLayout struct {
    Worktrees paneRect
    Workspace paneRect
    Body      paneRect
}
~~~

Names can vary. The responsibilities cannot.

## Geometry input

The layout function should receive:

- terminal width;
- terminal height;
- status bar height;
- normalized personal layout preference.

## Geometry output

It should return explicit rectangles for:

- worktrees pane;
- workspace pane;
- body area above the status bar.

View rendering, child SetSize calls, and mouse routing must all use these same resolved rectangles.

There must not be separate competing layout calculations for render, update, and mouse handling.

---

# Terminal-size safety

Persisted percentages express user intent.

Actual terminal dimensions determine what can physically render.

The runtime geometry resolver must enforce pane minimums without changing the stored preference.

## Horizontal layout minimums

Preserve the spirit of the current minimums:

- worktrees must retain enough width for a usable list;
- workspace must retain enough width for a usable tab strip and viewport;
- neither pane may receive zero or negative width.

The current code effectively protects approximately 16 outer columns for the list and 12 for the workspace. Keep those as the baseline unless tests prove a slightly larger minimum is required for correctness.

## Vertical layout minimums

Define explicit minimum outer heights for both panes.

At minimum:

- borders must fit;
- the workspace tab strip must fit;
- at least one workspace content row must remain;
- the worktree pane must retain at least one usable content row.

## Temporary clamping

Example:

A user stores 20/80, then opens Bonsai in a terminal too narrow to honor the 20% pane exactly.

Correct behavior:

1. use the stored 20/80 as intent;
2. clamp the rendered pane to its minimum safe cell size;
3. give remaining cells to the other pane;
4. do not modify the persisted 20/80 preference;
5. restore the intended split automatically when the terminal becomes large enough.

---

# Layout rendering rules

Refactor View so it does not assume left and right.

Rendering must be driven by the resolved layout.

## Horizontal

Use horizontal composition in the configured order.

## Vertical

Use vertical composition in the configured order.

## Borders

Keep the existing focused and blurred border styles.

Do not introduce new border styles for this feature.

## Status bar

The status bar remains below the pane body and spans the complete terminal width.

Its height must be computed before the pane body geometry is resolved, as it is today.

## Height and width invariants

For every supported terminal size:

- View must never exceed terminal width;
- View must never exceed terminal height;
- normal ready-state View should continue filling the terminal height exactly where the existing tests require it;
- no pane may render outside the body rectangle;
- no negative or zero child viewport size may be passed to Bubble Tea components.

---

# Child sizing rules

The worktree list and workspace terminal must be sized from their own pane rectangles.

Do not infer workspace width from total width minus a left pane.

Do not infer list width from a fixed percentage.

## Worktrees

Set the list component size from the inner dimensions of the worktrees rectangle.

## Workspace

Set the terminal component width from the inner dimensions of the workspace rectangle.

Set the terminal component height from the inner workspace height after subtracting:

- the workspace tab strip;
- the Processes footer when the Processes tab is active.

The current process-footer scrolling fix must survive all layouts.

The Processes tab must remain scrollable in:

- horizontal worktrees-first;
- horizontal workspace-first;
- vertical worktrees-first;
- vertical workspace-first.

---

# Mouse routing

The current wheel router checks whether the pointer X coordinate is left of the split.

That is invalid after panes can move vertically or reverse order.

Replace side-based routing with rectangle containment.

For wheel events:

1. resolve current pane rectangles;
2. determine whether the pointer is inside worktrees;
3. determine whether the pointer is inside workspace;
4. route the event to the component under the pointer;
5. preserve the existing mouseOff behavior;
6. preserve overlay ownership rules.

Do not add click-to-focus, drag-resize, or new mouse behavior in this branch.

This plan changes routing only enough to keep existing wheel scrolling correct after panes move.

---

# Focus and keyboard behavior

Do not create new focus states.

The existing two focus targets remain:

~~~text
focusList
focusTerminal
~~~

Tab continues toggling focus between the two existing panes.

Because there are still only two panes, visual order does not require a new focus engine.

Shift+Tab continues cycling tabs inside the workspace pane exactly as it does now.

Do not change:

- tab key semantics;
- shift+tab semantics;
- any existing tab shortcut;
- process-tab local keys;
- PR-tab local keys;
- Diff-tab local keys;
- global action keybindings.

No changes to internal/ui/keys.go should be required for this feature.

---

# Preferences UX

Extend the existing Preferences overlay rather than introducing a new screen or pane.

Add a top-level Layout section.

The section should contain exactly the controls required for this feature.

Recommended rows:

1. Orientation
2. Pane order
3. Pane sizes
4. Reset layout

## Orientation row

Values:

~~~text
horizontal
vertical
~~~

Left/right arrows cycle the value.

Apply immediately and persist through the existing SaveMsg flow.

## Pane order row

For horizontal orientation, render meaning similar to:

~~~text
worktrees → workspace
workspace → worktrees
~~~

For vertical orientation, render meaning similar to:

~~~text
worktrees ↓ workspace
workspace ↓ worktrees
~~~

The underlying stored value is still the ordered pane ID list.

## Pane sizes row

Render both values so the user can see the complete split:

~~~text
worktrees 35% / workspace 65%
~~~

Left/right adjusts the worktrees percentage by 5 points and derives workspace as the complement.

Clamp to 20 through 80.

Apply immediately so the user can close Preferences and see the new layout without restarting.

## Reset layout row

Reset only the layout preference to:

~~~text
horizontal
worktrees first
35 / 65
~~~

Do not reset theme, sort, editor, keys, or any other preference.

## No new layout keybindings

Do not add global resize/reorder shortcuts.

Do not add a layout-edit mode.

Do not add draggable splitters.

Those can be separate future features if requested.

---

# Preferences message flow

Extend the existing prefs.SaveMsg snapshot with layout state.

The parent TUI model remains responsible for applying and persisting the preference.

After a layout preference SaveMsg:

1. normalize the layout;
2. update the in-memory personal preference;
3. persist through existing user state storage;
4. recompute child sizes immediately;
5. keep the current focus unchanged;
6. keep the current workspace tab unchanged;
7. keep worktree selection unchanged;
8. do not reload Git data;
9. do not run any Git command;
10. do not restart any process.

A layout change is presentation-only and must not trigger data refresh.

---

# Model integration

The root Model may hold normalized/resolved layout state or derive it from state.Prefs.

Keep the source of truth unambiguous.

Recommended rule:

- persisted preference lives in state.Prefs.Layout;
- normalize once when constructing the model and whenever Preferences saves;
- geometry is derived from normalized preference plus current WindowSizeMsg dimensions;
- resolved rectangles are not persisted.

WindowSizeMsg must:

1. update width and height;
2. resize Preferences if open;
3. recompute layout;
4. mark the TUI ready as it does now.

It must not write state.json.

---

# Cyclic workspace behavior

The workspace pane remains one cyclic pane.

The existing rightTab values remain exactly the existing set.

The existing visibleTabs rules remain exactly the existing rules:

- Log, Processes, Inspect always available;
- Diff only when appropriate;
- Checks only when appropriate;
- PR only when connected to a PR.

Layout preferences do not control:

- which tabs exist;
- tab order;
- active tab;
- tab visibility;
- tab names.

Do not expand the scope into customizable tabs.

---

# Recommended implementation phases

## Phase 0 — Baseline and scope lock

Before code changes:

1. confirm feat/tui-configurable-layout points at the expected source commit;
2. run the existing TUI tests;
3. record the current default render behavior;
4. confirm current pane inventory is exactly worktrees plus workspace;
5. confirm there is one cyclic workspace pane;
6. do not modify any Git/server files.

No feature code should begin until the baseline passes or an existing failure is documented.

## Phase 1 — Add pure layout types and normalization

Create internal/ui/layout.go and internal/ui/layout_test.go.

Implement:

- stable pane IDs;
- default layout spec;
- layout preference normalization;
- order validation;
- size validation;
- cell allocation;
- pane rectangle calculation;
- horizontal and vertical composition metadata.

Keep this phase pure.

No Bubble Tea commands.

No Git calls.

No state writes.

## Phase 2 — Replace fixed split calculations

Refactor the existing dims/layout path to consume resolved layout rectangles.

Remove the hard dependency on leftPaneRatio from runtime layout.

The legacy 35/65 split becomes the default preference, not a rendering constant.

Keep current child component APIs.

Do not rewrite worktreelist or terminal internals unless a test demonstrates an actual sizing bug.

## Phase 3 — Render in configured orientation/order

Refactor View to render the same two existing panes according to the resolved axis and order.

Preserve:

- borders;
- focus highlighting;
- status bar;
- tab strip;
- process footer;
- overlays;
- update prompt behavior.

Do not change pane content.

## Phase 4 — Fix pointer routing

Replace X-only side detection with pane-rectangle hit testing.

Test both orientations and both orders.

Do not add new mouse features.

## Phase 5 — Add personal preference schema

Add only the layout preference data shape to the existing personal Prefs state.

No .bonsai.yaml changes.

No config editor changes.

No CLI config flags.

Existing state files with no layout field must decode normally and use the default layout.

## Phase 6 — Extend Preferences overlay

Add the Layout section and SaveMsg fields.

Implement:

- orientation cycling;
- pane order cycling;
- 5-point split adjustment;
- reset layout.

Changes should apply immediately.

Do not add another modal unless the existing Preferences component truly cannot express one of these controls. The preferred implementation uses the existing row interaction model.

## Phase 7 — Regression coverage

Add targeted tests for:

- normalization;
- invalid persisted preference fallback;
- geometry;
- horizontal order;
- vertical order;
- size split;
- terminal resize;
- mouse routing;
- focus preservation;
- workspace tab preservation;
- process footer height;
- tab strip width;
- exact ready-state terminal height;
- preference SaveMsg snapshots;
- reset layout behavior.

## Phase 8 — Full validation and scope audit

Run formatting, unit tests, full tests, and a diff scope audit.

Do not merge while any forbidden file has changed.

---

# Required tests

## Pure geometry table tests

Cover at least these terminal sizes:

~~~text
40x15
60x20
80x24
100x40
120x30
160x50
~~~

Cover both axes:

~~~text
horizontal
vertical
~~~

Cover both orders:

~~~text
worktrees first
workspace first
~~~

Cover representative splits:

~~~text
20/80
35/65
50/50
65/35
80/20
~~~

Assert:

- no negative rectangle;
- no zero-size renderable pane;
- body bounds are respected;
- pane rectangles do not overlap;
- pane rectangles fully consume the body along the configured axis;
- status bar space is excluded from panes;
- minimum pane dimensions are enforced.

## Preference validation tests

Cases:

- missing layout;
- missing axis;
- unknown axis;
- missing order;
- duplicate worktrees;
- duplicate workspace;
- unknown pane ID;
- missing size;
- negative size;
- zero size;
- sizes that do not total 100;
- split under 20;
- split over 80;
- future unknown version.

Invalid layouts must fall back to one deterministic default.

## Render tests

Keep all current render tests passing.

Add assertions for all four arrangements.

At minimum verify:

- View never exceeds terminal height;
- View never exceeds terminal width;
- default layout remains visually equivalent in structure;
- focus border follows the pane, not its old side;
- tab strip remains one line;
- active tab remains visible when constrained;
- process footer is not clipped incorrectly;
- workspace viewport remains scrollable.

## Mouse tests

For each of the four arrangements:

- wheel over worktrees routes to list;
- wheel over workspace routes to terminal;
- mouseOff suppresses routing;
- modal/prefs ownership continues to suppress background wheel handling.

## State compatibility tests

An old state.json with no layout field must load successfully.

Saving preferences with layout must preserve all pre-existing preference fields.

Resetting layout must not reset any non-layout preference.

---

# Strict file-change policy

Implementation should stay inside this whitelist.

Expected files:

~~~text
plan.md

internal/ui/layout.go
internal/ui/layout_test.go
internal/ui/model.go
internal/ui/view.go
internal/ui/update.go
internal/ui/render_test.go

internal/ui/components/prefs/prefs.go
internal/ui/components/prefs/prefs_test.go

internal/core/config/state.go
~~~

If state persistence needs a dedicated compatibility test, this additional test file is allowed:

~~~text
internal/core/config/state_test.go
~~~

No other file is pre-approved.

If another file appears necessary, stop before editing it and verify whether that means scope has expanded.

## Explicitly forbidden paths

No changes under:

~~~text
cmd/
web/
internal/cli/
internal/daemon/
internal/server/
internal/git/
internal/storage/
internal/core/git/
internal/core/exec/
internal/core/procstore/
~~~

Also do not modify:

~~~text
.bonsai.yaml
go.mod
go.sum
README.md
internal/ui/cmds.go
internal/ui/keys.go
~~~

The purpose of naming internal/ui/cmds.go and internal/ui/keys.go explicitly is to protect Git execution and key semantics from accidental cleanup while touching update/view code.

---

# Diff-hunk discipline

Some permitted files, especially internal/ui/update.go, contain unrelated Git and process behavior.

Permission to edit the file is not permission to refactor unrelated functions.

Changes inside update.go must be limited to:

- WindowSizeMsg/layout application;
- Preferences SaveMsg application for layout;
- mouse pane routing;
- minimal layout-related helpers.

Do not reformat, rename, or restructure adjacent Git action code.

Changes inside model.go must be limited to layout state and preference initialization.

Changes inside view.go must be limited to top-level geometry/render composition and layout-dependent sizing.

Avoid drive-by cleanup.

---

# Migration behavior

This feature must be backward compatible.

## Existing users

Existing state.json files do not contain layout.

They must get the legacy default:

~~~text
horizontal
worktrees first
35 / 65
~~~

No migration command is required.

No startup prompt is required.

## New users

New users get the same default.

## Downgrade behavior

Because layout is stored as an additive JSON preference field, older Bonsai versions should ignore it when decoding if their existing decoder behavior permits unknown fields.

Do not change the state format in a way that makes older versions unable to read unrelated preferences.

---

# Error-handling rules

Layout preference errors are non-fatal UI preference errors.

They must not prevent Bonsai from opening.

Rules:

1. malformed layout preference → default layout;
2. impossible terminal geometry → safe clamped layout;
3. preference save failure → keep current session layout, show existing status/error feedback, do not crash;
4. no layout error may trigger Git/data reload;
5. no layout error may invoke server/daemon recovery behavior.

---

# Performance constraints

Layout resolution is tiny and should remain synchronous.

Do not add goroutines for pane geometry.

Do not add timers.

Do not add background workers.

Do not add channels.

Window resize can recompute geometry directly.

The geometry function should be deterministic and effectively allocation-light.

---

# Accessibility and terminal compatibility

Keep the feature usable without mouse input.

All layout configuration must be available through the keyboard-driven Preferences overlay.

Do not require Nerd Font glyphs for layout controls.

Use the existing terminal styling conventions.

Do not assume a specific terminal emulator.

Do not use pixel measurements.

All layout calculations are terminal-cell based.

---

# Acceptance criteria

The feature is complete only when every statement below is true.

## Layout

- A fresh user sees the same 35/65 horizontal worktrees-left layout as today.
- A user can place worktrees before or after workspace.
- A user can switch between horizontal and vertical layout.
- A user can choose a 20–80 relative split in 5-point steps.
- The chosen layout persists per user across restart.
- The preference is not written to .bonsai.yaml.
- Both existing panes are always present.
- No new pane exists.
- There is still exactly one cyclic workspace pane.

## Behavior preservation

- Worktree selection behavior is unchanged.
- Workspace tab behavior is unchanged.
- Shift+Tab tab cycling is unchanged.
- Git Log behavior is unchanged.
- Processes behavior is unchanged.
- Inspect behavior is unchanged.
- Diff behavior is unchanged.
- Checks behavior is unchanged.
- PR behavior is unchanged.
- Git actions are unchanged.
- Process actions are unchanged.
- CLI behavior is unchanged.
- Server/daemon/local API behavior is unchanged.

## Geometry

- Resizing the terminal never panics.
- No pane gets a negative dimension.
- No ready-state render exceeds terminal dimensions.
- The status bar remains full-width at the bottom.
- Process footer calculations remain correct.
- Workspace scrolling still works.
- Mouse-wheel routing follows actual pane position.

## Persistence

- Old state files load with the legacy default layout.
- Invalid layout state falls back safely.
- Saving layout does not erase theme, sort, editor, keys, PR status, or prune preference.
- Temporary resize clamps do not mutate persisted user preference.

## Scope

- No forbidden path is modified.
- No Git implementation file is modified.
- No server file is modified.
- No web file is modified.
- No new dependency is added.
- No new pane is added.

---

# Validation commands

Run from the repository root.

First, format changed Go files:

~~~sh
gofmt -w internal/ui internal/core/config/state.go
~~~

Then run focused tests:

~~~sh
go test ./internal/ui/...
go test ./internal/core/config/...
~~~

Then run the repository test suite:

~~~sh
go test ./...
~~~

Then vet:

~~~sh
go vet ./...
~~~

Finally inspect scope:

~~~sh
git diff --name-only refactor/local-api-security...HEAD
~~~

Every changed path must be in the whitelist in this document.

Also inspect the actual diff:

~~~sh
git diff refactor/local-api-security...HEAD --   internal/ui   internal/core/config/state.go   internal/core/config/state_test.go
~~~

Confirm manually that no Git operation, Git command construction, server behavior, or daemon behavior changed.

---

# Implementation stop conditions

Stop implementation immediately if any of these becomes true:

- a third pane appears necessary;
- a tab must become a top-level pane;
- a Git command must change;
- a server/daemon/local API change seems necessary;
- a CLI flag seems necessary;
- layout must be stored in .bonsai.yaml;
- internal/ui/cmds.go must change;
- internal/ui/keys.go must change;
- a new dependency seems necessary;
- a new persistence file seems necessary;
- a change outside the whitelist seems necessary.

Do not work around a stop condition by broadening the implementation silently.

Update the plan first and obtain explicit approval for the expanded scope.

---

# Explicit non-goals

This branch does not implement:

- new panes;
- multiple workspace panes;
- duplicate cyclic panes;
- independent panes for Log/Processes/Inspect/Diff/Checks/PR;
- pane visibility toggles;
- pane creation/deletion;
- arbitrary grid layouts;
- freeform X/Y pane placement;
- floating panes;
- overlapping panes;
- drag-and-drop pane movement;
- draggable splitters;
- mouse resize handles;
- layout presets beyond the legacy default/reset;
- per-project layouts;
- layout sync across machines;
- CLI layout configuration;
- .bonsai.yaml layout configuration;
- tab order customization;
- tab visibility customization;
- new keybindings;
- new Git features;
- Git refactors;
- server refactors;
- daemon refactors;
- web UI work.

These are separate future features and must not be pulled into this branch.

---

# Final design rule

The layout system owns only geometry.

The existing panes continue to own their existing content and behavior.

The existing Git integration continues to own Git.

The existing daemon/server stack continues to own daemon/server behavior.

The user may move and resize the two panes Bonsai already has, but this branch must not invent anything else.
