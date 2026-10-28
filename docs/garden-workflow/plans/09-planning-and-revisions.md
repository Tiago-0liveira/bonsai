# 09 / Planning, plan review and focused feedback

Status: Done.

## Deliverable

Make the existing read-only planner and revision-bound decisions usable from the
ticket overlay without reconstructing session settings for each revision.

## Read only

Source: `internal/ui/gardenplanner.go`, `internal/ui/gardenplanner_view.go`,
`internal/ui/gardenplan.go`, `internal/ui/gardenplan_view.go`,
`internal/core/garden/plan/`, `internal/core/garden/planrev/`,
`internal/gardenbackend/planner.go`, `internal/gardenbackend/planreview.go`.
Images: [planning](../design/png/planning-120x35.png),
[review](../design/png/plan-review-120x35.png),
[feedback](../design/png/plan-feedback-120x35.png).

Entry: the overlay host can display real planner attempts and documents; canonical
spec acceptance and exact plan/spec approvals remain enforced.

## Specification

- Approved work offers a previewed read-only planning request. Reuse supported
  account/model/effort and native capability checks; keep checkout/base and dirty
  read consent explicit. Leaving the overlay never stops the holder.
- Show actual elapsed time, latest reported activity, needs-input and plan-file
  state. Do not infer a finished plan from terminal prose or idle status alone.
  Plan publication remains an explicit preview of exact text and source spec.
- Open the current reviewable candidate by default. Summarize approach, changes
  from its predecessor, risks and verification; retain full document navigation,
  comparisons and history. Keep latest, current and approved snapshots distinct.
- Request changes captures feedback against one exact revision. Send feedback &
  revise plan explicitly records that decision and requests one local attempt with
  the retained supported settings. Journal/reconcile partial success; reopening or
  rereading feedback never starts another attempt. Plain GitHub comments are hints.
- Approve names the exact current plan/spec pair and grants Ready without coding.
  Changed/revoked spec or plan explains the stale gate, retains history and requires
  another deliberate decision; no automatic rebasing of approval.

## Checks and exit

Run `go test ./internal/core/garden/plan/... ./internal/core/garden/planrev/... ./internal/gardenbackend/... ./internal/ui/...`
and affected race tests. Cover feedback/start partial failures, duplicate actions,
late replies, moved heads and keyboard/scroll at narrow sizes. Stop at usable
planning/review; no implementation start or automatic reviewer launch changes.

## Implementation plan

### Source capability found (verified in code)

- `plan.Service` (`internal/core/garden/plan/service.go`) previews, starts, observes,
  cancels and reconciles read-only planner attempts. `Attempt` records provider,
  account, model, effort, spec, base, dirty consent and plan path. `Deps.Feedback`
  injects the reason of the *current* changes-requested revision into every start;
  it is not bound to an exact revision or decision and the predecessor plan text is
  not in the prompt.
- `planrev.Service.Decide` records approve/changes-requested/reject against one exact
  revision hash + spec fingerprint, replays identical decisions and verifies after
  writing. `planrev.Review` keeps Latest/Current/Approved apart; `Diff`, `Collapse`,
  `Parse` and `Required` sections exist.
- Published revisions do not name their attempt. `Attempt.PlanPublishedAt` marks
  publication locally but not which revision hash it became.
- UI: the planner hub/session (`gardenplanner*.go`) and plan review
  (`gardenplan*.go`) are separate full-body screens; the overlay closes while they
  are active. The overlay Plan section (`gardenDocument` case 3,
  `gardenboard_view.go`) is a static revision list; overlay controls
  (`issueControls`) are static (`plan`, `review`, `start`, `attempts`).
- `gardenapi.LocalRunSummary` (board local work) has attempt ID, state, live,
  activity, plan-file state and update time only.

### Gaps this phase closes

1. Overlay Plan tab shows real planner/plan-review state and contextual actions.
2. A "revise" attempt bound to one exact revision + one exact changes-requested
   decision, carrying that feedback and the predecessor plan text, started with the
   source attempt's retained settings, idempotent per decision.
3. Partial success (decision recorded, attempt not started) is derived and shown,
   with an explicit retry; nothing restarts on reopen/refresh.

### Step 1: core `plan` (go-backend)

- `Attempt`: add `PublishedRevision string` (`json:"published_revision,omitempty"`)
  and `Revises *RevisionRef` (`json:"revises,omitempty"`) where
  `RevisionRef{RevisionHash string; Ordinal int; DecisionCommentID int64}`.
  Old records stay valid (both optional); `valid()` requires a non-empty hash and a
  non-zero comment ID when `Revises` is set.
