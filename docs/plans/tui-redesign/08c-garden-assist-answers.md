# Plan 08c — Assist answer UX and blocker fixes

Status: implemented (C1–C4).
Trigger: Issue #115. After answering every question, validation still listed 9 open
questions, and nobody had skipped any.

## Problems

1. **Literal digits.** `1`–`3` pick a suggestion only on the question rail. In the
   answer editor they are typed as text, so "1" was sent as the answer and the model
   flagged it as ambiguous (`internal/ui/gardenassist.go:1236`).
2. **No clear choice vs. free text.** Suggestions are plain lines above one text box.
   It is unclear whether a suggestion is selected, and a choice cannot carry notes.
3. **Suggestions block approval.** Unconfirmed suggestions are rendered into Open
   questions (08b decision D4). Review offers no way to confirm or drop them, so each
   one blocks gate one (`internal/core/garden/assist/render.go:55`).
4. **Unbounded AI open questions.** The draft prompt allows any number of new open
   questions. The model adds doubts about answers it already received. The person
   cannot answer them inside the assistant.

## Goals

- One answer panel per question, with a visible split between **choosing** an option
  and **writing** an answer.
- A choice can carry optional notes. Skip and "I don't know" are explicit options.
- Only confirmed suggestions are saved. Open questions added by the AI are few and
  answerable in place.
- Gate-one rules stay unchanged. Skipped and unknown questions still always produce an
  Open questions line.

Non-goals: multi-select answers, new providers, changes to the gate-one rules.

## Answer model

Change `assist.Answer` (`internal/core/garden/assist/assist.go`):

```go
type Answer struct {
	QuestionID, Question string
	State                AnswerState // open, answered, skipped, unknown
	Choice               string      // chosen suggestion text; "" when none
	Text                 string      // own answer, or notes on Choice / unknown
}
```

| Selection | State | Choice | Text | Valid when |
|---|---|---|---|---|
| A suggestion | answered | its text | notes (optional) | always |
| Write my own answer | answered | "" | answer | Text is not empty; otherwise open |
| I don't know | unknown | "" | notes (optional) | always |
| Skip | skipped | "" | "" | always |
| Nothing | open | "" | "" | — |

Input JSON `answered[]` gains `choice` (`{"id","question","status","choice","text"}`).
Schema stays `bonsai.garden.assist.input/v1`; the field is additive.

## TUI: answer panel

The right pane of the Questions stage has three focus zones: **rail**, **options**
and **text**. The text field's label follows the selection.

```
 Q2 of 5 · behavior
 When the export fails, what do you see?
 Why: decides whether the fix reports failures.
 Choose one
  (•) An error appears
  ( ) It looks successful
  ( ) The download never starts
  ─────────────
  ( ) Write my own answer
  ( ) I don't know
  ( ) Skip for now
 Notes (optional)                       ← "Your answer" for Write my own;
 ┌───────────────────────────────────┐    hidden for Skip
 │ only on files over 50 MB          │
 └───────────────────────────────────┘
```

Keys:

| Zone | Keys |
|---|---|
| Any zone | `tab` / `shift+tab` cycle rail → options → text; `pgdown` / `pgup` next / previous question; `ctrl+s` Draft; `esc` back one zone, then leave |
| Rail | `↑/↓` question, `enter` → options |
| Options | `↑/↓` move; `space` or `enter` select; `1`–`9` select that row directly; selecting moves focus to text, except for Skip |
| Text | printable keys and textarea bindings only; digits are text |

Notes:

- A question without suggestions opens with focus on text and "Write my own answer"
  preselected.
- Rail glyphs: `●` chose an option, `✎` wrote own, `?` don't know, `-` skipped, `·` open.
  ASCII: `o w ? - .`. A choice with notes shows `●+`.
- Switching from a suggestion to Write my own copies the notes into the answer field,
  so no text is lost. Switching back keeps the text as notes.
- At 80×24 the option rows truncate to the 59-column pane. If the panel is taller than
  the body, the question text scrolls and the options and text field stay visible.

