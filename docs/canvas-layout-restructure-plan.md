# Canvas Layout Engine Restructure Plan

## Branch strategy

This work must **not** be implemented directly on `feat/web-frontend-prototype`.

Create and use the dedicated branch:

```text
refactor/canvas-layout-engine
```

This branch is created from the current head of:

```text
feat/web-frontend-prototype
```

The base commit is:

```text
fb79a62 — fix(web): stop automatic canvas fitting after node changes
```

All canvas layout-engine restructuring, incremental placement work, tests, and follow-up fixes described in this document should happen on `refactor/canvas-layout-engine`. The existing `feat/web-frontend-prototype` branch should remain the stable source branch until the restructure is complete and validated.

---

## Goal

The canvas should feel spatially stable and deliberate.

The core UX rule after this restructure is:

> The canvas must never globally rearrange itself unless the user explicitly clicks **Auto-layout**.

Everything else should make the smallest possible spatial change.

That means:

- expanding a worktree stack should only affect that stack and the nodes that appear from it;
- detaching one worktree should not move unrelated branches;
- adding or stopping an agent should not move worktrees around;
- moving an agent into History should not trigger layout;
- CI, PR, label, or tag metadata changes should not trigger layout;
- dragging a node should never trigger Fit;
- manual node positions should remain authoritative;
- explicit Auto-layout may reorganize the entire graph and may Fit once at the end.

The existing forced Fit tied to node changes has already been removed on the base branch and must stay removed.

---

## Current problems

The current layout code couples graph topology, saved positions, automatic layout, and viewport behavior too tightly.

The main issues are:

1. Stack operations currently clear `nodePositions`, which destroys manual placement.
2. The canvas watches a broad graph shape key and runs `layoutGraph()` automatically when visible topology changes.
3. Expanding a stack replaces one stack node with several worktree nodes and therefore behaves like clicking Auto-layout.
4. Agents participate in the same recursive width calculation as worktrees, so one worktree with several agents can push unrelated branches sideways.
5. Layout dimensions are fixed estimates even though node heights can now vary.
6. There is no local placement strategy for newly appearing nodes.
7. There is no collision resolver for incremental changes.
8. Manual positions are not distinguished from generated positions.
9. The current layout tree mixes structural branch hierarchy and visual child content.
10. PR merge edges are visual relationships but can become entangled with hierarchy logic.
11. Layout triggers are too broad because visual metadata and topology are not clearly separated.
12. Auto-layout is not yet modeled around stable branch blocks.

---

# Phase 1 — Separate graph state from layout state

The first change should be architectural.

Do not treat `nodePositions` as a generic resettable bag of coordinates. Separate:

- graph topology;
- persisted/manual placement;
- generated placement.

Introduce placement metadata, for example:

```ts
interface NodePlacement {
  x: number
  y: number
  mode: 'manual' | 'generated'
}

nodePlacements: Record<string, NodePlacement>
```

A normal drag should write:

```ts
mode: 'manual'
```

Auto-layout should write:

```ts
mode: 'generated'
```

This distinction gives the layout engine enough information to preserve user intent.

### Required store changes

Replace or evolve:

```ts
nodePositions: Record<string, { x: number; y: number }>
```

into placement-aware state.

Add actions such as:

```ts
setManualNodePlacement(id, position)
setGeneratedNodePlacements(placements)
removeNodePlacement(id)
removeNodePlacements(ids)
```

Do not add any global `clear all positions` behavior to normal worktree operations.

---

# Phase 2 — Remove automatic global layout

The current behavior:

```text
graph shape changed
→ run layoutGraph()
→ rewrite many positions
```

must be removed.

Full Auto-layout should run only when:

1. a project is first opened and it has no usable positions;
2. the user explicitly clicks **Auto-layout**;
3. a future explicit **Reset layout** action is added.

It must not run when:

- a stack expands;
- a stack collapses;
- a worktree is detached from a stack;
- a worktree is marked never-stack;
- an agent starts;
- an agent stops;
- an agent is moved to History;
- an agent is restored from History;
- an agent is archived;
- CI status changes;
- PR status changes;
- tags change;
- branch labels change;
- a node is manually moved.

### Important

The existing change that removed forced Fit after graph changes must remain intact.

