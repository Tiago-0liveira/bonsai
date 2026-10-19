package accounts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

func stripsFixture(t *testing.T) Quick {
	t.Helper()
	api := &fake.Accounts{Usages: map[agents.AccountID]agentsapi.Usage{}}
	reset := testNow.Add(2*time.Hour + 10*time.Minute)
	weekReset := testNow.Add(6 * 24 * time.Hour)
	creditReset := time.Date(testNow.Year(), testNow.Month()+1, 1, 0, 0, 0, 0, time.Local)
	expiry := time.Date(testNow.Year(), testNow.Month(), testNow.Day()+12, 12, 0, 0, 0, time.Local)
	for _, provider := range groupOrder {
		for i, name := range []string{"personal", "work"} {
			id := agents.AccountID(fmt.Sprintf("%s-%d", provider, i))
			api.Items = append(api.Items, agentsapi.Account{Account: agents.Account{ID: id, Provider: provider, Name: name}, Info: agents.AccountInfo{Plan: "Pro"}})
			u := agentsapi.Usage{Snapshot: agents.UsageSnapshot{Provider: provider}, FetchedAt: testNow.Add(-35 * time.Second)}
			if provider != "antigravity" {
				u.Headline = agents.NewUsageHeadline(
					&agents.UsageLimit{Window: "5h", RemainingFraction: ptr(.72), ResetsAt: &reset},
					&agents.UsageLimit{Window: "weekly", RemainingFraction: ptr(.96), ResetsAt: &weekReset}, nil)
			}
			if provider == "claude" {
				u.Snapshot.Credits = []agents.UsageCredit{{Label: "Credits", Unit: agents.CreditUnitUSD, Enabled: true, Balance: ptr(12.5), ResetsAt: &creditReset}}
			}
			if provider == "codex" {
				u.Snapshot.ResetCredits = []agents.ResetCredit{{Count: 3, Status: "available", ExpiresAt: &expiry}}
			}
			api.Usages[id] = u
		}
	}
	q := newQuick(api, "claude-0", false, 100, 20)
	q, _ = quickDrain(q, q.Init())
	return q
}

func TestMeterStripsSeparateAccountsWithBlankRows(t *testing.T) {
	q := stripsFixture(t)
	q.SetSize(100, 26)
	blanks := 0
	for _, line := range q.body(98, 100) {
		if line == "" {
			blanks++
		}
	}
	if blanks != len(q.accounts) {
		t.Fatalf("blank account separators = %d, want %d", blanks, len(q.accounts))
	}
	out := quickPlain(q)
	for _, want := range []string{"Remaining capacity", "CLAUDE", "CODEX", "ANTIGRAVITY", "Credits $12.50 available", "Resets ", "3 resets available", "First expires ", "Reset 2h10m", "Reset 6d"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "personal") != 3 || strings.Count(out, "work") != 3 || strings.Count(out, "No shared windows") != 2 {
		t.Fatalf("six accounts must fit:\n%s", out)
	}
	for _, a := range q.ordered() {
		if got := len(q.accountStrip(a, q.columns([]agentsapi.Account{a}, 98), 95)); got != 2 {
			t.Errorf("%s has %d rows, want 2", a.ID, got)
		}
	}
	for _, hint := range []string{"↑↓ Profile ·", "r Refresh", "m Profiles", "Esc Close"} {
		if strings.Count(out, hint) != 1 {
			t.Errorf("hint %q repeated or absent", hint)
		}
	}
	checkGolden(t, "usage-strips-100x26", out)
	q.SetSize(50, 20)
	checkGolden(t, "usage-strips-sidebar-80x24", quickPlain(q))
}

func TestMeterStripProviderRailsAndCapacityColors(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	q := stripsFixture(t)
	for _, ascii := range []bool{false, true} {
		q.ascii = ascii
		rail := "▌"
		if ascii {
			rail = "|"
		}
		for _, focus := range q.ordered() {
			q.focus = focus.ID
			for _, a := range q.ordered() {
				c := q.columns([]agentsapi.Account{a}, 98)
				lines := q.accountStrip(a, c, 95)
				for _, line := range lines {
					if !strings.Contains(line, theme.ProviderStyle(a.Provider).Render(rail)) {
						t.Errorf("%s rail lost provider color: %q", a.ID, line)
					}
				}
				marker := ">" + rail
				if got := strings.HasPrefix(ansi.Strip(lines[0]), marker); got != (a.ID == focus.ID) {
					t.Fatalf("selection mismatch: %q", lines[0])
				}
			}
		}
		for _, fraction := range []float64{.9, .6, .3, .1} {
			filled, empty := "▰", "▱"
			if ascii {
				filled, empty = "#", "."
			}
			meter := stripMeter(fraction, 10, ascii)
			n := int(fraction*10 + .5)
			if !strings.Contains(meter, rankStyle(usagefmt.RankOf(fraction)).Render(strings.Repeat(filled, n))) ||
				!strings.Contains(meter, lipgloss.NewStyle().Foreground(theme.Current.Border).Render(strings.Repeat(empty, 10-n))) {
				t.Fatalf("capacity colors lost: %q", meter)
			}
		}
	}
}

