package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const (
	schemaVersion = 1
	// MaxStoreBytes bounds the whole file before JSON parsing.
	MaxStoreBytes = 16 << 20
	// Per-clone caps. A full store refuses new records rather than dropping history.
	MaxReports    = 500
	MaxFindingsIn = 5000
	MaxDismissals = 2000
	MaxDecisions  = 500
)

// repoData is everything recorded for one local clone.
type repoData struct {
	Reports    []Report    `json:"reports"`
	Dismissals []Dismissal `json:"dismissals"`
	Decisions  []Decision  `json:"decisions"`
}

type disk struct {
	SchemaVersion int                 `json:"schema_version"`
	Repos         map[string]repoData `json:"repos"`
}

// Snapshot is a copy of one clone's records. Another clone's records are never in it.
type Snapshot struct {
	Reports    []Report
	Dismissals []Dismissal
	Decisions  []Decision
}

// FileStore is the locked, atomically replaced JSON file every Bonsai process shares, keyed by local repo ID.
// It stores run identity, findings, dismissal reasons and decisions only: never a prompt, terminal output,
// command line, environment value or credential.
type FileStore struct {
	Path string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Open resolves the default location (config.GardenReviewPath).
func Open() (*FileStore, error) {
	p, err := config.GardenReviewPath()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: p}, nil
}

