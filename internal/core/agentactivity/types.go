// Package agentactivity turns raw agent signals (provider hooks, terminal
// escapes, keystrokes, lifecycle edges) into one provider-neutral activity
// state and fans it out. It is pure and stdlib-only: core/agents imports it,
// so it must never import agents, providers, agenthost or agentterminal.
package agentactivity

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// State is what a running agent is doing. It is only meaningful while the
// session lifecycle is "running".
type State string

const (
	Unknown State = "unknown" // running, nothing reported yet or provider reports nothing
	Working State = "working"
	Waiting State = "waiting" // needs the user
	Idle    State = "idle"    // turn over, ready for a prompt
)

// Reason refines Waiting and Idle.
type Reason string

const (
	ReasonPermission  Reason = "permission"
	ReasonQuestion    Reason = "question"
	ReasonPlan        Reason = "plan"
	ReasonElicitation Reason = "elicitation"

	ReasonReady        Reason = "ready"
	ReasonTurnComplete Reason = "turn_complete"
	ReasonInterrupted  Reason = "interrupted"
	ReasonError        Reason = "error"
)

// Source says where a signal or state came from.
type Source string

const (
	SourceHook      Source = "hook"
	SourceTerminal  Source = "terminal"
	SourceInput     Source = "input"
	SourceLifecycle Source = "lifecycle"
)

// MaxDetailRunes caps Activity.Detail and Signal.Detail.
const MaxDetailRunes = 120

// Activity is the current activity of one session.
type Activity struct {
	State   State     `json:"state"`
	Reason  Reason    `json:"reason,omitempty"`
	Detail  string    `json:"detail,omitempty"` // tool name or short sanitized text, at most MaxDetailRunes
	Since   time.Time `json:"since"`
	Source  Source    `json:"source"`
	Stalled bool      `json:"stalled,omitempty"` // Working with no signal for the stall window
	// Inferred is true when the state came from a heuristic (terminal or input)
	// rather than a provider hook, so clients can soften it.
	Inferred bool `json:"inferred,omitempty"`
	// Seen is the newest hook signal time the reducer has taken in. Since only
	// moves on a transition, so a delayed hook of an earlier turn could still look
	// newer than Since; Seen is what hooks are ordered against. It is reducer
	// bookkeeping: never serialized, never part of Changed.
	Seen time.Time `json:"-"`
	// PendingInput outlives an async question tool and the turn that queued it.
	// Only an answer, cancellation or failure resolves the outstanding question.
	PendingInput bool `json:"-"`
}

// Changed reports a transition worth publishing everywhere: State, Reason,
// Stalled or Inferred differ. Detail and Since churn constantly and are
// deliberately ignored so they never cost a record write.
func (a Activity) Changed(b Activity) bool {
	return a.State != b.State || a.Reason != b.Reason || a.Stalled != b.Stalled || a.Inferred != b.Inferred
}

// Kind is a normalized signal. Providers translate their raw payloads into
// kinds; only the reducer decides states.
type Kind string

const (
	SessionStarted      Kind = "session_started"
	PromptSubmitted     Kind = "prompt_submitted"
	ToolStarted         Kind = "tool_started"
	ToolFinished        Kind = "tool_finished"
	PermissionRequested Kind = "permission_requested"
	InputRequested      Kind = "input_requested" // question / plan / elicitation (Reason says which)
	AsyncInputRequested Kind = "async_input_requested"
	InputResolved       Kind = "input_resolved"
	TurnCompleted       Kind = "turn_completed"
	TurnFailed          Kind = "turn_failed"
	Interrupted         Kind = "interrupted"
	SessionEnded        Kind = "session_ended"

	// Produced by the holder itself, not by providers.
	TerminalBusy         Kind = "terminal_busy"         // OSC 9;4;1|3
	TerminalClear        Kind = "terminal_clear"        // OSC 9;4;0
	TerminalNotification Kind = "terminal_notification" // OSC 9 / 777 / 99 / BEL, classified by the provider
	UserSubmitted        Kind = "user_submitted"        // Enter or Esc from the writing client
)

// Signal is one normalized observation. At is when it happened (for hooks:
// when the hook command started), which is what makes late async hooks
// harmless.
type Signal struct {
	Kind   Kind      `json:"kind"`
	At     time.Time `json:"at"`
	Source Source    `json:"source"`
	Reason Reason    `json:"reason,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// SanitizeDetail makes s safe to store and show: invalid UTF-8, control and
// format characters (escape sequences, bidi overrides) are dropped, surrounding
// space is trimmed and the result is capped at MaxDetailRunes. Details travel to
// records, snapshots and warm-start data, so callers must never pass prompts,
// tool inputs or output.
func SanitizeDetail(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxDetailRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxDetailRunes]))
	}
	return s
}