func TestUsageMetadataPreservesCreditPools(t *testing.T) {
	date := testNow.Add(48 * time.Hour)
	otherDate := date.Add(24 * time.Hour)
	s := agents.UsageSnapshot{Credits: []agents.UsageCredit{
		{Label: "Usage credits", Unit: agents.CreditUnitUSD, Enabled: true, Balance: ptr(12.5), Limit: ptr(100), Used: ptr(20), ResetsAt: &date},
		{Label: "Bonus credits", Unit: "tokens", Enabled: true, Limit: ptr(200), Used: ptr(70), ResetsAt: &otherDate},
		{Label: "Unknown", Enabled: true},
		{Label: "Disabled"},
		{Label: "Unlimited", Enabled: true, Unlimited: true},
	}}
	pools := usageMetadata(s, testNow)
	if len(pools) != 5 {
		t.Fatalf("pools merged: %v", pools)
	}
	out := fmt.Sprint(pools)
	for _, want := range []string{"Usage credits $12.50 available", "Bonus credits 130 tokens available", usageDate(date, testNow), usageDate(otherDate, testNow), "Balance unavailable", "Reset date unavailable", "Disabled Off", "Unlimited Unlimited"} {
		if !strings.Contains(out, want) {
			t.Errorf("metadata lacks %q: %s", want, out)
		}
	}
	q := stripsFixture(t)
	a := q.ordered()[0]
	e := q.usage[a.ID]
	e.Usage.Snapshot = s
	q.usage[a.ID] = e
	for _, w := range []int{25, 43, 95} {
		lines := q.accountStrip(a, q.columns([]agentsapi.Account{a}, w+3), w)
		out := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"$12.50", "130 tokens", usageDate(date, testNow), usageDate(otherDate, testNow)} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d dropped %q:\n%s", w, want, out)
			}
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > w+3 {
				t.Errorf("width %d: overflow %q", w, line)
			}
		}
	}
}

func TestResetGrantsUseOnlyAvailableUnexpiredCounts(t *testing.T) {
	sooner, later, past := testNow.Add(time.Hour), testNow.Add(24*time.Hour), testNow.Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		grants []agents.ResetCredit
		want   string
		absent bool
	}{
		{name: "missing", absent: true},
		{name: "ineligible", grants: []agents.ResetCredit{{Status: "used"}, {Status: "paused"}, {Status: "unavailable"}, {ExpiresAt: &past}, {ExpiresAt: &testNow}}, absent: true},
		{name: "unknown expiry", grants: []agents.ResetCredit{{Count: 0}}, want: "1 reset available · Expiry unavailable"},
		{name: "counts and earliest eligible", grants: []agents.ResetCredit{{Status: "available", Count: 2, ExpiresAt: &later}, {Count: 0, ExpiresAt: &sooner}, {Status: "paused", Count: 10, ExpiresAt: &past}, {Count: 99, ExpiresAt: &past}}, want: "3 resets available · First expires " + usageDate(sooner, testNow)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pools := usageMetadata(agents.UsageSnapshot{ResetCredits: tc.grants}, testNow)
			if tc.absent {
				if len(pools) != 0 {
					t.Fatalf("missing or unusable grants claimed: %v", pools)
				}
				return
			}
			if len(pools) != 1 || strings.Join(pools[0], " · ") != tc.want {
				t.Fatalf("got %v, want %s", pools, tc.want)
			}
		})
	}
}

