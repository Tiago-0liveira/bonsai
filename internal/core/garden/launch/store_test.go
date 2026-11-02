package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func fakeLaunchRecord(t *testing.T, id string) Attempt {
	t.Helper()
	steps := map[Step]StepRecord{}
	for _, s := range []Step{StepPreflight, StepWorktree, StepHolder, StepMirror, StepPush, StepPR, StepCleanup} {
		steps[s] = StepRecord{State: StepPending, UpdatedAt: fakeLaunchTime}
	}
	steps[StepPreflight] = StepRecord{State: StepDone, UpdatedAt: fakeLaunchTime}
	return Attempt{ID: id, RequestHash: strings.Repeat("a", 64), Local: "project-v1-launch-test", Repo: 7, Issue: 101, Number: 9, ActorID: "1001", Epoch: 1, Inputs: Inputs{Spec: plan.SpecRef{RevisionHash: strings.Repeat("b", 64), CommentID: 1, Ordinal: 1, Fingerprint: strings.Repeat("b", 64)}, SpecApprovalID: 2, Plan: PlanRef{RevisionHash: strings.Repeat("c", 64), CommentID: 3, Ordinal: 1, DecisionID: 4, BaseCommit: strings.Repeat("d", 40)}}, Proposal: domain.IsolationProposal{IsolationRequest: domain.IsolationRequest{OperationID: id, RepositoryID: "local", Branch: "bonsai/" + id, BaseRef: "main", BaseSHA: strings.Repeat("d", 40), RemoteName: "origin", Repository: "owner/repo"}, Path: filepath.Join(t.TempDir(), id), RootRevision: 1, ConfigHash: strings.Repeat("e", 64)}, Provider: "codex", AccountID: "acct_test", AccountName: "personal", Options: Options{Sandbox: "workspace-write", Approval: "on-request"}, Name: "Implement #9", Cols: 100, Rows: 25, State: StatePreparing, Steps: steps, Runs: []Run{}, PR: PRLink{State: PRNotCreated}, Revision: 1, RequestedAt: fakeLaunchTime, UpdatedAt: fakeLaunchTime}
}
func fakeLaunchReserve(t *testing.T, s Store, a Attempt) Attempt {
	t.Helper()
	got, created, err := s.Reserve(context.Background(), a)
	if err != nil || !created {
		t.Fatalf("reserve=%+v created=%v err=%v", got, created, err)
	}
	return got
}
func TestLaunchStoreExactReplayAndImmutableIdentity(t *testing.T) {
	ctx := context.Background()
	s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	a := fakeLaunchRecord(t, "impl-store")
	a.State = StateStopped
	a.EndedAt = &fakeLaunchTime
	original := fakeLaunchReserve(t, s, a)
	again, created, err := (&FileStore{Path: s.Path}).Reserve(ctx, a)
	if err != nil || created || !reflect.DeepEqual(again, original) {
		t.Fatalf("replay=%+v created=%v err=%v", again, created, err)
	}
	a.RequestHash = strings.Repeat("f", 64)
	if _, _, err = s.Reserve(ctx, a); !errors.Is(err, ErrIdempotency) {
		t.Fatalf("changed hash=%v", err)
	}
	for _, change := range []func(*Attempt){func(a *Attempt) { a.ID = "impl-other" }, func(a *Attempt) { a.RequestHash = strings.Repeat("f", 64) }, func(a *Attempt) { a.Local = "foreign" }, func(a *Attempt) { a.Repo++ }, func(a *Attempt) { a.Issue++ }, func(a *Attempt) { a.Inputs.SpecApprovalID++ }, func(a *Attempt) { a.Proposal.Path = filepath.Join(a.Proposal.Path, "other") }, func(a *Attempt) { a.AccountID = "acct_other" }, func(a *Attempt) { a.Options.Approval = "never" }} {
		before, _ := os.ReadFile(s.Path)
		_, err = s.Update(ctx, original.ID, func(cur Attempt) (Attempt, bool, error) { change(&cur); return cur, true, nil })
		if err == nil {
			t.Fatal("immutable mutation accepted")
		}
		after, _ := os.ReadFile(s.Path)
		if !bytes.Equal(before, after) {
			t.Fatal("refused mutation changed bytes")
		}
	}
}
func TestLaunchStoreTransactionsDeepCopiesAndOrder(t *testing.T) {
	ctx := context.Background()
	s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	a := fakeLaunchRecord(t, "impl-b")
	a.State = StateStopped
	a.EndedAt = &fakeLaunchTime
	fakeLaunchReserve(t, s, a)
	b := fakeLaunchRecord(t, "impl-a")
	b.State = StateStopped
	b.EndedAt = &fakeLaunchTime
	fakeLaunchReserve(t, s, b)
	list, err := s.List(ctx, a.Local, a.Issue)
	if err != nil || len(list) != 2 || list[0].ID != "impl-a" {
		t.Fatalf("order=%+v err=%v", list, err)
	}
	list[0].Steps[StepPR] = StepRecord{State: StepUnknown}
	got, err := s.Get(ctx, b.ID)
	if err != nil || got.Steps[StepPR].State != StepPending {
		t.Fatalf("Get alias=%+v err=%v", got, err)
	}
	before, _ := os.ReadFile(s.Path)
	sentinel := errors.New("callback failure")
	_, err = s.Update(ctx, a.ID, func(cur Attempt) (Attempt, bool, error) {
		cur.Steps[StepPR] = StepRecord{State: StepUnknown}
		return cur, true, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("callback failure committed")
	}
	noOp, err := s.Update(ctx, a.ID, func(cur Attempt) (Attempt, bool, error) { cur.State = StateCleaned; return cur, false, nil })
	after, _ = os.ReadFile(s.Path)
	if err != nil || noOp.Revision != a.Revision || !bytes.Equal(before, after) {
		t.Fatalf("no-op changed revision/bytes: %+v %v", noOp, err)
	}
	changed, err := s.Update(ctx, a.ID, func(cur Attempt) (Attempt, bool, error) { cur.PR = PRLink{State: PRPending}; return cur, true, nil })
	if err != nil || changed.Revision != a.Revision+1 {
		t.Fatalf("revision=%+v err=%v", changed, err)
	}
	changed.Steps[StepPR] = StepRecord{State: StepFailed}
	fresh, _ := s.Get(ctx, a.ID)
	if fresh.Steps[StepPR].State == StepFailed {
		t.Fatal("Update returned alias")
	}
}
func TestLaunchStoreCrossInstanceReservations(t *testing.T) {
	for _, collision := range []string{"active issue", "branch", "normalized path"} {
		t.Run(collision, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "launch.json")
			a := fakeLaunchRecord(t, "impl-first")
			b := fakeLaunchRecord(t, "impl-second")
			want := ErrAttemptActive
			switch collision {
			case "branch":
				a.State = StateStopped
				a.EndedAt = &fakeLaunchTime
				b.Issue++
				b.Proposal.Branch = a.Proposal.Branch
				want = ErrBranchCollision
			case "normalized path":
				a.State = StateStopped
				a.EndedAt = &fakeLaunchTime
				b.Issue++
				b.Proposal.Path = filepath.Join(a.Proposal.Path, "child", "..")
				want = ErrPathCollision
			}
			fakeLaunchReserve(t, &FileStore{Path: path}, a)
			if _, _, err := (&FileStore{Path: path}).Reserve(ctx, b); !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
		})
	}
}
func TestLaunchStoreConcurrentReservationsHaveOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch.json")
	a := fakeLaunchRecord(t, "impl-one")
	b := fakeLaunchRecord(t, "impl-two")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, record := range []Attempt{a, b} {
		wg.Add(1)
		go func(record Attempt) {
			defer wg.Done()
			<-start
			_, _, err := (&FileStore{Path: path}).Reserve(context.Background(), record)
			results <- err
		}(record)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrAttemptActive) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners=%d", wins)
	}
}
func TestLaunchStoreCorruptionNeverOverwritten(t *testing.T) {
	a := fakeLaunchRecord(t, "impl-corrupt")
	valid, _ := json.Marshal(map[string]any{"schema_version": 1, "attempts": map[string]Attempt{a.ID: a}})
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"future", []byte(`{"schema_version":2,"attempts":{}}`), ErrUnsupportedSchema},
		{"duplicate root", []byte(`{"schema_version":1,"schema_version":1,"attempts":{}}`), ErrCorrupt},
		{"duplicate nested", bytes.Replace(valid, []byte(`"model"`), []byte(`"model"`), 1), nil},
		{"unknown root", []byte(`{"schema_version":1,"attempts":{},"secret":true}`), ErrCorrupt},
		{"trailing", append(append([]byte{}, valid...), []byte(` {}`)...), ErrCorrupt},
		{"null map", []byte(`{"schema_version":1,"attempts":null}`), ErrCorrupt},
		{"map identity", bytes.Replace(valid, []byte(`"impl-corrupt":`), []byte(`"impl-other":`), 1), ErrCorrupt},
		{"unknown nested", bytes.Replace(valid, []byte(`"options":{`), []byte(`"options":{"prompt":"secret",`), 1), ErrCorrupt},
		{"invalid enum", bytes.Replace(valid, []byte(`"preparing"`), []byte(`"done"`), 1), ErrCorrupt},
		{"invalid utf8", append([]byte{'"'}, 0xff, '"'), ErrCorrupt},
		{"oversize", bytes.Repeat([]byte(" "), MaxStoreBytes+1), ErrTooLarge},
	}
	cases[2].data = bytes.Replace(valid, []byte(`"sandbox":"workspace-write"`), []byte(`"sandbox":"workspace-write","sandbox":"bypass"`), 1)
	cases[2].want = ErrCorrupt
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
			if err := os.WriteFile(s.Path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, op := range []func() error{func() error { _, err := s.Get(context.Background(), a.ID); return err }, func() error { _, _, err := s.Reserve(context.Background(), a); return err }, func() error {
				_, err := s.Update(context.Background(), a.ID, func(a Attempt) (Attempt, bool, error) { t.Error("callback on corrupt store"); return a, true, nil })
				return err
			}} {
				if err := op(); !errors.Is(err, tc.want) {
					t.Fatalf("err=%v want=%v", err, tc.want)
				}
				after, _ := os.ReadFile(s.Path)
				if !bytes.Equal(after, tc.data) {
					t.Fatal("corrupt source overwritten")
				}
			}
		})
	}
}
func TestLaunchStoreCapRetainsAudit(t *testing.T) {
	s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	base := fakeLaunchRecord(t, "impl-cap")
	base.State = StateCleaned
	base.EndedAt = &fakeLaunchTime
	attempts := map[string]Attempt{}
	for i := 0; i < MaxAttempts; i++ {
		a := fakeLaunchClone(base)
		a.ID = fmt.Sprintf("impl-cap-%d", i)
		a.Proposal.OperationID = a.ID
		a.Proposal.Branch = "bonsai/" + a.ID
		a.Issue = garden.IssueID(i + 1)
		attempts[a.ID] = a
	}
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "attempts": attempts})
	if len(raw) > MaxStoreBytes {
		t.Fatalf("fixture over byte cap: %d", len(raw))
	}
	if err := os.WriteFile(s.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reserve(context.Background(), fakeLaunchRecord(t, "impl-overflow")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("cap=%v", err)
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(after, raw) {
		t.Fatal("evicted retained audit")
	}
}
func TestLaunchStoreCanceledOperationLockDoesNotLeak(t *testing.T) {
	s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	release, err := s.Lock(context.Background(), "impl-lock")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if unlock, err := (&FileStore{Path: s.Path}).Lock(ctx, "impl-lock"); !errors.Is(err, context.Canceled) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("canceled lock=%v", err)
	}
	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	unlock, err := s.Lock(ctx2, "impl-lock")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
func TestLaunchStoreOpenUsesIsolatedConfigAndPrivatePermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(s.Path) != "garden-launch.json" {
		t.Fatalf("path=%s", s.Path)
	}
	fakeLaunchReserve(t, s, fakeLaunchRecord(t, "impl-permissions"))
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("public execution state: %v", info.Mode())
	}
}

