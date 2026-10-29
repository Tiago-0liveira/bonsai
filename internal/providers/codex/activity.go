package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// hookEvent is one Codex hook event Bonsai observes. Hooks only observe: they
// print nothing and never block, so approval and sandbox behaviour is untouched.
type hookEvent struct {
	name string
	// sync events are never async: Codex runs Interrupt and SessionEnd hooks to
	// completion before it moves on, so an async one could be cut short.
	sync bool
}

var hookEvents = []hookEvent{
	{name: "SessionStart"}, {name: "UserPromptSubmit"}, {name: "PreToolUse"}, {name: "PermissionRequest"},
	{name: "PostToolUse"}, {name: "Stop"}, {name: "Interrupt", sync: true}, {name: "SessionEnd", sync: true},
}

// hookTimeoutSeconds bounds a hook command, so a hung disk can never stall the
// agent.
const hookTimeoutSeconds = 5

// hooksPath is the profile's hooks file. Codex reads it next to config.toml of
// the same CODEX_HOME, which only this profile's sessions share.
func hooksPath(home string) string { return filepath.Join(home, "hooks.json") }

// commandHook is one command handler in Codex's hooks.json.
type commandHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async,omitempty"`
	Timeout int    `json:"timeout"`
}

// bonsaiCommandRE recognises the commands Bonsai writes: every argument is
// single-quoted by hookCommandLine, so the hook subcommand and the provider
// are always these two words, followed by the plain event name. Whatever
// binary path precedes them, an entry like this is Bonsai's own and is
// replaced, never kept next to the new one.
var bonsaiCommandRE = regexp.MustCompile(`'__agent-hook' 'codex' [A-Za-z]+$`)

func isBonsaiCommand(command string) bool { return bonsaiCommandRE.MatchString(command) }

// hookCommandLine is the shell command Codex runs: every argument is
// single-quoted, so paths with spaces or quotes stay one word. The event is
// ours and plain.
func hookCommandLine(argv []string, event string) string {
	words := make([]string, 0, len(argv)+1)
	for _, arg := range argv {
		words = append(words, "'"+strings.ReplaceAll(arg, "'", `'\''`)+"'")
	}
	return strings.Join(append(words, event), " ")
}

// ourGroup is the matcher group Bonsai registers for event. It has no matcher,
// which Codex reads as "every occurrence".
func ourGroup(request *agents.ActivityRequest, event hookEvent) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"hooks": []commandHook{{
		Type: "command", Command: hookCommandLine(request.HookCommand, event.name), Async: !event.sync, Timeout: hookTimeoutSeconds,
	}}})
}

// upsertHooks returns the hooks.json that keeps everything in existing except
// Bonsai's own handlers, and has exactly one Bonsai handler per observed
// event. Bonsai's group keeps its position when it is already there, so the
// keys Codex derives from positions (and the trust stored under them) stay
// stable. Foreign events, groups, handlers and top-level keys are preserved
// as they are. Unreadable JSON is an error: it is the user's file, so it is
// never overwritten.
func upsertHooks(existing []byte, request *agents.ActivityRequest) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("hooks.json: %w", err)
		}
		// A literal null decodes without error into a nil map, which cannot hold
		// the hooks key. It is not a hooks file, and it is the user's.
		if doc == nil {
			return nil, errors.New("hooks.json: not a JSON object")
		}
	}
	events := map[string][]json.RawMessage{}
	if raw, ok := doc["hooks"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &events); err != nil {
			return nil, fmt.Errorf("hooks.json: hooks: %w", err)
		}
	}
	ours := map[string]json.RawMessage{}
	for _, event := range hookEvents {
		group, err := ourGroup(request, event)
		if err != nil {
			return nil, err
		}
		ours[event.name] = group
	}
	for name, groups := range events {
		merged, err := mergeEvent(groups, ours[name])
		if err != nil {
			return nil, err
		}
		if len(merged) == 0 {
			delete(events, name)
			continue
		}
		events[name] = merged
	}
	for name, group := range ours {
		if _, ok := events[name]; !ok {
			events[name] = []json.RawMessage{group}
		}
	}
	raw, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}
	doc["hooks"] = raw
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// mergeEvent drops Bonsai's handlers from groups and puts ours (nil for an
// event Bonsai no longer observes) where the first of them was.
func mergeEvent(groups []json.RawMessage, ours json.RawMessage) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(groups)+1)
	placed := ours == nil
	for _, group := range groups {
		kept, touched, err := stripBonsai(group)
		if err != nil {
			return nil, err
		}
		switch {
		case !touched:
			out = append(out, group)
		case kept != nil:
			out = append(out, kept)
		case !placed:
			out = append(out, ours)
			placed = true
		}
	}
	if !placed {
		out = append(out, ours)
	}
	return out, nil
}

// stripBonsai removes Bonsai's handlers from one matcher group. It reports
// whether it found any, and the group without them (nil when none is left).
// A group that is not the expected shape is someone else's and stays.
func stripBonsai(group json.RawMessage) (kept json.RawMessage, touched bool, err error) {
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(group, &fields) != nil {
		return nil, false, nil
	}
	var handlers []json.RawMessage
	if json.Unmarshal(fields["hooks"], &handlers) != nil {
		return nil, false, nil
	}
	remaining := make([]json.RawMessage, 0, len(handlers))
	for _, handler := range handlers {
		var h struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(handler, &h) == nil && isBonsaiCommand(h.Command) {
			touched = true
			continue
		}
		remaining = append(remaining, handler)
	}
	if !touched {
		return nil, false, nil
	}
	if len(remaining) == 0 {
		return nil, true, nil
	}
	if fields["hooks"], err = json.Marshal(remaining); err != nil {
		return nil, false, err
	}
	kept, err = json.Marshal(fields)
	return kept, true, err
}

