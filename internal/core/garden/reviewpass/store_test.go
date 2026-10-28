package reviewpass

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *FileStore {
	t.Helper()
	return &FileStore{Path: filepath.Join(t.TempDir(), "private", "passes.json")}
}
func mustReserve(t *testing.T, s *FileStore, p Pass) Pass {
	t.Helper()
	out, created, err := s.Reserve(context.Background(), p)
	if err != nil || !created {
		t.Fatalf("reserve = %+v, %v, %v", out, created, err)
	}
	return out
}
func mustUpdate(t *testing.T, s *FileStore, id string, edit func(*Pass)) Pass {
	t.Helper()
	out, err := s.Update(context.Background(), id, func(p Pass) (Pass, bool, error) { edit(&p); return p, true, nil })
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStoreReserveAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	p := testPass(t, 1)
	p.Authorization.ConfirmedAt = time.Now()
	p.Snapshot.Policy.Checks[0].Args = []string{}
	p.Snapshot.Policy.Checks[0].ID = CheckFrom(p.Snapshot.Policy.Checks[0].config()).ID
	p.Snapshot.PolicyHash = p.Snapshot.Policy.Hash()
	p = mustReserve(t, s, p)
	if got, created, err := s.Reserve(ctx, p); err != nil || created || !sameJSON(got, p) {
		t.Fatalf("idempotent reserve = %+v %v %v", got, created, err)
	}
	other := clonePass(p)
	other.Authorization.PreviewID = "other"
	if _, _, err := s.Reserve(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict = %v", err)
	}
	other = clonePass(p)
	other.ID = "rp-" + strings.Repeat("e", 32)
	if _, _, err := s.Reserve(ctx, other); !errors.Is(err, ErrActive) {
		t.Fatalf("active = %v", err)
	}
	other = clonePass(p)
	other.ID = "bad"
	if _, _, err := s.Reserve(ctx, other); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid = %v", err)
	}
	if _, err := s.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	list, err := s.List(ctx, p.Snapshot.Local, p.Snapshot.Issue)
	if err != nil || len(list) != 1 || !sameJSON(list[0], p) {
		t.Fatalf("list = %+v, %v", list, err)
	}
	list, err = s.List(ctx, "other", 0)
	if err != nil || len(list) != 0 {
		t.Fatalf("other local = %+v, %v", list, err)
	}
	list, err = s.List(ctx, p.Snapshot.Local, 99)
	if err != nil || len(list) != 0 {
		t.Fatalf("other issue = %+v, %v", list, err)
	}
	list, err = s.ForAttempt(ctx, p.Snapshot.AttemptID)
	if err != nil || len(list) != 1 {
		t.Fatalf("for attempt = %+v, %v", list, err)
	}
	got, err := s.Get(ctx, p.ID)
	if err != nil || !sameJSON(got, p) {
		t.Fatalf("get = %+v, %v", got, err)
	}
	got.Snapshot.Policy.Checks[0].Name = "mutation"
	reread, err := s.Get(ctx, p.ID)
	if err != nil || reread.Snapshot.Policy.Checks[0].Name == "mutation" {
		t.Fatal("get shares data")
	}
	p = mustUpdate(t, s, p.ID, func(p *Pass) { p.State = StateStopped; p.EndedAt = ptr(p.UpdatedAt) })
	if p.Revision != 2 {
		t.Fatalf("revision = %d", p.Revision)
	}
	other = clonePass(p)
	other.ID = "rp-" + strings.Repeat("e", 32)
	other.State = StatePending
	other.EndedAt = nil
	mustReserve(t, s, other)
	list, err = s.ForAttempt(ctx, p.Snapshot.AttemptID)
	if err != nil || len(list) != 2 || list[0].ID != p.ID {
		t.Fatalf("attempt history = %+v, %v", list, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.Path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode=%v err=%v", info, err)
		}
		info, err = os.Stat(filepath.Dir(s.Path))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode=%v err=%v", info, err)
		}
	}
}