The viewport should not move just because graph content changed.

---

# Phase 3 — Build a branch-centric graph model

The layout engine should stop thinking in terms of a flat collection of generic React Flow nodes.

Create a branch layout model.

Suggested structure:

```ts
interface BranchBlock {
  worktreeId: string
  parentWorktreeId?: string
  canvasAgentIds: string[]
  childBlocks: BranchBlock[]
}
```

Conceptually:

```text
Project
  ├── Worktree A
  │    ├── Agent shelf
  │    └── Worktree C
  │         └── Agent shelf
  │
  └── Worktree B
       └── Agent shelf
```

The branch hierarchy must be derived from `mergeTargetBranch`.

Only actual branch relationships should determine worktree hierarchy.

Agents belong to their worktree block rather than acting like peer tree branches.

---

# Phase 4 — Introduce branch block geometry

Each worktree should own a visual block.

That block includes:

- the worktree card;
- its local agent shelf;
- embedded History area if expanded;
- child worktree blocks.

Example:

```text
        [ Worktree ]

   [ Agent ][ Agent ][ Agent ]

           ↓

   [ Child A ]   [ Child B ]
```

The block should calculate its own bounding box.

Suggested spacing constants:

```ts
const LAYOUT = {
  branchGapX: 72,
  branchGapY: 90,
  agentGapX: 14,
  agentGapY: 12,
  agentTopGap: 24,
  childTopGap: 70,
  projectTopGap: 90,
  collisionPadding: 28,
}
```

Keep these values centralized so the design can be tuned without rewriting algorithms.

---

# Phase 5 — Separate agent shelf layout from branch layout

Agents should no longer expand the global branch tree directly.

Lay agents out locally under their owning worktree.

Suggested behavior:

- 1 agent: centered below the worktree;
- 2 agents: one row of two;
- 3 agents: one row of three if width allows;
- 4–6 agents: two rows;
- larger sets: wrap into additional rows;
- archived/history agents are excluded from the canvas shelf.

Finished agents that are still in the explicit `canvas` presentation state remain in the shelf.

The branch block width becomes approximately:

```ts
Math.max(
  worktreeWidth,
  agentShelfWidth,
  childrenCombinedWidth,
)
```

The important difference is that agent width belongs only to its own branch block.

An unrelated sibling branch should not be pushed around because another branch gained several agents.

---

# Phase 6 — Use dynamic node dimensions

The current layout relies on fixed size estimates.

That is now too simplistic because:

- stack height changes with member count;
- worktree cards can expose History;
- agent cards can differ in content;
- future node variants may be denser or larger.

Create a geometry module that can calculate expected sizes.

Suggested API:

```ts
getNodeSize(node): Size
getBranchBlockSize(block): Size
getNodeRect(nodeId, placement): Rect
rectsOverlap(a, b, padding): boolean
```

Where possible, use known design dimensions.

If React Flow measured dimensions are available reliably, the engine can later use measured widths/heights as an enhancement.

For the first version, deterministic estimated dimensions are preferable to introducing asynchronous layout instability.

---

# Phase 7 — Make stack expand/collapse spatially local

Stack transitions need dedicated placement logic.

## Stack collapse

When several worktrees collapse into one stack:

1. Read current member positions.
2. Calculate their centroid.
3. Place the new stack near that centroid.
4. Save the member positions instead of deleting them.
5. Remove/hide only the now-invisible member placements from active rendering if necessary.
6. Do not move unrelated nodes.

## Stack expansion

When a stack expands:

1. Use the current stack position as the anchor.
2. Try restoring the previous member positions first.
3. If old positions are no longer usable, create a compact local arrangement around the old stack position.
4. Run local collision resolution only for those newly visible worktrees.
5. Leave all unrelated branches fixed.
6. Do not Fit.

Suggested compact patterns:

### Two worktrees

```text
[A] [B]
```

### Three worktrees

```text
[A] [B]
   [C]
```

### Four worktrees

```text
[A] [B]
[C] [D]
```

Larger stacks can wrap by row.

---

# Phase 8 — Make detach-from-stack local

Clicking the stack row `x` must not trigger global layout.

When one member leaves a stack:

