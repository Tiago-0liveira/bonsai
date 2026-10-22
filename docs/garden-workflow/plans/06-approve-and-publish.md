# 06 / Approve a draft and create its Issue

Status: planned.

## Deliverable

One explicit, reviewable operation publishes an exact private draft and records
verified specification acceptance in the selected Garden's GitHub repository.

## Read only

Source: `internal/core/garden/capture/`, `internal/core/garden/approve/`,
`internal/core/garden/ops/`, `internal/gardenbackend/capture.go`,
`internal/gardenbackend/validate.go`, `internal/ui/gardenvalidate.go`,
`internal/ui/gardenidea.go`.
Image: [publication preview](../design/png/publish-120x35.png).

Entry: private drafts have CAS revisions and a publication identity; existing
capture, spec revisions, approval verification and unknown-outcome recovery work.

## Specification

- Preview exact repository/actor, local draft revision, title/type, specification
  and managed labels. Primary action: Approve specification & create issue. Back
  returns to editing. Recheck connection, Write access, readiness and exact content
  at confirmation; changed inputs invalidate the preview.
- Reserve a durable operation bound to Draft ID/revision, target and content hash
  before effects. Serialize cross-process submissions. Reuse canonical protocols:
  create the Issue at Draft, append the spec, record its approval, then project the
  accepted stage. Never infer acceptance merely from labels or rewrite issue bodies.
- Each effect is recoverable and content/identity verified. An unknown create or
  comment outcome reconciles before retry; reuse an existing result rather than
  creating another Issue, revision or approval. Partial success retains its Issue
  link and says Publishing incomplete, not Private or Published.
- After read-back verification, move the same Issue into accepted work/Backlog
  and mark the local draft published. Preserve provenance and the original text;
  user archive remains explicit. No planner, worktree, agent or PR starts.
- Preserve old managed GitHub drafts and their direct spec-validation route.
  Publication is additive; do not migrate/relabel existing backlog items.

## Checks and exit

Test each partial failure/crash, unknown outcome, duplicate confirmation, concurrent
process and edited-preview case through fake GitHub and temporary stores. Run
`go test ./internal/core/garden/... ./internal/gardenbackend/... ./internal/ui/...`
and race checks for orchestration. Stop at one verified Issue/spec approval per
operation and a recoverable UI; no automatic planning.
