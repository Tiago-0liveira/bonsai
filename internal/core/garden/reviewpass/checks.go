package reviewpass

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"regexp"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type Check struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	Dir        string   `json:"dir,omitempty"`
}

func CheckFrom(c config.GardenCheck) Check {
	h := sha256.New()
	for _, part := range append(append([]string{c.Executable}, c.Args...), c.Dir) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(part))
	}
	var args []string
	if c.Args != nil {
		args = append([]string{}, c.Args...)
	}
	return Check{ID: fmt.Sprintf("ck-%x", h.Sum(nil))[:19], Name: c.Name, Executable: c.Executable, Args: args, Dir: c.Dir}
}

func (c Check) config() config.GardenCheck {
	return config.GardenCheck{Name: c.Name, Executable: c.Executable, Args: c.Args, Dir: c.Dir}
}

type Outcome string

const (
	OutcomePassed  Outcome = "passed"
	OutcomeFailed  Outcome = "failed"
	OutcomeSkipped Outcome = "skipped"
	OutcomeUnknown Outcome = "unknown"
)

type CheckResult struct {
	CheckID   string     `json:"check_id"`
	Outcome   Outcome    `json:"outcome"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Evidence  string     `json:"evidence,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

var evidenceRE = regexp.MustCompile(`^ev-[0-9a-f]{32}$`)

func evidenceValid(s string) bool { return s == "" || evidenceRE.MatchString(s) }
func timesValid(start, end *time.Time) bool {
	return (start == nil || !start.IsZero()) && (end == nil || !end.IsZero()) && (start == nil || end == nil || !end.Before(*start))
}
func (r CheckResult) valid() bool {
	if !evidenceValid(r.Evidence) || !timesValid(r.StartedAt, r.EndedAt) {
		return false
	}
	switch r.Outcome {
	case OutcomePassed:
		return r.ExitCode != nil && *r.ExitCode == 0
	case OutcomeFailed:
		return r.ExitCode == nil || *r.ExitCode != 0
	case OutcomeSkipped, OutcomeUnknown:
		return r.ExitCode == nil
	}
	return false
}

type CheckSummary struct{ Passed, Failed, Skipped, Unknown []string }

func Summary(policy Policy, results []CheckResult) CheckSummary {
	byID := map[string]Outcome{}
	for _, r := range results {
		byID[r.CheckID] = r.Outcome
	}
	var out CheckSummary
	for _, c := range policy.Checks {
		switch byID[c.ID] {
		case OutcomePassed:
			out.Passed = append(out.Passed, c.ID)
		case OutcomeFailed:
			out.Failed = append(out.Failed, c.ID)
		case OutcomeSkipped:
			out.Skipped = append(out.Skipped, c.ID)
		default:
			out.Unknown = append(out.Unknown, c.ID)
		}
	}
	return out
}
