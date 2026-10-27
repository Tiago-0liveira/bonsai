# 05 / Optional assistance beside the draft

Status: done.

Completion note: **Invite assistant…** / **Discuss with AI…** / `Ctrl+G` open an
invitation (the shared account/model/effort dialog, at most three questions per round,
exact disclosure) and then a discussion pane beside the draft (a tab on compact screens)
in `internal/ui/gardenprivateassist{,_view}.go`; it reuses the Issue assistant's options,
answer panel and review state. Requests for a private draft carry
`gardenapi.PrivateDraftRef` (draft ID, base revision, request generation) through the same
`AssistQuestions`/`AssistDraft` path (`gardenbackend/assist.go`: stored, unarchived,
unpublished draft; read access suffices; no GitHub call). `drafts.Draft.Decisions`
(`deferred`/`assumed`, advisory only) is persisted beside `ConfirmedAnswers`, and
`drafts.Assess` counts a deferral only when the spec carries its exact recorded line.
`assist.AdvisoryArea` decides which questions may be deferred. Checks:
`go test ./internal/ui/... ./internal/core/garden/... ./internal/gardenbackend/...`, race on
drafts and gardenbackend, and a tmux session at 80×24, 120×35 and 160×48 against fake
providers. Limitations: one section is editable at a time beside the pane (others are
previews); pending questions and proposals are memory only (skipped/unknown answers become open Questions lines when applied); Review & create issue is still
a read-only preview; no new provider integration or publication logic.

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