- `MarkPublished(ctx, id, revisionHash string)`: also stores the hash (idempotent;
  never overwrites a different stored hash; fills an empty one). Update the one caller
  in `gardenbackend/planreview.go` to pass `PublishView.Hash`.
- `StartRequest`: add `Revise *ReviseRequest{RevisionHash string; DecisionCommentID
  int64}`. Replace `Deps.Feedback` with a resolver bound to the ref, e.g.
  `Deps.Feedback func(ctx, approve.Target, *ReviseRequest) (Feedback, error)` where
  `Feedback{Reason, PlanText string; Ordinal int}`; nil ref returns empty (a plain
  Request plan no longer silently picks up the latest reason; plain GitHub comments
  stay hints). For a revise start the resolver must verify, from one fresh gate read:
  the revision exists, is trusted, not stale, not superseded, is `Current`, its latest
  non-revoked decision is changes-requested with exactly that comment ID. Otherwise a
  typed sentinel (`ErrFeedbackChanged`) in `errors.go`.
- Prompt: for a revise start append the quoted predecessor plan (`planrev`-composed
  content of that revision) and the quoted reason, both labelled untrusted review
  context, inside `MaxPromptBytes` (`ErrSpecTooLarge` when over). Keep `cleanLine`
  for the reason only where a single line is required; multi-line reason must keep
  newlines but strip control/format runes except `\n`.
- Idempotency inside `Start` under `s.mu`, before anything is recorded: if the store
  already has an attempt for this Issue whose `Revises.DecisionCommentID` matches and
  whose state is not `failed`, return `ErrRevisionRequested` plus that attempt (add a
  small typed error carrying the attempt ID). A `failed` one allows an explicit retry.
  Also refuse a revise start while any planner attempt of the Issue is live.
- New `Service.SourceAttempt(ctx, issue, revisionHash) (Attempt, bool, error)`: the
  attempt whose `PublishedRevision` equals the hash (newest wins). Used to retain
  settings.
- Tests (table, temp dirs, fakes): resolver mismatch cases, duplicate request
  returns existing attempt, failed-then-retry allowed, live attempt blocks, prompt
  contains predecessor + reason and is bounded, old JSON without new fields loads,
  `MarkPublished` hash semantics, crash between record and session still reconciles.

### Step 2: backend + `gardenapi` contract (go-backend)

- `gardenapi.LocalRunSummary`: add `Provider`, `AccountName`, `Model`, `Effort`
  strings, `RequestedAt time.Time`, `Revises *plan.RevisionRef`. Fill them for
  planner summaries in `gardenbackend` local-work (no tokens, prompts, env or output).
- New contract types in `gardenapi.go`:
  - `PlanReviseRequest{Stamp; IssueID; RevisionHash; Fingerprint; SpecHash;
    ActorID; Reason string; Settings PlanReviseSettings; IncludeDirty,
    AcceptReadOnly bool; Base plan.Base; Cols, Rows int}` and
    `PlanReviseSettings{AccountID agents.AccountID; Model, Effort string}`.
  - `PlanReviseView{Stamp; IssueID; Decision PlanDecisionView; Attempt
    *plan.Attempt; StartErr *Error; Existing bool}`.
  - `PlanRevisePreviewView{Stamp; IssueID; Revision planrev.Revision;
    Settings *PlanReviseSettings; SettingsSource string /* attempt ID */;
    SettingsUnavailable string; Base plan.Base; Pending *PendingRevise;
    Unavailable string}` where `PendingRevise{DecisionCommentID int64; Reason string;
    Attempt *plan.Attempt}` describes a recorded changes-requested decision on the
    current revision and the revise attempt for it (nil Attempt = not started).
- Backend methods (add to `gardenapi.Backend`, implement in
  `internal/gardenbackend/planreview.go` or a new `planrevise.go`):
  - `PreviewPlanRevise(ctx, issue)`: fresh verified review read + planner preview
    (base inspect) + `SourceAttempt` for `Current`. Settings retained only if the
    source account still exists and is read-only/plan-file capable (reuse
    `plan.Agents.Account`); else `SettingsUnavailable` with a reason, so the UI falls
    back to the normal planner account dialog. Derive `Pending` from the current
    revision's latest changes-requested decision and the attempt store. Reading only.
  - `RevisePlan(ctx, req)`: (a) `planrev.Decide` changes-requested with the reason
    (replays if already recorded); (b) on success `plan.Start` with `Revise` ref =
    that revision hash + decision comment ID, retained settings, the previewed base
    and dirty consent. Return both outcomes. Decision unknown outcome →
    `Decision.State=unknown`, no start. Start failure after decision → decision done,
    `StartErr` set, no rollback. `ErrRevisionRequested` → `Existing=true` with that
    attempt, not an error.
  - `StartPlanRevise(ctx, req)`: the explicit retry for a pending decision: same as
    (b) only, requiring the decision comment ID to be the pending one.
