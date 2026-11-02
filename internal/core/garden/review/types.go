// Package review is the domain and durable record of Garden's local, read-only verification: reviewer reports,
// finding dismissals and the human acceptance decision of an attempt.
//
// It holds no provider, Git, process or network code. Acceptance, merge and cleanup are three distinct acts: this
// package records only that a human accepted an attempt at a commit, and exports no way to merge, close, delete or
// clean anything. Reports are append-only history; a new run adds a revision and never rewrites an old one.
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
	SeverityInfo   Severity = "info"
)

// Severities lists severities most severe first.
var Severities = []Severity{SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo}

func (s Severity) valid() bool {
	for _, v := range Severities {
		if s == v {
			return true
		}
	}
	return false
}

// Trigger is why a run started. It mirrors the config policy values.
type Trigger string

const (
	TriggerManual         Trigger = "manual"
	TriggerAwaitingReview Trigger = "awaiting-review"
	TriggerNewCommit      Trigger = "new-commit"
)

func (t Trigger) valid() bool {
	return t == TriggerManual || t == TriggerAwaitingReview || t == TriggerNewCommit
}

// State is where a reviewer run stands. Only running moves on; the other states are final.
type State string

const (
	StateRunning State = "running"
	StateReady   State = "ready"
	StateFailed  State = "failed"
	StateInvalid State = "invalid"
	StateBlocked State = "blocked"
)

func (s State) valid() bool {
	switch s {
	case StateRunning, StateReady, StateFailed, StateInvalid, StateBlocked:
		return true
	}
	return false
}

// FailCode is a fixed reason for a run that did not produce a report. Provider or Git text is never stored.
type FailCode string

const (
	FailStart       FailCode = "start-failed"
	FailInterrupted FailCode = "interrupted"
	FailTimeout     FailCode = "timeout"
	FailNoReport    FailCode = "no-report-file"
	FailInvalid     FailCode = "invalid-report"
	FailHeadMoved   FailCode = "head-moved"
	FailWorktree    FailCode = "worktree-unavailable"
	FailPolicy      FailCode = "policy-off"
	FailTooLarge    FailCode = "store-full"
)

func (c FailCode) valid() bool {
	switch c {
	case FailStart, FailInterrupted, FailTimeout, FailNoReport, FailInvalid, FailHeadMoved, FailWorktree, FailPolicy, FailTooLarge:
		return true
	}
	return false
}

// Finding is one reviewer observation. ID is a stable fingerprint, so the same observation in a later report has
// the same ID.
type Finding struct {
	ID             string   `json:"id"`
	Severity       Severity `json:"severity"`
	Title          string   `json:"title"`
	Path           string   `json:"path"`
	Line           int      `json:"line"`
	Evidence       string   `json:"evidence"`
	Recommendation string   `json:"recommendation,omitempty"`
}

// FindingID derives the stable ID from path, line and the normalized title and evidence. It does not depend on the
// finding's position in the report or on its severity.
func FindingID(path string, line int, title, evidence string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s", path, line, normalizeText(title), normalizeText(evidence))
	return "f-" + hex.EncodeToString(h.Sum(nil))[:16]
}

func normalizeText(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// Base is the ref and merge-base commit the reviewed work is measured against.
type Base struct {
	Ref       string `json:"ref"`
	MergeBase string `json:"merge_base"`
}

// Report is one reviewer run of one attempt. While running it is the durable record of the run (RunSession is
// allocated before any holder starts). Once ready it carries the findings. It is never changed after leaving
// running and never deleted.
type Report struct {
	ID        string `json:"id"`
	AttemptID string `json:"attempt_id"`
	Issue     int64  `json:"issue"`
	// Revision is monotonic per attempt, starting at 1.
	Revision int `json:"revision"`
	// Commit is the full SHA the reviewer ran on.
	Commit        string    `json:"commit"`
	Base          Base      `json:"base"`
	Summary       string    `json:"summary,omitempty"`
	Findings      []Finding `json:"findings,omitempty"`
	RunSession    string    `json:"run_session"`
	Trigger       Trigger   `json:"trigger"`
	PolicyAccount string    `json:"policy_account"`
	State         State     `json:"state"`
	FailCode      FailCode  `json:"fail_code,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ReadyAt       time.Time `json:"ready_at,omitempty"`
	// RunKey makes starting the run idempotent (see RunKey). Auto marks runs a policy started, which MaxAutoRuns counts.
	RunKey string `json:"run_key"`
	Auto   bool   `json:"auto,omitempty"`
}

// Superseded is derived: a later ready report of the same attempt exists.
func Superseded(reports []Report, r Report) bool {
	for _, o := range reports {
		if o.AttemptID == r.AttemptID && o.Revision > r.Revision && o.State == StateReady {
			return true
		}
	}
	return false
}

// LatestReady returns the highest-revision ready report of an attempt.
func LatestReady(reports []Report, attempt string) (Report, bool) {
	var best Report
	found := false
	for _, r := range reports {
		if r.AttemptID == attempt && r.State == StateReady && (!found || r.Revision > best.Revision) {
			best, found = r, true
		}
	}
	return best, found
}

var (
	shaRE     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	idRE      = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	findingRE = regexp.MustCompile(`^f-[0-9a-f]{16}$`)
)

// ValidSHA reports whether s is a full lowercase commit SHA.
func ValidSHA(s string) bool { return shaRE.MatchString(s) }

// ReportID is deterministic so a replayed record call cannot create a second report.
func ReportID(attempt string, revision int) string {
	return "rv-" + hashHex(attempt, fmt.Sprint(revision))[:20]
}

// RunKey identifies a run request: attempt, commit and trigger kind. Automatic runs pass an empty nonce, so the
// same trigger at the same commit has one key forever. A manual retry passes a fresh nonce (a request ID), so a
// double submit is idempotent but an explicit retry is a new run.
func RunKey(attempt, commit string, trigger Trigger, nonce string) string {
	return "rk-" + hashHex(attempt, commit, string(trigger), nonce)[:24]
}

func hashHex(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s\x00", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))
}