1. Keep the stack exactly where it is.
2. Create/show the detached worktree next to the stack.
3. Prefer the nearest open position to the left or right.
4. Preserve that worktree's agents beneath it.
5. Resolve collisions only around the stack area.
6. Save the detached node placement as generated unless the user subsequently moves it manually.
7. Do not Fit.

This interaction should feel like pulling one card out of a group, not rebuilding the entire canvas.

---

# Phase 9 — Add local collision resolution

Create a dedicated collision resolver.

Suggested module:

```text
canvas/layout/collision.ts
```

Suggested interface:

```ts
resolveLocalCollisions({
  movingRects,
  fixedRects,
  preferredDirection,
  padding,
})
```

Algorithm:

1. Place new nodes at their preferred anchor position.
2. Build bounding rectangles.
3. Detect intersection with fixed nodes plus safety padding.
4. Shift the new block horizontally.
5. Prefer alternating directions around the anchor.
6. Repeat until the new block is clear.
7. Only move nodes involved in the local operation.

Use this for:

- expanding a stack;
- detaching from a stack;
- creating a worktree;
- restoring an agent to the canvas;
- adding a new agent when the shelf grows;
- changing merge target where a branch must move under a new parent.

Existing manual nodes must be treated as immovable obstacles during local placement.

---

# Phase 10 — Preserve manual placement

Manual dragging must become authoritative.

Normal node drag:

```text
drag node
→ save exact position
→ placement.mode = manual
```

Subtree grip drag:

```text
drag subtree
→ save all moved node positions
→ placement.mode = manual for moved nodes
```

Normal graph mutations should never overwrite manual placements.

This rule should apply even if:

- CI changes;
- an agent finishes;
- history changes;
- another stack expands nearby;
- a sibling gets a new agent.

Only explicit Auto-layout may replace manual positions globally.

---

# Phase 11 — Rewrite global Auto-layout

The **Auto-layout** button should become the one intentionally global operation.

It should:

1. Build the branch tree from merge targets.
2. Build branch blocks.
3. Calculate sizes bottom-up.
4. Sort siblings deterministically.
5. Position the project.
6. Attach the default branch on the left.
7. Attach `.env` on the right.
8. Position direct-main worktrees beneath the project.
9. Position nested worktrees below their parent worktree.
10. Lay out local agent shelves.
11. Run final overlap validation.
12. Save all generated placements.
13. Fit the viewport once at the end.

Auto-layout should be deterministic.

Given identical topology, running it twice should produce identical coordinates.

---

# Phase 12 — Stable sibling ordering

Sibling branches should not randomly change order.

Suggested ordering:

1. existing X coordinate when one exists;
2. tag name;
3. branch name;
4. ID as final stable tiebreaker.

This lets Auto-layout preserve some of the user's previous visual ordering while remaining deterministic.

---

# Phase 13 — Separate structural edges from PR edges

PR merge edges should not influence layout geometry.

The layout hierarchy should use branch parent relationships only.

For example:

```text
feat/child
mergeTargetBranch = feat/parent
```

determines spatial hierarchy.

The special dotted PR edge with the PR icon should be routed only after node positions are decided.

This keeps edge rendering independent from placement.

---

# Phase 14 — Split graph identity from render state

The existing broad shape key should not be used as an action trigger.

Introduce a structural topology key only if needed for memoization/debugging.

It should include:

- visible worktree IDs;
- visible stack IDs;
- worktree parent relationships;
- visible canvas agent IDs.

It should not include:

- CI status;
- PR check status;
- running/finished state unless visibility changes;
- labels;
- commit names;
- history counts;
- tag colors;
- status text.

More importantly:

> topology changes should still not automatically invoke global layout.

The key should describe state, not trigger behavior.

---

# Phase 15 — Create a placement coordinator

Move placement logic out of `BonsaiCanvas.tsx`.

Suggested structure:

```text
web/src/features/workspace/canvas/layout/
  graphModel.ts
  geometry.ts
  globalLayout.ts
  localPlacement.ts
  collision.ts
  stackPlacement.ts
  types.ts
```

Responsibilities:

## `graphModel.ts`

- Convert projects/worktrees/agents into branch hierarchy.
- Resolve merge targets.
- Produce stable parent/child relationships.

## `geometry.ts`

