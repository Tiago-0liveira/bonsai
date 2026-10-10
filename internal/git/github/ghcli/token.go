package ghcli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

const (
	// tokenTTL bounds how long a token read from gh is reused before gh is
	// asked again. A 401 re-reads it immediately.
	tokenTTL = 10 * time.Minute
	// tokenFailureTTL keeps a missing or logged-out gh from being spawned for
	// every request.
	tokenFailureTTL = 30 * time.Second
	tokenTimeout    = 10 * time.Second
)

// errUseCLI marks a token failure that the gh api subprocess may still get
// past, such as a gh release without `gh auth token`.
var errUseCLI = errors.New("gh auth token is unavailable")

// AuthState is the GitHub login state reported to the setup TUI and doctor.
type AuthState string

const (
	AuthUnknown         AuthState = ""
	AuthOK              AuthState = "ok"
	AuthMissing         AuthState = "gh_missing"
	AuthUnauthenticated AuthState = "unauthenticated"
	AuthUnavailable     AuthState = "error"
)

// AuthStatus describes whether Bonsai can read GitHub through the gh login of
// Host. Detail and Fix never contain the token.
type AuthStatus struct {
	Host   string    `json:"host"`
	State  AuthState `json:"state"`
	Detail string    `json:"detail,omitempty"`
	Fix    string    `json:"fix,omitempty"`
}

// TokenRunner runs `gh auth token` for host and returns its output.
type TokenRunner func(ctx context.Context, host string) (stdout, stderr []byte, err error)

type tokenEntry struct {
	token   string
	err     error
	status  AuthStatus
	expires time.Time
	wait    chan struct{}
	// refreshing is set while a background read replaces an expired token.
	refreshing bool
	// forced makes the next Token call wait for gh (after a 401).
	forced bool
	// gen numbers reads of gh; only the latest read may store its result.
	gen uint64
}

// TokenSource reads the gh login's token on demand and keeps it in memory
// only. It is never logged, persisted or placed in a process argv.
type TokenSource struct {
	mu      sync.Mutex
	entries map[string]*tokenEntry
	now     func() time.Time
	run     TokenRunner
}

func NewTokenSource() *TokenSource {
	return &TokenSource{entries: map[string]*tokenEntry{}, now: time.Now, run: runGHAuthToken}
}

// Token returns the token for host. Concurrent callers share one gh process.
func (s *TokenSource) Token(ctx context.Context, host string) (string, error) {
	for {
		s.mu.Lock()
		entry := s.entries[host]
		if entry == nil {
			entry = &tokenEntry{}
			s.entries[host] = entry
		}
		if entry.wait != nil {
			wait := entry.wait
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-wait:
				continue
			}
		}
		if s.now().Before(entry.expires) {
			token, err := entry.token, entry.err
			s.mu.Unlock()
			return token, err
		}
		if entry.token != "" && entry.err == nil && !entry.forced {
			// A working token past its TTL stays in use while gh is asked again
			// in the background, so a refresh never waits on the gh process. A
			// 401 forces the synchronous path through Invalidate.
			token := entry.token
			if !entry.refreshing {
				entry.refreshing = true
				entry.gen++
				go s.refresh(host, entry, entry.gen)
			}
			s.mu.Unlock()
			return token, nil
		}
		entry.wait = make(chan struct{})
		entry.gen++
		run := s.run
		s.mu.Unlock()

		token, status, err := readToken(ctx, run, host)

		s.mu.Lock()
		// Any background read still running is older than this one.
		entry.token, entry.err, entry.status, entry.forced, entry.refreshing = token, err, status, false, false
		switch {
		case err == nil:
			entry.expires = s.now().Add(tokenTTL)
		case ctx.Err() != nil:
			// The caller gave up; that says nothing about the login.
			entry.expires = time.Time{}
		default:
			entry.expires = s.now().Add(tokenFailureTTL)
		}
		close(entry.wait)
		entry.wait = nil
		s.mu.Unlock()
		return token, err
	}
}