func TestStoreImmutableAndTransitions(t *testing.T) {
	s := testStore(t)
	p := mustReserve(t, s, testPass(t, 1))
	ctx := context.Background()
	edits := []struct {
		name string
		edit func(*Pass)
		want error
	}{
		{"snapshot", func(p *Pass) { p.Snapshot.Number++ }, ErrImmutable},
		{"nested snapshot", func(p *Pass) {
			p.Snapshot.Policy.Checks[0].Args[0] = "change"
			p.Snapshot.PolicyHash = p.Snapshot.Policy.Hash()
		}, ErrImmutable},
		{"authorization", func(p *Pass) { p.Authorization.PreviewID = "change" }, ErrImmutable},
		{"confirmed time", func(p *Pass) { p.Authorization.ConfirmedAt = p.Authorization.ConfirmedAt.Add(time.Second) }, ErrImmutable},
		{"id", func(p *Pass) { p.ID = "rp-" + strings.Repeat("f", 32) }, ErrImmutable},
		{"created", func(p *Pass) { p.CreatedAt = p.CreatedAt.Add(time.Second) }, ErrImmutable},
		{"transition", func(p *Pass) { p.State = StateChecking }, ErrInvalid},
		{"invalid code", func(p *Pass) { p.Code = "prose" }, ErrInvalid},
	}
	for _, tt := range edits {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { tt.edit(&p); return p, true, nil })
			if !errors.Is(err, tt.want) {
				t.Fatalf("update = %v, want %v", err, tt.want)
			}
		})
	}
	original, _ := os.ReadFile(s.Path)
	noChange, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { p.State = StateRunning; p.Snapshot.Number++; return p, false, nil })
	if err != nil || noChange.Revision != 1 {
		t.Fatalf("no change = %+v, %v", noChange, err)
	}
	sentinel := errors.New("callback failed")
	if _, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { return p, true, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(original, after) {
		t.Fatal("refused changes rewrote file")
	}
	if _, err := s.Update(ctx, "missing", func(p Pass) (Pass, bool, error) { return p, true, nil }); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	p = mustUpdate(t, s, p.ID, func(p *Pass) {
		p.ImplementationRun = p.Snapshot.AttemptID + "/start/1"
		p.ReviewedHead = strings.Repeat("a", 40)
		p.FinalHead = strings.Repeat("b", 40)
		p.Report = "ev-" + strings.Repeat("e", 32)
		p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "starting"}
		p.State = StateStarting
	})
	for _, edit := range []func(*Pass){func(p *Pass) { p.ImplementationRun = "" }, func(p *Pass) { p.ImplementationRun = p.Snapshot.AttemptID + "/start/2" }, func(p *Pass) { p.ReviewedHead = "" }, func(p *Pass) { p.ReviewedHead = strings.Repeat("c", 40) }, func(p *Pass) { p.FinalHead = "" }, func(p *Pass) { p.FinalHead = strings.Repeat("c", 40) }, func(p *Pass) { p.Report = "" }, func(p *Pass) { p.Report = "ev-" + strings.Repeat("f", 32) }, func(p *Pass) { p.Reviewer = nil }, func(p *Pass) { p.Reviewer.SessionID = "sess_b" }} {
		if _, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { edit(&p); return p, true, nil }); !errors.Is(err, ErrImmutable) {
			t.Fatalf("write-once = %v", err)
		}
	}
	p = mustUpdate(t, s, p.ID, func(p *Pass) { p.State = StateStartUnknown })
	p = mustUpdate(t, s, p.ID, func(p *Pass) { p.State = StateRunning; p.Reviewer.State = "running"; p.Reviewer.Acknowledged = true })
	p = mustUpdate(t, s, p.ID, func(p *Pass) {
		p.State = StateChecking
		p.Reviewer.State = "exited"
		p.Reviewer.ExitCode = ptr(0)
		p.Reviewer.EndedAt = ptr(time.Now().UTC())
	})
	p = mustUpdate(t, s, p.ID, func(p *Pass) { p.State = StateAwaitingHumanReview; p.EndedAt = ptr(time.Now().UTC()) })
	if p.Revision != 6 || p.UpdatedAt.Equal(p.CreatedAt) {
		t.Fatalf("store metadata = %+v", p)
	}
	if _, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { p.State = StatePending; p.EndedAt = nil; return p, true, nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("final transition = %v", err)
	}
}

