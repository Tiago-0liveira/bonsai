package agentactivity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func act(state State, reason Reason) Activity {
	return Activity{State: state, Reason: reason, Since: at(10), Source: SourceHook}
}

func sig(kind Kind, src Source, sec int) Signal {
	return Signal{Kind: kind, At: at(sec), Source: src}
}

func TestReduceRules(t *testing.T) {
	now := at(100)
	tests := []struct {
		name     string
		cur      Activity
		sig      Signal
		want     State
		reason   Reason
		inferred bool
		detail   string
		moved    bool // Since moves to the signal time
	}{
		{"started from unknown", act(Unknown, ""), sig(SessionStarted, SourceHook, 20), Idle, ReasonReady, false, "", true},
		{"started from working", act(Working, ""), sig(SessionStarted, SourceHook, 20), Idle, ReasonReady, false, "", true},
		{"started from waiting", act(Waiting, ReasonPermission), sig(SessionStarted, SourceHook, 20), Idle, ReasonReady, false, "", true},
		{"started when already ready", act(Idle, ReasonReady), sig(SessionStarted, SourceHook, 20), Idle, ReasonReady, false, "", false},

		{"prompt from unknown", act(Unknown, ""), sig(PromptSubmitted, SourceHook, 20), Working, "", false, "", true},
		{"prompt from idle", act(Idle, ReasonReady), sig(PromptSubmitted, SourceHook, 20), Working, "", false, "", true},
		{"prompt from working", act(Working, ""), sig(PromptSubmitted, SourceHook, 20), Working, "", false, "", false},
		{"prompt resolves waiting", act(Waiting, ReasonQuestion), sig(PromptSubmitted, SourceHook, 20), Working, "", false, "", true},

		{"tool started from unknown", act(Unknown, ""), Signal{Kind: ToolStarted, At: at(20), Source: SourceHook, Detail: "Bash"}, Working, "", false, "Bash", true},
		{"tool started from idle", act(Idle, ReasonTurnComplete), sig(ToolStarted, SourceHook, 20), Working, "", false, "", true},
		{"tool started keeps waiting", act(Waiting, ReasonPermission), sig(ToolStarted, SourceHook, 20), Waiting, ReasonPermission, false, "", false},

		{"busy from unknown", act(Unknown, ""), sig(TerminalBusy, SourceTerminal, 20), Working, "", true, "", true},
		{"busy from idle", act(Idle, ReasonReady), sig(TerminalBusy, SourceTerminal, 20), Working, "", true, "", true},
		{"busy keeps waiting", act(Waiting, ReasonQuestion), sig(TerminalBusy, SourceTerminal, 20), Waiting, ReasonQuestion, false, "", false},

		{"tool finished from idle", act(Idle, ReasonReady), sig(ToolFinished, SourceHook, 20), Working, "", false, "", true},
		{"tool finished from waiting", act(Waiting, ReasonPermission), sig(ToolFinished, SourceHook, 20), Working, "", false, "", true},
		{"input resolved", act(Waiting, ReasonQuestion), sig(InputResolved, SourceHook, 20), Working, "", false, "", true},

		{"permission from working", act(Working, ""), Signal{Kind: PermissionRequested, At: at(20), Source: SourceHook, Detail: "Bash"}, Waiting, ReasonPermission, false, "Bash", true},
		{"permission from idle", act(Idle, ReasonReady), sig(PermissionRequested, SourceHook, 20), Waiting, ReasonPermission, false, "", true},
		{"permission classified from terminal", act(Working, ""), sig(PermissionRequested, SourceTerminal, 20), Waiting, ReasonPermission, true, "", true},
		{"generic permission keeps question", act(Waiting, ReasonQuestion), sig(PermissionRequested, SourceHook, 20), Waiting, ReasonQuestion, false, "", false},
		{"generic permission keeps plan", act(Waiting, ReasonPlan), sig(PermissionRequested, SourceHook, 20), Waiting, ReasonPlan, false, "", false},
		{"generic permission keeps elicitation", act(Waiting, ReasonElicitation), sig(PermissionRequested, SourceHook, 20), Waiting, ReasonElicitation, false, "", false},
		{"generic permission keeps permission", act(Waiting, ReasonPermission), sig(PermissionRequested, SourceHook, 20), Waiting, ReasonPermission, false, "", false},
		{"named permission replaces question", act(Waiting, ReasonQuestion), Signal{Kind: PermissionRequested, At: at(20), Source: SourceHook, Detail: "Bash"}, Waiting, ReasonPermission, false, "Bash", true},

		{"input requested question", act(Working, ""), Signal{Kind: InputRequested, At: at(20), Source: SourceHook, Reason: ReasonQuestion}, Waiting, ReasonQuestion, false, "", true},
		{"input requested plan", act(Working, ""), Signal{Kind: InputRequested, At: at(20), Source: SourceHook, Reason: ReasonPlan}, Waiting, ReasonPlan, false, "", true},
		{"input requested elicitation", act(Working, ""), Signal{Kind: InputRequested, At: at(20), Source: SourceHook, Reason: ReasonElicitation}, Waiting, ReasonElicitation, false, "", true},
		{"input requested default reason", act(Working, ""), sig(InputRequested, SourceHook, 20), Waiting, ReasonQuestion, false, "", true},
		{"input requested bogus reason", act(Working, ""), Signal{Kind: InputRequested, At: at(20), Source: SourceHook, Reason: ReasonError}, Waiting, ReasonQuestion, false, "", true},
		{"input requested replaces permission", act(Waiting, ReasonPermission), sig(InputRequested, SourceHook, 20), Waiting, ReasonQuestion, false, "", true},

		{"user answers waiting", act(Waiting, ReasonPermission), sig(UserSubmitted, SourceInput, 20), Working, "", true, "", true},
		{"user submit while working", act(Working, ""), sig(UserSubmitted, SourceInput, 20), Working, "", false, "", false},
		{"user submit while idle", act(Idle, ReasonReady), sig(UserSubmitted, SourceInput, 20), Idle, ReasonReady, false, "", false},
		{"user submit while unknown", act(Unknown, ""), sig(UserSubmitted, SourceInput, 20), Unknown, "", false, "", false},

		{"turn completed", act(Working, ""), sig(TurnCompleted, SourceHook, 20), Idle, ReasonTurnComplete, false, "", true},
		{"turn completed from waiting", act(Waiting, ReasonPermission), sig(TurnCompleted, SourceHook, 20), Idle, ReasonTurnComplete, false, "", true},
		{"turn failed", act(Working, ""), Signal{Kind: TurnFailed, At: at(20), Source: SourceHook, Detail: "rate_limit"}, Idle, ReasonError, false, "rate_limit", true},
		{"interrupted", act(Working, ""), sig(Interrupted, SourceHook, 20), Idle, ReasonInterrupted, false, "", true},
		{"interrupted from waiting", act(Waiting, ReasonQuestion), sig(Interrupted, SourceHook, 20), Idle, ReasonInterrupted, false, "", true},

		{"clear ends working turn", act(Working, ""), sig(TerminalClear, SourceTerminal, 20), Idle, ReasonTurnComplete, true, "", true},
		{"clear while idle", act(Idle, ReasonReady), sig(TerminalClear, SourceTerminal, 20), Idle, ReasonReady, false, "", false},
		{"clear while waiting", act(Waiting, ReasonPermission), sig(TerminalClear, SourceTerminal, 20), Waiting, ReasonPermission, false, "", false},
		{"clear while unknown", act(Unknown, ""), sig(TerminalClear, SourceTerminal, 20), Unknown, "", false, "", false},

		{"session ended changes nothing", act(Working, ""), sig(SessionEnded, SourceHook, 20), Working, "", false, "", false},
		{"unclassified notification dropped", act(Working, ""), sig(TerminalNotification, SourceTerminal, 20), Working, "", false, "", false},
		{"unknown kind dropped", act(Idle, ReasonReady), sig("bogus", SourceHook, 20), Idle, ReasonReady, false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reduce(tt.cur, tt.sig, now)
			if got.State != tt.want || got.Reason != tt.reason {
				t.Fatalf("state = %s/%s, want %s/%s", got.State, got.Reason, tt.want, tt.reason)
			}
			if got.Inferred != tt.inferred {
				t.Fatalf("Inferred = %v, want %v", got.Inferred, tt.inferred)
			}
			if got.Detail != tt.detail {
				t.Fatalf("Detail = %q, want %q", got.Detail, tt.detail)
			}
			wantSince := tt.cur.Since
			if tt.moved {
				wantSince = tt.sig.At
			}
			if !got.Since.Equal(wantSince) {
				t.Fatalf("Since = %v, want %v", got.Since, wantSince)
			}
		})
	}
}