// Invalidate forgets token so the next Token call asks gh again. A token that
// was already replaced is left alone.
func (s *TokenSource) Invalidate(host, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[host]; entry != nil && entry.wait == nil && entry.token == token {
		entry.expires = time.Time{}
		entry.forced = true
	}
}

// refresh replaces an expired working token in the background. Only a new
// token or a definitive login failure replaces it: a timeout, a hung keyring
// or an old gh keeps the working token until the next attempt.
func (s *TokenSource) refresh(host string, entry *tokenEntry, gen uint64) {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), tokenTimeout)
	defer cancel()
	token, status, err := readToken(ctx, run, host)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[host] != entry {
		return
	}
	if entry.gen == gen {
		entry.refreshing = false
	}
	if entry.gen != gen || entry.wait != nil || entry.forced {
		// A later read (or a 401 that asked for one) owns the entry; this
		// result may predate a token rotation.
		return
	}
	switch {
	case err == nil:
		entry.token, entry.err, entry.status = token, nil, status
		entry.expires = s.now().Add(tokenTTL)
	case domain.Code(err) == "github_auth":
		entry.token, entry.err, entry.status = "", err, status
		entry.expires = s.now().Add(tokenFailureTTL)
	default:
		entry.expires = s.now().Add(tokenFailureTTL)
	}
}

// Status reports the login state for host, reading the token if needed.
func (s *TokenSource) Status(ctx context.Context, host string) AuthStatus {
	_, _ = s.Token(ctx, host)
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[host]; entry != nil && entry.status.State != AuthUnknown {
		return entry.status
	}
	return AuthStatus{Host: host, State: AuthUnavailable, Detail: "The GitHub login could not be checked", Fix: "gh auth status --hostname " + host}
}

func readToken(ctx context.Context, run TokenRunner, host string) (string, AuthStatus, error) {
	stdout, stderr, err := run(ctx, host)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			status := AuthStatus{
				Host:   host,
				State:  AuthMissing,
				Detail: "GitHub CLI (gh) is not installed, so Bonsai cannot read GitHub",
				Fix:    "install gh from https://cli.github.com, then run: gh auth login --hostname " + host,
			}
			return "", status, domain.E("github_auth", status.Detail+". Fix: "+status.Fix)
		}
		if ctx.Err() != nil {
			return "", AuthStatus{}, ctx.Err()
		}
		message := strings.ToLower(string(stderr))
		if strings.Contains(message, "no oauth token") || strings.Contains(message, "not logged in") || strings.Contains(message, "gh auth login") {
			status := AuthStatus{
				Host:   host,
				State:  AuthUnauthenticated,
				Detail: "gh is not logged in to " + host,
				Fix:    "gh auth login --hostname " + host,
			}
			return "", status, domain.E("github_auth", status.Detail+". Fix: "+status.Fix)
		}
		// Other failures (an old gh, a keyring error) fall back to gh api, which
		// reports its own error if it cannot authenticate either.
		return "", AuthStatus{Host: host, State: AuthUnavailable, Detail: "gh auth token failed; Bonsai uses gh api instead", Fix: "gh auth status --hostname " + host}, errUseCLI
	}
	token := strings.TrimSpace(string(stdout))
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", AuthStatus{Host: host, State: AuthUnavailable, Detail: "gh auth token returned no usable token; Bonsai uses gh api instead", Fix: "gh auth status --hostname " + host}, errUseCLI
	}
	return token, AuthStatus{Host: host, State: AuthOK}, nil
}

func runGHAuthToken(ctx context.Context, host string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenTimeout)
	defer cancel()
	trace.AddGH()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", host)
	cmd.Env = ghEnv(cmd.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// ghEnv drops terminal styling overrides: gh output is parsed, not shown.
func ghEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, variable := range environ {
		key, _, _ := strings.Cut(variable, "=")
		switch key {
		case "CLICOLOR_FORCE", "FORCE_COLOR", "GH_FORCE_TTY", "NO_COLOR":
			continue
		}
		out = append(out, variable)
	}
	return append(out, "NO_COLOR=1")
}
