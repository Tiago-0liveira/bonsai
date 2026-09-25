package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/mattn/go-isatty"
)

const (
	usageBarWidth       = 10
	usageFleetBarWidth  = 16
	usageCellWidth      = 30
	usageFleetInnerSize = 86
	usageLowThreshold   = 0.20
)

type usageDashboardRow struct {
	account    agents.Account
	usage      *agents.UsageSnapshot
	gemini5h   *agents.UsageLimit
	geminiWeek *agents.UsageLimit
	third5h    *agents.UsageLimit
	thirdWeek  *agents.UsageLimit
	err        error
}

func printUsageDashboard(out io.Writer, results []agents.AccountUsageResult) {
	renderUsageDashboard(out, results, time.Now(), usageColorEnabled(out))
}

func renderUsageDashboard(out io.Writer, results []agents.AccountUsageResult, now time.Time, color bool) {
	rows := make([]usageDashboardRow, 0, len(results))
	for _, result := range results {
		row := usageDashboardRow{account: result.Account, usage: result.Usage, err: result.Error}
		if result.Usage != nil {
			for i := range result.Usage.Limits {
				limit := &result.Usage.Limits[i]
				switch classifyUsageLimit(*limit) {
				case "gemini-5h":
					if row.gemini5h == nil {
						row.gemini5h = limit
					}
				case "gemini-weekly":
					if row.geminiWeek == nil {
						row.geminiWeek = limit
					}
				case "third-5h":
					if row.third5h == nil {
						row.third5h = limit
					}
				case "third-weekly":
					if row.thirdWeek == nil {
						row.thirdWeek = limit
					}
				}
			}
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].err != nil && rows[j].err == nil {
			return false
		}
		if rows[i].err == nil && rows[j].err != nil {
			return true
		}
		is, iok := usageRowScore(rows[i])
		js, jok := usageRowScore(rows[j])
		if iok != jok {
			return iok
		}
		if is != js {
			return is > js
		}
		return strings.ToLower(rows[i].account.Name) < strings.ToLower(rows[j].account.Name)
	})

	fmt.Fprintln(out, usageStyle("Agent Fleet Usage", "\x1b[1m", color))
	fmt.Fprintln(out)

	renderFleetSummary(out, rows, now, color)
	for _, provider := range usageProviders(rows) {
		fmt.Fprintln(out)
		renderUsageProvider(out, provider, usageRowsForProvider(rows, provider), now, color)
	}
}

func renderFleetSummary(out io.Writer, rows []usageDashboardRow, now time.Time, color bool) {
	avg, avgOK := usageFleetAverage(rows)
	ready, active, low := usageFleetStatuses(rows)
	providerCount := len(usageProviders(rows))
	title := fmt.Sprintf(
		"Fleet Capacity (%d %s · %d %s)",
		len(rows), plural(len(rows), "Account", "Accounts"),
		providerCount, plural(providerCount, "Provider", "Providers"),
	)
	fmt.Fprintln(out, usageFleetTop(title))

	poolText := "Usage Pool: " + usageSummaryValue(avg, avgOK, color)
	statusPlain := fmt.Sprintf("   ● %d Ready  ▲ %d Active  ✖ %d Low", ready, active, low)
	statusRendered := "   " +
		usageStyle(fmt.Sprintf("● %d Ready", ready), "\x1b[92m", color) + "  " +
		usageStyle(fmt.Sprintf("▲ %d Active", active), "\x1b[93m", color) + "  " +
		usageStyle(fmt.Sprintf("✖ %d Low", low), "\x1b[91m", color)
	usageFleetLine(out, poolText+statusRendered, stripUsageANSI(poolText)+statusPlain)

	nextPlain, nextRendered := usageNextReset(rows, now, color)
	if nextPlain == "" {
		usageFleetLine(out, "Next Reset: -", "Next Reset: -")
	} else {
		usageFleetLine(out, nextRendered, nextPlain)
	}
	fmt.Fprintln(out, "╰"+strings.Repeat("─", usageFleetInnerSize+2)+"╯")
}
func usageFleetTop(title string) string {
	maxTitle := usageFleetInnerSize - 2
	if utf8.RuneCountInString(title) > maxTitle {
		title = truncateRunes(title, maxTitle)
	}
	dashes := usageFleetInnerSize - utf8.RuneCountInString(title) - 1
	if dashes < 1 {
		dashes = 1
	}
	return "╭─ " + title + " " + strings.Repeat("─", dashes) + "╮"
}

func usageFleetLine(out io.Writer, rendered, plain string) {
	padding := usageFleetInnerSize - utf8.RuneCountInString(plain)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(out, "│ %s%s │\n", rendered, strings.Repeat(" ", padding))
}

func usageSummaryValue(value float64, ok, color bool) string {
	if !ok {
		return "[" + strings.Repeat("░", usageFleetBarWidth) + "]   - avg"
	}
	pct := int(value*100 + 0.5)
	plain := fmt.Sprintf("[%s] %3d%% avg", usageBar(value, usageFleetBarWidth), pct)
	return usageRankStyle(plain, value, color)
}

