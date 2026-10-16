package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
)

// agentRow is one agent session as `bonsai agents` shows it: its holder's
// record and, while the holder runs, what the holder reports.
type agentRow struct {
	agenthost.Record
	info *agenthost.Info
}

// agentRows reads every holder record, active sessions only unless all.
// Running holders are asked for their current state.
func agentRows(paths agenthost.Paths, all bool) ([]agentRow, error) {
	records, err := agenthost.List(paths)
	if err != nil {
		return nil, err
	}
	var rows []agentRow
	for _, r := range records {
		if !all && !(r.Alive && r.Summary.Active()) {
			continue
		}
		row := agentRow{Record: r}
		if r.Alive {
			if info, err := agenthost.Query(r); err == nil {
				row.info = &info
				row.Summary = info.Summary
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// liveAgents are the sessions whose holders run.
func liveAgents(paths agenthost.Paths) []agentRow {
	rows, _ := agentRows(paths, false)
	return rows
}

// shortAgentID is the session ID without its sess_ prefix, cut to 8.
func shortAgentID(id string) string {
	id = strings.TrimPrefix(id, "sess_")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// agentActivity is "working" while output arrived in the last 5 s, else how
// long the terminal has been quiet.
func agentActivity(row agentRow, now time.Time) string {
	if !row.Summary.Active() {
		return "-"
	}
	last := row.LastOutputAt
	if row.info != nil {
		last = row.info.LastOutputAt
	}
	if last == nil {
		return "idle"
	}
	if quiet := now.Sub(*last); quiet >= 5*time.Second {
		return "idle " + humanDuration(quiet)
	}
	return "working"
}

func agentState(row agentRow) string {
	s := row.Summary
	switch {
	case s.Active():
		if !row.Alive {
			return "lost"
		}
		return s.State
	case s.ExitCode != nil && *s.ExitCode >= 0:
		return fmt.Sprintf("%s (%d)", s.State, *s.ExitCode)
	}
	return s.State
}

func agentUptime(row agentRow, now time.Time) string {
	s := row.Summary
	start := s.CreatedAt
	if s.StartedAt != nil {
		start = *s.StartedAt
	}
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	if start.IsZero() || end.Before(start) {
		return "-"
	}
	return humanDuration(end.Sub(start))
}

// agentClients lists the attached terminals, the writer marked with *.
func agentClients(row agentRow) string {
	if row.info == nil || len(row.info.Clients) == 0 {
		return "-"
	}
	labels := make([]string, len(row.info.Clients))
	for i, c := range row.info.Clients {
		label := c.Client
		if label == "" {
			label = "?"
		}
		if c.Writer {
			label += "*"
		}
		labels[i] = label
	}
	return strings.Join(labels, ",")
}

func writeAgentTable(out io.Writer, rows []agentRow, now time.Time) error {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tPROFILE\tWORKTREE\tSTATE\tACTIVITY\tUP\tCLIENTS")
	for _, row := range rows {
		s := row.Summary
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", shortAgentID(row.ID), s.Name, s.Provider, s.ProfileName, tildePath(row.WorkDir), agentState(row), agentActivity(row, now), agentUptime(row, now), agentClients(row))
	}
	return tw.Flush()
}

// stopAgents stops each session's holder and waits up to timeout for them
// all to exit. It returns how many were asked to stop.
func stopAgents(paths agenthost.Paths, rows []agentRow, timeout time.Duration) int {
	for _, row := range rows {
		_ = agenthost.StopHolder(paths, row.Record, "")
	}
	deadline := time.Now().Add(timeout)
	for _, row := range rows {
		agenthost.Wait(paths, row.ID, time.Until(deadline))
	}
	return len(rows)
}

func agentCount(n int) string {
	if n == 1 {
		return "1 agent"
	}
	return fmt.Sprintf("%d agents", n)
}
