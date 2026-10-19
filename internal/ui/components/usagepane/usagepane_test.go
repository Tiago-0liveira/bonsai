package usagepane

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func lim(window string, frac float64, reset time.Duration) *agents.UsageLimit {
	l := &agents.UsageLimit{ID: window, Window: window, RemainingFraction: &frac}
	if reset > 0 {
		r := testNow.Add(reset)
		l.ResetsAt = &r
	}
	return l
}

func twoAccounts() []Account {
	return []Account{
		{Provider: "claude", Name: "work-claude",
			Primary: lim("5h", 0.72, 2*time.Hour+14*time.Minute), Secondary: lim("weekly", 0.41, 76*time.Hour)},
		{Provider: "codex", Name: "codex-main",
			Primary: lim("5h", 0.98, 4*time.Hour+50*time.Minute), Secondary: lim("weekly", 0.12, 30*time.Hour)},
	}
}

func plain(s string) string { return ansi.Strip(s) }

func TestViewGeometry(t *testing.T) {
	three := append(twoAccounts(), Account{Provider: "antigravity", Name: "ag", Primary: lim("5h", 0.5, time.Hour)})
	for _, tc := range []struct {
		name     string
		accounts []Account
		width    int
		maxRows  int
		wantRows int
		contains []string
		absent   []string
	}{
		{"full blocks", twoAccounts(), 40, 20, 8, []string{"work-claude", "5h", "Week", "72%", "codex-main"}, nil},
		{"full blocks carry reset", twoAccounts(), 40, 20, 8, []string{"2h14m"}, nil},
		{"compact when blocks do not fit", three, 40, 6, 5, []string{"work-claude", "codex-main", "ag", "W"}, []string{"Week", "more", "↻"}},
		{"compact shows both headline windows", twoAccounts(), 40, 4, 4, []string{" 72%", " 41%", " 98%", " 12%"}, nil},
		{"more line when accounts do not fit", three, 40, 4, 4, []string{"+2 more"}, nil},
		{"single row shows one account", three, 40, 3, 3, []string{"work-claude"}, []string{"more"}},
		{"too small hides the pane", three, 40, 2, 0, nil, nil},
		{"narrow drops reset", twoAccounts(), 24, 20, 8, []string{"72%", "▰"}, []string{"↻"}},
		{"narrow compact drops bar", twoAccounts(), 28, 4, 4, []string{"5h", "72%", "W", "41%"}, []string{"▰", "▱", "↻"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Height(tc.accounts, tc.maxRows); got != tc.wantRows {
				t.Fatalf("Height = %d, want %d", got, tc.wantRows)
			}
			out := plain(View(tc.accounts, "2m", tc.width, tc.maxRows, testNow, false))
			if tc.wantRows == 0 {
				if out != "" {
					t.Fatalf("expected no pane, got:\n%s", out)
				}
				return
			}
			lines := strings.Split(out, "\n")
			if len(lines) != tc.wantRows {
				t.Fatalf("rows = %d, want %d:\n%s", len(lines), tc.wantRows, out)
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w != tc.width {
					t.Fatalf("line %d width = %d, want %d: %q", i, w, tc.width, l)
				}
			}
			for _, s := range tc.contains {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q in:\n%s", s, out)
				}
			}
			for _, s := range tc.absent {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q in:\n%s", s, out)
				}
			}
		})
	}
}

func TestViewStaleKeepsThreeLinesAndValues(t *testing.T) {
	a := twoAccounts()
	a[0].Err = "usage unavailable"
	a[0].Stale = true
	for _, rows := range []int{8, 20} {
		out := plain(View(a, "", 40, rows, testNow, false))
		if strings.Count(out, "\n") != 7 || strings.Count(out, "▌") != 6 ||
			!strings.Contains(out, "!") || !strings.Contains(out, "72%") || !strings.Contains(out, "41%") {
			t.Fatalf("stale account lost its three-line strip or retained values:\n%s", out)
		}
	}
}

