//go:build linux || darwin

package agentsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// fakeService runs a script with the job's IO instead of a provider.
type fakeService struct {
	setup func(context.Context, agents.SetupOptions, agents.SetupIO) (agents.SetupResult, error)
}

func (f fakeService) SetupWithIO(ctx context.Context, provider agents.ProviderID, name string, options agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
	return f.setup(ctx, options, setupIO)
}

func (f fakeService) Reauthenticate(ctx context.Context, id agents.AccountID, options agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
	result, err := f.setup(ctx, options, setupIO)
	result.Account.ID = id
	return result, err
}

func sh(script string) agents.PreparedSession {
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", script}, Dir: "/"}
}

// recorder collects the states the only job goes through.
type recorder struct {
	mu     sync.Mutex
	jobs   *Jobs
	states []State
}

func (r *recorder) changed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs.List() {
		if len(r.states) == 0 || r.states[len(r.states)-1] != job.State {
			r.states = append(r.states, job.State)
		}
	}
}

func awaitJob(t *testing.T, jobs *Jobs, id string, done func(Job) bool) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := jobs.Get(id)
		if !ok {
			t.Fatal("job evicted")
		}
		if done(job) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := jobs.Get(id)
	t.Fatalf("job stuck in %+v", job)
	return Job{}
}

func ended(job Job) bool { return !job.Active() }

func output(t *testing.T, jobs *Jobs, id string) string {
	t.Helper()
	sub, _, replay, _, err := jobs.Attach(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Detach(id, sub)
	var b strings.Builder
	for _, f := range replay {
		b.Write(f.Data)
	}
	return b.String()
}

func TestJobRunsStepsInOneTerminal(t *testing.T) {
	r := &recorder{}
	service := fakeService{setup: func(ctx context.Context, _ agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
		fmt.Fprintf(setupIO.Out, "seeded profile from ~/.fake\n  skipped a\n")
		if err := setupIO.Launcher.RunForeground(ctx, sh(`echo first`)); err != nil {
			return agents.SetupResult{}, err
		}
		if err := setupIO.Launcher.RunForeground(ctx, sh(`echo second`)); err != nil {
			return agents.SetupResult{}, err
		}
		return agents.SetupResult{Account: agents.Account{ID: "acct_new"}, Warnings: []string{"shared login"}}, nil
	}}
	jobs := New(service, r.changed)
	r.jobs = jobs
	job, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "work", Options: agents.SetupOptions{AuthMode: "device"}})
	if err != nil || job.State != StatePreparing || job.AuthMode != "device" || job.Name != "work" {
		t.Fatalf("start %+v %v", job, err)
	}
	job = awaitJob(t, jobs, job.ID, ended)
	if job.State != StateSucceeded || job.AccountID != "acct_new" || len(job.Warnings) != 1 || job.EndedAt == nil {
		t.Fatalf("result %+v", job)
	}
	out := output(t, jobs, job.ID)
	if !strings.Contains(out, "seeded profile from ~/.fake\r\n  skipped a\r\n") {
		t.Fatalf("progress lines not CRLF: %q", out)
	}
	if !strings.Contains(out, "\r\n── sh -c echo first ──\r\n") || !strings.Contains(out, "\r\n── sh -c echo second ──\r\n") {
		t.Fatalf("missing step separators: %q", out)
	}
	if strings.Index(out, "first") > strings.Index(out, "second") {
		t.Fatalf("steps out of order: %q", out)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	want := []State{StatePreparing, StateRunning, StateVerifying, StateRunning, StateVerifying, StateSucceeded}
	if fmt.Sprint(r.states) != fmt.Sprint(want) {
		t.Fatalf("states %v, want %v", r.states, want)
	}
}

func TestJobPromptRoundTripAndSecretNeverShown(t *testing.T) {
	const secret = "sk-proj-TOPSECRET-1234567890"
	got := make(chan string, 1)
	service := fakeService{setup: func(ctx context.Context, _ agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
		value, err := setupIO.Prompt.Secret(ctx, "OpenAI API key")
		got <- value
		return agents.SetupResult{Account: agents.Account{ID: "acct_key"}}, err
	}}
	jobs := New(service, nil)
	job, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Answer(job.ID, secret); !errors.Is(err, ErrNoPrompt) && err != nil {
		t.Fatal(err)
	}
	waiting := awaitJob(t, jobs, job.ID, func(j Job) bool { return j.State == StateWaitingSecret })
	if waiting.Prompt == nil || waiting.Prompt.Label != "OpenAI API key" {
		t.Fatalf("prompt %+v", waiting)
	}
	if err := jobs.Answer("missing", secret); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := jobs.Answer(job.ID, secret); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Answer(job.ID, secret); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("second answer: %v", err)
	}
	if <-got != secret {
		t.Fatal("secret not delivered")
	}
	job = awaitJob(t, jobs, job.ID, ended)
	encoded, _ := json.Marshal(jobs.List())
	if job.State != StateSucceeded || strings.Contains(string(encoded), secret) || strings.Contains(output(t, jobs, job.ID), secret) {
		t.Fatalf("secret leaked or job failed: %s", encoded)
	}
}