- Node dimensions.
- Branch block dimensions.
- Rectangle utilities.
- Centroid calculations.

## `globalLayout.ts`

- Explicit Auto-layout.
- Deterministic tree layout.
- Worktree block placement.
- Agent shelf placement.

## `localPlacement.ts`

- Add worktree.
- Add/restore agent.
- Merge-target changes.
- Nearest-free-space placement.

## `stackPlacement.ts`

- Stack collapse position.
- Stack expansion position.
- Detach one stack member.

## `collision.ts`

- Overlap detection.
- Collision-safe translation.
- Local empty-space search.

`BonsaiCanvas.tsx` should mainly:

- build React Flow nodes;
- build edges;
- apply saved placements;
- handle interactions;
- call placement services when needed.

It should not own the actual layout algorithm.

---

# Phase 16 — Visual hierarchy target

A clean Auto-layout result should resemble:

```text
[default] ─── [ project ] ─── [.env]

              │
       ┌──────┴──────┐
       │             │

   [worktree]     [worktree]
   [a] [a] [a]     [a] [a]

       │
   [nested wt]
     [a] [a]

       │
   [nested wt]
```

Nested worktrees should clearly sit under the worktree they merge into.

Agents should visually belong to their worktree.

Sibling branches should have obvious whitespace separating their visual blocks.

---

# Phase 17 — Viewport behavior

Viewport state must remain independent from graph placement.

Do not call Fit on:

- drag;
- drag stop;
- stack expand;
- stack collapse;
- detach;
- new agent;
- stopped agent;
- History change;
- CI change;
- PR change;
- tag update;
- merge-target update;
- local collision adjustment.

Fit is allowed only for:

- user clicking **Fit**;
- explicit Auto-layout completion;
- explicit Worktrees/Agents focus action;
- initial first-ever canvas initialization if necessary.

The base branch already contains a fix removing the automatic Fit tied to graph/node initialization changes. Do not reintroduce it.

---

# Phase 18 — First-load behavior

Initial project load is the one case where generated layout may run automatically.

Rules:

1. If the project has saved placements, use them.
2. If only some nodes have placements, preserve those and locally place missing nodes.
3. If no meaningful placements exist at all, run global generated layout once.
4. Fit once only on that first generated initialization.

Reloading a project with saved positions should not produce visible movement.

---

# Phase 19 — Merge-target changes

Changing a worktree's merge target changes hierarchy.

It still should not globally Auto-layout.

Suggested local behavior:

1. Keep the worktree branch block intact.
2. Move that block near/below its new parent.
3. Preserve child/agent relative offsets.
4. Run local collision resolution.
5. Mark the moved placement as generated.
6. Keep unrelated branches untouched.

If the user dislikes the result, explicit Auto-layout remains available.

---

# Phase 20 — New worktree placement

Creating a worktree should not trigger Auto-layout.

If merging into main:

- place it on the first branch row near the project;
- find the nearest open horizontal slot.

If merging into another worktree:

- place it beneath that parent;
- preserve enough space for the parent's agent shelf;
- resolve local collisions.

Do not Fit automatically.

The user should remain looking at the same canvas region unless the newly created worktree is intentionally selected and an explicit product decision later chooses to pan toward it.

---

# Phase 21 — Agent add/remove behavior

Starting an agent:

- add it to the local worktree shelf;
- recompute only that shelf;
- move only generated agents in that shelf if required;
- never move the owning worktree because one agent appeared;
- never move sibling worktrees.

Stopping an agent:

- keep it exactly where it is;
- only update visual state.

Moving to History:

- remove it from the shelf;
- compact only that local shelf;
- leave the worktree fixed.

Restoring from History:

- add it to the next open shelf position;
- resolve local shelf spacing only.

Archiving:

- remove it from the shelf;
- compact the shelf only.

---

# Phase 22 — Manual versus generated local children

A manually dragged agent should also be respected.

Possible rule:

- generated shelf agents participate in automatic local shelf compaction;
- manually dragged agents remain fixed;
- new generated agents occupy available shelf slots around manual agents.

This can be added after the branch-level placement architecture is working.

For the first implementation, it is acceptable for explicit worktree-local shelf refresh to regenerate agent placements while keeping worktree positions fixed, but branch positions must not move.

