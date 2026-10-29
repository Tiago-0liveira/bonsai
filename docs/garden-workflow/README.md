# Garden workflow implementation

Branch: `feat/garden-workflow`, created from `feat/tui-redesign` at
`6ea2e7ea535d34c55dbe5849657e2203b9b798dc`. Phases 01–11 and 13 are
complete; phase 12 is planned. Existing Garden services and the opt-in redesign are
the baseline. Earlier Garden features provide capabilities for later phases, but
do not by themselves satisfy those phases' deliverables and exit criteria.

[Image gallery](design/index.html) · [Overview](design/overview.png) ·
[Design notes](design/README.md). The design folder contains all **44 PNG/SVG
pairs**, the manifest, offline gallery and generators. Mock data is illustrative.

## Agent rules

1. Read the repository and applicable area `AGENTS.md`, this README and **only the
   assigned plan**. Open its named source seams and images on demand. Do not load
   the other plans or the historical plan archive as background context.
2. Execute in the index order. Each plan states its required **source capability**;
   verify that capability in code. If missing, report the concrete missing
   interface/behavior; do not read sibling plans or invent a replacement subsystem.
3. Implement one plan's deliverable and stop at its exit criteria. No opportunistic
   features, framework upgrades, broad refactors, web redesign or legacy-TUI cutover.
   Update only the assigned plan's status and a short completion note with files,
   checks and remaining limitations. Do not claim acceptance from a screenshot alone.
4. Use Go, Bubble Tea v1 and Lip Gloss v1. Terminal dimensions and ANSI/grapheme
   widths govern rendering; PNG pixel dimensions and fonts are references only.
   Support configured themes, ASCII glyphs and 80×24, 120×35 and 160×48 layouts.
5. UI code performs no file/process/Git I/O: use commands and injected core/backend
   services. Reuse account isolation, holders, terminals, daemon Git authority and
   atomic locked stores. Never touch real provider homes, refresh tokens or expose
   secrets. Tests use fakes and temporary homes/repos, never live providers/GitHub.
6. Private authored drafts use a dedicated protected store, never sanitized UI or
   browser warm-start caches. After publication, verified GitHub records remain
   canonical. Keep existing managed GitHub drafts usable; do not migrate them
   silently or weaken revision-bound approvals.
7. Reading/navigation never launches work. Spec approval, plan approval, starting
   implementation and external publication remain distinguishable decisions.
   Automatic review requires explicit prior authorization of that bounded sequence.
   Preserve native permission acknowledgments, operation locks and unknown-outcome
   reconciliation. No automatic merge or cleanup.
8. Keep last good data after failed/incomplete reads. Scope async results to the
   repository, item, request generation and attempt. Missing activity is unknown,
   never invented progress or success. Input belongs to the overlay or explicitly
   controlled terminal; preserve `Ctrl+]` as the terminal escape.

## Ordered index

The index is the only place that links plans. Individual plans contain no sibling
plan references; their entry contracts describe code, not reading dependencies.

| Order | Plan | Status | Bounded result |
|---|---|---|---|
| 01 | [Overlay renderer](plans/01-overlay-renderer.md) | Done (`1e45960`) | Actual board + modal composition in a terminal |
| 02 | [Ticket overlay host](plans/02-ticket-overlays.md) | Done (`426c0e8`) | One item shell, focus and action routing |
| 03 | [Private draft storage](plans/03-private-draft-storage.md) | Done (`1e8b722`) | Durable local drafts with stable IDs and conflict protection |
| 04 | [Manual drafting](plans/04-manual-drafting.md) | Done (`8189081`) | Complete no-AI capture/editor/inbox |
| 05 | [AI drafting](plans/05-ai-drafting.md) | Done | Optional discussion beside the same specification |
| 06 | [Approve and publish](plans/06-approve-and-publish.md) | Done | Exact, recoverable GitHub creation and spec acceptance |
| 07 | [Board and live state](plans/07-board-and-live-state.md) | Done | Six phases plus detailed stages and truthful activity |
| 08 | [Tables and attention](plans/08-tables-and-attention.md) | Done | Shared row/grouped views and Needs you |
| 09 | [Planning and revisions](plans/09-planning-and-revisions.md) | Done | Focused plan reading, comparisons and feedback |
| 10 | [Review policy](plans/10-review-policy.md) | Done | Explicit account/effort/permission policy and durable review identity |
| 11 | [Launch and supervision](plans/11-launch-and-supervision.md) | Done | Previewed implementation + review intent and real run controls |
| 12 | [Bounded review execution](plans/12-bounded-review-execution.md) | Planned | Detached one-pass review, fixes and verification |
| 13 | [Human handoff and completion](plans/13-human-handoff.md) | Done | Evidence, explicit rework and verified completion |

## Verification

Run the assigned plan's focused checks, then the repository's required Go gates:

```sh
gofmt -l $(git ls-files '*.go')
go vet ./...
go build ./...
go test ./...
```

Formatting must print nothing, including newly added Go files before staging.
Add `go test -race` for affected concurrent/agent packages. Keep plain Go builds
independent of Node. Actual behavior changes update root `README.md` and relevant
user docs. Rendering changes require a real TUI demo plus bounded cell/focus tests;
SVGs or matching screenshots alone do not prove terminal behavior.

For this documentation package, verify paths, image/checksum integrity and absence
of cross-plan references. No application tests are required until code changes.