// writeActivityHooks upserts Bonsai's handlers into the profile's hooks.json.
// It writes only when the content changes, so a launch that finds the file as
// it left it touches nothing. The caller holds the account's hooks lock.
//
// The file is the user's and its callers ignore the error, so a panic on a
// shape nobody anticipated would kill the launch instead of only the hooks:
// it becomes an error here.
func writeActivityHooks(home string, request *agents.ActivityRequest) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hooks.json: unexpected content: %v", r)
		}
	}()
	path := hooksPath(home)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next, err := upsertHooks(existing, request)
	if err != nil {
		return err
	}
	if bytes.Equal(existing, next) {
		return nil
	}
	return writePrivateFile(path, next)
}

// notificationConfig are the -c overrides that make Codex's TUI send OSC 9
// notifications for approvals and questions even while its terminal has focus
// (the holder's PTY never reports focus). They are the terminal fallback for
// when the hooks cannot run. Turn completion is not asked for: the Stop hook
// reports it.
var notificationConfig = []struct{ key, value string }{
	{"tui.notifications", `["approval-requested","async-question"]`},
	{"tui.notification_method", `"osc9"`},
	{"tui.notification_condition", `"always"`},
}

// notificationArgs are the -c flags for the notification settings args do not
// already choose.
func notificationArgs(args []string) []string {
	var out []string
	for _, c := range notificationConfig {
		if !hasConfig(args, c.key) {
			out = append(out, "-c", c.key+"="+c.value)
		}
	}
	return out
}

// ActivityProfile: Codex's hooks report every state.
func (p *Provider) ActivityProfile() agents.ActivityProfile {
	return agents.ActivityProfile{Native: []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle}}
}

// HookOutput is empty: every Codex hook output field is optional, and a hook
// that prints nothing changes nothing.
func (p *Provider) HookOutput(string) []byte { return nil }

// userInputTool is the tool Codex asks the user a question with.
const userInputTool = "request_user_input"

// Codex's async variant returns control to the model while the question is
// still waiting for the user's answer. Its completion hook therefore does not
// resolve the input request.
const asyncUserInputTool = "request_user_input_async"

// hookPayload is the part of a hook's stdin that matters. Tool inputs,
// prompts, tool responses and assistant messages are present in the real
// payload and deliberately not decoded.
type hookPayload struct {
	ToolName string `json:"tool_name"`
	Source   string `json:"source"`
}

// TranslateHook maps one hook payload to signals. The payload is untrusted
// input from a process we do not control: anything unreadable yields nothing.
func (p *Provider) TranslateHook(event string, payload []byte) []agentactivity.Signal {
	var in hookPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil
	}
	one := func(kind agentactivity.Kind, reason agentactivity.Reason, detail string) []agentactivity.Signal {
		return []agentactivity.Signal{{Kind: kind, Reason: reason, Detail: detail}}
	}
	switch event {
	case "SessionStart":
		// A compaction restarts the session hook in the middle of a turn; it says
		// nothing about the user being ready.
		if in.Source == "compact" {
			return nil
		}
		return one(agentactivity.SessionStarted, "", "")
	case "UserPromptSubmit":
		return one(agentactivity.PromptSubmitted, "", "")
	case "PreToolUse":
		if in.ToolName == asyncUserInputTool {
			return one(agentactivity.AsyncInputRequested, agentactivity.ReasonQuestion, "")
		}
		if in.ToolName == userInputTool {
			return one(agentactivity.InputRequested, agentactivity.ReasonQuestion, "")
		}
		return one(agentactivity.ToolStarted, "", in.ToolName)
	case "PostToolUse":
		if in.ToolName == asyncUserInputTool {
			return nil
		}
		if in.ToolName == userInputTool {
			return one(agentactivity.InputResolved, "", "")
		}
		return one(agentactivity.ToolFinished, "", in.ToolName)
	case "PermissionRequest":
		return one(agentactivity.PermissionRequested, "", in.ToolName)
	case "Stop":
		return one(agentactivity.TurnCompleted, "", "")
	case "Interrupt":
		return one(agentactivity.Interrupted, "", "")
	case "SessionEnd":
		return one(agentactivity.SessionEnded, "", "")
	}
	return nil
}

// notificationPrefixes are the texts Codex's TUI starts its OSC 9 messages
// with (read from the 0.162 binary; the rest of each message is a command,
// file or question, and is never looked at). Matching is by prefix so the
// variable tail cannot matter, and anything else (turn complete, plan or
// usage notices) is left unclassified.
var notificationPrefixes = []struct {
	prefix string
	kind   agentactivity.Kind
	reason agentactivity.Reason
}{
	{"Approval requested", agentactivity.PermissionRequested, ""},
	{"Codex wants to edit", agentactivity.PermissionRequested, ""},
	{"Question:", agentactivity.InputRequested, agentactivity.ReasonQuestion},
	{"Plan mode prompt", agentactivity.InputRequested, agentactivity.ReasonPlan},
}

// ClassifyTerminalNotification maps Codex's approval and question
// notifications to signals. OSC 9 carries only a message, so the text is the
// body; the title is read too in case another method puts it there.
func (p *Provider) ClassifyTerminalNotification(title, body string) (agentactivity.Signal, bool) {
	for _, text := range []string{body, title} {
		text = strings.TrimSpace(text)
		for _, n := range notificationPrefixes {
			if strings.HasPrefix(text, n.prefix) {
				return agentactivity.Signal{Kind: n.kind, Reason: n.reason}, true
			}
		}
	}
	return agentactivity.Signal{}, false
}
