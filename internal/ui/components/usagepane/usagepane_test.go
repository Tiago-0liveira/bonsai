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

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
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

func TestGolden(t *testing.T) {
	stale := twoAccounts()
	stale[1].Stale = true
	stale[1].Err = "usage unavailable"
	loading := append(stale, Account{Provider: "antigravity", Name: "new", Loading: true})
	states := append(loading, Account{Provider: "codex", Name: "unavailable", Primary: &agents.UsageLimit{Window: "5h"}}, Account{Provider: "antigravity", Name: "studio"})
	for name, got := range map[string]string{
		"summary-40x5":          ViewSummaryAt(append(twoAccounts(), twoAccounts()[0]), "2m", 40, 5, testNow, false),
		"summary-ascii-40x5":    ViewSummaryAt(twoAccounts(), "2m", 40, 5, testNow, true),
		"two-accounts-40x12":    ViewPage(twoAccounts(), "2m", 40, 12, testNow, false, 0),
		"stale-loading-40x12":   ViewPage(loading, "refreshing…", 40, 12, testNow, false, 0),
		"compact-40x6":          ViewPage(twoAccounts(), "2m", 40, 6, testNow, false, 0),
		"compact-wide-64x4":     ViewPage(twoAccounts(), "2m", 64, 4, testNow, false, 0),
		"compact-states-40x7":   ViewPage(states, "refreshing…", 40, 7, testNow, false, 0),
		"expanded-states-64x17": ViewPage(states, "refreshing…", 64, 17, testNow, false, 0),
		"compact-narrow-28x4":   ViewPage(twoAccounts(), "refreshing…", 28, 4, testNow, false, 0),
		"expanded-narrow-24x8":  ViewPage(twoAccounts(), "refreshing…", 24, 8, testNow, false, 0),
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

func TestSummaryUsesMostLimitedAccount(t *testing.T) {
	rows := twoAccounts()
	other := rows[0]
	other.Name = "limited"
	other.Primary = rows[1].Secondary
	got, counts := Summary(append(rows, other))
	if len(got) != 2 || got[0].Name != "limited" || counts[0] != 2 {
		t.Fatalf("%+v %v", got, counts)
	}
}

func TestSummaryColumnsAlign(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		rows := append(twoAccounts(), Account{Provider: "antigravity", Primary: lim("5h", 1, 0), Secondary: lim("weekly", .98, 0)})
		rows = append(rows, rows[2])
		for _, width := range []int{24, 40, 60} {
			view := plain(ViewSummary(rows, "", width, 10, ascii))
			column, weekly := -1, -1
			for _, row := range strings.Split(view, "\n") {
				if i := strings.Index(row, "5h"); i >= 0 {
					cells := ansi.StringWidth(row[:i])
					if column >= 0 && cells != column {
						t.Fatalf("unaligned columns:\n%s", view)
					}
					column = cells
				}
				if i := strings.Index(row, " W "); i >= 0 {
					cells := ansi.StringWidth(row[:i])
					if weekly >= 0 && cells != weekly {
						t.Fatalf("unaligned weekly columns:\n%s", view)
					}
					weekly = cells
				}
				if ansi.StringWidth(row) > width {
					t.Fatalf("row exceeds width %d: %s", width, row)
				}
			}
		}
	}
}

func TestPageGolden(t *testing.T) {
	for _, tc := range []struct {
		name              string
		width, rows, page int
		ascii             bool
	}{
		{"page-wide", 80, 4, 0, false}, {"page-narrow", 40, 4, 0, false},
		{"page-tiny", 26, 4, 0, false}, {"page-one", 54, 3, 0, false},
		{"page-two", 54, 3, 1, false}, {"page-ascii", 54, 3, 1, true}, {"page-ascii-wide", 80, 4, 0, true}, {"page-ascii-tiny", 24, 3, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := "1/2 ▰▰▱▱"
			if tc.page == 1 {
				status = "2/2 ▰▱▱▱"
			}
			if tc.ascii {
				status = fmt.Sprintf("%d/2 #...", tc.page+1)
			}
			out := ViewPage(twoAccounts(), status, tc.width, tc.rows, testNow, tc.ascii, tc.page)
			checkGolden(t, tc.name, plain(out))
			for _, line := range strings.Split(out, "\n") {
				if lipgloss.Width(line) != tc.width {
					t.Fatalf("bad width: %q", line)
				}
			}
			if tc.width == 40 {
				if !strings.Contains(plain(out), "2h14m") {
					t.Fatal("lost primary reset")
				}
				if strings.Contains(plain(out), "▰▰▰") {
					t.Fatal("bar displaced reset")
				}
			}
			if tc.ascii {
				for _, r := range plain(out) {
					if r > 127 {
						t.Fatalf("non ASCII: %q", r)
					}
				}
			}
		})
	}
}

func TestPageGeometryAndStates(t *testing.T) {
	accounts := append(twoAccounts(), Account{Provider: "claude", Name: "loading", Loading: true}, Account{Provider: "codex", Name: "failed", Err: "unavailable"}, Account{Provider: "claude", Name: "empty"})
	for _, width := range []int{12, 24, 40, 80} {
		for _, rows := range []int{3, 4, 7} {
			for page := 0; page < Pages(len(accounts), rows); page++ {
				out := ViewPage(accounts, "1/3 #...", width, rows, testNow, true, page)
				lines := strings.Split(out, "\n")
				if len(lines) != rows {
					t.Fatalf("height = %d", len(lines))
				}
				for _, line := range lines {
					if lipgloss.Width(line) != width {
						t.Fatalf("width %d: %q", width, line)
					}
				}
			}
		}
	}
	out := plain(ViewPage(accounts, "", 80, 7, testNow, false, 0))
	for _, want := range []string{"Loading", "Unavailable", "No shared windows", "2h14m", "3d4h"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestCompactReset(t *testing.T) {
	for _, tc := range []struct {
		after time.Duration
		want  string
	}{
		{-time.Minute, "now"}, {45 * time.Minute, "45m"}, {3*time.Hour + 26*time.Minute, "3h26m"}, {52 * time.Hour, "2d4h"},
	} {
		if got := compactReset(testNow, testNow.Add(tc.after)); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.after, got, tc.want)
		}
	}
}

func TestMixedColumnsAcrossPages(t *testing.T) {
	accounts := twoAccounts()
	accounts[0].Name = "tfg"
	accounts[0].Stale = true
	accounts[1].Name = "a-very-long-account"
	for _, ascii := range []bool{false, true} {
		columns := map[string]int{}
		for page := 0; page < 2; page++ {
			view := plain(ViewPage(accounts, fmt.Sprintf("%d/2", page+1), 80, 3, testNow, ascii, page))
			checkGolden(t, fmt.Sprintf("aligned-%t-page-%d", ascii, page+1), view)
			row := strings.Split(view, "\n")[1]
			bar := "▰"
			if ascii {
				bar = "#"
			}
			for _, token := range []string{"5h", " W ", bar} {
				index := strings.Index(row, token)
				if index < 0 {
					t.Fatalf("missing %s: %s", token, view)
				}
				col := ansi.StringWidth(row[:index])
				if previous, ok := columns[token]; ok && previous != col {
					t.Fatalf("%s moved: %s", token, view)
				}
				columns[token] = col
			}
			if strings.Contains(view, "! stale") != (page == 0) {
				t.Fatalf("incorrect stale hint: %s", view)
			}
		}
	}
	checkGolden(t, "summary-reset", plain(ViewSummaryAt(accounts, "", 64, 4, testNow, false)))
}