---

# Phase 23 — Debug tooling

Add development-only diagnostics to make layout work easier to tune.

Useful optional helpers:

- draw branch block bounds;
- draw collision rectangles;
- label manual/generated placements;
- show parent relationships;
- log local placement decisions.

These should be disabled in production.

A simple query param or development constant is enough.

---

# Phase 24 — Tests

Add unit tests before doing final visual tuning.

## Global layout tests

- two sibling worktrees never overlap;
- nested worktree appears below its parent;
- direct-main worktrees occupy stable sibling order;
- default branch stays left of project;
- `.env` stays right of project;
- Auto-layout is deterministic;
- two consecutive Auto-layout runs produce identical coordinates;
- PR edges do not affect placement;
- branch blocks do not overlap;
- a worktree with six agents remains inside its calculated block.

## Stability tests

- adding an agent does not change unrelated worktree positions;
- stopping an agent does not change any worktree position;
- moving an agent to History does not change any worktree position;
- changing CI state does not move nodes;
- changing PR state does not move nodes;
- manual drag survives metadata changes;
- viewport does not change after drag;
- viewport does not change after stack expansion.

## Stack tests

- collapsing a stack puts the stack near member centroid;
- expanding a stack restores prior positions when possible;
- expanding a stack never moves unrelated worktrees;
- detaching a stack member leaves stack position unchanged;
- detached node is placed in a nearby collision-free position;
- toggling a stack no longer clears all node placements.

## Local placement tests

- new main-target worktree is placed near project;
- new nested worktree is placed near its parent;
- merge-target change moves only the affected branch block;
- local collision resolver respects manual nodes.

## Playwright tests

- drag one worktree, unstack another group, confirm dragged worktree bounding box stays unchanged;
- click stack Expand and confirm unrelated worktree does not move;
- detach one stack member and confirm stack itself does not move;
- stop an agent and confirm worktree bounding box does not move;
- click Auto-layout and confirm nodes reorganize;
- click Auto-layout and verify Fit occurs once;
- drag after Auto-layout and verify no automatic Fit occurs;
- reload and verify saved placements survive.

---

# Suggested implementation commits

Keep the restructure broken into focused commits.

## Commit 1

```text
refactor(web): decouple canvas topology and placement
```

Scope:

- placement metadata;
- remove implicit full-layout triggers;
- stop clearing placements from stack actions;
- preserve manual coordinates;
- keep viewport independent.

## Commit 2

```text
feat(web): add branch-block canvas layout engine
```

Scope:

- graph model;
- branch blocks;
- geometry;
- agent shelves;
- deterministic global Auto-layout.

## Commit 3

```text
feat(web): add local canvas placement and stack transitions
```

Scope:

- stack centroid collapse;
- stack expansion;
- detach behavior;
- local collision resolver;
- new worktree placement;
- merge-target local relocation.

## Commit 4

```text
test(web): cover canvas layout stability
```

Scope:

- layout unit tests;
- store placement tests;
- Playwright spatial regression coverage.

---

# Acceptance criteria

The restructure is complete when all of the following are true:

- Expanding a stack does not trigger global Auto-layout.
- Detaching one worktree does not move unrelated nodes.
- Collapsing a stack does not wipe node placements.
- Adding an agent does not move sibling worktrees.
- Stopping an agent does not change placement.
- Moving an agent into History does not change branch placement.
- Manual drag coordinates survive normal graph updates.
- Dragging never causes automatic Fit.
- Stack operations never cause automatic Fit.
- Metadata changes never cause automatic Fit.
- Explicit Auto-layout produces a clean, deterministic hierarchy.
- Explicit Auto-layout may Fit exactly once at the end.
- Nested worktrees clearly appear beneath their merge target.
- Agent shelves stay visually associated with their worktree.
- Worktree branch blocks do not overlap.
- PR merge edges remain visual overlays and do not distort layout.
- Reloading preserves saved manual positions.
- All typecheck, unit, build, and Playwright jobs pass.

---

## Final implementation rule

The guiding rule for all future canvas work should be:

> **Global layout is user-initiated. Local graph changes use local placement. Manual positions are preserved. Viewport movement is never a side effect of ordinary node changes.**
