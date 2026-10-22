package playgroundbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// fakeHost is the agent runtime: it records every start and stop and answers
// queries from a script. Nothing here starts a process.
type fakeHost struct {
	mu        sync.Mutex
	supported bool
	starts    []agentsapi.OperationStartRequest
	stops     []string
	startErr  error
	queryErr  error
	answers   map[string]agentsapi.OperationSession
}

func (h *fakeHost) Supported() bool { return h.supported }

func (h *fakeHost) StartOperation(_ context.Context, req agentsapi.OperationStartRequest) (agentsapi.OperationSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starts = append(h.starts, req)
	if h.startErr != nil {
		return agentsapi.OperationSession{}, h.startErr
	}
	res := h.session(string(req.SessionID), req.OperationID, "running", true, false)
	h.answers[string(req.SessionID)] = res
	return res, nil
}

func (h *fakeHost) QueryOperation(_ context.Context, id agents.SessionID, op string) (agentsapi.OperationSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.queryErr != nil {
		return agentsapi.OperationSession{}, h.queryErr
	}
	if a, ok := h.answers[string(id)]; ok {
		return a, nil
	}
	return h.session(string(id), op, "", false, true), nil // no holder: fenced
}

func (h *fakeHost) Stop(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stops = append(h.stops, id)
	return nil
}

func (h *fakeHost) session(id, op, state string, ack, closed bool) agentsapi.OperationSession {
	return agentsapi.OperationSession{Session: agentsapi.Session{Summary: agentterminal.Summary{ID: id, OperationID: op, State: state}}, Acknowledged: ack, Closed: closed}
}

// finish scripts the holder's answer for a started session.
func (h *fakeHost) finish(id, state string, ack, closed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.answers[id] = h.session(id, runOperationID(h.opKey(id)), state, ack, closed)
}

func (h *fakeHost) opKey(id string) string {
	for _, s := range h.starts {
		if string(s.SessionID) == id {
			return strings.TrimPrefix(s.OperationID, "review/")
		}
	}
	return ""
}

func (h *fakeHost) startCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.starts)
}

type fakeAccounts struct {
	list []agentsapi.Account
	err  error
}

func (f *fakeAccounts) List(context.Context) ([]agentsapi.Account, error) { return f.list, f.err }

func account(id string, ro agents.ReadOnlySupport) agentsapi.Account {
	return agentsapi.Account{Account: agents.Account{ID: agents.AccountID(id), Name: "Reviewer " + id, Provider: "claude"}, ReadOnly: ro}
}

var enforced = agents.ReadOnlySupport{Supported: true, PlanFile: true, Mechanism: "sandbox"}

type fakeLister struct{ list []launch.Attempt }

func (f *fakeLister) List(_ context.Context, local garden.LocalRepoID, _ garden.IssueID) ([]launch.Attempt, error) {
	var out []launch.Attempt
	for _, a := range f.list {
		if a.Local == local {
			out = append(out, a)
		}
	}
	return out, nil
}

// reviewRig is a rig whose backend also has the review services, over a real
// review store and report directory in temp dirs.
type reviewRig struct {
	*rig
	host    *fakeHost
	store   *review.FileStore
	accts   *fakeAccounts
	lister  *fakeLister
	prefs   *config.GardenReviewPrefs
	reports review.ReportDir
	storeAt string
}

func newReviewRig(t *testing.T) *reviewRig {
	t.Helper()
	r := newRig(t, "main")
	rr := &reviewRig{rig: r, host: &fakeHost{supported: true, answers: map[string]agentsapi.OperationSession{}},
		accts: &fakeAccounts{list: []agentsapi.Account{account("acct", enforced)}}, lister: &fakeLister{}}
	rr.storeAt = filepath.Join(t.TempDir(), "review.json")
	rr.store = &review.FileStore{Path: rr.storeAt, Now: func() time.Time { return r.tick }}
	reportBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rr.reports = review.ReportDir{Root: filepath.Join(reportBase, "reviews")}
	rr.prefs = &config.GardenReviewPrefs{Enabled: true, AccountID: "acct", Trigger: config.GardenReviewTriggerAwaitingReview}
	rr.wire(r.b)
	return rr
}

func (rr *reviewRig) wire(b *Backend) {
	b.deps.Review, b.deps.Host, b.deps.Accounts, b.deps.LaunchAttempts, b.deps.ReportDir = rr.store, rr.host, rr.accts, rr.lister, rr.reports
	b.deps.Prefs = func() (*config.GardenReviewPrefs, error) { return rr.prefs, nil }
}

// attempt adds a Bonsai-launched attempt in its own worktree with one commit
// ahead of main, and an implementation session in it.
func (rr *reviewRig) attempt(id string, issue int64, state launch.State) (wt, session string) {
	base := sh(rr.t, rr.main, "rev-parse", "HEAD")
	wt = rr.worktree("feat-" + id)
	write(rr.t, filepath.Join(wt, id+".txt"), "work\n")
	rr.commit(wt, "work "+id)
	a := launch.Attempt{ID: id, Local: rr.b.local, Issue: garden.IssueID(issue), Number: issue, Repo: 99, State: state, Cols: 100, Rows: 30}
	a.Proposal.Path, a.Proposal.Branch, a.Proposal.BaseRef, a.Proposal.BaseSHA = wt, "feat-"+id, "main", base
	rr.attempts.byID[id] = a
	rr.lister.list = append(rr.lister.list, a)
	session = "s-" + id
	rr.add(session, wt)
	rr.sessions.list[len(rr.sessions.list)-1].Task = &agents.TaskRef{IssueID: uint64(issue), AttemptID: id, Role: "implementation"}
	return wt, session
}

func (rr *reviewRig) head(wt string) string { return sh(rr.t, wt, "rev-parse", "HEAD") }

const validReport = `{"schema":1,"summary":"looks fine","findings":[{"severity":"high","title":"Null deref","path":"base.txt","line":1,"evidence":"line one","recommendation":"check it"}]}`

// finishWithReport writes the run's report file (as the provider would) and
// scripts the session as exited.
func (rr *reviewRig) finishWithReport(start int, body string) {
	rr.t.Helper()
	req := rr.host.starts[start]
	if body != "" {
		if err := os.WriteFile(req.Launch.PlanFile, []byte(body), 0o600); err != nil {
			rr.t.Fatal(err)
		}
	}
	rr.host.finish(string(req.SessionID), "exited", true, true)
}

func (rr *reviewRig) snapshot() review.Snapshot {
	rr.t.Helper()
	s, err := rr.store.Load(context.Background(), rr.b.local)
	if err != nil {
		rr.t.Fatal(err)
	}
	return s
}

func writeFileBytes(path string, b []byte) error { return os.WriteFile(path, b, 0o600) }

// prefsErrBackend makes the policy unreadable.
func (rr *reviewRig) prefsErrBackend() {
	rr.b.deps.Prefs = func() (*config.GardenReviewPrefs, error) { return nil, errors.New("state unreadable") }
}
