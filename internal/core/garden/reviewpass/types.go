package reviewpass

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

type State string

const (
	StatePending             State = "pending"
	StateStarting            State = "starting"
	StateStartUnknown        State = "start-unknown"
	StateRunning             State = "running"
	StateChecking            State = "checking"
	StateAwaitingHumanReview State = "awaiting-human-review"
	StateFailed              State = "failed"
	StateStopped             State = "stopped"
	StateBlocked             State = "blocked"
)

func (s State) Active() bool {
	switch s {
	case StatePending, StateStarting, StateStartUnknown, StateRunning, StateChecking:
		return true
	}
	return false
}
func (s State) Final() bool {
	switch s {
	case StateAwaitingHumanReview, StateFailed, StateStopped, StateBlocked:
		return true
	}
	return false
}
func CanMove(from, to State) bool {
	if !(from.Active() || from.Final()) || !(to.Active() || to.Final()) {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case StatePending:
		return to == StateStarting || to == StateBlocked || to == StateStopped || to == StateFailed
	case StateStarting:
		return to == StateRunning || to == StateStartUnknown || to == StateFailed || to == StateStopped || to == StateBlocked
	case StateStartUnknown:
		return to == StateRunning || to == StateFailed || to == StateStopped
	case StateRunning:
		return to == StateChecking || to == StateFailed || to == StateStopped || to == StateBlocked
	case StateChecking:
		return to == StateAwaitingHumanReview || to == StateFailed || to == StateStopped
	}
	return false
}

const (
	CodeReviewerStartFailed  = "reviewer-start-failed"
	CodeReviewerStartUnknown = "reviewer-start-unknown"
	CodeReviewerLost         = "reviewer-lost"
	CodeReviewerExitFailed   = "reviewer-exit-failed"
	CodeScopeChanged         = "scope-changed"
	CodePermissionsMissing   = "permissions-missing"
	CodeInputsChanged        = "inputs-changed"
	CodeHeadMoved            = "head-moved"
	CodeStopRequested        = "stop-requested"
	CodeChecksFailed         = "checks-failed"
	CodeReportInvalid        = "report-invalid"
	// CodeSuperseded closes a pass that never started its reviewer because newer work replaced it.
	CodeSuperseded = "superseded"
	// CodeImplementationNotStarted closes a pass reserved for an implementation run that definitively never started.
	CodeImplementationNotStarted = "implementation-not-started"
)

var fixedCodes = map[string]bool{"": true, CodeReviewerStartFailed: true, CodeReviewerStartUnknown: true, CodeReviewerLost: true, CodeReviewerExitFailed: true, CodeScopeChanged: true, CodePermissionsMissing: true, CodeInputsChanged: true, CodeHeadMoved: true, CodeStopRequested: true, CodeChecksFailed: true, CodeReportInvalid: true, CodeSuperseded: true, CodeImplementationNotStarted: true}

