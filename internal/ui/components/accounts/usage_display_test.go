package accounts

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func TestUsageWindowsUseBarsOnlyForFiveHoursAndColorRemaining(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	for _, tc := range []struct {
		remaining float64
		color     lipgloss.Color
	}{{0.9, theme.Current.Success}, {0.6, theme.Current.Success}, {0.3, theme.Current.Warning}, {0.1, theme.Current.Danger}} {
		t.Run(fmt.Sprint(tc.remaining), func(t *testing.T) {
			five := &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(tc.remaining)}
			week := &agents.UsageLimit{Window: "weekly", RemainingFraction: ptr(tc.remaining)}
			h := agents.NewUsageHeadline(five, week, nil)
			for _, ascii := range []bool{false, true} {
				bar := ansi.Strip(meter(tc.remaining, ascii))
				pct := fmt.Sprintf("%d%%", int(tc.remaining*100))
				style := lipgloss.NewStyle().Foreground(tc.color)
				for _, compact := range []bool{false, true} {
					out := Headline(h, testNow, ascii, compact)
					if !strings.Contains(out, style.Render(bar)) || !strings.Contains(out, style.Render(pct)) {
						t.Fatalf("bar and weekly percentage must use remaining-capacity colors: %q", out)
					}
					if strings.Count(ansi.Strip(out), "%") != 1 || strings.Count(ansi.Strip(out), bar) != 1 {
						t.Fatalf("expected one bar and one percentage: %q", out)
					}
				}
				if got := ansi.Strip(limitValue(five, ascii, true)); got != bar {
					t.Fatalf("5h value = %q, want only a bar", got)
				}
				if got := ansi.Strip(limitValue(week, ascii, true)); got != pct+" remaining" {
					t.Fatalf("weekly value = %q", got)
				}
			}
		})
	}
}

func TestAntigravityUsesHeadlineWindows(t *testing.T) {
	api := mockupFake()
	five := agents.UsageLimit{ID: "gemini-5h", Group: "Gemini Models", Label: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: ptr(0.72)}
	week := agents.UsageLimit{ID: "gemini-weekly", Group: "Gemini Models", Label: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: ptr(0.41)}
	extra := []agents.UsageLimit{
		{ID: "3p-5h", Group: "Claude and GPT models", Label: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: ptr(0.3)},
		{ID: "3p-weekly", Group: "Claude and GPT models", Label: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: ptr(0.9)},
	}
	api.Usages["a1"] = agentsapi.Usage{
		Snapshot: agents.UsageSnapshot{Limits: append([]agents.UsageLimit{week, five}, extra...)},
		Headline: agents.NewUsageHeadline(&five, &week, extra),
	}
	q := opennedQuick(t, api, "a1", false)
	out := quickPlain(q)
	for _, want := range []string{"ANTIGRAVITY", ">▌ personal", "5h", "Claude/GPT 5h", "▰▰▰▰▰▰▰▱▱▱", "72% left", "41% left", "30% left", "90% left"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Antigravity headline lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Limit Remaining") {
		t.Fatalf("raw pool text in quick usage:\n%s", out)
	}

	m := opened(t, api, 120, 40)
	m = selectByID(t, m, "a1")
	m, _ = press(m, "2")
	out = plain(m)
	for _, want := range []string{"5h", "Week", "▰▰▰▰▰▰▰▱▱▱", "41% left", "▰▰▰▱▱▱▱▱▱▱", "90% left"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile usage lacks %q:\n%s", want, out)
		}
	}
}

func TestProfilesAndUsageGroupByProviderWithIdentityColors(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	q := opennedQuick(t, quickFake(), "c1", false)
	q.SetSize(120, 30)
	m := opened(t, quickFake(), 120, 30)
	for _, provider := range []agents.ProviderID{"claude", "codex", "antigravity"} {
		// Antigravity is the widest provider name, which sizes the column.
		cellText := theme.ProviderStyle(provider).Render(cell(providerName(provider), len("Antigravity")))
		if !strings.Contains(m.View(), cellText) {
			t.Fatalf("missing colored provider cell %s", provider)
		}
		if !strings.Contains(q.View(), theme.ProviderStyle(provider).Render("▌")) {
			t.Fatalf("missing provider rail %s", provider)
		}
	}
	if strings.Contains(plain(m), "+ Add account") || !strings.Contains(plain(m), "n Add") {
		t.Fatal("adding a profile must be advertised as a shortcut")
	}
}

func TestGeminiRefreshKeepsWindowLayout(t *testing.T) {
	api := mockupFake()
	q := opennedQuick(t, api, "a1", false)
	m := selectByID(t, opened(t, api, 120, 40), "a1")
	m, _ = press(m, "2")
	// Refresh returns the API's display labels and alternate window spelling,
	// while the first view used canonical cached windows.
	limits, err := antigravity.ParseUsage([]byte(`{"groups":[{"name":"Gemini Models","buckets":[
		{"id":"g1","name":"Five Hour Limit Remaining","window":"five_hour","remaining_fraction":0.3},
		{"id":"g2","name":"Weekly Limit Remaining","window":"7_day","remaining_fraction":0.6}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := agents.UsageSnapshot{Provider: "antigravity", Limits: limits}
	api.Usages["a1"] = agentsapi.Usage{Snapshot: snapshot, Headline: antigravity.New(nil, nil).UsageHeadline(snapshot)}
	q, _ = quickKey(q, "r")
	m, _ = press(m, "r")
	for _, want := range []string{"5h", "Week", "30% left", "60% left"} {
		if !strings.Contains(quickPlain(q), want) {
			t.Fatalf("refreshed strip lost %q:\n%s", want, quickPlain(q))
		}
	}
	out := plain(m)
	for _, want := range []string{"5h", "Week", "▰▰▰▱▱▱▱▱▱▱", "60% left"} {
		if !strings.Contains(out, want) {
			t.Fatalf("refresh lost %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Limit Remaining") {
		t.Fatalf("refresh restored the pool layout:\n%s", out)
	}
}