func TestViewStatesAndASCII(t *testing.T) {
	accounts := []Account{
		{Provider: "claude", Name: "fresh", Loading: true},
		{Provider: "codex", Name: "old", Stale: true, Primary: lim("5h", 0.3, time.Hour)},
		{Provider: "codex", Name: "empty"},
	}
	out := plain(View(accounts, "", 40, 20, testNow, false))
	for _, want := range []string{"Loading…", "old", "!", "No shared windows"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	ascii := plain(View(twoAccounts(), "", 40, 20, testNow, true))
	for _, r := range ascii {
		if r > 127 {
			t.Fatalf("non-ASCII rune %q in ascii mode:\n%s", r, ascii)
		}
	}
	if !strings.Contains(ascii, "#") || !strings.Contains(ascii, "| |") {
		t.Fatalf("ascii meters or rails missing:\n%s", ascii)
	}
}

func TestViewEmpty(t *testing.T) {
	out := plain(View(nil, "", 30, 5, testNow, false))
	if !strings.Contains(out, "No agent accounts") {
		t.Fatalf("empty state missing:\n%s", out)
	}
}

func TestGolden(t *testing.T) {
	stale := twoAccounts()
	stale[1].Stale = true
	stale[1].Err = "usage unavailable"
	loading := append(stale, Account{Provider: "antigravity", Name: "new", Loading: true})
	states := append(loading, Account{Provider: "codex", Name: "unavailable", Primary: &agents.UsageLimit{Window: "5h"}}, Account{Provider: "antigravity", Name: "studio"})
	for name, got := range map[string]string{
		"two-accounts-40x12":    View(twoAccounts(), "2m", 40, 12, testNow, false),
		"stale-loading-40x12":   View(loading, "refreshing…", 40, 12, testNow, false),
		"compact-40x6":          View(twoAccounts(), "2m", 40, 6, testNow, false),
		"compact-wide-64x4":     View(twoAccounts(), "2m", 64, 4, testNow, false),
		"compact-states-40x7":   View(states, "refreshing…", 40, 7, testNow, false),
		"expanded-states-64x17": View(states, "refreshing…", 64, 17, testNow, false),
		"compact-narrow-28x4":   View(twoAccounts(), "refreshing…", 28, 4, testNow, false),
		"expanded-narrow-24x8":  View(twoAccounts(), "refreshing…", 24, 8, testNow, false),
	} {
		checkGolden(t, name, plain(got))
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	// Windows checkouts may convert the golden to CRLF.
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s differs from the golden (run with -update to accept):\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func TestViewFitsStatesAtNarrowAndWideWidths(t *testing.T) {
	states := []Account{
		{Provider: "claude", Name: "personal-with-a-very-long-name", Primary: lim("5h", .724, time.Hour), Secondary: lim("weekly", .964, 6*24*time.Hour)},
		{Provider: "codex", Name: "古いアカウント", Stale: true, Err: "refresh failed", Primary: lim("5h", .12, time.Hour), Secondary: lim("weekly", .9, 0)},
		{Provider: "antigravity", Name: "studio", Loading: true},
		{Provider: "codex", Name: "unavailable", Err: "provider unavailable"},
		{Provider: "antigravity", Name: "no-shared-windows-with-a-long-name"},
		{Provider: "codex", Name: "one-window", Secondary: lim("daily", .38, 0)},
		{Provider: "claude", Name: "unknown", Primary: &agents.UsageLimit{Window: "5h"}},
	}
	for _, ascii := range []bool{false, true} {
		for _, width := range []int{12, 16, 24, 28, 32, 40, 64} {
			for _, contentRows := range []int{len(states), len(states) * 3} {
				t.Run(fmt.Sprintf("ascii=%t/%dx%d", ascii, width, contentRows), func(t *testing.T) {
					out := plain(View(states, "refreshing...", width, contentRows+2, testNow, ascii))
					lines := strings.Split(out, "\n")
					if len(lines) != contentRows+2 {
						t.Fatalf("got %d rows, want %d:\n%s", len(lines), contentRows+2, out)
					}
					for _, line := range lines {
						if lipgloss.Width(line) != width {
							t.Fatalf("line does not fit width %d: %q", width, line)
						}
					}
					if !ascii && strings.Count(out, "▌") != contentRows {
						t.Fatalf("rail missing from account content:\n%s", out)
					}
					if width >= 24 {
						for _, percent := range []string{"72%", "96%", "12%", "90%", "38%"} {
							if !strings.Contains(out, percent) {
								t.Errorf("lost numeric value %s:\n%s", percent, out)
							}
						}
					}
					if width >= 40 {
						for _, state := range []string{"Loading", "No shared windows"} {
							if !strings.Contains(out, state) {
								t.Errorf("lost state %s:\n%s", state, out)
							}
						}
					}
					if ascii {
						// Account names are user text; the generated chrome must be ASCII.
						for _, glyph := range []string{"▌", "▰", "▱", "↻", "…", "·", "✹", "❃", "✦"} {
							if strings.Contains(out, glyph) {
								t.Errorf("non-ASCII chrome %s:\n%s", glyph, out)
							}
						}
					}
				})
			}
		}
	}
}

func TestViewUnavailableNeverImpliesCapacity(t *testing.T) {
	accounts := []Account{
		{Provider: "claude", Name: "unknown", Primary: &agents.UsageLimit{Window: "5h"}},
		{Provider: "codex", Name: "failed", Err: "usage unavailable"},
		{Provider: "antigravity", Name: "empty"},
		{Provider: "codex", Name: "loading", Loading: true},
	}
	for _, rows := range []int{6, 20} {
		out := plain(View(accounts, "", 40, rows, testNow, false))
		for _, unexpected := range []string{"%", "▰", "▱", "↻"} {
			if strings.Contains(out, unexpected) {
				t.Errorf("unavailable state implies capacity with %q:\n%s", unexpected, out)
			}
		}
		for _, want := range []string{"No shared windows", "Loading…"} {
			if !strings.Contains(out, want) {
				t.Errorf("state %q missing:\n%s", want, out)
			}
		}
		unavailable := "Unavailable"
		if rows == 6 {
			unavailable = "N/A"
		}
		if !strings.Contains(out, "5h") || !strings.Contains(out, unavailable) {
			t.Errorf("unknown window lost label or unavailable state:\n%s", out)
		}
	}
}

func TestViewCompactColumnsAndWindowLabels(t *testing.T) {
	accounts := twoAccounts()
	accounts[0].Name = "very-long-personal-account-name"
	accounts[1].Secondary = lim("daily", .9, 0)
	for _, width := range []int{28, 40, 64} {
		out := plain(View(accounts, "", width, 4, testNow, false))
		lines := strings.Split(out, "\n")[1:3]
		column := func(line, label string) int {
			return lipgloss.Width(line[:strings.Index(line, label)])
		}
		if column(lines[0], "5h") != column(lines[1], "5h") ||
			column(lines[0], "72%") != column(lines[1], "98%") {
			t.Errorf("compact columns do not align:\n%s", out)
		}
		for _, want := range []string{"5h", "W", "daily", "72%", "41%", "98%", "90%"} {
			if !strings.Contains(out, want) {
				t.Errorf("lost window label or percentage %q:\n%s", want, out)
			}
		}
	}
}

func TestViewProviderRails(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	accounts := append(twoAccounts(), Account{Provider: "antigravity", Name: "studio"})
	for _, ascii := range []bool{false, true} {
		glyph := "▌ "
		if ascii {
			glyph = "| "
		}
		for _, rows := range []int{5, 11} {
			out := View(accounts, "", 40, rows, testNow, ascii)
			for _, a := range accounts {
				want := theme.ProviderStyle(a.Provider).Render(glyph)
				count := 1
				if rows == 11 {
					count = 3
				}
				if strings.Count(out, want) != count {
					t.Errorf("%s: want %d provider-colored rails, got:\n%q", a.Provider, count, out)
				}
			}
		}
	}
}

func TestViewMetersUseAvailableWidth(t *testing.T) {
	meterWidths := func(line string) []int {
		var widths []int
		for _, field := range strings.Fields(line) {
			if strings.Trim(field, "▰▱") == "" {
				widths = append(widths, lipgloss.Width(field))
			}
		}
		return widths
	}
	for _, tc := range []struct {
		width, meters int
	}{{40, 1}, {48, 1}, {64, 2}, {80, 2}} {
		out := plain(View(twoAccounts(), "", tc.width, 4, testNow, false))
		for _, line := range strings.Split(out, "\n")[1:3] {
			widths := meterWidths(line)
			if len(widths) != tc.meters || widths[0] <= 4 {
				t.Fatalf("width %d: expected larger primary meter and %d meters, got %v:\n%s", tc.width, tc.meters, widths, out)
			}
			if len(widths) == 2 && (widths[0] < 12 || widths[1] < 4 || widths[1] > widths[0]) {
				t.Errorf("secondary meter displaced the primary: %v", widths)
			}
			if strings.HasSuffix(line, "%  │") {
				t.Errorf("compact meters left unused space at width %d: %q", tc.width, line)
			}
		}
	}
	previous := 0
	for _, width := range []int{24, 40, 64} {
		out := plain(View(twoAccounts(), "", width, 8, testNow, false))
		lines := strings.Split(out, "\n")
		five, week := meterWidths(lines[2]), meterWidths(lines[3])
		if len(five) != 1 || len(week) != 1 || five[0] != week[0] || five[0] <= previous {
			t.Fatalf("expanded meters must align and grow with width %d:\n%s", width, out)
		}
		previous = five[0]
		if width >= 40 && !strings.Contains(out, "2h14m") {
			t.Errorf("expanded meters crowded out reset captions:\n%s", out)
		}
	}
}