func TestMeterStripLoadingStaleUnavailableAndExtraLimits(t *testing.T) {
	q := stripsFixture(t)
	a := q.ordered()[0]
	for _, tc := range []struct {
		name    string
		entry   usagestore.Entry
		wants   []string
		noValue bool
	}{
		{name: "loading", entry: usagestore.Entry{Refreshing: true}, wants: []string{"Loading…"}, noValue: true},
		{name: "unavailable", entry: usagestore.Entry{Err: "provider failed"}, wants: []string{"Usage unavailable", "provider failed"}, noValue: true},
		{name: "unknown window", entry: usagestore.Entry{Have: true, Usage: agentsapi.Usage{Headline: agents.NewUsageHeadline(&agents.UsageLimit{Window: "5h"}, nil, nil)}}, wants: []string{"5h", "Unavailable"}, noValue: true},
		{name: "extra limits", entry: usagestore.Entry{Have: true, Usage: agentsapi.Usage{Snapshot: agents.UsageSnapshot{Limits: []agents.UsageLimit{{Label: "Model quota", RemainingFraction: ptr(.3)}}}}}, wants: []string{"No shared windows", "Model quota", "30% left"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q.usage[a.ID] = tc.entry
			out := ansi.Strip(strings.Join(q.accountStrip(a, q.columns([]agentsapi.Account{a}, 98), 95), "\n"))
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			if tc.noValue && strings.ContainsAny(out, "%▰▱") {
				t.Fatalf("unknown shown as capacity: %s", out)
			}
		})
	}
	q = stripsFixture(t)
	e := q.usage[a.ID]
	e.Refreshing = true
	e.Usage.Stale = true
	q.usage[a.ID] = e
	out := quickPlain(q)
	for _, want := range []string{"Updated 35 seconds ago", "stale", "loading…", "72% left", "▰"} {
		if !strings.Contains(out, want) {
			t.Errorf("retained values lost %q:\n%s", want, out)
		}
	}
}

func TestUsageDatesUseLocalTimeAndDisambiguateYear(t *testing.T) {
	now := time.Date(2026, 12, 31, 22, 0, 0, 0, time.UTC)
	date := time.Date(2027, 1, 1, 1, 30, 0, 0, time.FixedZone("provider", 2*3600))
	want := date.Local().Format("2 Jan 15:04")
	if date.Local().Year() != now.Local().Year() {
		want = date.Local().Format("2 Jan 2006 15:04")
	}
	if got := usageDate(date, now); got != want {
		t.Fatalf("date=%q want %q", got, want)
	}
	nextYear := now.Add(370 * 24 * time.Hour)
	if !strings.Contains(usageDate(nextYear, now), fmt.Sprint(nextYear.Local().Year())) {
		t.Fatal("missing year for next-year deadline")
	}
}

func TestMeterStripsAlignColumnsAndKeepNarrowResets(t *testing.T) {
	q := stripsFixture(t)
	accts := q.ordered()[:2]
	e := q.usage[accts[1].ID]
	secondary := *e.Usage.Headline.Secondary
	secondary.RemainingFraction = ptr(1)
	e.Usage.Headline.Secondary = &secondary
	q.usage[accts[1].ID] = e
	for _, width := range []int{98, 88, 78} {
		c := q.columns(accts, width)
		var positions [2]int
		for i, a := range accts {
			lines := q.accountStrip(a, c, width-3)
			out := ansi.Strip(strings.Join(lines, "\n"))
			for _, want := range []string{"5h", "Week", "Reset 2h10m", "Reset 6d"} {
				if !strings.Contains(out, want) {
					t.Errorf("width %d lost %q:\n%s", width, want, out)
				}
			}
			for _, line := range lines {
				plain := ansi.Strip(line)
				if ansi.StringWidth(plain) > width {
					t.Errorf("width %d overflow: %s", width, plain)
				}
				if strings.Contains(plain, "Week") {
					positions[i] = ansi.StringWidth(strings.Split(plain, "Week")[0])
				}
			}
		}
		if positions[0] != positions[1] {
			t.Errorf("width %d: window columns shifted: %v", width, positions)
		}
	}
	l := q.usage[accts[0].ID].Usage.Headline.Primary
	lines := q.stackedWindow(l, 0, 2, 20)
	out := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"5h", "72% left", "Reset 2h10m"} {
		if !strings.Contains(out, want) {
			t.Errorf("narrow row dropped %q: %s", want, out)
		}
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Errorf("narrow row overflow: %q", line)
		}
	}
	// When even a short meter would crowd the value, the numeric reading wins.
	out = ansi.Strip(strings.Join(q.stackedWindow(l, 0, 2, 14), "\n"))
	if !strings.Contains(out, "72% left") || strings.ContainsAny(out, "▰▱") {
		t.Errorf("narrow numeric fallback: %s", out)
	}
}

