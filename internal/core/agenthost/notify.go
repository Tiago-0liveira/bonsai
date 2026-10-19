package agenthost

import (
	"strings"
	"time"
	"unicode"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/notify"
)

// NotifyPrefs says which activity changes raise a desktop notification. The
// zero value sends nothing. A holder gets it once, at start: preferences
// changed later apply to sessions started afterwards.
type NotifyPrefs struct {
	Enabled      bool
	NeedsInput   bool // the agent became Waiting
	TurnComplete bool // the agent became Idle after a turn (finished or failed)
}

// notifyInterval is the least time between two notifications of one type.
const notifyInterval = 2 * time.Second

// notifyTextRunes caps each interpolated name. Text is also reduced to a
// conservative alphabet because notify passes it through osascript and
// PowerShell one-liners.
const notifyTextRunes = 60

// startNotifier subscribes the desktop notifier to the holder's bus. The
// returned func cancels it; it is safe to call more than once.
//
// Notifications carry only the session name, provider, reason and (for a
// permission prompt) the tool name, never prompts, tool input or output.
func (h *host) startNotifier() func() {
	prefs := h.deps.Notifications
	if !prefs.Enabled || (!prefs.NeedsInput && !prefs.TurnComplete) {
		return func() {} // no subscriber at all
	}
	send := h.deps.Notify
	if send == nil {
		send = notify.Send
	}
	// The bus runs the subscriber on one goroutine, so last needs no lock.
	last := map[string]time.Time{}
	return h.bus.Subscribe("desktop-notify", agentactivity.Became(agentactivity.Waiting, agentactivity.Idle), func(e agentactivity.Event) {
		kind, title, body, ok := notification(prefs, e)
		if !ok {
			return
		}
		if at, seen := last[kind]; seen && e.At.Sub(at) < notifyInterval {
			return
		}
		last[kind] = e.At
		send(title, body)
	})
}

// notification maps an activity change to the notification it deserves.
func notification(prefs NotifyPrefs, e agentactivity.Event) (kind, title, body string, ok bool) {
	to := e.To
	if !prefs.Enabled || e.Type != agentactivity.ActivityChanged || to == nil {
		return
	}
	// The user's own keystrokes and the session's end are never news to them.
	if to.Source == agentactivity.SourceInput || to.Source == agentactivity.SourceLifecycle {
		return
	}
	// A repeat of the same state (Inferred confirmed by a hook) is not a new event.
	if e.From != nil && e.From.State == to.State && e.From.Reason == to.Reason {
		return
	}
	name := notifyText(e.Session.Name)
	if name == "" {
		name = "agent"
	}
	provider := notifyText(e.Session.Provider)
	switch to.State {
	case agentactivity.Waiting:
		if !prefs.NeedsInput {
			return
		}
		detail := string(to.Reason)
		if to.Reason == agentactivity.ReasonPermission {
			if tool := notifyText(to.Detail); tool != "" {
				detail += ": " + tool
			}
		}
		return "needs_input", "Bonsai: " + name + " needs input", joinNonEmpty(provider, detail), true
	case agentactivity.Idle:
		if !prefs.TurnComplete {
			return
		}
		switch to.Reason {
		case agentactivity.ReasonTurnComplete:
			return "turn_complete", "Bonsai: " + name + " finished", joinNonEmpty(provider, ""), true
		case agentactivity.ReasonError:
			return "turn_complete", "Bonsai: " + name + " failed", joinNonEmpty(provider, ""), true
		}
	}
	return
}

func joinNonEmpty(provider, detail string) string {
	switch {
	case provider != "" && detail != "":
		return provider + " (" + detail + ")"
	case provider != "":
		return provider
	}
	return detail
}

// notifyText keeps letters, digits, space and a few separators, drops the
// rest, and caps the length.
func notifyText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == ' ', r == '-', r == '_', r == '.', r == ':', r == '/', r == '#', r == '@', r == '+':
			return r
		}
		return -1
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > notifyTextRunes {
		s = strings.TrimSpace(string(r[:notifyTextRunes]))
	}
	return s
}
