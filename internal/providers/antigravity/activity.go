package antigravity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthook"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// hooksGroup names Bonsai's group in hooks.json. agy merges named groups, so a
// group of the user's own would sit next to it.
const hooksGroup = "bonsai-activity"

// hookTimeoutSeconds bounds a hook command. agy runs hooks synchronously, so a
// hung disk must not hold the agent loop for its default 30 s.
const hookTimeoutSeconds = 5

// flatHookEvents take a list of handlers directly. PreToolUse is absent on
// purpose: agy requires it to answer with a permission decision, so any hook on
// it would change what the agent may do. Observing must not.
var flatHookEvents = []string{"SessionStart", "PreInvocation", "PostInvocation", "Stop"}

// groupedHookEvents take handlers wrapped in a matcher group.
var groupedHookEvents = []string{"PostToolUse"}

type hookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type hookMatcherGroup struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookHandler `json:"hooks"`
}

// activityHooks is the value of Bonsai's group in hooks.json.
func activityHooks(request *agents.ActivityRequest) map[string]any {
	group := make(map[string]any, len(flatHookEvents)+len(groupedHookEvents))
	handler := func(event string) hookHandler {
		return hookHandler{Type: "command", Command: hookCommandLine(request, event), Timeout: hookTimeoutSeconds}
	}
	for _, event := range flatHookEvents {
		group[event] = []hookHandler{handler(event)}
	}
	for _, event := range groupedHookEvents {
		group[event] = []hookMatcherGroup{{Matcher: "*", Hooks: []hookHandler{handler(event)}}}
	}
	return group
}

// hookCommandLine is the shell command agy runs (sh -c, cmd /c on Windows).
// agy runs with HOME set to the session home, so the hook cannot derive the
// sessions root on its own: it is named on the command line, which Bonsai wrote
// into the session's private hooks.json, rather than in the environment.
func hookCommandLine(request *agents.ActivityRequest, event string) string {
	root := filepath.Dir(filepath.Dir(request.SpoolPath))
	words := make([]string, 0, len(request.HookCommand)+2)
	for _, arg := range request.HookCommand {
		words = append(words, quoteWord(arg))
	}
	words = append(words, event, quoteWord(agenthook.RootFlag+root))
	return strings.Join(words, " ")
}

func quoteWord(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// installActivity points agy at Bonsai's hooks for this session and turns on
// its terminal notifications, the only way a permission prompt or question
// reaches the terminal for the holder to classify. Both live in the session
// home, which is private and thrown away with the session; neither is copied
// back into the profile. It reports whether the hooks are in place: without
// them the spool env must not be set either.
func installActivity(session agents.Session, request *agents.ActivityRequest) (installed bool) {
	// The files come from the profile and the caller has no way to handle a
	// panic: a shape nobody anticipated costs the hooks, never the launch.
	defer func() {
		if recover() != nil {
			installed = false
		}
	}()
	if err := writeActivityHooks(hooksPath(session.HomeDir), request); err != nil {
		return false
	}
	// Notifications only add the inferred "needs input" state; the hooks work
	// without them.
	_ = enableNotifications(session)
	return true
}

// writeActivityHooks sets Bonsai's group in the hooks.json at path and keeps
// any other group already there.
func writeActivityHooks(path string, request *agents.ActivityRequest) error {
	groups := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &groups); err != nil {
			return err
		}
		if groups == nil { // a literal null decodes into a nil map
			return errors.New("hooks.json: not a JSON object")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(activityHooks(request))
	if err != nil {
		return err
	}
	groups[hooksGroup] = raw
	return writePrivateJSON(path, groups)
}

// notificationsKey is agy's settings.json switch for terminal notifications
// (`/config` > Notifications). A boolean: agy rejects any other value.
const notificationsKey = "notifications"

// enableNotifications sets notifications in the session's settings.json and
// marks the session so Reconcile can undo it before the settings go back to the
// profile. The marker comes first: a session that cannot be marked must not
// change the setting.
func enableNotifications(session agents.Session) error {
	path := providerSettingsPath(session.HomeDir)
	fields := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &fields); err != nil {
			return err // someone else's file: leave it be
		}
		if fields == nil { // a literal null decodes into a nil map
			return errors.New("settings.json: not a JSON object")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writePrivateFile(notificationsMarkerPath(session), nil); err != nil {
		return err
	}
	fields[notificationsKey] = json.RawMessage("true")
	return writePrivateJSON(path, fields)
}

// restoreNotifications returns the settings to store in the profile: what the
// session ended with, except that notifications is back to what the profile
// had. When that makes them equal to the profile's, its bytes are kept.
func restoreNotifications(session, profile json.RawMessage) json.RawMessage {
	var fields, original map[string]json.RawMessage
	if err := json.Unmarshal(session, &fields); err != nil || fields == nil {
		return session
	}
	if len(profile) > 0 {
		if err := json.Unmarshal(profile, &original); err != nil {
			return session
		}
	}
	if v, ok := original[notificationsKey]; ok {
		fields[notificationsKey] = v
	} else {
		delete(fields, notificationsKey)
	}
	if sameJSON(fields, original) {
		return profile
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return session
	}
	return out
}

func sameJSON(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		var x, y any
		if json.Unmarshal(av, &x) != nil || json.Unmarshal(bv, &y) != nil {
			return string(av) == string(bv)
		}
		xb, _ := json.Marshal(x)
		yb, _ := json.Marshal(y)
		if string(xb) != string(yb) {
			return false
		}
	}
	return true
}