func TestAsyncQuestionStaysPendingUntilAnswered(t *testing.T) {
	cur := Reduce(act(Working, ""), sig(AsyncInputRequested, SourceHook, 20), at(20))
	if cur.State != Waiting || cur.Reason != ReasonQuestion || !cur.PendingInput {
		t.Fatalf("async question: %+v", cur)
	}
	for i, kind := range []Kind{ToolStarted, ToolFinished, TurnCompleted, TerminalBusy, TerminalClear} {
		cur = Reduce(cur, sig(kind, SourceHook, 21+i), at(30))
		if cur.State != Waiting || cur.Reason != ReasonQuestion || !cur.PendingInput || !cur.Since.Equal(at(20)) {
			t.Fatalf("%s hid the unanswered question: %+v", kind, cur)
		}
	}
	for _, kind := range []Kind{PromptSubmitted, InputResolved, UserSubmitted, Interrupted, TurnFailed, SessionStarted} {
		t.Run(string(kind), func(t *testing.T) {
			next := Reduce(cur, sig(kind, SourceHook, 40), at(40))
			if next.State == Waiting || next.PendingInput {
				t.Fatalf("%s did not resolve question: %+v", kind, next)
			}
			next = Reduce(next, sig(TurnCompleted, SourceHook, 41), at(41))
			if next.State != Idle {
				t.Fatalf("turn completion after resolution: %+v", next)
			}
		})
	}
}

