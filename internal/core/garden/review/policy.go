package review

import (
	"sort"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

// Policy is the local review policy in the shape this package needs. The only source of an enabled Policy is the
// user's local state file (config.GardenReviewPrefs); nothing read from GitHub may build one.
type Policy struct {
	Enabled     bool
	AccountID   string
	Trigger     Trigger
	MaxAutoRuns int
	RequireCI   bool
	BlockOnHigh bool
}

// PolicyFromPrefs applies defaults. nil, an invalid or a disabled preference is the off policy.
func PolicyFromPrefs(p *config.GardenReviewPrefs) Policy {
	if p == nil || p.Validate() != nil {
		return Policy{Trigger: TriggerManual}
	}
	e := p.Effective()
	return Policy{Enabled: e.Enabled, AccountID: e.AccountID, Trigger: Trigger(e.Trigger), MaxAutoRuns: e.MaxAutoRuns,
		RequireCI: e.RequireCI, BlockOnHigh: e.BlockOnHigh}
}

// AttemptStateAwaitingReview and AttemptStateRunning are the launch states reconcile reacts to. They are plain
// strings so this package stays independent of the launch package.
const (
	AttemptStateAwaitingReview = "awaiting-human-review"
	AttemptStateRunning        = "running"
)

// AttemptFacts is what the caller observed about one Bonsai-launched attempt right now.
type AttemptFacts struct {
	AttemptID string
	State     string
	HeadSHA   string
	// WorktreeOK: the isolated worktree exists and HEAD was read from it.
	WorktreeOK bool
}

// RunRequest is a reviewer run to start. The caller records it with FileStore.RecordRun before starting a holder.
type RunRequest struct {
	AttemptID string
	Commit    string
	Trigger   Trigger
	Key       string
}

// PlanReconcile lists the automatic reviewer runs to start. It is pure and idempotent: a key already recorded in
// any state (failed and blocked included) is never produced again, so retrying is an explicit manual act.
//
// awaiting-review: the attempt awaits human review and no run exists for its current HEAD.
// new-commit: the same, and also a running attempt whose HEAD moved past the last reviewed commit.
// Both honor MaxAutoRuns (counted over automatic runs of the attempt) and allow one run in flight per attempt.
// A disabled or manual policy yields nothing.
func PlanReconcile(p Policy, attempts []AttemptFacts, runs []Report) []RunRequest {
	if !p.Enabled || p.AccountID == "" || (p.Trigger != TriggerAwaitingReview && p.Trigger != TriggerNewCommit) {
		return nil
	}
	max := p.MaxAutoRuns
	if max <= 0 {
		max = config.DefaultGardenReviewMaxAutoRuns
	}
	keys := map[string]bool{}
	inFlight := map[string]bool{}
	auto := map[string]int{}
	atCommit := map[string]map[string]bool{}
	for _, r := range runs {
		keys[r.RunKey] = true
		if r.State == StateRunning {
			inFlight[r.AttemptID] = true
		}
		if r.Auto {
			auto[r.AttemptID]++
		}
		if atCommit[r.AttemptID] == nil {
			atCommit[r.AttemptID] = map[string]bool{}
		}
		atCommit[r.AttemptID][r.Commit] = true
	}
	var out []RunRequest
	seen := map[string]bool{}
	for _, a := range attempts {
		if seen[a.AttemptID] || !a.WorktreeOK || !ValidSHA(a.HeadSHA) || inFlight[a.AttemptID] ||
			auto[a.AttemptID] >= max || atCommit[a.AttemptID][a.HeadSHA] {
			continue
		}
		switch {
		case a.State == AttemptStateAwaitingReview:
		case a.State == AttemptStateRunning && p.Trigger == TriggerNewCommit && len(atCommit[a.AttemptID]) > 0:
		default:
			continue
		}
		key := RunKey(a.AttemptID, a.HeadSHA, p.Trigger, "")
		if keys[key] {
			continue
		}
		seen[a.AttemptID] = true
		out = append(out, RunRequest{AttemptID: a.AttemptID, Commit: a.HeadSHA, Trigger: p.Trigger, Key: key})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptID < out[j].AttemptID })
	return out
}