func (s *FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *FileStore) read() (disk, error) {
	d := disk{SchemaVersion: schemaVersion, Repos: map[string]repoData{}}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(b) > MaxStoreBytes {
		return disk{}, fmt.Errorf("%w: file", ErrFull)
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return disk{}, ErrCorrupt
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > schemaVersion {
		return disk{}, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	d.Repos = nil
	if err := dec.Decode(&d); err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dec.Decode(new(any)) != io.EOF || d.SchemaVersion != schemaVersion || d.Repos == nil {
		return disk{}, ErrCorrupt
	}
	for k, r := range d.Repos {
		if k == "" || !validRepo(r) {
			return disk{}, ErrCorrupt
		}
	}
	return d, nil
}

func validRepo(r repoData) bool {
	if len(r.Reports) > MaxReports || len(r.Dismissals) > MaxDismissals || len(r.Decisions) > MaxDecisions {
		return false
	}
	revs := map[string]bool{}
	findings := 0
	for _, rp := range r.Reports {
		k := fmt.Sprintf("%s\x00%d", rp.AttemptID, rp.Revision)
		if revs[k] || !rp.valid() {
			return false
		}
		revs[k] = true
		findings += len(rp.Findings)
	}
	if findings > MaxFindingsIn {
		return false
	}
	for _, d := range r.Dismissals {
		if !idRE.MatchString(d.AttemptID) || d.ReportRevision < 1 || !findingRE.MatchString(d.FindingID) ||
			!ValidSHA(d.Commit) || d.At.IsZero() {
			return false
		}
		if _, err := CleanReason(d.Reason); err != nil {
			return false
		}
	}
	for _, d := range r.Decisions {
		if !idRE.MatchString(d.ID) || !idRE.MatchString(d.AttemptID) || !ValidSHA(d.ReviewedHead) || d.Origin != OriginLocal ||
			d.ReportRevision < 0 || !d.CI.State.valid() || d.DecidedAt.IsZero() {
			return false
		}
	}
	return true
}

func (r Report) valid() bool {
	if !idRE.MatchString(r.ID) || !idRE.MatchString(r.AttemptID) || r.Revision < 1 || !ValidSHA(r.Commit) ||
		!idRE.MatchString(r.RunSession) || !r.Trigger.valid() || !r.State.valid() || r.CreatedAt.IsZero() ||
		!idRE.MatchString(r.RunKey) || len(r.Findings) > MaxFindings {
		return false
	}
	if (r.State == StateFailed || r.State == StateBlocked || r.State == StateInvalid) != (r.FailCode != "") ||
		(r.FailCode != "" && !r.FailCode.valid()) {
		return false
	}
	if r.State != StateReady && (len(r.Findings) > 0 || r.Summary != "") {
		return false
	}
	for _, f := range r.Findings {
		if !findingRE.MatchString(f.ID) || !f.Severity.valid() || !validRepoPath(f.Path) || f.Line < 0 {
			return false
		}
	}
	return true
}

func (s *FileStore) write(d disk) error {
	d.SchemaVersion = schemaVersion
	// Never write what read would refuse.
	for _, r := range d.Repos {
		if !validRepo(r) {
			return fmt.Errorf("%w: record", ErrInvalid)
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(b) > MaxStoreBytes {
		return ErrFull
	}
	return gitstore.WriteJSON(s.Path, b)
}

func (s *FileStore) withLock(ctx context.Context, fn func(*disk) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l, err := procstore.TryLock(s.Path + ".lock")
		if err == nil {
			defer func() { _ = l.Unlock() }()
			d, err := s.read()
			if err != nil {
				return err
			}
			write, err := fn(&d)
			if err != nil || !write {
				return err
			}
			return s.write(d)
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func repoKey(local garden.LocalRepoID) (string, error) {
	if local == "" {
		return "", fmt.Errorf("%w: local repository", ErrInvalid)
	}
	return string(local), nil
}

// Load returns a copy of one clone's records. Reports are ordered by attempt then revision.
func (s *FileStore) Load(ctx context.Context, local garden.LocalRepoID) (Snapshot, error) {
	k, err := repoKey(local)
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		r := d.Repos[k]
		out = Snapshot{Reports: append([]Report(nil), r.Reports...), Dismissals: append([]Dismissal(nil), r.Dismissals...),
			Decisions: append([]Decision(nil), r.Decisions...)}
		return false, nil
	})
	sort.SliceStable(out.Reports, func(i, j int) bool {
		if out.Reports[i].AttemptID != out.Reports[j].AttemptID {
			return out.Reports[i].AttemptID < out.Reports[j].AttemptID
		}
		return out.Reports[i].Revision < out.Reports[j].Revision
	})
	return out, err
}

// RunInput is a run to record before its holder starts.
type RunInput struct {
	AttemptID string
	Issue     int64
	Commit    string
	Base      Base
	// RunSession is the holder session ID allocated before the holder starts.
	RunSession    string
	Trigger       Trigger
	PolicyAccount string
	// Key is the idempotency key (RunKey). Auto marks a policy-started run.
	Key  string
	Auto bool
	// MaxAutoRuns is enforced under the store lock for automatic runs. Zero uses the default.
	MaxAutoRuns int
}

// RecordRun durably records a running run. The same Key again returns the stored report (created false), in any
// state, so a restarted reconcile or a double submit cannot start a second holder. A different key while the
// attempt has a run in flight fails with ErrRunInFlight. The new revision is the attempt's highest plus one.
func (s *FileStore) RecordRun(ctx context.Context, local garden.LocalRepoID, in RunInput) (Report, bool, error) {
	k, err := repoKey(local)
	if err != nil {
		return Report{}, false, err
	}
	if !idRE.MatchString(in.AttemptID) || !ValidSHA(in.Commit) || !idRE.MatchString(in.RunSession) || !in.Trigger.valid() ||
		!idRE.MatchString(in.Key) || in.PolicyAccount == "" || len(in.PolicyAccount) > 128 || in.Issue < 0 ||
		in.Base.Ref == "" || len(in.Base.Ref) > 256 || (in.Base.MergeBase != "" && !ValidSHA(in.Base.MergeBase)) {
		return Report{}, false, ErrInvalid
	}
	var out Report
	var created bool
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		r := d.Repos[k]
		rev, auto := 0, 0
		for _, x := range r.Reports {
			if x.RunKey == in.Key {
				out = x
				return false, nil
			}
			if x.AttemptID == in.AttemptID {
				if x.Auto {
					auto++
				}
				if x.State == StateRunning {
					return false, ErrRunInFlight
				}
				if x.Revision > rev {
					rev = x.Revision
				}
			}
		}
		limit := in.MaxAutoRuns
		if limit <= 0 {
			limit = config.DefaultGardenReviewMaxAutoRuns
		}
		limit = min(limit, config.MaxGardenReviewMaxAutoRuns)
		if in.Auto && auto >= limit {
			return false, ErrAutoLimit
		}
		if len(r.Reports) >= MaxReports {
			return false, ErrFull
		}
		rev++
		out = Report{ID: ReportID(in.AttemptID, rev), AttemptID: in.AttemptID, Issue: in.Issue, Revision: rev, Commit: in.Commit,
			Base: in.Base, RunSession: in.RunSession, Trigger: in.Trigger, PolicyAccount: in.PolicyAccount, State: StateRunning,
			CreatedAt: s.now(), RunKey: in.Key, Auto: in.Auto}
		r.Reports = append(r.Reports, out)
		d.Repos[k] = r
		created = true
		return true, nil
	})
	if err != nil {
		return Report{}, false, err
	}
	return out, created, nil
}

// finish moves a running report to its final state. The report must be the one the caller holds (id and revision)
// and still running: a final report is never rewritten.
func (s *FileStore) finish(ctx context.Context, local garden.LocalRepoID, id string, revision int, fn func(*Report) error) (Report, error) {
	k, err := repoKey(local)
	if err != nil {
		return Report{}, err
	}
	var out Report
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		r := d.Repos[k]
		for i := range r.Reports {
			x := &r.Reports[i]
			if x.ID != id {
				continue
			}
			if x.Revision != revision || x.State != StateRunning {
				return false, ErrConflict
			}
			next := *x
			if err := fn(&next); err != nil {
				return false, err
			}
			total := len(next.Findings)
			for j, o := range r.Reports {
				if j != i {
					total += len(o.Findings)
				}
			}
			if total > MaxFindingsIn {
				return false, ErrFull
			}
			*x = next
			d.Repos[k] = r
			out = next
			return true, nil
		}
		return false, ErrNotFound
	})
	return out, err
}

