package agentactivity

import (
	"strings"
	"testing"
)

func TestActivityChanged(t *testing.T) {
	base := Activity{State: Working, Reason: "", Detail: "Bash", Source: SourceHook}
	tests := []struct {
		name string
		mut  func(*Activity)
		want bool
	}{
		{"identical", func(*Activity) {}, false},
		{"detail only", func(a *Activity) { a.Detail = "Edit" }, false},
		{"since only", func(a *Activity) { a.Since = a.Since.Add(1) }, false},
		{"source only", func(a *Activity) { a.Source = SourceTerminal }, false},
		{"state", func(a *Activity) { a.State = Idle }, true},
		{"reason", func(a *Activity) { a.Reason = ReasonReady }, true},
		{"stalled", func(a *Activity) { a.Stalled = true }, true},
		{"inferred", func(a *Activity) { a.Inferred = true }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := base
			tt.mut(&b)
			if got := base.Changed(b); got != tt.want {
				t.Fatalf("Changed = %v, want %v", got, tt.want)
			}
			if got := b.Changed(base); got != tt.want {
				t.Fatalf("Changed (reversed) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeDetail(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Bash", "Bash"},
		{"trims", "  Bash \n", "Bash"},
		{"strips escapes", "\x1b[31mBash\x1b[0m\x07", "[31mBash[0m"},
		{"strips newline and tab", "a\nb\tc", "abc"},
		{"strips bidi override", "a‮b", "ab"},
		{"invalid utf8", "a\xffb", "ab"},
		{"keeps unicode", "ferramenta-é-日本", "ferramenta-é-日本"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeDetail(tt.in); got != tt.want {
				t.Fatalf("SanitizeDetail(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeDetailCapsRunes(t *testing.T) {
	got := SanitizeDetail(strings.Repeat("é", MaxDetailRunes+30))
	if n := len([]rune(got)); n != MaxDetailRunes {
		t.Fatalf("runes = %d, want %d", n, MaxDetailRunes)
	}
	// Trailing space at the cut must not survive.
	got = SanitizeDetail(strings.Repeat("a", MaxDetailRunes-1) + " bbb")
	if strings.HasSuffix(got, " ") {
		t.Fatalf("trailing space kept: %q", got)
	}
}