type Inputs struct {
	SpecRevision    string `json:"spec_revision"`
	SpecFingerprint string `json:"spec_fingerprint"`
	SpecApprovalID  int64  `json:"spec_approval_id"`
	PlanRevision    string `json:"plan_revision"`
	PlanDecisionID  int64  `json:"plan_decision_id"`
}
type Worktree struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	BaseSHA string `json:"base_sha"`
}
type Snapshot struct {
	Local      garden.LocalRepoID `json:"local_repo_id"`
	Repo       garden.RepoID      `json:"repo_id"`
	Issue      garden.IssueID     `json:"issue_id"`
	Number     int64              `json:"number"`
	Inputs     Inputs             `json:"inputs"`
	AttemptID  string             `json:"attempt_id"`
	Worktree   Worktree           `json:"worktree"`
	Policy     Policy             `json:"policy"`
	PolicyHash string             `json:"policy_hash"`
}
type Authorization struct {
	ActorID                string    `json:"actor_id"`
	PreviewID              string    `json:"preview_id"`
	ConfirmedAt            time.Time `json:"confirmed_at"`
	EscalationAcknowledged bool      `json:"escalation_acknowledged"`
}
type Session struct {
	OperationID  string     `json:"operation_id"`
	SessionID    string     `json:"session_id"`
	State        string     `json:"state"`
	Acknowledged bool       `json:"acknowledged"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

func validateReviewerTransition(cur, next *Session) error {
	if cur == nil {
		return nil
	}
	if next == nil || cur.OperationID != next.OperationID || cur.SessionID != next.SessionID ||
		cur.Acknowledged && !next.Acknowledged ||
		cur.ExitCode != nil && (next.ExitCode == nil || *cur.ExitCode != *next.ExitCode) ||
		!writeOnceTime(cur.StartedAt, next.StartedAt) || !writeOnceTime(cur.EndedAt, next.EndedAt) {
		return ErrImmutable
	}
	if next.State == "not-started" && next.Acknowledged {
		return ErrInvalid
	}
	if cur.State == next.State {
		return nil
	}
	switch cur.State {
	case "starting":
		if next.State == "running" || next.State == "stopping" || next.State == "exited" || next.State == "failed" || next.State == "not-started" {
			return nil
		}
	case "running":
		if next.State == "stopping" || next.State == "exited" || next.State == "failed" {
			return nil
		}
	case "stopping":
		if next.State == "exited" || next.State == "failed" {
			return nil
		}
	}
	return ErrInvalid
}

func writeOnceTime(cur, next *time.Time) bool {
	return cur == nil || next != nil && cur.Equal(*next)
}

// Pass becomes final only after its reviewer is proven closed, so reserving a
// later pass cannot leave two reviewers active for the same attempt.
type Pass struct {
	ID                string        `json:"id"`
	Revision          uint64        `json:"revision"`
	Snapshot          Snapshot      `json:"snapshot"`
	Authorization     Authorization `json:"authorization"`
	State             State         `json:"state"`
	Code              string        `json:"code,omitempty"`
	ImplementationRun string        `json:"implementation_run,omitempty"`
	ReviewedHead      string        `json:"reviewed_head,omitempty"`
	FinalHead         string        `json:"final_head,omitempty"`
	Reviewer          *Session      `json:"reviewer,omitempty"`
	Checks            []CheckResult `json:"checks,omitempty"`
	Report            string        `json:"report,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	EndedAt           *time.Time    `json:"ended_at,omitempty"`
}

