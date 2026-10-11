package ghcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

type fakeTokens struct {
	mu     sync.Mutex
	calls  atomic.Int32
	tokens []string
	stderr string
	err    error
	gate   chan struct{}
}

func (f *fakeTokens) run(ctx context.Context, host string) ([]byte, []byte, error) {
	f.calls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, []byte(f.stderr), f.err
	}
	token := f.tokens[0]
	if len(f.tokens) > 1 {
		f.tokens = f.tokens[1:]
	}
	return []byte(token + "\n"), nil, nil
}

func newTestTokenSource(f *fakeTokens, now *time.Time) *TokenSource {
	s := NewTokenSource()
	s.run = f.run
	s.now = func() time.Time { return *now }
	return s
}

func TestTokenIsReadOnceAndReusedWithinTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	f := &fakeTokens{tokens: []string{"tok-1"}, gate: make(chan struct{})}
	s := newTestTokenSource(f, &now)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if token, err := s.Token(context.Background(), "github.com"); err != nil || token != "tok-1" {
				t.Errorf("Token = %q, %v", token, err)
			}
		}()
	}
	for f.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(f.gate)
	wg.Wait()
	now = now.Add(tokenTTL - time.Second)
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-1" {
		t.Fatal(token)
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("gh auth token ran %d times, want 1", got)
	}
}

func TestExpiredTokenIsRefreshedInTheBackground(t *testing.T) {
	now := time.Unix(1000, 0)
	f := &fakeTokens{tokens: []string{"tok-1", "tok-2"}}
	s := newTestTokenSource(f, &now)
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-1" {
		t.Fatal(token)
	}
	f.gate = make(chan struct{})
	now = now.Add(tokenTTL + time.Second)
	// Past the TTL the working token is still returned without waiting.
	if token, err := s.Token(context.Background(), "github.com"); err != nil || token != "tok-1" {
		t.Fatalf("Token = %q, %v", token, err)
	}
	close(f.gate)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if token, _ := s.Token(context.Background(), "github.com"); token == "tok-2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh never replaced the token")
		}
		time.Sleep(time.Millisecond)
	}
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("gh auth token ran %d times, want 2", got)
	}
}

func TestInvalidateForcesASynchronousReRead(t *testing.T) {
	now := time.Unix(1000, 0)
	f := &fakeTokens{tokens: []string{"tok-1", "tok-2"}}
	s := newTestTokenSource(f, &now)
	first, _ := s.Token(context.Background(), "github.com")
	s.Invalidate("github.com", "stale-token-not-held")
	if token, _ := s.Token(context.Background(), "github.com"); token != first {
		t.Fatal("invalidating another token dropped the current one")
	}
	s.Invalidate("github.com", first)
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-2" {
		t.Fatalf("after Invalidate Token = %q, want tok-2", token)
	}
}

func TestTokenFailuresAreClassifiedAndCachedBriefly(t *testing.T) {
	missing := &exec.Error{Name: "gh", Err: exec.ErrNotFound}
	for _, test := range []struct {
		name   string
		err    error
		stderr string
		code   string
		state  AuthState
		cli    bool
	}{
		{name: "gh missing", err: missing, code: "github_auth", state: AuthMissing},
		{name: "logged out", err: errors.New("exit status 1"), stderr: "no oauth token found for github.com\n", code: "github_auth", state: AuthUnauthenticated},
		{name: "old gh", err: errors.New("exit status 1"), stderr: `unknown command "token" for "gh auth"`, state: AuthUnavailable, cli: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			f := &fakeTokens{err: test.err, stderr: test.stderr}
			s := newTestTokenSource(f, &now)
			_, err := s.Token(context.Background(), "github.com")
			if test.cli {
				if !errors.Is(err, errUseCLI) {
					t.Fatalf("err = %v, want gh api fallback", err)
				}
			} else if domain.Code(err) != test.code {
				t.Fatalf("code = %q (%v), want %q", domain.Code(err), err, test.code)
			}
			if test.code != "" && !strings.Contains(err.Error(), "gh auth login --hostname github.com") {
				t.Fatalf("error has no fix: %v", err)
			}
			if status := s.Status(context.Background(), "github.com"); status.State != test.state || status.Fix == "" {
				t.Fatalf("status = %+v", status)
			}
			_, _ = s.Token(context.Background(), "github.com")
			if got := f.calls.Load(); got != 1 {
				t.Fatalf("failure was not cached: %d runs", got)
			}
			now = now.Add(tokenFailureTTL + time.Second)
			_, _ = s.Token(context.Background(), "github.com")
			if got := f.calls.Load(); got != 2 {
				t.Fatalf("failure was cached past its TTL: %d runs", got)
			}
		})
	}
}