func TestAntigravityPoolsShareMeterColumns(t *testing.T) {
	q := stripsFixture(t)
	a := q.ordered()[4]
	e := q.usage[a.ID]
	reset, weekReset := testNow.Add(2*time.Hour+10*time.Minute), testNow.Add(6*24*time.Hour)
	five := &agents.UsageLimit{ID: "gemini-5h", Group: "Gemini Models", Window: "5h", RemainingFraction: ptr(.72), ResetsAt: &reset}
	week := &agents.UsageLimit{ID: "gemini-weekly", Group: "Gemini Models", Window: "weekly", RemainingFraction: ptr(.96), ResetsAt: &weekReset}
	e.Usage.Headline = agents.NewUsageHeadline(five, week, []agents.UsageLimit{
		{ID: "3p-5h", Group: "Claude and GPT models", Window: "5h", RemainingFraction: ptr(.3), ResetsAt: &reset},
		{ID: "3p-weekly", Group: "Claude and GPT models", Window: "weekly", RemainingFraction: ptr(.9), ResetsAt: &weekReset},
	})
	q.usage[a.ID] = e
	q.SetSize(120, 40)
	out := quickPlain(q)
	var firstMeter, secondMeter int
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "% left") {
			continue
		}
		if strings.Count(line, "% left") != 2 {
			t.Fatalf("headline windows must share a row: %s", line)
		}
		first := strings.IndexAny(line, "▰▱")
		second := strings.IndexAny(line[strings.Index(line, "· Reset 2h10m")+len("· Reset 2h10m"):], "▰▱") + strings.Index(line, "· Reset 2h10m") + len("· Reset 2h10m")
		if firstMeter == 0 {
			firstMeter, secondMeter = first, second
		}
		if first != firstMeter || second != secondMeter {
			t.Errorf("meter columns shifted:\n%s", out)
		}
		if strings.Contains(line, "5h") && !strings.Contains(line, "Week") {
			t.Errorf("Gemini windows split: %s", line)
		}
		if strings.Contains(line, "Claude/GPT 5h") && !strings.Contains(line, "Claude/GPT Week") {
			t.Errorf("Claude/GPT windows split: %s", line)
		}
	}
	for _, want := range []string{"5h", "Week", "Claude/GPT 5h", "Claude/GPT Week"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing pool %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Gemini") {
		t.Fatalf("default windows must use plain 5h and Week labels:\n%s", out)
	}
	if five.Window != "5h" || week.Window != "weekly" || e.Usage.Headline.Extra[0].Window != "5h" {
		t.Fatal("rendering changed stored windows")
	}
	checkGolden(t, "usage-strips-antigravity-120x40", out)
}

func TestUsageValuesKeepColorsWhenMetadataWraps(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	date := testNow.Add(48 * time.Hour)
	s := agents.UsageSnapshot{
		Credits: []agents.UsageCredit{
			{Label: "Credits", Unit: agents.CreditUnitUSD, Enabled: true, Balance: ptr(12.5), ResetsAt: &date},
			{Label: "Disabled"},
			{Label: "Unlimited", Enabled: true, Unlimited: true},
		},
		ResetCredits: []agents.ResetCredit{{Count: 3, ExpiresAt: &date}},
	}
	pools := usageMetadata(s, testNow)
	out := fmt.Sprint(pools)
	for _, value := range []struct {
		text  string
		color lipgloss.Color
	}{
		{"$12.50", theme.Current.Success}, {"Off", theme.Current.Warning}, {"Unlimited", theme.Current.Success}, {"3", theme.Current.Success},
	} {
		if !strings.Contains(out, lipgloss.NewStyle().Foreground(value.color).Bold(true).Render(value.text)) {
			t.Errorf("missing color for %s: %q", value.text, out)
		}
	}
	for _, pool := range []int{0, 3} {
		color := theme.Current.Accent
		if pool == 3 {
			color = theme.Current.Warning
		}
		for _, word := range strings.Fields(usageDate(date, testNow)) {
			if !strings.Contains(pools[pool][1], lipgloss.NewStyle().Foreground(color).Bold(true).Render(word)) {
				t.Errorf("date word %s lost color", word)
			}
		}
	}
	lines := wrapUsageFields(pools[3], 20)
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Fatalf("colored metadata overflow: %q", line)
		}
		if strings.Contains(line, "\x1b[") && !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("metadata style crosses line boundary: %q", line)
		}
	}
	q := stripsFixture(t)
	l := q.usage[q.focus].Usage.Headline.Primary
	color := lipgloss.NewStyle().Foreground(theme.Current.Accent).Bold(true)
	for _, width := range []int{95, 20} {
		out := strings.Join(q.stackedWindow(l, 0, 2, width), "\n")
		if !strings.Contains(out, color.Render("2h10m")) {
			t.Errorf("width %d: automatic reset lost color: %q", width, out)
		}
	}
}

