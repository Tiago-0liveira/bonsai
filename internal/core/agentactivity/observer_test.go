package agentactivity

import (
	"reflect"
	"strings"
	"testing"
)

func prog(n int) Observation { return Observation{Kind: ObservedProgress, Progress: n} }

func note(title, body string) Observation {
	return Observation{Kind: ObservedNotification, Title: title, Body: body}
}

var bell = Observation{Kind: ObservedBell}

func TestObserverSequences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Observation
	}{
		{"plain text", "hello\r\nworld", nil},
		{"progress busy BEL", "\x1b]9;4;3\x07", []Observation{prog(3)}},
		{"progress normal ST", "\x1b]9;4;1;40\x1b\\", []Observation{prog(1)}},
		{"progress clear", "\x1b]9;4;0\x07", []Observation{prog(0)}},
		{"progress error and paused", "\x1b]9;4;2\x07\x1b]9;4;4;10\x07", []Observation{prog(2), prog(4)}},
		{"progress clear without value", "\x1b]9;4;0;\x07", []Observation{prog(0)}},
		{"progress without state", "\x1b]9;4\x07", nil},
		{"progress bad state", "\x1b]9;4;x\x07\x1b]9;4;9\x07\x1b]9;4;-1\x07", nil},
		{"osc9 text", "\x1b]9;Codex needs approval\x07", []Observation{note("", "Codex needs approval")}},
		{"osc9 text ST", "\x1b]9;done\x1b\\", []Observation{note("", "done")}},
		{"osc9 text starting with 4", "\x1b]9;40 files changed\x07", []Observation{note("", "40 files changed")}},
		{"osc777 notify", "\x1b]777;notify;Title;Body;with;semis\x07", []Observation{note("Title", "Body;with;semis")}},
		{"osc777 title only", "\x1b]777;notify;Title\x07", []Observation{note("Title", "")}},
		{"osc777 other subcommand", "\x1b]777;precmd\x07", nil},
		{"osc99 title", "\x1b]99;i=1:d=0;Build finished\x1b\\", []Observation{note("Build finished", "")}},
		{"osc99 body", "\x1b]99;i=1:p=body;details\x1b\\", []Observation{note("", "details")}},
		{"osc99 no metadata", "\x1b]99;;hi\x07", []Observation{note("hi", "")}},
		{"lone bell", "a\x07b", []Observation{bell}},
		{"two bells", "\x07\x07", []Observation{bell, bell}},
		{"bell terminating osc is not a bell", "\x1b]0;window title\x07", nil},
		{"unrelated osc ignored", "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\", nil},
		{"csi is not osc", "\x1b[31m\x1b[0m", nil},
		{"mixed stream order", "x\x1b]9;4;3\x07\x07y\x1b]9;4;0\x07", []Observation{prog(3), bell, prog(0)}},
		{"utf8 text", "\x1b]9;aprovação\x07", []Observation{note("", "aprovação")}},
		{"invalid utf8 dropped from text", "\x1b]9;a\xffb\x07", []Observation{note("", "ab")}},
		{"CAN aborts osc", "\x1b]9;4;3\x18tail\x07", []Observation{bell}},
		{"SUB aborts osc", "\x1b]9;4;3\x1a\x07", []Observation{bell}},
		{"nested osc restarts", "\x1b]9;junk\x1b]9;4;3\x07", []Observation{prog(3)}},
		{"other escape aborts osc", "\x1b]9;junk\x1b[0m\x07", []Observation{bell}},
		{"esc esc opens a new sequence", "\x1b]9;junk\x1b\x1b]9;4;1\x07", []Observation{prog(1)}},
		{"esc in ground then osc", "\x1b\x1b]9;4;0\x07", []Observation{prog(0)}},
		{"stray esc then text", "\x1bq\x07", []Observation{bell}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o OutputObserver
			got := o.Observe([]byte(tt.in))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Every split point of a stream must give the same result as one read, which
// is what makes the observer safe on arbitrary PTY chunking.
func TestObserverSplitAcrossReads(t *testing.T) {
	streams := []string{
		"hi\x1b]9;4;3\x07mid\x1b]777;notify;T;B\x1b\\\x07\x1b]99;p=body;x\x07",
		"\x1b]9;junk\x1b]9;4;1;5\x1b\\after\x1b]9;4;0\x07",
		"\x1b]9;aprovação\x07",
	}
	for _, s := range streams {
		var whole OutputObserver
		want := whole.Observe([]byte(s))
		for i := 0; i <= len(s); i++ {
			var o OutputObserver
			got := append(o.Observe([]byte(s[:i])), o.Observe([]byte(s[i:]))...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("split at %d of %q: got %+v, want %+v", i, s, got, want)
			}
		}
		var o OutputObserver
		var got []Observation
		for i := range len(s) {
			got = append(got, o.Observe([]byte{s[i]})...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("byte-wise %q: got %+v, want %+v", s, got, want)
		}
	}
}

func TestObserverOversizePayloadDropped(t *testing.T) {
	var o OutputObserver
	huge := "\x1b]9;" + strings.Repeat("a", 3*maxOSCBytes) + "\x07"
	if got := o.Observe([]byte(huge + "\x1b]9;4;3\x07")); !reflect.DeepEqual(got, []Observation{prog(3)}) {
		t.Fatalf("got %+v", got)
	}
	if cap(o.payload) > 2*maxOSCBytes {
		t.Fatalf("payload buffer grew to %d", cap(o.payload))
	}
}

func TestObservationSignal(t *testing.T) {
	tests := []struct {
		obs  Observation
		want Kind
		ok   bool
	}{
		{prog(1), TerminalBusy, true},
		{prog(3), TerminalBusy, true},
		{prog(0), TerminalClear, true},
		{prog(2), "", false},
		{prog(4), "", false},
		{note("t", "b"), "", false},
		{bell, "", false},
	}
	for _, tt := range tests {
		s, ok := tt.obs.Signal(t0)
		if ok != tt.ok || s.Kind != tt.want {
			t.Fatalf("%+v -> %+v ok=%v", tt.obs, s, ok)
		}
		if ok && (s.Source != SourceTerminal || !s.At.Equal(t0)) {
			t.Fatalf("signal = %+v", s)
		}
	}
}