// MarkReady stores a validated report body on its running report. The body must be for the same attempt and commit.
func (s *FileStore) MarkReady(ctx context.Context, local garden.LocalRepoID, id string, revision int, body Body) (Report, error) {
	now := s.now()
	return s.finish(ctx, local, id, revision, func(r *Report) error {
		if body.AttemptID != r.AttemptID || body.Commit != r.Commit || len(body.Findings) > MaxFindings {
			return ErrInvalid
		}
		r.State, r.Summary, r.Findings, r.ReadyAt = StateReady, body.Summary, append([]Finding(nil), body.Findings...), now
		return nil
	})
}

func (s *FileStore) end(ctx context.Context, local garden.LocalRepoID, id string, revision int, state State, code FailCode) (Report, error) {
	if !code.valid() {
		return Report{}, ErrInvalid
	}
	return s.finish(ctx, local, id, revision, func(r *Report) error {
		r.State, r.FailCode = state, code
		return nil
	})
}

// MarkFailed ends a running report with a fixed failure code.
func (s *FileStore) MarkFailed(ctx context.Context, local garden.LocalRepoID, id string, revision int, code FailCode) (Report, error) {
	return s.end(ctx, local, id, revision, StateFailed, code)
}

// MarkInvalid ends a running report whose file broke the contract (FailInvalid).
func (s *FileStore) MarkInvalid(ctx context.Context, local garden.LocalRepoID, id string, revision int) (Report, error) {
	return s.end(ctx, local, id, revision, StateInvalid, FailInvalid)
}

// MarkBlocked ends a running report that could not proceed because of a precondition (for example FailWorktree).
func (s *FileStore) MarkBlocked(ctx context.Context, local garden.LocalRepoID, id string, revision int, code FailCode) (Report, error) {
	return s.end(ctx, local, id, revision, StateBlocked, code)
}

// Dismiss records a reason for one finding of one ready report revision. The commit must be the report's. The same
// binding again returns the first dismissal (created false). A dismissal applies to that revision only.
func (s *FileStore) Dismiss(ctx context.Context, local garden.LocalRepoID, in Dismissal) (Dismissal, bool, error) {
	k, err := repoKey(local)
	if err != nil {
		return Dismissal{}, false, err
	}
	reason, err := CleanReason(in.Reason)
	if err != nil {
		return Dismissal{}, false, err
	}
	in.Reason, in.At = reason, s.now()
	var out Dismissal
	var created bool
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		r := d.Repos[k]
		var rep *Report
		for i := range r.Reports {
			if x := &r.Reports[i]; x.AttemptID == in.AttemptID && x.Revision == in.ReportRevision {
				rep = x
			}
		}
		if rep == nil {
			return false, ErrNotFound
		}
		if rep.State != StateReady || rep.Commit != in.Commit {
			return false, ErrConflict
		}
		known := false
		for _, f := range rep.Findings {
			known = known || f.ID == in.FindingID
		}
		if !known {
			return false, ErrNotFound
		}
		for _, x := range r.Dismissals {
			if x.AttemptID == in.AttemptID && x.ReportRevision == in.ReportRevision && x.FindingID == in.FindingID {
				out = x
				return false, nil
			}
		}
		if len(r.Dismissals) >= MaxDismissals {
			return false, ErrFull
		}
		r.Dismissals = append(r.Dismissals, in)
		d.Repos[k] = r
		out, created = in, true
		return true, nil
	})
	if err != nil {
		return Dismissal{}, false, err
	}
	return out, created, nil
}

// Accept validates and records a human acceptance (see NewDecision). The same attempt, HEAD and report revision
// again returns the stored decision (created false). It records a decision only: nothing is merged, closed or removed.
func (s *FileStore) Accept(ctx context.Context, local garden.LocalRepoID, p Preview, currentHead string, acks []string, confirmed bool) (Decision, bool, error) {
	k, err := repoKey(local)
	if err != nil {
		return Decision{}, false, err
	}
	dec, err := NewDecision(p, currentHead, acks, confirmed, s.now())
	if err != nil {
		return Decision{}, false, err
	}
	var out Decision
	var created bool
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		r := d.Repos[k]
		for _, x := range r.Decisions {
			if x.ID == dec.ID {
				out = x
				return false, nil
			}
		}
		if len(r.Decisions) >= MaxDecisions {
			return false, ErrFull
		}
		r.Decisions = append(r.Decisions, dec)
		d.Repos[k] = r
		out, created = dec, true
		return true, nil
	})
	if err != nil {
		return Decision{}, false, err
	}
	return out, created, nil
}
