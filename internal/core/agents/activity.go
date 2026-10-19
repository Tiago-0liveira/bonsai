package agents

import "github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"

// ActivityEnv names the environment variable that carries the activity spool
// path to a provider's hook command.
const ActivityEnv = "BONSAI_AGENT_ACTIVITY"

// ActivityRequest is set on PrepareSessionRequest when the holder wants the
// session's activity. Nil means: install nothing.
type ActivityRequest struct {
	// HookCommand is the absolute argv of `bonsai __agent-hook <provider>`; the
	// provider appends the hook event.
	HookCommand []string
	// SpoolEnv is the environment variable the hook command reads (ActivityEnv).
	SpoolEnv string
	// SpoolPath is RuntimeDir/activity.jsonl, created 0600 by the holder.
	SpoolPath string
}

// Usable reports whether r asks for anything a provider can install.
func (r *ActivityRequest) Usable() bool {
	return r != nil && len(r.HookCommand) > 0 && r.SpoolEnv != "" && r.SpoolPath != ""
}

// ActivityReporter is implemented by providers whose CLI can report activity.
// PrepareSession installs the hooks (flags, env, files) when req.Activity is set.
type ActivityReporter interface {
	ActivityProfile() ActivityProfile
	// TranslateHook turns one raw hook payload into signals. It runs inside the
	// hook command, so it must be fast and must never return tool inputs,
	// prompts or file contents, only kinds, reasons and short details. It leaves
	// At and Source empty: the hook command stamps them with the time the hook
	// started and SourceHook. Malformed payloads yield no signals.
	TranslateHook(event string, payload []byte) []agentactivity.Signal
	// HookOutput is what the hook command prints on stdout (agy needs "{}").
	HookOutput(event string) []byte
}

// TerminalClassifier maps a terminal notification to a signal (Codex
// "approval-requested", agy permission notice). Optional.
type TerminalClassifier interface {
	ClassifyTerminalNotification(title, body string) (agentactivity.Signal, bool)
}

// TerminalScreenClassifier recognizes a provider's input dialog when its
// hooks do not report it. The holder supplies a bounded, in-memory terminal
// grid. Implementations match fixed UI framing and never return its contents.
type TerminalScreenClassifier interface {
	ClassifyTerminalScreen(lines []string) (agentactivity.Signal, bool)
}

// ActivityProfile says what a provider can report.
type ActivityProfile struct {
	Native            []agentactivity.State // states reported by hooks
	Inferred          []agentactivity.State // states only inferred from the terminal
	ProgressMeansTurn bool                  // OSC 9;4 clear means the turn ended (agy)
}

// ActivitySpoolName is the spool file in a session's runtime directory.
const ActivitySpoolName = "activity.jsonl"