func notificationsEnabledBy(session agents.Session) bool {
	_, err := os.Stat(notificationsMarkerPath(session))
	return err == nil
}

// ActivityProfile: agy's hooks report working and idle. A turn that waits for
// the user fires no hook at all (there is no permission or question event), so
// Waiting is only ever inferred from a terminal notification. The progress bar
// agy draws in the terminal is clear only between turns.
func (p *Provider) ActivityProfile() agents.ActivityProfile {
	return agents.ActivityProfile{
		Native:            []agentactivity.State{agentactivity.Working, agentactivity.Idle},
		Inferred:          []agentactivity.State{agentactivity.Waiting},
		ProgressMeansTurn: true,
	}
}

// HookOutput is "{}" for every event: agy parses a hook's stdout as JSON, and
// an empty object changes nothing (for Stop, anything but decision "continue"
// lets the agent stop).
func (p *Provider) HookOutput(string) []byte { return []byte("{}") }

// hookPayload is the part of a hook's stdin that matters. Prompts, tool
// arguments and error text are present in the real payload and deliberately
// not decoded.
type hookPayload struct {
	TerminationReason string `json:"terminationReason"`
	ToolCall          struct {
		Name string `json:"name"`
	} `json:"toolCall"`
}

const terminationReasonPrefix = "EXECUTOR_TERMINATION_REASON_"

// TranslateHook maps one hook payload to signals. The payload is untrusted
// input from a process we do not control: anything unreadable yields nothing.
func (p *Provider) TranslateHook(event string, payload []byte) []agentactivity.Signal {
	var in hookPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil
	}
	one := func(kind agentactivity.Kind, detail string) []agentactivity.Signal {
		return []agentactivity.Signal{{Kind: kind, Detail: detail}}
	}
	switch event {
	case "SessionStart":
		return one(agentactivity.SessionStarted, "")
	case "PreInvocation":
		// Before every model call, so it also covers the turns the user did not
		// start by typing (a background task waking the agent).
		return one(agentactivity.PromptSubmitted, "")
	case "PostInvocation":
		// Tool calls of this invocation are over, whatever a prompt asked.
		return one(agentactivity.ToolFinished, "")
	case "PostToolUse":
		return one(agentactivity.ToolFinished, in.ToolCall.Name)
	case "Stop":
		switch strings.TrimPrefix(in.TerminationReason, terminationReasonPrefix) {
		case "ERROR":
			// The kind, never the error text, which can quote anything.
			return one(agentactivity.TurnFailed, "error")
		case "USER_CANCELED":
			return one(agentactivity.Interrupted, "")
		}
		// NO_TOOL_CALL, hitting a limit, a terminal step, a hook or an injected
		// response: the agent stopped and waits for the user.
		return one(agentactivity.TurnCompleted, "")
	}
	return nil
}

// ClassifyTerminalNotification maps the notification agy writes when it needs
// the user (a tool permission prompt, or a question) to a signal. The texts are
// matched loosely and the signal is marked as coming from the terminal, so the
// state is shown as inferred. Anything else, such as a completion notice, is
// left unclassified.
func (p *Provider) ClassifyTerminalNotification(title, body string) (agentactivity.Signal, bool) {
	text := strings.ToLower(title + "\n" + body)
	switch {
	case strings.TrimSpace(title) == "Antigravity CLI needs tool confirmation" || strings.TrimSpace(body) == "Antigravity CLI needs tool confirmation":
		return agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true
	case strings.Contains(text, "has a question"), strings.Contains(text, "question for you"):
		return agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, true
	case strings.Contains(text, "permission"), strings.Contains(text, "approv"), strings.Contains(text, "requires your attention"):
		return agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true
	}
	return agentactivity.Signal{}, false
}

var antigravityQuestionHeader = regexp.MustCompile(`(?i)^question\s+\d+\s*/\s*\d+\s*:\s*\S.*$`)

// ClassifyTerminalScreen recognizes the fixed, rendered AskQuestion framing.
// The question title and answer options are user-controlled, so only the
// numbered header and navigation footer participate in classification.
func (p *Provider) ClassifyTerminalScreen(lines []string) (agentactivity.Signal, bool) {
	header, footer := false, false
	permissionIntro, permissionQuestion, permissionFooter := false, false, false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if antigravityQuestionHeader.MatchString(line) {
			header = true
		}
		if strings.EqualFold(line, "↑/↓ Navigate · enter Select · esc Skip") {
			footer = true
		}
		if strings.EqualFold(line, "Requesting permission for:") {
			permissionIntro = true
		}
		if strings.EqualFold(line, "Run this command?") {
			permissionQuestion = true
		}
		if strings.EqualFold(line, "↑/↓ Navigate · tab Amend · ctrl+g edit/expand command") {
			permissionFooter = true
		}
	}
	if header && footer {
		return agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, true
	}
	if permissionIntro && permissionQuestion && permissionFooter {
		return agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true
	}
	return agentactivity.Signal{}, false
}