func usageFleetStatuses(rows []usageDashboardRow) (ready, active, low int) {
	usable := 0
	for _, row := range rows {
		if row.err != nil {
			continue
		}
		if _, ok := usageRowScore(row); !ok {
			continue
		}
		usable++
		if usageRowLow(row) {
			low++
		}
	}
	if usable == 0 {
		return 0, 0, low
	}
	for _, row := range rows {
		if row.err == nil {
			if _, ok := usageRowScore(row); ok && !usageRowLow(row) {
				ready = 1
				break
			}
		}
	}
	active = usable - low - ready
	if active < 0 {
		active = 0
	}
	return ready, active, low
}

func usageRowLow(row usageDashboardRow) bool {
	score, ok := usageRowScore(row)
	return ok && score < usageLowThreshold
}

func usageRowScore(row usageDashboardRow) (float64, bool) {
	switch row.account.Provider {
	case "antigravity":
		return usageMinRemaining(row.gemini5h, row.geminiWeek)
	default:
		return 0, false
	}
}

func usageMinRemaining(limits ...*agents.UsageLimit) (float64, bool) {
	var value float64
	found := false
	for _, limit := range limits {
		remaining, ok := usageFraction(limit)
		if !ok {
			continue
		}
		if !found || remaining < value {
			value = remaining
			found = true
		}
	}
	return value, found
}

func usagePrimaryReset(row usageDashboardRow) *time.Time {
	switch row.account.Provider {
	case "antigravity":
		if row.geminiWeek != nil && row.geminiWeek.ResetsAt != nil {
			return row.geminiWeek.ResetsAt
		}
		if row.gemini5h != nil {
			return row.gemini5h.ResetsAt
		}
	}
	return nil
}
func usageNextReset(rows []usageDashboardRow, now time.Time, color bool) (string, string) {
	var account string
	var provider agents.ProviderID
	var reset time.Time
	for _, row := range rows {
		candidate := usagePrimaryReset(row)
		if candidate == nil || !candidate.After(now) {
			continue
		}
		if reset.IsZero() || candidate.Before(reset) {
			reset = *candidate
			account = row.account.Name
			provider = row.account.Provider
		}
	}
	if reset.IsZero() {
		return "", ""
	}
	account = truncateRunes(account, 16)
	owner := account
	if len(usageProviders(rows)) > 1 {
		owner = usageProviderTitle(provider) + "/" + account
	}
	plain := fmt.Sprintf("Next Reset: %s in %s", owner, usageDuration(reset.Sub(now), true))
	return plain, usageStyle(plain, "\x1b[36m", color)
}

func renderUsageProvider(out io.Writer, provider agents.ProviderID, rows []usageDashboardRow, now time.Time, color bool) {
	title := fmt.Sprintf("◆ %s · %d %s", usageProviderTitle(provider), len(rows), plural(len(rows), "account", "accounts"))
	fmt.Fprintln(out, usageStyle(title, "\x1b[1;36m", color))

	switch provider {
	case "antigravity":
		fmt.Fprintln(out, "  "+usageStyle("Gemini Models", "\x1b[1m", color))
		renderAntigravityUsageTable(out, rows, now, color, false)

		hasThirdParty := false
		for _, row := range rows {
			if row.third5h != nil || row.thirdWeek != nil {
				hasThirdParty = true
				break
			}
		}
		if hasThirdParty {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "  "+usageStyle("Claude & GPT Models", "\x1b[1m", color))
			renderAntigravityUsageTable(out, rows, now, color, true)
		}
	default:
		fmt.Fprintln(out, "  Usage dashboard adapter not implemented yet.")
	}
}

func usageProviders(rows []usageDashboardRow) []agents.ProviderID {
	seen := make(map[agents.ProviderID]struct{})
	providers := make([]agents.ProviderID, 0)
	for _, row := range rows {
		if _, ok := seen[row.account.Provider]; ok {
			continue
		}
		seen[row.account.Provider] = struct{}{}
		providers = append(providers, row.account.Provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		return strings.ToLower(usageProviderTitle(providers[i])) < strings.ToLower(usageProviderTitle(providers[j]))
	})
	return providers
}