- `fake` backend / harness updates so UI tests can drive these.
- Tests in `internal/gardenbackend`: decide ok + start ok; decide ok + start fails
  (pending shown on next preview, no auto start on preview/refresh); decide unknown;
  duplicate `RevisePlan` returns existing attempt; heads moved between preview and
  revise; spec changed → refused; retained settings with deleted account →
  `SettingsUnavailable`; late reply stamps ignored by generation. `-race` on the
  package.

### Step 3: overlay Plan tab and actions (tui)

All reads via `tea.Cmd` over `gardenapi`; replies scoped by repo, Issue and request
generation (existing `Stamp` pattern); keep last good data on error.

- Plan tab body (replace `gardenDocument` case 3) renders one of, from board data
  (`Verification` via `planrev.FromVerification`, `LocalRunSummary`) plus the
  on-demand `PlanRevisePreviewView` loaded when the Plan tab is shown or the overlay
  opens on it (a read; never starts anything):
  - Spec not approved / stale: why planning is unavailable; history retained.
  - Live planner: provider / account · effort · elapsed since `RequestedAt` (render
    time only; no stored ticking), latest reported activity text and age, needs-input
    when activity says waiting, plan-file state (none/writing/ready/invalid), "read-only
    access". No step checklist or inferred progress; idle ≠ finished. Footer: "Leave
    this overlay and planning keeps running." Actions: `Open planner` (existing
    session body), `Stop attempt…` (existing cancel with confirmation).
  - Ended with ready, unpublished plan file: actions `Publish plan…` (existing
    compose → preview of exact text + source spec → confirm) and `Open planner`.
  - Current reviewable revision awaiting decision (default view): heading
    `PLAN rN / NEEDS YOUR APPROVAL`; summary from `Parse`: Summary (or first Steps
    lines), `CHANGED SINCE rM` from `Diff`+`Collapse` against its predecessor
    (green +, red −, words/symbols too), Risks, Tests. Left facts: spec sN state,
    Latest / Current / Approved kept distinct, revision chips. Actions:
    `Approve plan rN…`, `Request changes…`, `Open review` (existing full screen for
    whole document, comparisons, search, history).
  - Changes requested with pending revise (no attempt / failed attempt): "Feedback
    recorded on rN. No revision attempt is running." + reason; action
    `Start revision attempt…` (explicit `StartPlanRevise`, same confirmation as the
    form). With a revise attempt live: the live planner view, noting it revises rN.
  - Approved current pair: "Approved rN against sM. Ready; coding starts only when you
    choose." Stale approval: explain which side changed (spec or plan), keep
    snapshot visible, offer the deliberate path (Open review / Request plan), never
    auto-rebase.
- `issueControls` becomes contextual for the Plan tab (keep `close`, `start`,
  `attempts` and their existing rules; plan actions replace the static `plan`/
  `review` when the Plan tab is active, static ones remain on other tabs). Disabled
  actions stay visible with reasons. Keyboard shortcuts, Tab focus and clicks resolve
  through `issueActivate` as today.
- Approve: fresh `PlanReview` read, then the existing exact confirmation
  (`gardenPlanStartDecide(DecisionApproved)` semantics: names plan rN + spec sM +
  actor; "Coding starts only when you choose"). Render it inside the overlay body;
  moved heads → refusal text + reload, no write.
