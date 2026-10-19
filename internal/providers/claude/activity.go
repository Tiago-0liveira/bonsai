package claude

import (
	"encoding/json"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// hookEvents are the Claude Code hook events Bonsai observes. Hooks only
// observe: they print nothing and never block, so permission behaviour is
// untouched.
var hookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
	"PostToolUse", "PostToolUseFailure", "Notification", "Elicitation",
	"ElicitationResult", "Stop", "StopFailure", "SessionEnd",
}

// hookTimeoutSeconds bounds a hook command, so a hung disk can never stall the
// agent.
const hookTimeoutSeconds = 5

// settingsEntry is one hook matcher group in Claude Code's settings format.
type settingsEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []commandHook `json:"hooks"`
}

type commandHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async"`
	Timeout int    `json:"timeout"`
}

// activitySettings is the JSON for `--settings`: command hooks for every
// observed event. Claude merges hooks across settings layers, so this adds to
// the profile's own hooks instead of replacing them.
func activitySettings(request *agents.ActivityRequest) (string, error) {
	hooks := make(map[string][]settingsEntry, len(hookEvents))
	for _, event := range hookEvents {
		entry := settingsEntry{Hooks: []commandHook{{
			Type: "command", Command: hookCommandLine(request.HookCommand, event), Async: true, Timeout: hookTimeoutSeconds,
		}}}
		if event == "PreToolUse" {
			entry.Matcher = "*"
		}
		hooks[event] = []settingsEntry{entry}
	}
	out, err := json.Marshal(map[string]any{"hooks": hooks})
	return string(out), err
}

// hookCommandLine is the shell command Claude Code runs: every argument is
// single-quoted, so paths with spaces or quotes stay one word. The event is
// ours and plain.
func hookCommandLine(argv []string, event string) string {
	words := make([]string, 0, len(argv)+1)
	for _, arg := range argv {
		words = append(words, shellQuote(arg))
	}
	return strings.Join(append(words, event), " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ActivityProfile: Claude's hooks report every state.
func (p *Provider) ActivityProfile() agents.ActivityProfile {
	return agents.ActivityProfile{Native: []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle}}
}

// HookOutput is empty: a Claude hook that prints nothing changes nothing.
func (p *Provider) HookOutput(string) []byte { return nil }

// hookPayload is the part of a hook's stdin that matters. Tool inputs,
// prompts and responses are present in the real payload and deliberately not
// decoded.
type hookPayload struct {
	ToolName         string `json:"tool_name"`
	NotificationType string `json:"notification_type"`
	Error            string `json:"error"`
	Source           string `json:"source"`
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
		// A compaction mid-turn restarts the session hook; it says nothing about
		// the user being ready.
		if in.Source == "compact" {
			return nil
		}
		return one(agentactivity.SessionStarted, "", "")
	case "UserPromptSubmit":
		return one(agentactivity.PromptSubmitted, "", "")
	case "PreToolUse", "PermissionRequest":
		switch in.ToolName {
		case "AskUserQuestion":
			return one(agentactivity.InputRequested, agentactivity.ReasonQuestion, "")
		case "ExitPlanMode":
			return one(agentactivity.InputRequested, agentactivity.ReasonPlan, "")
		}
		// Claude also fires PermissionRequest for its question and plan
		// dialogs, so their input reason takes precedence over the hook name.
		if event == "PermissionRequest" {
			return one(agentactivity.PermissionRequested, "", in.ToolName)
		}
		return one(agentactivity.ToolStarted, "", in.ToolName)
	case "PostToolUse", "PostToolUseFailure":
		switch in.ToolName {
		case "AskUserQuestion", "ExitPlanMode":
			return one(agentactivity.InputResolved, "", "")
		}
		return one(agentactivity.ToolFinished, "", in.ToolName)
	case "Notification":
		switch in.NotificationType {
		case "permission_prompt":
			return one(agentactivity.PermissionRequested, "", "")
		case "idle_prompt":
			return one(agentactivity.TurnCompleted, "", "")
		case "elicitation_dialog":
			return one(agentactivity.InputRequested, agentactivity.ReasonElicitation, "")
		case "agent_needs_input":
			return one(agentactivity.InputRequested, agentactivity.ReasonQuestion, "")
		}
		return nil
	case "Elicitation":
		return one(agentactivity.InputRequested, agentactivity.ReasonElicitation, "")
	case "ElicitationResult":
		return one(agentactivity.InputResolved, "", "")
	case "Stop":
		return one(agentactivity.TurnCompleted, "", "")
	case "StopFailure":
		return one(agentactivity.TurnFailed, "", in.Error)
	case "SessionEnd":
		return one(agentactivity.SessionEnded, "", "")
	}
	return nil
}
