package accounts

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
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
	for _, want := range []string{"Antigravity · personal", "5-hour  ███████░░░", "Weekly  41%", "5h ███████░░░ · W 41%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Antigravity headline lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Limit Remaining") || strings.Contains(out, "72%") {
		t.Fatalf("raw pool text or 5h percentage in quick usage:\n%s", out)
	}

	m := opened(t, api, 120, 40)
	m = selectByID(t, m, "a1")
	m, _ = press(m, "2")
	out = plain(m)
	for _, want := range []string{"5-hour window", "Weekly window", "███████░░░", "41% remaining", "███░░░░░░░", "90% remaining"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile usage lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "72%") || strings.Contains(out, "30%") {
		t.Fatalf("five-hour pool reported as a percentage:\n%s", out)
	}
}