func TestJobLimitsAndBusy(t *testing.T) {
	release := make(chan struct{})
	service := fakeService{setup: func(ctx context.Context, _ agents.SetupOptions, _ agents.SetupIO) (agents.SetupResult, error) {
		select {
		case <-release:
			return agents.SetupResult{}, nil
		case <-ctx.Done():
			return agents.SetupResult{}, ctx.Err()
		}
	}}
	jobs := New(service, nil)
	defer jobs.Close()
	first, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "one"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("same profile twice: %v", err)
	}
	if _, err := jobs.Start(Request{Kind: KindLogin, Provider: "fake", Name: "a", AccountID: "acct_a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Start(Request{Kind: KindLogin, Provider: "fake", Name: "a", AccountID: "acct_a"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("same account twice: %v", err)
	}
	if _, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "three"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("third job: %v", err)
	}
	close(release)
	awaitJob(t, jobs, first.ID, ended)
	if _, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "one"}); err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestJobCancelTimeoutAndClose(t *testing.T) {
	hang := fakeService{setup: func(ctx context.Context, _ agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
		err := setupIO.Launcher.RunForeground(ctx, sh(`trap '' TERM; echo WAITING; while :; do sleep 1; done`))
		return agents.SetupResult{}, err
	}}
	jobs := New(hang, nil)
	job, _ := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "cancel"})
	awaitJob(t, jobs, job.ID, func(j Job) bool { return j.State == StateRunning })
	canceled, err := jobs.Cancel(job.ID)
	if err != nil || canceled.State != StateCanceled || canceled.Error != "" {
		t.Fatalf("cancel %+v %v", canceled, err)
	}
	if _, err := jobs.Cancel("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	jobs.Timeout = 300 * time.Millisecond
	job, _ = jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "timeout"})
	job = awaitJob(t, jobs, job.ID, ended)
	if job.State != StateFailed || !strings.Contains(job.Error, "timed out") {
		t.Fatalf("timeout %+v", job)
	}

	jobs.Timeout = DefaultTimeout
	job, _ = jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "close"})
	awaitJob(t, jobs, job.ID, func(j Job) bool { return j.State == StateRunning })
	jobs.Close()
	if job, _ = jobs.Get(job.ID); job.State != StateCanceled {
		t.Fatalf("close left %+v", job)
	}
	if _, err := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "late"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close: %v", err)
	}
}

func TestJobFailureMessageIsOneLine(t *testing.T) {
	jobs := New(fakeService{setup: func(context.Context, agents.SetupOptions, agents.SetupIO) (agents.SetupResult, error) {
		return agents.SetupResult{}, errors.Join(errors.New("codex login failed: exit status 1"), errors.New("cleanup failed"))
	}}, nil)
	job, _ := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "fail"})
	job = awaitJob(t, jobs, job.ID, ended)
	if job.State != StateFailed || job.Error != "codex login failed: exit status 1; cleanup failed" {
		t.Fatalf("%+v", job)
	}
}

func TestEndedJobsAreEvictedAfterRetention(t *testing.T) {
	jobs := New(fakeService{setup: func(ctx context.Context, _ agents.SetupOptions, setupIO agents.SetupIO) (agents.SetupResult, error) {
		_, _ = io.WriteString(setupIO.Out, "token printed here\n")
		return agents.SetupResult{Account: agents.Account{ID: "acct_x"}}, nil
	}}, nil)
	var mu sync.Mutex
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var timers []func()
	jobs.SetClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now }, func(d time.Duration, f func()) {
		mu.Lock()
		defer mu.Unlock()
		if d != DefaultRetention {
			t.Errorf("retention %s", d)
		}
		timers = append(timers, f)
	})
	job, _ := jobs.Start(Request{Kind: KindCreate, Provider: "fake", Name: "evict"})
	job = awaitJob(t, jobs, job.ID, ended)
	if !strings.Contains(output(t, jobs, job.ID), "token printed here") {
		t.Fatal("output missing before retention")
	}
	_, _, _, frames, _ := jobs.Attach(job.ID, 0)
	stream := jobs.stream(job.ID)
	mu.Lock()
	now = now.Add(DefaultRetention - time.Second)
	fire := timers
	mu.Unlock()
	for _, f := range fire {
		f()
	}
	if _, ok := jobs.Get(job.ID); !ok {
		t.Fatal("evicted early")
	}
	mu.Lock()
	now = now.Add(time.Second)
	mu.Unlock()
	for _, f := range fire {
		f()
	}
	if _, ok := jobs.Get(job.ID); ok {
		t.Fatal("job retained after 2 minutes")
	}
	for range frames {
	}
	// The released stream replays only a gap and the status: no bytes left.
	_, _, replay, _ := stream.Attach(0, Job{})
	for _, f := range replay {
		if len(f.Data) > 0 {
			t.Fatalf("output buffer not freed: %q", f.Data)
		}
	}
}