- Request changes form (overlay body, mock `plan-feedback`): title "Request changes
  to plan rN", subtitle "Feedback stays attached to this exact plan revision.",
  multi-line `textarea` (bounded by `planrev.MaxReasonBytes`, required), line
  "Revise with <provider> / <account> · <effort>" from retained settings (or "Choose
  account…" opening the existing planner account dialog when unavailable), dirty
  checkout consent toggle when the base is dirty, "The new attempt receives this
  feedback and plan rN." Footer "Starts one read-only revision attempt. Nothing
  starts coding." Actions `Back`, `Send feedback & revise plan`. Submit is disabled
  while a write is in flight (no double submit). Typed text survives Back/Esc and
  tab switches for that revision; it is dropped if the revision under review changes
  (say so).
- Result handling: done+started → notice and switch the Plan tab to the live view;
  decision done + start failed → pending state with the failure text; decision
  unknown → "may be recorded; reopen to reconcile" and reload; `Existing` → show that
  attempt. Reopening the overlay or rereading never calls `RevisePlan`/`Start`.
- Esc from the planner/review full screens opened from the overlay returns to the
  overlay with the Plan tab selected and scroll restored.
- Narrow sizes: at 80×24 the summary drops the left facts rail (existing `rail`
  geometry), body scrolls with ↑/↓, actions use short labels; ASCII glyph mode works.
- Tests (`internal/ui`, fake backend): each Plan tab state renders at 80×24, 120×35,
  160×48 with no row overflow; control focus/Tab/click hits; form keeps text, refuses
  empty reason, single submit under repeated Enter; late `PlanRevisePreviewView` /
  `PlanReviseView` for another Issue or older generation ignored; partial failure
  shows pending + explicit retry; reopen does not start anything (assert fake call
  counts); moved heads on approve shows refusal; elapsed/activity shown only from
  reported data.

### Step 4: docs

Update `docs/garden.md`, `docs/tui-cli.md` (new keys/actions) and the root
`README.md` Garden line if behavior summary changes. Then set this plan's status to
Done with a short completion note (files, checks, limitations) and the index row in
`docs/garden-workflow/README.md`.

### Out of scope

Implementation start, review policy, automatic reviewer launch, web UI, migration of
existing attempts beyond optional new fields, inferring progress from terminal text.

## Completion

Implemented the overlay Plan tab and its actions on top of the Step 1-2 core and backend work
(`plan.Service` revise attempts, `RevisionRef`, `PublishedRevision`; `PreviewPlanRevise`,
`RevisePlan`, `StartPlanRevise`). The tab derives one of nine states (unavailable, none, live
planner, ready unpublished plan, revision awaiting decision, pending feedback, changes
requested, approved, stale) from verified board data, the local planner summary and an
on-demand revise preview. Elapsed time is measured at render from `RequestedAt` through
`agentNow`; activity and plan-file state are shown only as reported, idle is never finished,
and no checklist or progress is inferred. Approve rereads the review and shows the existing
exact confirmation inside the overlay; Request changes is an in-overlay form (required
multi-line reason, retained account or the planner account choice, explicit dirty-checkout
consent) whose single submit records changes requested and starts one read-only attempt.
Partial success shows pending feedback with an explicit `b` retry; an unknown decision
starts nothing and keeps the text. Typed feedback survives Back, Esc and tab switches for
the same revision and is dropped with a notice when the revision changes. Reading, reopening
or rereading never calls a write. Esc from the planner session, publish box or review opened
from the tab returns to it with the scroll restored. A plain Request plan no longer carries
earlier feedback (documented).

Files: `internal/ui/gardenplantab.go`, `gardenplantab_view.go`, `gardenplantab_test.go`;
`internal/ui/{gardenoverlay,gardenboard,gardenboard_view,gardenlocal,gardenplanner,gardenplan,gardensetup,garden,shell,agentlaunch,agentlaunch_view}.go`;
`internal/ui/gardenapi/fake/{fake,planreview}.go`, `gardenapi/gardenapi.go`;
`internal/ui/gardengatetwo_test.go` (Plans section text). Core and backend:
`internal/core/garden/plan/`, `internal/gardenbackend/{planner,planreview,planrevise,errors}.go`.
User documentation: `README.md`, `docs/garden.md` (Plan tab), `docs/tui-cli.md` (overlay keys)
and the workflow index.

Checks: `gofmt -l $(git ls-files '*.go') $(git ls-files --others --exclude-standard '*.go')`
(empty), `git diff --check`, `go vet ./...`, `go build ./...`, `GOOS=windows go build ./...`,
`go test -count=1 ./internal/ui/... ./internal/core/garden/... ./internal/gardenbackend/...`,
`go test ./...` and `go test -race -run TestPlanTab ./internal/ui/`, all passing. The tab
tests cover each state at 80x24, 120x35 and 160x48 in Unicode and ASCII with exact row widths,
control focus/Tab/click routes, the form, single submit, partial failure and retry, late and
foreign replies, moved heads, and that reading never calls `RevisePlan`, `StartPlanRevise`,
`StartPlanner` or `DecidePlan`.

Remaining limitations: the left facts column of the mock is the existing summary rail (from
about 100 columns) with a PLAN block, and a compact facts line in the body at narrow sizes. The
reason row under the actions is one line, so long notices are repeated in full at the top of
the Plan tab. The planner account choice reused for "Choose account…" keeps its own layout and
sends the feedback on its primary action. The Plan tab does not show plain GitHub comments
beyond the existing review hint. Account names in the revise line come from one fast account
list read and fall back to the account ID. The board's local badge still reads "planner
working" for a live planner that reports no activity; the Plan tab says "not reported".