func TestAsyncQuestionBookkeepingStaysPrivate(t *testing.T) {
	cur := Reduce(act(Working, ""), sig(AsyncInputRequested, SourceHook, 20), at(20))
	raw, err := json.Marshal(cur)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "pending") {
		t.Fatalf("internal bookkeeping serialized: %s", raw)
	}
	got := Reduce(cur, sig(PromptSubmitted, SourceHook, 19), at(30))
	if !got.PendingInput || got.State != Waiting {
		t.Fatalf("stale answer resolved current question: %+v", got)
	}
}

func TestReduceSourceOnTransition(t *testing.T) {
	got := Reduce(act(Idle, ReasonReady), sig(TerminalBusy, SourceTerminal, 20), at(30))
	if got.Source != SourceTerminal {
		t.Fatalf("Source = %q, want terminal", got.Source)
	}
}

func TestReduceUsesNowForUntimedSignal(t *testing.T) {
	got := Reduce(act(Idle, ReasonReady), Signal{Kind: PromptSubmitted, Source: SourceInput}, at(77))
	if !got.Since.Equal(at(77)) {
		t.Fatalf("Since = %v, want now", got.Since)
	}
}

func TestReduceSameStateSemantics(t *testing.T) {
	t.Run("duplicate permission is not a transition", func(t *testing.T) {
		cur := Activity{State: Waiting, Reason: ReasonPermission, Detail: "Bash", Since: at(10), Source: SourceHook}
		got := Reduce(cur, Signal{Kind: PermissionRequested, At: at(11), Source: SourceHook}, at(12))
		if cur.Changed(got) || !got.Since.Equal(cur.Since) || got.Detail != "Bash" {
			t.Fatalf("duplicate moved state: %+v", got)
		}
	})
	t.Run("detail refreshes in place", func(t *testing.T) {
		cur := Activity{State: Working, Detail: "Bash", Since: at(10), Source: SourceHook}
		got := Reduce(cur, Signal{Kind: ToolStarted, At: at(11), Source: SourceHook, Detail: "Edit"}, at(12))
		if got.Detail != "Edit" || cur.Changed(got) || !got.Since.Equal(cur.Since) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("signal without detail keeps it", func(t *testing.T) {
		cur := Activity{State: Working, Detail: "Bash", Since: at(10), Source: SourceHook}
		got := Reduce(cur, sig(TerminalBusy, SourceTerminal, 11), at(12))
		if got.Detail != "Bash" {
			t.Fatalf("Detail = %q", got.Detail)
		}
	})
	t.Run("hook confirms inferred state", func(t *testing.T) {
		cur := Activity{State: Working, Since: at(10), Source: SourceTerminal, Inferred: true}
		got := Reduce(cur, sig(ToolStarted, SourceHook, 11), at(12))
		if got.Inferred || got.Source != SourceHook {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("terminal never downgrades a confirmed state", func(t *testing.T) {
		cur := Activity{State: Working, Since: at(10), Source: SourceHook}
		got := Reduce(cur, sig(TerminalBusy, SourceTerminal, 11), at(12))
		if got.Inferred || got.Source != SourceHook {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("signal clears stalled", func(t *testing.T) {
		cur := Activity{State: Working, Since: at(10), Source: SourceHook, Stalled: true}
		got := Reduce(cur, sig(ToolFinished, SourceHook, 11), at(12))
		if got.Stalled {
			t.Fatal("still stalled")
		}
	})
	t.Run("ignored signal keeps stalled", func(t *testing.T) {
		cur := Activity{State: Working, Since: at(10), Source: SourceHook, Stalled: true}
		got := Reduce(cur, sig(UserSubmitted, SourceInput, 11), at(12))
		if !got.Stalled {
			t.Fatal("stall cleared by a keystroke")
		}
	})
	t.Run("transition clears stalled", func(t *testing.T) {
		cur := Activity{State: Working, Since: at(10), Source: SourceHook, Stalled: true}
		got := Reduce(cur, sig(TurnCompleted, SourceHook, 11), at(12))
		if got.Stalled {
			t.Fatal("idle is stalled")
		}
	})
}

func TestReduceOrdering(t *testing.T) {
	now := at(100)
	t.Run("stale hook never moves state backwards", func(t *testing.T) {
		cur := act(Idle, ReasonTurnComplete) // Since = 10
		for _, k := range []Kind{ToolStarted, PromptSubmitted, ToolFinished, PermissionRequested, InputRequested, TurnFailed, Interrupted, SessionStarted} {
			got := Reduce(cur, sig(k, SourceHook, 5), now)
			if got != cur {
				t.Fatalf("%s moved state: %+v", k, got)
			}
		}
	})
	t.Run("stale hook fills empty detail of a live state", func(t *testing.T) {
		cur := act(Working, "")
		got := Reduce(cur, Signal{Kind: ToolStarted, At: at(5), Source: SourceHook, Detail: "Bash\x1b"}, now)
		want := cur
		want.Detail = "Bash"
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		cur.State, cur.Reason = Waiting, ReasonPermission
		if got := Reduce(cur, Signal{Kind: PermissionRequested, At: at(5), Source: SourceHook, Detail: "Edit"}, now); got.Detail != "Edit" {
			t.Fatalf("waiting detail = %q", got.Detail)
		}
	})
	t.Run("stale hook does not overwrite detail", func(t *testing.T) {
		cur := act(Working, "")
		cur.Detail = "Bash"
		got := Reduce(cur, Signal{Kind: ToolStarted, At: at(5), Source: SourceHook, Detail: "Edit"}, now)
		if got != cur {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("stale hook does not decorate idle", func(t *testing.T) {
		cur := act(Idle, ReasonTurnComplete)
		got := Reduce(cur, Signal{Kind: ToolStarted, At: at(5), Source: SourceHook, Detail: "Bash"}, now)
		if got != cur {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("empty stale detail is a no-op", func(t *testing.T) {
		cur := act(Working, "")
		if got := Reduce(cur, sig(ToolStarted, SourceHook, 5), now); got != cur {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("equal time still applies", func(t *testing.T) {
		got := Reduce(act(Working, ""), sig(TurnCompleted, SourceHook, 10), now)
		if got.State != Idle {
			t.Fatalf("state = %s", got.State)
		}
	})
	t.Run("holder signals ignore the hook ordering rule", func(t *testing.T) {
		for _, src := range []Source{SourceTerminal, SourceInput, SourceLifecycle} {
			got := Reduce(act(Waiting, ReasonPermission), sig(UserSubmitted, src, 5), now)
			if got.State != Working {
				t.Fatalf("%s: state = %s", src, got.State)
			}
		}
	})
	t.Run("out of order async hooks converge", func(t *testing.T) {
		// PreToolUse(t=11) , PostToolUse(t=12), Stop(t=13) arriving 13, 11, 12.
		cur := act(Working, "")
		cur = Reduce(cur, sig(TurnCompleted, SourceHook, 13), now)
		cur = Reduce(cur, sig(ToolStarted, SourceHook, 11), now)
		cur = Reduce(cur, sig(ToolFinished, SourceHook, 12), now)
		if cur.State != Idle || cur.Reason != ReasonTurnComplete {
			t.Fatalf("got %+v", cur)
		}
	})
}

func TestReduceDelayedHookOfPreviousTurn(t *testing.T) {
	now := at(100)
	t.Run("late stop does not end the next turn", func(t *testing.T) {
		// Working since 11. The next prompt (13) repeats the state, so Since stays;
		// the previous turn's async Stop (12) then lands.
		cur := act(Working, "")
		cur.Since = at(11)
		cur = Reduce(cur, sig(PromptSubmitted, SourceHook, 13), now)
		if !cur.Since.Equal(at(11)) {
			t.Fatalf("since = %s, want unchanged", cur.Since)
		}
		got := Reduce(cur, sig(TurnCompleted, SourceHook, 12), now)
		if got.State != Working || got.Reason != "" {
			t.Fatalf("late stop moved state: %+v", got)
		}
		if got.Changed(cur) {
			t.Fatalf("late stop reported a change: %+v", got)
		}
	})
	t.Run("late hook still fills empty detail", func(t *testing.T) {
		cur := act(Working, "")
		cur = Reduce(cur, sig(PromptSubmitted, SourceHook, 13), now)
		got := Reduce(cur, Signal{Kind: ToolStarted, At: at(12), Source: SourceHook, Detail: "Bash"}, now)
		if got.State != Working || got.Detail != "Bash" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("a later stop of the new turn still applies", func(t *testing.T) {
		cur := act(Working, "")
		cur = Reduce(cur, sig(PromptSubmitted, SourceHook, 13), now)
		got := Reduce(cur, sig(TurnCompleted, SourceHook, 14), now)
		if got.State != Idle || got.Reason != ReasonTurnComplete {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("seen survives a holder transition", func(t *testing.T) {
		cur := act(Working, "")
		cur = Reduce(cur, sig(PromptSubmitted, SourceHook, 13), now)
		cur = Reduce(cur, sig(TerminalClear, SourceTerminal, 14), now) // Idle since 14
		got := Reduce(cur, sig(TurnCompleted, SourceHook, 12), now)
		if got != cur {
			t.Fatalf("got %+v, want %+v", got, cur)
		}
	})
	t.Run("holder signals ignore seen", func(t *testing.T) {
		cur := act(Waiting, ReasonPermission)
		cur = Reduce(cur, Signal{Kind: PermissionRequested, At: at(20), Source: SourceHook}, now)
		if got := Reduce(cur, sig(UserSubmitted, SourceInput, 15), now); got.State != Working {
			t.Fatalf("state = %s", got.State)
		}
	})
	t.Run("seen is not serialized", func(t *testing.T) {
		cur := Reduce(act(Working, ""), sig(PromptSubmitted, SourceHook, 13), now)
		data, err := json.Marshal(cur)
		if err != nil || strings.Contains(string(data), "seen") || strings.Contains(string(data), "Seen") {
			t.Fatalf("json = %s, err = %v", data, err)
		}
	})
}

func TestReduceSanitizesDetail(t *testing.T) {
	got := Reduce(act(Idle, ReasonReady), Signal{
		Kind: PermissionRequested, At: at(20), Source: SourceHook,
		Detail: "\x1b]0;evil\x07" + strings.Repeat("x", 500),
	}, at(30))
	if n := len([]rune(got.Detail)); n > MaxDetailRunes {
		t.Fatalf("detail has %d runes", n)
	}
	if strings.ContainsAny(got.Detail, "\x1b\x07") {
		t.Fatalf("control characters survived: %q", got.Detail)
	}
}