func NewPass(id string, snap Snapshot, auth Authorization, now time.Time) (Pass, error) {
	if !authorized(snap, auth) {
		return Pass{}, ErrNotAuthorized
	}
	snap.PolicyHash = snap.Policy.Hash()
	p := Pass{ID: id, Revision: 1, Snapshot: snap, Authorization: auth, State: StatePending, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if !p.valid() {
		return Pass{}, ErrInvalid
	}
	return clonePass(p), nil
}
func authorized(s Snapshot, a Authorization) bool {
	return strings.TrimSpace(a.ActorID) != "" && strings.TrimSpace(a.PreviewID) != "" && !a.ConfirmedAt.IsZero() && (!s.Policy.Reviewer.Escalated || a.EscalationAcknowledged)
}

var (
	passRE      = regexp.MustCompile(`^rp-[0-9a-f]{32}$`)
	attemptRE   = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
	hashRE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	shaRE       = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	sessionRE   = regexp.MustCompile(`^sess_[0-9a-f]{1,64}$`)
	runSuffixRE = regexp.MustCompile(`^[1-9][0-9]{0,3}$`)
)

func safeText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) < 0
}
func (p Pass) valid() bool {
	s := p.Snapshot
	i := s.Inputs
	w := s.Worktree
	if !passRE.MatchString(p.ID) || p.Revision == 0 || s.Local == "" || !safeText(string(s.Local), 256) || s.Repo == 0 || s.Issue == 0 || s.Number <= 0 ||
		!hashRE.MatchString(i.SpecRevision) || !hashRE.MatchString(i.SpecFingerprint) || i.SpecApprovalID <= 0 || !hashRE.MatchString(i.PlanRevision) || i.PlanDecisionID <= 0 ||
		!attemptRE.MatchString(s.AttemptID) || !filepath.IsAbs(w.Path) || filepath.Clean(w.Path) != w.Path || !safeText(w.Path, 4096) || w.Branch == "" || !safeText(w.Branch, 255) || !shaRE.MatchString(w.BaseSHA) ||
		!hashRE.MatchString(s.PolicyHash) || s.PolicyHash != s.Policy.Hash() || s.Policy.Validate() != nil || !authorized(s, p.Authorization) || !safeText(p.Authorization.ActorID, 128) || !safeText(p.Authorization.PreviewID, 128) || !safeText(s.Policy.Reviewer.AccountName, 128) ||
		!(p.State.Active() || p.State.Final()) || !fixedCodes[p.Code] || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) || !evidenceValid(p.Report) {
		return false
	}
	if p.EndedAt != nil && (p.EndedAt.IsZero() || p.EndedAt.Before(p.CreatedAt)) {
		return false
	}
	if p.State.Final() != (p.EndedAt != nil) {
		return false
	}
	if p.ImplementationRun != "" {
		suffix, ok := strings.CutPrefix(p.ImplementationRun, s.AttemptID+"/start/")
		if !ok || !runSuffixRE.MatchString(suffix) {
			return false
		}
	}
	if (p.ReviewedHead != "" && !shaRE.MatchString(p.ReviewedHead)) || (p.FinalHead != "" && (!shaRE.MatchString(p.FinalHead) || p.ReviewedHead == "")) {
		return false
	}
	if r := p.Reviewer; r != nil {
		if r.OperationID != p.ID+"/review" || !sessionRE.MatchString(r.SessionID) || !timesValid(r.StartedAt, r.EndedAt) {
			return false
		}
		switch r.State {
		case "starting", "stopping":
			if r.EndedAt != nil || r.ExitCode != nil {
				return false
			}
		case "running":
			if !r.Acknowledged || r.EndedAt != nil || r.ExitCode != nil {
				return false
			}
		case "exited", "failed":
			if !r.Acknowledged || r.EndedAt == nil {
				return false
			}
		case "not-started":
			if r.Acknowledged || r.ExitCode != nil {
				return false
			}
		default:
			return false
		}
	}
	r := p.Reviewer
	switch p.State {
	case StatePending:
		if r != nil {
			return false
		}
	case StateStarting, StateStartUnknown:
		if r == nil || r.State != "starting" || p.State == StateStartUnknown && r.Acknowledged {
			return false
		}
	case StateRunning:
		if r == nil || (r.State != "running" && r.State != "stopping") || !r.Acknowledged {
			return false
		}
	case StateChecking, StateAwaitingHumanReview:
		if r == nil || r.State != "exited" || p.State == StateAwaitingHumanReview && p.FinalHead == "" {
			return false
		}
	case StateFailed, StateStopped, StateBlocked:
		if r != nil && r.State != "exited" && r.State != "failed" && r.State != "not-started" {
			return false
		}
	}
	ids := map[string]bool{}
	for _, c := range s.Policy.Checks {
		ids[c.ID] = true
	}
	seen := map[string]bool{}
	for _, r := range p.Checks {
		if !ids[r.CheckID] || seen[r.CheckID] || !r.valid() {
			return false
		}
		seen[r.CheckID] = true
	}
	return true
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func clonePass(p Pass) Pass {
	p.Snapshot.Policy.Reviewer.Native.FullAccess = clonePtr(p.Snapshot.Policy.Reviewer.Native.FullAccess)
	if p.Snapshot.Policy.Checks != nil {
		checks := make([]Check, len(p.Snapshot.Policy.Checks))
		for i, c := range p.Snapshot.Policy.Checks {
			checks[i] = c
			if c.Args != nil {
				checks[i].Args = append([]string{}, c.Args...)
			}
		}
		p.Snapshot.Policy.Checks = checks
	}
	if p.Reviewer != nil {
		r := *p.Reviewer
		r.ExitCode = clonePtr(r.ExitCode)
		r.StartedAt = clonePtr(r.StartedAt)
		r.EndedAt = clonePtr(r.EndedAt)
		p.Reviewer = &r
	}
	if p.Checks != nil {
		checks := make([]CheckResult, len(p.Checks))
		for i, r := range p.Checks {
			checks[i] = r
			checks[i].ExitCode = clonePtr(r.ExitCode)
			checks[i].StartedAt = clonePtr(r.StartedAt)
			checks[i].EndedAt = clonePtr(r.EndedAt)
		}
		p.Checks = checks
	}
	p.EndedAt = clonePtr(p.EndedAt)
	return p
}

const (
	MaxStoreBytes = 4 << 20
	MaxPasses     = 2000
)

type Store interface {
	Get(context.Context, string) (Pass, error)
	List(context.Context, garden.LocalRepoID, garden.IssueID) ([]Pass, error)
	ForAttempt(context.Context, string) ([]Pass, error)
	Reserve(context.Context, Pass) (Pass, bool, error)
	Update(context.Context, string, func(Pass) (Pass, bool, error)) (Pass, error)
	Lock(context.Context, string) (func(), error)
}