## Prompt and decode changes (`assist/prompt.go`, `decode.go`, `render.go`)

- **Draft prompt, answers.** "An answer has `choice` (the option the person picked)
  and/or `text` (their own answer, or notes on the choice). Notes refine the choice. If
  notes contradict the choice, add an open question. Never treat a bare digit or a
  vague reply as a choice."
- **Draft prompt, open questions.** "Add at most 3 new open questions, and only for
  uncertainties that change scope or acceptance and that no answer settles. Do not
  restate an answer as a question."
- Decode: keep at most 3 new model open questions, in reply order, and count the rest
  as dropped. Carried skipped/unknown lines and existing lines are not capped.
- Render: unconfirmed suggestions are **not** rendered. `Proposal.Spec(confirmed)`
  renders confirmed suggestions as normal items in their section. This reverses 08b D4.
- Questions prompt: unchanged, except that `answered` now explains `choice`.

## TUI: review changes (`gardenassistreview.go`)

- **Suggestions row** in the review rail (`? Suggestions N`). In its pane, `↑/↓`
  moves and `space` toggles confirmed. Confirmed ones join their section's proposed
  text. Unconfirmed ones are discarded when the decisions are applied.
- **Open questions notice.** "AI added N open questions; they block validation."
  `o` starts an answer round built from the working draft's unresolved open questions
  (≤10). Each becomes a question with no suggestions; no AI call is made. The next
  Draft then resolves them through the existing `resolved` mapping.

## Milestones (one green commit each)

| # | Scope | Agent |
|---|---|---|
| C1 | `assist`: Answer.Choice, input JSON, prompt texts, 3-question cap, `Spec(confirmed)`; tests | Codex |
| C2 | TUI answer panel: zones, options list, notes/answer field, glyphs, keys; tests | `tui` (Sonnet) |
| C3 | Review Suggestions row, `o` answer round, open-question notice; tests | `tui`, continuing C2's session |
| C4 | `docs/garden.md`, `docs/tui-cli.md`, garden contract, 08b decision D4 note | Codex |

## Tests

- **C1:**
  - choice and notes reach the input JSON;
  - a skipped or unknown question with notes still carries its own Open questions line;
  - the open-question cap drops extras and counts them;
  - unconfirmed suggestions never render, and confirmed ones render in their section;
  - `approve.OpenQuestions` on the rendered output of the #115 shape lists only real
    blockers.
  - an `o` answer round can resolve an earlier skipped, unknown or open question;
    subsequent refinement does not recreate the old blocker, and an unanswered
    repeat still blocks.
- **C2:**
  - digits in the text zone are typed as text;
  - `1`–`9` and `space` in options select a row;
  - switching option and own answer keeps the typed text;
  - Write my own with an empty answer stays open;
  - `pgdown` and `pgup` work from the text zone;
  - `q` never quits from text;
  - the first question of a round without suggestions accepts text immediately;
  - rendering at 80×24 and 120×35 in Unicode and ASCII.
- **C3:**
  - toggling a suggestion changes the applied section;
  - unconfirmed suggestions are absent after `e`;
  - `o` builds questions from the open lines, and the next draft request carries them
    as answers;
  - still no Validate, StartPlanner or DecidePlan calls.

Verification:

```sh
go test -race ./internal/core/garden/assist/... ./internal/ui/...
gofmt -l $(git ls-files '*.go')
go vet ./...
go build ./...
```

## Acceptance

- [x] In #115's flow, answering every question by choice or own text leaves no
      "unanswered" or digit-ambiguity open questions.
- [x] Choose and write are visibly separate. Every choice can carry notes. Skip and
      "I don't know" are explicit rows.
- [x] Unconfirmed suggestions are never saved and never block validation.
- [x] At most 3 new open questions come from the AI, and they are answerable with `o`.
- [x] Skipped and unknown answers still block gate one; no gate rule changed.

## Decisions

- **D1 Unconfirmed suggestions:** discard them.
- **D2 AI open questions cap:** fixed at 3.
- **D3 `o` round:** local and free, without suggestions or an AI call.
