package agentactivity

import "time"

// Reduce applies one signal to the current activity. It is pure: no clock, no
// I/O. now stamps signals that carry no time.
//
// Rules (docs/agent-status-plan.md section 4):
//   - Waiting is never hidden by ToolStarted or TerminalBusy, because parallel
//     tools keep running while a prompt is pending.
//   - A hook signal older than current.Since or current.Seen (the newest hook
//     signal taken in; Since alone only moves on a transition) never moves
//     state: async hooks may arrive out of order. It may only fill an empty
//     Detail of a live state.
//   - TerminalClear always means "turn ended" here; whether the provider's
//     progress bar really means that is the Tracker's call (ProgressMeansTurn).
//   - TerminalNotification must be classified into another kind by the provider
//     first; an unclassified one changes nothing.
//   - PromptSubmitted also resolves Waiting: the table lists it only for the
//     "stay Waiting" row of tools and terminal output, and a submitted prompt
//     means the user answered.
func Reduce(current Activity, s Signal, now time.Time) Activity {
	at := s.At
	if at.IsZero() {
		at = now
	}
	detail := SanitizeDetail(s.Detail)
	seen := current.Seen
	if s.Source == SourceHook {
		if at.Before(current.Since) || at.Before(seen) {
			return fillDetail(current, detail)
		}
		seen = at
	}
	// An async question returns before the answer arrives. Neither background
	// tool completions nor the end of that turn mean the user answered it.
	switch s.Kind {
	case AsyncInputRequested:
		current.PendingInput = true
	case PromptSubmitted, InputResolved, UserSubmitted, Interrupted, TurnFailed, SessionStarted:
		current.PendingInput = false
	case ToolFinished, TurnCompleted:
		if current.PendingInput {
			current.Seen = seen
			return current
		}
	}

	state, reason, inferred, ok := target(current, s)
	if !ok {
		current.Seen = seen
		return current
	}
	if s.Source == SourceTerminal || s.Source == SourceInput {
		inferred = true
	}

	if state == current.State && reason == current.Reason {
		// Same state: a repeat (Claude's PermissionRequest + Notification) is not
		// a transition, so Since stays. A hook confirms a heuristic state, but a
		// heuristic never downgrades a confirmed one, or terminal output would
		// flip Inferred back and forth on every busy frame.
		next := current
		next.Seen = seen
		next.Stalled = false
		if detail != "" {
			next.Detail = detail
		}
		if current.Inferred && !inferred {
			next.Inferred = false
			next.Source = s.Source
		}
		return next
	}
	return Activity{
		State:        state,
		Reason:       reason,
		Detail:       detail,
		Since:        at,
		Source:       s.Source,
		Inferred:     inferred,
		Seen:         seen,
		PendingInput: current.PendingInput,
	}
}

// target maps a signal to the state it asks for. ok is false when the signal
// leaves the activity untouched. inferred forces Inferred for rules that are
// heuristics whatever the source says.
func target(cur Activity, s Signal) (state State, reason Reason, inferred, ok bool) {
	switch s.Kind {
	case SessionStarted:
		return Idle, ReasonReady, false, true
	case PromptSubmitted, ToolFinished, InputResolved:
		return Working, "", false, true
	case ToolStarted, TerminalBusy:
		if cur.State == Waiting {
			return "", "", false, false
		}
		return Working, "", false, true
	case PermissionRequested:
		// Generic permission notifications repeat question and plan dialogs
		// too. Keep their known reason; a named tool request can still replace it.
		if cur.State == Waiting && s.Detail == "" {
			switch cur.Reason {
			case ReasonQuestion, ReasonPlan, ReasonElicitation:
				return "", "", false, false
			}
		}
		return Waiting, ReasonPermission, false, true
	case InputRequested, AsyncInputRequested:
		switch s.Reason {
		case ReasonPlan, ReasonElicitation:
			return Waiting, s.Reason, false, true
		}
		return Waiting, ReasonQuestion, false, true
	case UserSubmitted:
		if cur.State == Waiting {
			return Working, "", true, true
		}
		return "", "", false, false
	case TurnCompleted:
		return Idle, ReasonTurnComplete, false, true
	case TurnFailed:
		return Idle, ReasonError, false, true
	case Interrupted:
		return Idle, ReasonInterrupted, false, true
	case TerminalClear:
		if cur.State == Working {
			return Idle, ReasonTurnComplete, true, true
		}
		return "", "", false, false
	}
	// SessionEnded (lifecycle exit follows), unclassified TerminalNotification
	// and unknown kinds.
	return "", "", false, false
}

// fillDetail lets a late hook describe the current state without moving it.
// Idle details (error kinds) belong to the signal that set them.
func fillDetail(cur Activity, detail string) Activity {
	if detail != "" && cur.Detail == "" && (cur.State == Working || cur.State == Waiting) {
		cur.Detail = detail
	}
	return cur
}