func TestLaunchStoreRejectsImpossibleRecordsAndPreservesBytes(t *testing.T) {
	base := fakeLaunchRecord(t, "impl-invalid-record")
	for _, tc := range []struct {
		name   string
		change func(*Attempt)
	}{
		{"missing actor", func(a *Attempt) { a.ActorID = "" }}, {"invalid hash", func(a *Attempt) { a.RequestHash = "not-a-hash" }},
		{"zero timestamp", func(a *Attempt) { a.RequestedAt = time.Time{} }}, {"invalid PR state", func(a *Attempt) { a.PR.State = "created" }},
		{"unknown step", func(a *Attempt) { a.Steps[Step("exec")] = StepRecord{State: StepPending, UpdatedAt: fakeLaunchTime} }},
		{"run session mismatch", func(a *Attempt) {
			a.State = StateRunning
			a.SessionID = "sess_aa"
			a.Runs = []Run{{Number: 1, OperationID: a.ID + "/start/1", SessionID: "sess_bb", State: "running", Acknowledged: true}}
		}},
		{"running without run", func(a *Attempt) { a.State = StateRunning }}, {"invalid run state", func(a *Attempt) {
			a.Runs = []Run{{Number: 1, OperationID: a.ID + "/start/1", SessionID: "sess_aa", State: "idle"}}
			a.SessionID = "sess_aa"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fakeLaunchClone(base)
			tc.change(&a)
			raw, err := json.Marshal(map[string]any{"schema_version": 1, "attempts": map[string]Attempt{a.ID: a}})
			if err != nil {
				t.Fatal(err)
			}
			s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
			if err = os.WriteFile(s.Path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Get(context.Background(), a.ID); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("impossible record read err=%v", err)
			}
			after, _ := os.ReadFile(s.Path)
			if !bytes.Equal(after, raw) {
				t.Fatal("invalid record overwritten")
			}
		})
	}
}

func TestLaunchStoreWriteFailureRollsBackTransaction(t *testing.T) {
	s := &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	a := fakeLaunchReserve(t, s, fakeLaunchRecord(t, "impl-write-failure"))
	before, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = s.Update(context.Background(), a.ID, func(cur Attempt) (Attempt, bool, error) {
		called = true
		if err := os.Rename(s.Path, s.Path+".previous"); err != nil {
			return cur, false, err
		}
		if err := os.Mkdir(s.Path, 0700); err != nil {
			return cur, false, err
		}
		cur.PR.State = PRPending
		return cur, true, nil
	})
	if err == nil || !called {
		t.Fatalf("atomic replace fault not exercised: called=%v err=%v", called, err)
	}
	retained, err := os.ReadFile(s.Path + ".previous")
	if err != nil || !bytes.Equal(retained, before) {
		t.Fatal("write failure corrupted original", err)
	}
	if err = os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(s.Path+".previous", s.Path); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), a.ID)
	if err != nil || got.Revision != a.Revision || got.PR.State != PRNotCreated {
		t.Fatalf("failed write leaked mutation=%+v %v", got, err)
	}
}