func TestTokenErrorsNeverContainTheToken(t *testing.T) {
	now := time.Unix(1000, 0)
	const secret = "gho_SECRETSECRETSECRET"
	f := &fakeTokens{err: errors.New("exit status 1"), stderr: fmt.Sprintf("not logged in (token %s rejected)", secret)}
	s := newTestTokenSource(f, &now)
	_, err := s.Token(context.Background(), "github.com")
	status := s.Status(context.Background(), "github.com")
	if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprint(status), secret) {
		t.Fatal("gh stderr leaked into the error")
	}
}

// A background refresh that times out or fails transiently keeps the working
// token; only a definitive login failure replaces it.
func TestBackgroundRefreshFailuresKeepTheWorkingToken(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		stderr string
		keep   bool
	}{
		{name: "timeout", err: context.DeadlineExceeded, keep: true},
		{name: "unknown failure", err: errors.New("exit status 1"), stderr: "keyring locked", keep: true},
		{name: "logged out", err: errors.New("exit status 1"), stderr: "no oauth token found for github.com", keep: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			f := &fakeTokens{tokens: []string{"tok-1"}}
			s := newTestTokenSource(f, &now)
			if token, _ := s.Token(context.Background(), "github.com"); token != "tok-1" {
				t.Fatal(token)
			}
			f.mu.Lock()
			f.err, f.stderr = test.err, test.stderr
			f.mu.Unlock()
			now = now.Add(tokenTTL + time.Second)
			if token, _ := s.Token(context.Background(), "github.com"); token != "tok-1" {
				t.Fatal("expired token not served while refreshing")
			}
			waitRefreshed(t, s, "github.com")
			token, err := s.Token(context.Background(), "github.com")
			if test.keep && (token != "tok-1" || err != nil) {
				t.Fatalf("Token = %q, %v; want the working token", token, err)
			}
			if !test.keep && (token != "" || domain.Code(err) != "github_auth") {
				t.Fatalf("Token = %q, %v; want github_auth", token, err)
			}
		})
	}
}

// A background read that started before a 401-forced re-read must not
// overwrite the newer token when it finishes later.
func TestStaleBackgroundRefreshDoesNotOverwriteANewerToken(t *testing.T) {
	now := time.Unix(1000, 0)
	f := &fakeTokens{tokens: []string{"tok-1"}}
	s := newTestTokenSource(f, &now)
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-1" {
		t.Fatal(token)
	}
	// The background read will return the pre-rotation token, late.
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.mu.Lock()
	s.run = func(ctx context.Context, host string) ([]byte, []byte, error) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(started)
			<-gate
			return []byte("tok-old"), nil, nil
		}
		return []byte("tok-new"), nil, nil
	}
	s.mu.Unlock()
	now = now.Add(tokenTTL + time.Second)
	_, _ = s.Token(context.Background(), "github.com") // starts the background read
	<-started
	s.Invalidate("github.com", "tok-1") // a 401
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-new" {
		t.Fatalf("forced read = %q", token)
	}
	close(gate)
	time.Sleep(20 * time.Millisecond)
	if token, _ := s.Token(context.Background(), "github.com"); token != "tok-new" {
		t.Fatalf("stale background read replaced the token with %q", token)
	}
	now = now.Add(tokenTTL + time.Second)
	_, _ = s.Token(context.Background(), "github.com")
	waitRefreshed(t, s, "github.com")
}

func waitRefreshed(t *testing.T, s *TokenSource, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		refreshing := s.entries[host].refreshing
		s.mu.Unlock()
		if !refreshing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh never finished")
		}
		time.Sleep(time.Millisecond)
	}
}