func TestStoreBoundedDecoding(t *testing.T) {
	p := testPass(t, 1)
	d := disk{SchemaVersion: 1, Records: map[string]Pass{p.ID: p}}
	good, _ := json.Marshal(d)
	bad := clonePass(p)
	bad.Revision = 0
	invalid, _ := json.Marshal(disk{SchemaVersion: 1, Records: map[string]Pass{bad.ID: bad}})
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"oversize", bytes.Repeat([]byte(" "), MaxStoreBytes+1), ErrTooLarge},
		{"duplicate", []byte(`{"schema_version":1,"schema_version":1,"passes":{}}`), ErrCorrupt},
		{"nested duplicate", bytes.Replace(good, []byte(`"revision":1`), []byte(`"revision":1,"revision":1`), 1), ErrCorrupt},
		{"folded schema collision", []byte(`{"schema_version":2,"SCHEMA_VERSION":1,"passes":{}}`), ErrUnsupportedSchema},
		{"folded schema collision reversed", []byte(`{"SCHEMA_VERSION":1,"schema_version":2,"passes":{}}`), ErrUnsupportedSchema},
		{"future schema with unknown field", []byte(`{"schema_version":2,"passes":{},"future":true}`), ErrUnsupportedSchema},
		{"future folded schema only", []byte(`{"SCHEMA_VERSION":2,"passes":{}}`), ErrCorrupt},
		{"folded schema only", []byte(`{"SCHEMA_VERSION":1,"passes":{}}`), ErrCorrupt},
		{"folded passes only", []byte(`{"schema_version":1,"PASSES":{}}`), ErrCorrupt},
		{"unicode state collision", bytes.Replace(good, []byte(`"state":"pending"`), []byte(`"state":"pending","ſtate":"running"`), 1), ErrCorrupt},
		{"unicode state only", bytes.Replace(good, []byte(`"state":"pending"`), []byte(`"ſtate":"pending"`), 1), ErrCorrupt},
		{"nested case variant", bytes.Replace(good, []byte(`"state":"pending"`), []byte(`"STATE":"pending"`), 1), ErrCorrupt},
		{"unknown nested key", bytes.Replace(good, []byte(`"state":"pending"`), []byte(`"state":"pending","future":true`), 1), ErrCorrupt},
		{"folded record collision", bytes.Replace(good, []byte(`"revision":1`), []byte(`"revision":1,"REVISION":1`), 1), ErrCorrupt},
		{"folded nested collision", bytes.Replace(good, []byte(`"spec_approval_id":4`), []byte(`"spec_approval_id":4,"SPEC_APPROVAL_ID":5`), 1), ErrCorrupt},
		{"folded array object collision", bytes.Replace(good, []byte(`"executable":"go"`), []byte(`"executable":"go","EXECUTABLE":"make"`), 1), ErrCorrupt},
		{"invalid utf8", []byte("{\"schema_version\":1,\"passes\":{},\"bad\":\"\xff\"}"), ErrCorrupt},
		{"corrupt record", invalid, ErrCorrupt},
		{"invalid json", []byte(`{`), ErrCorrupt},
		{"trailing json", append(append([]byte{}, good...), []byte(` {}`)...), ErrCorrupt},
		{"unknown field", []byte(`{"schema_version":1,"passes":{},"private":true}`), ErrCorrupt},
		{"missing schema", []byte(`{"passes":{}}`), ErrCorrupt},
		{"null passes", []byte(`{"schema_version":1,"passes":null}`), ErrCorrupt},
		{"wrong map key", bytes.Replace(good, []byte(`"`+p.ID+`":`), []byte(`"wrong":`), 1), ErrCorrupt},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.Path, tt.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Get(context.Background(), p.ID); !errors.Is(err, tt.want) {
				t.Fatalf("read = %v, want %v", err, tt.want)
			}
			if _, _, err := s.Reserve(context.Background(), p); !errors.Is(err, tt.want) {
				t.Fatalf("reserve = %v", err)
			}
			after, _ := os.ReadFile(s.Path)
			if !bytes.Equal(after, tt.data) {
				t.Fatal("corrupt file rewritten")
			}
		})
	}
}

func TestStoreNewerSchemaUntouched(t *testing.T) {
	s := testStore(t)
	p := testPass(t, 1)
	ctx := context.Background()
	data := []byte("{\n  \"schema_version\":2, \"passes\":{}\n}\n")
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error { _, err := s.Get(ctx, p.ID); return err }, func() error { _, err := s.List(ctx, "local", 0); return err }, func() error { _, err := s.ForAttempt(ctx, p.Snapshot.AttemptID); return err },
		func() error { _, _, err := s.Reserve(ctx, p); return err }, func() error {
			_, err := s.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { t.Error("callback called for newer schema"); return p, true, nil })
			return err
		},
		func() error {
			unlock, err := s.Lock(ctx, p.ID)
			if unlock != nil {
				unlock()
			}
			return err
		},
	}
	for _, call := range calls {
		if err := call(); !errors.Is(err, ErrUnsupportedSchema) {
			t.Errorf("newer schema = %v", err)
		}
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(data, after) {
		t.Fatal("newer schema modified")
	}
}

