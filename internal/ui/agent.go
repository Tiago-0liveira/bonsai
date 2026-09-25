package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/gym"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// formatElapsed turns duration into "mm:ss" or "hh:mm:ss".
func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// formatQuotaLine formats quota windows and observed freshness.
func formatQuotaLine(u *agym.Usage) string {
	if u == nil || len(u.Windows) == 0 {
		return "unknown"
	}
	var parts []string
	for _, w := range u.Windows {
		if w.Remaining != nil {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", w.Name, *w.Remaining*100))
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	res := strings.Join(parts, " · ")
	if !u.ObservedAt.IsZero() {
		ago := time.Since(u.ObservedAt).Round(time.Minute)
		if ago < time.Minute {
			res += " (just now)"
		} else {
			res += fmt.Sprintf(" (%v ago)", ago)
		}
	}
	return res
}

// renderAgentHeader builds the metadata header lines above the terminal pane.
func renderAgentHeader(view *gym.AgentView, width int) string {
	accent := lipgloss.NewStyle().Foreground(theme.Current.Accent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	valStyle := lipgloss.NewStyle().Foreground(theme.Current.Text)

	if view == nil || (view.Run == nil && view.Binding.RunID == "") {
		return labelStyle.Render("No active or recorded AI agent for this worktree.\nPress 's' or open the command palette to launch a task.")
	}

	profile := "auto"
	status := "pending"
	task := "none"
	runID := "unavailable"
	sessionID := "unavailable"
	elapsed := "00:00"

	if view.Binding.RunID != "" {
		runID = view.Binding.RunID
	}
	if view.Run != nil {
		if view.Run.SelectedProfile != "" {
			profile = view.Run.SelectedProfile
		}
		if view.Run.Status != "" {
			status = view.Run.Status
		}
		if view.Run.Task != "" {
			task = view.Run.Task
		}
		if view.Run.SessionID != "" {
			sessionID = view.Run.SessionID
		}
		if view.Run.StartedAt != nil {
			if view.Run.FinishedAt != nil {
				elapsed = formatElapsed(view.Run.FinishedAt.Sub(*view.Run.StartedAt))
			} else {
				elapsed = formatElapsed(time.Since(*view.Run.StartedAt))
			}
		}
	}

	quota := formatQuotaLine(view.Usage)

	statusStyle := valStyle
	switch status {
	case agym.RunStateRunning:
		statusStyle = lipgloss.NewStyle().Foreground(theme.Current.Success).Bold(true)
	case agym.RunStateFailed:
		statusStyle = lipgloss.NewStyle().Foreground(theme.Current.Danger).Bold(true)
	case agym.RunStateStarting, agym.RunStateStopping:
		statusStyle = lipgloss.NewStyle().Foreground(theme.Current.Warning)
	}

	var sb strings.Builder
	sb.WriteString(accent.Render("Agent") + "\n")
	sb.WriteString(fmt.Sprintf("%s %-20s %s %s\n", labelStyle.Render("Profile"), valStyle.Render(profile), labelStyle.Render("Status"), statusStyle.Render(status)))
	sb.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Task   "), valStyle.Render(task)))
	sb.WriteString(fmt.Sprintf("%s %-20s %s %s\n", labelStyle.Render("Elapsed"), valStyle.Render(elapsed), labelStyle.Render("Quota "), valStyle.Render(quota)))
	sb.WriteString(fmt.Sprintf("%s %-20s %s %s\n", labelStyle.Render("Run    "), valStyle.Render(runID), labelStyle.Render("Session"), valStyle.Render(sessionID)))
	sb.WriteString(labelStyle.Render(strings.Repeat("─", max(20, width-2))) + "\n")

	return sb.String()
}
