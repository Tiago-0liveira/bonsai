package accounts

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

func fiveHourUsage(frac *float64, reset *time.Time, stale bool) usagestore.Entry {
	l := &agents.UsageLimit{ID: "five_hour", Window: "5h", RemainingFraction: frac, ResetsAt: reset}
	return usagestore.Entry{Have: true, Usage: agentsapi.Usage{Headline: agents.UsageHeadline{Primary: l}, Stale: stale}}
}

func TestLaunchColumnsCellRules(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in := now.Add(2*time.Hour + 10*time.Minute)
	f72, f0 := 0.72, 0.0
	weekly := &agents.UsageLimit{ID: "weekly", Window: "weekly", RemainingFraction: &f72, ResetsAt: &in}
	const id = agents.AccountID("claude:a")
	cases := []struct {
		name        string
		e           usagestore.Entry
		left, reset string
	}{
		{"value and reset", fiveHourUsage(&f72, &in, false), "72%", "2h10m"},
		{"zero is real zero", fiveHourUsage(&f0, &in, false), "0%", "2h10m"},
		{"unknown value", fiveHourUsage(nil, &in, false), "Unknown", "2h10m"},
		{"unknown reset", fiveHourUsage(&f72, nil, false), "72%", "Unknown"},
		{"stale keeps countdown", fiveHourUsage(&f72, &in, true), "72% Stale", "2h10m"},
		{"stale unknown value", fiveHourUsage(nil, &in, true), "Unknown · Stale", "2h10m"},
		{"loading", usagestore.Entry{Refreshing: true}, "Loading…", "Loading…"},
		{"never requested", usagestore.Entry{}, "Loading…", "Loading…"},
		{"failed", usagestore.Entry{Err: "boom"}, "Unavailable", "—"},
		{"no window", usagestore.Entry{Have: true, Usage: agentsapi.Usage{Headline: agents.UsageHeadline{Secondary: weekly}}}, "Not reported", "—"},
	}
	for _, c := range cases {
		cols := LaunchColumns(usagestore.Store{id: c.e}, func() time.Time { return now }, false)
		a := agentsapi.Account{Account: agents.Account{ID: id, Provider: "claude", Name: "a"}}
		left, reset := ansi.Strip(cols[0].Cell(a)), ansi.Strip(cols[1].Cell(a))
		if !strings.Contains(left, c.left) || reset != c.reset {
			t.Errorf("%s: left %q reset %q, want %q / %q", c.name, left, reset, c.left, c.reset)
		}
		if strings.Contains(c.name, "no window") && strings.Contains(left, "72") {
			t.Errorf("weekly must not stand in for the five-hour window: %q", left)
		}
	}
}

func TestLaunchColumnsNarrowing(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in := now.Add(2*time.Hour + 10*time.Minute)
	f := 0.72
	var accts []agentsapi.Account
	st := usagestore.Store{}
	for _, n := range []string{"personal", "work"} {
		a := agentsapi.Account{Account: agents.Account{ID: agents.AccountID("claude:" + n), Provider: "claude", Name: n}}
		accts = append(accts, a)
		st[a.ID] = fiveHourUsage(&f, &in, false)
	}
	tbl := Table{Accounts: accts, Selected: accts[0].ID, UnderSep: " · ", Focused: true,
		Columns: LaunchColumns(st, func() time.Time { return now }, true)}
	for _, tc := range []struct {
		w    int
		want []string
		not  []string
	}{
		{60, []string{"5H LEFT", "#######...", "72%", "2h10m"}, nil},
		{52, []string{"5H LEFT", "####.", "72%", "2h10m"}, []string{"#######..."}},
		{48, []string{"5H", "72%", "2h10m"}, []string{"#####"}},
		{34, []string{"5h left 72% · Resets in 2h10m"}, []string{"#####"}},
	} {
		out := ansi.Strip(strings.Join(tbl.View(tc.w, 8), "\n"))
		for _, s := range tc.want {
			if !strings.Contains(out, s) {
				t.Errorf("w=%d missing %q:\n%s", tc.w, s, out)
			}
		}
		for _, s := range tc.not {
			if strings.Contains(out, s) {
				t.Errorf("w=%d must not contain %q:\n%s", tc.w, s, out)
			}
		}
		for _, l := range strings.Split(out, "\n") {
			if ansi.StringWidth(l) > tc.w {
				t.Errorf("w=%d wide row %q", tc.w, l)
			}
		}
	}
}

func TestLaunchColumnsStaleUnknownFitsNarrowForms(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := agentsapi.Account{Account: agents.Account{ID: "claude:a", Provider: "claude", Name: "a"}}
	st := usagestore.Store{a.ID: fiveHourUsage(nil, nil, true)}
	col := LaunchColumns(st, func() time.Time { return now }, false)[0]
	check := func(name, got string, width int) {
		t.Helper()
		plain := ansi.Strip(got)
		if !strings.Contains(plain, "Stale") || ansi.StringWidth(plain) > width {
			t.Errorf("%s: %q must say Stale within %d cells", name, plain, width)
		}
	}
	check("wide", col.Cell(a), col.Width)
	check("medium", col.Narrow[0].Cell(a), col.Narrow[0].Width)
	check("narrow", col.Narrow[1].Cell(a), col.Narrow[1].Width)
}

func TestUsedMeterReportsUsedNotLeftAndSkipsUnknown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f66 := 0.66
	a := agentsapi.Account{}
	a.ID = "claude:a"
	meter, used, ok := UsedMeter(usagestore.Store{a.ID: fiveHourUsage(&f66, nil, false)}, a, now, 10, true)
	if !ok || used != 34 || ansi.Strip(meter) != "###......." {
		t.Fatalf("meter %q used %d ok %v", ansi.Strip(meter), used, ok)
	}
	if _, _, ok := UsedMeter(usagestore.Store{a.ID: fiveHourUsage(nil, nil, false)}, a, now, 10, true); ok {
		t.Fatal("an unknown value drew a meter")
	}
	if _, _, ok := UsedMeter(usagestore.Store{}, a, now, 10, false); ok {
		t.Fatal("a missing reading drew a meter")
	}
}