func TestAntigravityThirdPartyOnlyDoesNotDuplicateSnapshot(t *testing.T) {
	for _, headline := range []bool{false, true} {
		t.Run(fmt.Sprint("headline=", headline), func(t *testing.T) {
			q := stripsFixture(t)
			a := q.ordered()[4]
			reset, weekReset := testNow.Add(4*time.Hour+57*time.Minute), testNow.Add(7*24*time.Hour)
			limits := []agents.UsageLimit{
				{ID: "3p-weekly", Group: "Claude and GPT models", Window: "weekly", RemainingFraction: ptr(.9), ResetsAt: &weekReset},
				{ID: "3p-5h", Group: "Claude and GPT models", Window: "5h", RemainingFraction: ptr(.3), ResetsAt: &reset},
			}
			e := q.usage[a.ID]
			e.Usage.Snapshot.Limits = limits
			if headline {
				e.Usage.Headline = agents.NewUsageHeadline(nil, nil, []agents.UsageLimit{limits[1], limits[0]})
			}
			q.usage[a.ID] = e
			rows := q.accountStrip(a, q.columns(q.ordered(), 118), 115)
			if len(rows) != 3 {
				t.Fatalf("want title, default pair and one model pair; got:\n%s", ansi.Strip(strings.Join(rows, "\n")))
			}
			defaults, third := ansi.Strip(rows[1]), ansi.Strip(rows[2])
			for _, want := range []string{"5h", "Week"} {
				if !strings.Contains(defaults, want) {
					t.Errorf("missing default %s: %s", want, defaults)
				}
			}
			if strings.Count(defaults, "Unavailable") != 2 || strings.ContainsAny(defaults, "%▰▱") || strings.Contains(defaults, "Claude/GPT") {
				t.Errorf("model usage substituted for default usage: %s", defaults)
			}
			for _, want := range []string{"Claude/GPT 5h", "30% left", "Reset 4h57m", "Claude/GPT Week", "90% left", "Reset 7d"} {
				if !strings.Contains(third, want) {
					t.Errorf("missing %q from single model row: %s", want, third)
				}
			}
			out := ansi.Strip(strings.Join(rows, "\n"))
			if strings.Count(out, "% left") != 2 || strings.Contains(out, "No shared windows") || strings.Contains(out, "Claude and GPT models") {
				t.Fatalf("duplicated model quotas:\n%s", out)
			}
			if headline {
				q.SetSize(120, 40)
				checkGolden(t, "usage-strips-antigravity-missing-defaults-120x40", quickPlain(q))
			}
		})
	}
}

func TestAntigravitySnapshotFallbackKeepsDefaultAndOtherPools(t *testing.T) {
	q := stripsFixture(t)
	a := q.ordered()[4]
	e := q.usage[a.ID]
	e.Usage.Snapshot.Limits = []agents.UsageLimit{
		{ID: "gemini-weekly", Group: "Gemini Models", Window: "weekly", RemainingFraction: ptr(.72)},
		{ID: "gemini-5h", Group: "Gemini Models", Window: "5h", RemainingFraction: ptr(.41)},
		{ID: "3p-weekly", Group: "Claude and GPT models", Window: "weekly", RemainingFraction: ptr(.9)},
		{ID: "3p-5h", Group: "Claude and GPT models", Window: "5h", RemainingFraction: ptr(.3)},
		{ID: "other", Group: "Other", Label: "Model quota", Window: "weekly", RemainingFraction: ptr(.15)},
	}
	q.usage[a.ID] = e
	rows := q.accountStrip(a, q.columns(q.ordered(), 118), 115)
	if len(rows) != 4 {
		t.Fatalf("expected title, two window pairs and other quota: %v", rows)
	}
	defaults := ansi.Strip(rows[1])
	for _, want := range []string{"5h", "41% left", "Week", "72% left"} {
		if !strings.Contains(defaults, want) {
			t.Errorf("default value missing %s: %s", want, defaults)
		}
	}
	if strings.Contains(defaults, "Claude/GPT") || strings.Contains(defaults, "Gemini") {
		t.Errorf("default labels differ from Claude and Codex: %s", defaults)
	}
	if !strings.Contains(ansi.Strip(rows[3]), "15% left") {
		t.Error("unfamiliar pool was consumed as a default or Claude/GPT quota")
	}
}