func usageRowsForProvider(rows []usageDashboardRow, provider agents.ProviderID) []usageDashboardRow {
	filtered := make([]usageDashboardRow, 0)
	for _, row := range rows {
		if row.account.Provider == provider {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func usageProviderTitle(provider agents.ProviderID) string {
	switch provider {
	case "antigravity":
		return "Antigravity"
	case "codex":
		return "Codex"
	case "claude-code":
		return "Claude Code"
	default:
		if provider == "" {
			return "Unknown Provider"
		}
		return string(provider)
	}
}
func renderAntigravityUsageTable(out io.Writer, rows []usageDashboardRow, now time.Time, color, thirdParty bool) {
	nameWidth := len("Account")
	for _, row := range rows {
		if n := utf8.RuneCountInString(row.account.Name); n > nameWidth {
			nameWidth = n
		}
	}
	if nameWidth > 18 {
		nameWidth = 18
	}

	leftHeader := "5h"
	rightHeader := "Weekly"
	fmt.Fprintf(out, "%s  %s  %s\n",
		padUsage("Account", nameWidth),
		padUsage(leftHeader, usageCellWidth),
		rightHeader,
	)

	for _, row := range rows {
		name := truncateRunes(row.account.Name, nameWidth)
		nameFraction, nameOK := usageFraction(row.geminiWeek)
		if !nameOK {
			nameFraction, nameOK = usageFraction(row.gemini5h)
		}
		renderedName := padUsage(name, nameWidth)
		if nameOK {
			renderedName = usageRankStyle(renderedName, nameFraction, color)
		}

		if row.err != nil {
			errCell := usageStyle(padUsage("ERR", usageCellWidth), "\x1b[91m", color)
			fmt.Fprintf(out, "%s  %s  %s\n", renderedName, errCell, usageStyle("ERR", "\x1b[91m", color))
			continue
		}

		left := row.gemini5h
		right := row.geminiWeek
		leftLabel := "5h"
		rightLabel := "Wk"
		if thirdParty {
			left = row.third5h
			right = row.thirdWeek
		}
		fmt.Fprintf(out, "%s  %s  %s\n",
			renderedName,
			renderUsageLimitCell(leftLabel, left, now, color, usageCellWidth),
			renderUsageLimitCell(rightLabel, right, now, color, 0),
		)
	}
}

func renderUsageLimitCell(label string, limit *agents.UsageLimit, now time.Time, color bool, width int) string {
	fraction, ok := usageFraction(limit)
	if !ok {
		plain := "-"
		if width > 0 {
			plain = padUsage(plain, width)
		}
		return plain
	}

	pct := int(fraction*100 + 0.5)
	reset := "-"
	if limit != nil && limit.ResetsAt != nil {
		if limit.ResetsAt.After(now) {
			reset = usageDuration(limit.ResetsAt.Sub(now), false)
		} else {
			reset = "now"
		}
	}
	plain := fmt.Sprintf("%s: [%s] %3d%%  %s", label, usageBar(fraction, usageBarWidth), pct, reset)
	if width > 0 {
		plain = padUsage(plain, width)
	}
	return usageRankStyle(plain, fraction, color)
}

func usageDuration(d time.Duration, precise bool) string {
	if d <= 0 {
		return "now"
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		return "<1m"
	}
	hours := minutes / 60
	mins := minutes % 60
	if precise && hours < 72 {
		if hours == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	if hours < 24 {
		if mins >= 30 {
			hours++
		}
		return fmt.Sprintf("%dh", hours)
	}
	days := hours / 24
	if hours%24 >= 12 {
		days++
	}
	return fmt.Sprintf("%dd", days)
}

func usageFleetAverage(rows []usageDashboardRow) (float64, bool) {
	var total float64
	count := 0
	for _, row := range rows {
		if row.err != nil {
			continue
		}
		if value, ok := usageRowScore(row); ok {
			total += value
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}
func usageFraction(limit *agents.UsageLimit) (float64, bool) {
	if limit == nil || limit.RemainingFraction == nil {
		return 0, false
	}
	value := *limit.RemainingFraction
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return value, true
}

func classifyUsageLimit(limit agents.UsageLimit) string {
	text := strings.ToLower(strings.Join([]string{limit.ID, limit.Group, limit.Label}, " "))
	window := strings.ToLower(limit.Window)
	weekly := strings.Contains(window, "week") || strings.Contains(text, "weekly")
	fiveHour := window == "5h" || strings.Contains(window, "5h") || strings.Contains(text, "5h")
	if !weekly && !fiveHour {
		return ""
	}

	switch {
	case strings.Contains(text, "gemini"):
		if weekly {
			return "gemini-weekly"
		}
		return "gemini-5h"
	case strings.Contains(text, "claude"), strings.Contains(text, "gpt"), strings.Contains(text, "3p"), strings.Contains(text, "third"):
		if weekly {
			return "third-weekly"
		}
		return "third-5h"
	default:
		return ""
	}
}

func usageBar(value float64, width int) string {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	filled := int(value*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func usageRankStyle(s string, value float64, color bool) string {
	switch {
	case value >= 0.75:
		return usageStyle(s, "\x1b[92m", color)
	case value >= 0.50:
		return usageStyle(s, "\x1b[93m", color)
	case value >= 0.25:
		return usageStyle(s, "\x1b[38;5;208m", color)
	default:
		return usageStyle(s, "\x1b[91m", color)
	}
}

func usageStyle(s, code string, enabled bool) string {
	if !enabled {
		return s
	}
	return code + s + "\x1b[0m"
}

func usageColorEnabled(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func stripUsageANSI(s string) string {
	for {
		start := strings.IndexByte(s, 0x1b)
		if start < 0 {
			return s
		}
		end := strings.IndexByte(s[start:], 'm')
		if end < 0 {
			return s[:start]
		}
		s = s[:start] + s[start+end+1:]
	}
}

func padUsage(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