func TestStoreFull(t *testing.T) {
	s := testStore(t)
	p := testPass(t, 1)
	p.State = StateStopped
	p.EndedAt = ptr(p.CreatedAt)
	d := disk{SchemaVersion: 1, Records: map[string]Pass{}}
	for i := 1; i <= MaxPasses; i++ {
		q := clonePass(p)
		q.ID = fmt.Sprintf("rp-%032x", i)
		d.Records[q.ID] = q
	}
	if err := s.write(d); err != nil {
		t.Fatal(err)
	}
	q := clonePass(p)
	q.ID = fmt.Sprintf("rp-%032x", MaxPasses+1)
	if _, _, err := s.Reserve(context.Background(), q); !errors.Is(err, ErrFull) {
		t.Fatalf("full = %v", err)
	}
	if _, created, err := s.Reserve(context.Background(), p); err != nil || created {
		t.Fatalf("full idempotency = %v %v", created, err)
	}
}

func TestStoreConcurrentUpdates(t *testing.T) {
	s := testStore(t)
	p := mustReserve(t, s, testPass(t, 1))
	const workers = 12
	const updates = 5
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := &FileStore{Path: s.Path}
			for j := 0; j < updates; j++ {
				_, err := other.Update(ctx, p.ID, func(p Pass) (Pass, bool, error) { p.Code = CodePermissionsMissing; return p, true, nil })
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	got, err := s.Get(ctx, p.ID)
	if err != nil || got.Revision != 1+workers*updates {
		t.Fatalf("lost revision: %d, %v", got.Revision, err)
	}
}

func TestHelperLock(t *testing.T) {
	if os.Getenv("BONSAI_REVIEWPASS_LOCK_HELPER") != "1" {
		return
	}
	s := &FileStore{Path: os.Getenv("BONSAI_REVIEWPASS_LOCK_PATH")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := s.Lock(ctx, "pass-lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	fmt.Println("LOCKED")
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestStoreCrossProcessLock(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("cross-process locking not supported on this platform")
	}
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperLock$", "-test.count=1")
	cmd.Env = append(os.Environ(), "BONSAI_REVIEWPASS_LOCK_HELPER=1", "BONSAI_REVIEWPASS_LOCK_PATH="+s.Path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot re-execute test binary: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "LOCKED" {
				ready <- nil
				_, _ = io.Copy(io.Discard, stdout)
				return
			}
		}
		ready <- fmt.Errorf("child lock not ready: %v", scanner.Err())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	blockedCtx, blockedCancel := context.WithTimeout(ctx, 120*time.Millisecond)
	unlock, err := s.Lock(blockedCtx, "pass-lock")
	blockedCancel()
	if unlock != nil {
		unlock()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock did not block on child: %v", err)
	}
	acquired := make(chan error, 1)
	go func() {
		unlock, err := s.Lock(ctx, "pass-lock")
		if unlock != nil {
			unlock()
		}
		acquired <- err
	}()
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child failed: %v: %s", err, stderr.String())
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestOpenPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	s, err := Open()
	if err != nil || s.Path != filepath.Join(dir, "bonsai", "garden-review-passes.json") {
		t.Fatalf("Open = %+v, %v", s, err)
	}
}

func TestStoreFinalPassRequiresClosedReviewer(t *testing.T) {
	for _, reviewer := range []string{"starting", "running", "stopping"} {
		for _, final := range []State{StateStopped, StateBlocked, StateFailed} {
			t.Run(reviewer+"/"+string(final), func(t *testing.T) {
				s := testStore(t)
				p := mustReserve(t, s, testPass(t, 1))
				p = mustUpdate(t, s, p.ID, func(p *Pass) {
					p.State = StateStarting
					p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "starting"}
				})
				if reviewer != "starting" {
					p = mustUpdate(t, s, p.ID, func(p *Pass) {
						p.State = StateRunning
						p.Reviewer.State = reviewer
						p.Reviewer.Acknowledged = true
					})
				}
				before, err := os.ReadFile(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Update(context.Background(), p.ID, func(p Pass) (Pass, bool, error) {
					p.State, p.EndedAt = final, ptr(p.UpdatedAt)
					return p, true, nil
				})
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("finalized live reviewer: %v", err)
				}
				after, _ := os.ReadFile(s.Path)
				if !bytes.Equal(before, after) {
					t.Fatal("invalid final state rewrote store")
				}
				other := testPass(t, 2)
				other.Snapshot.AttemptID = p.Snapshot.AttemptID
				if _, _, err := s.Reserve(context.Background(), other); !errors.Is(err, ErrActive) {
					t.Fatalf("active pass exclusion released: %v", err)
				}
				got, err := s.Get(context.Background(), p.ID)
				if err != nil || !sameJSON(got, p) {
					t.Fatal("live pass changed", err)
				}
			})
		}
	}
}

func TestStoreReviewerCannotEraseAcknowledgedLaunch(t *testing.T) {
	for _, final := range []State{StateStopped, StateFailed, StateBlocked} {
		t.Run(string(final), func(t *testing.T) {
			s := testStore(t)
			p := mustReserve(t, s, testPass(t, 1))
			p = mustUpdate(t, s, p.ID, func(p *Pass) {
				p.State = StateStarting
				p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "starting"}
			})
			p = mustUpdate(t, s, p.ID, func(p *Pass) {
				p.State = StateRunning
				p.Reviewer.State, p.Reviewer.Acknowledged = "running", true
				p.Reviewer.StartedAt = ptr(p.CreatedAt)
			})
			before, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Update(context.Background(), p.ID, func(p Pass) (Pass, bool, error) {
				p.State, p.EndedAt = final, ptr(p.UpdatedAt)
				p.Reviewer.State, p.Reviewer.Acknowledged = "not-started", false
				return p, true, nil
			})
			if !errors.Is(err, ErrImmutable) {
				t.Fatalf("erased acknowledged launch: %v", err)
			}
			after, _ := os.ReadFile(s.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected update rewrote file")
			}
			other := testPass(t, 2)
			other.Snapshot.AttemptID = p.Snapshot.AttemptID
			if _, _, err := s.Reserve(context.Background(), other); !errors.Is(err, ErrActive) {
				t.Fatalf("active exclusion released: %v", err)
			}
			mustUpdate(t, s, p.ID, func(p *Pass) {
				p.Reviewer.State = "stopping"
			})
			mustUpdate(t, s, p.ID, func(p *Pass) {
				p.State, p.EndedAt = final, ptr(p.UpdatedAt)
				p.Reviewer.State, p.Reviewer.EndedAt = "exited", ptr(p.UpdatedAt)
				p.Reviewer.ExitCode = ptr(0)
			})
			mustReserve(t, s, other)
		})
	}
}

func TestStoreReviewerWriteOnce(t *testing.T) {
	s := testStore(t)
	p := mustReserve(t, s, testPass(t, 1))
	p = mustUpdate(t, s, p.ID, func(p *Pass) {
		p.State = StateStarting
		p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "starting"}
	})
	p = mustUpdate(t, s, p.ID, func(p *Pass) {
		p.State = StateFailed
		p.EndedAt = ptr(p.UpdatedAt)
		p.Reviewer.State, p.Reviewer.Acknowledged = "failed", true
		p.Reviewer.StartedAt, p.Reviewer.EndedAt = ptr(p.CreatedAt), ptr(p.UpdatedAt)
		p.Reviewer.ExitCode = ptr(1)
	})
	cases := []struct {
		name string
		edit func(*Session)
	}{
		{"operation", func(r *Session) { r.OperationID += "other" }},
		{"session", func(r *Session) { r.SessionID = "sess_b" }},
		{"ack", func(r *Session) { r.Acknowledged = false }},
		{"clear start", func(r *Session) { r.StartedAt = nil }},
		{"change start", func(r *Session) { r.StartedAt = ptr(r.StartedAt.Add(time.Nanosecond)) }},
		{"clear end", func(r *Session) { r.EndedAt = nil }},
		{"change end", func(r *Session) { r.EndedAt = ptr(r.EndedAt.Add(time.Nanosecond)) }},
		{"clear exit", func(r *Session) { r.ExitCode = nil }},
		{"change exit", func(r *Session) { r.ExitCode = ptr(2) }},
	}
	before, _ := os.ReadFile(s.Path)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Update(context.Background(), p.ID, func(p Pass) (Pass, bool, error) {
				tt.edit(p.Reviewer)
				return p, true, nil
			})
			if !errors.Is(err, ErrImmutable) {
				t.Fatalf("write-once field changed: %v", err)
			}
		})
	}
	_, err := s.Update(context.Background(), p.ID, func(p Pass) (Pass, bool, error) {
		p.Reviewer.State = "exited"
		return p, true, nil
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("terminal reviewer moved: %v", err)
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected changes rewrote store")
	}
	mustUpdate(t, s, p.ID, func(p *Pass) {
		// Equal timestamps may use a different location after a round trip.
		p.Reviewer.StartedAt = ptr(p.Reviewer.StartedAt.In(time.FixedZone("offset", 3600)))
	})
}
