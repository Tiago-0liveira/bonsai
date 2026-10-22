# 05 / Optional assistance beside the draft

Status: planned.

## Deliverable

Integrate existing structured AI assistance into the manual workspace, keeping the
specification visible and editable throughout a short clarification conversation.

## Read only

Source: `internal/ui/gardenassist.go`, `internal/ui/gardenassist_view.go`,
`internal/ui/gardenassistreview.go`, `internal/core/garden/assist/`,
`internal/gardenbackend/assist.go`, `internal/ui/settings.go`.
Images: [question](../design/png/clarify-120x35.png),
[assistant options](../design/png/assistant-options-120x35.png),
[ready draft](../design/png/draft-ready-120x35.png).

Entry: the private drafting workspace can save versioned authored text and open an
optional side pane; existing structured-call provider isolation remains enforced.

## Specification

- Invite assistant opens compact account/model/effort controls using supported
  role capabilities and saved defaults. Disclose exactly what is sent; never read
  repository files during drafting or silently substitute a provider/model.
- Show understanding plus one focused question at a time, with up to three useful
  questions in a round. Keep suggested choices, own answer, notes and don't-know/
  defer behavior. Show why the decision matters on demand; avoid a mandatory quiz.
- Apply an answer or proposal explicitly to the same specification. Highlight the
  resulting changes and preserve direct edits made after a request began. Keep
  proposal suggestions distinct from confirmed user requirements.
- Block approval for unresolved scope/acceptance decisions. An advisory detail may
  be deferred or an assumption accepted only through a recorded user decision in
  the spec; skipped/unknown answers never resolve themselves or bypass validation.
- Persist confirmed conversation state through the private-draft backend; treat
  pending responses as transient. Scope replies to draft revision/request generation,
  preserve text on failure/cancel and offer explicit retry. AI-off remains complete.

## Checks and exit

Run targeted assist tests, then `go test ./internal/ui/... ./internal/core/garden/assist/... ./internal/gardenbackend/...`.
Cover direct-edit conflicts, repeated questions, unresolved/deferred decisions,
restart recovery, cancellation and late replies with fake providers. Demonstrate
side-by-side and compact navigation. No new provider integration or publication
logic is included.
