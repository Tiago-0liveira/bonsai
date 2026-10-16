// Package agentsbackend implements the TUI's agentsapi contract on the real
// agent runtime and holder records, shared with the web UI and the CLI.
package agentsbackend

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// Option customizes New.
type Option func(*config)

type config struct {
	paths    agenthost.Paths
	pathsSet bool
	launcher agenthost.Launcher
	runtime  *agentruntime.Runtime
}

// WithPaths overrides the holder record and socket locations.
func WithPaths(p agenthost.Paths) Option {
	return func(c *config) { c.paths, c.pathsSet = p, true }
}

// WithLauncher overrides how holders start (tests run them in process).
func WithLauncher(l agenthost.Launcher) Option { return func(c *config) { c.launcher = l } }

// WithRuntime supplies the agent runtime instead of building the default.
func WithRuntime(rt *agentruntime.Runtime) Option { return func(c *config) { c.runtime = rt } }

// Backend owns the runtime and attachments so TUI exit leaves holders running.
type Backend struct {
	runtime  *agentruntime.Runtime
	sessions *sessions
	accounts agentsapi.Accounts

	mu      sync.Mutex
	streams map[*stream]struct{}
	closed  bool
}

// New builds a backend. Missing holder support leaves API().Sessions unsupported.
func New(opts ...Option) (*Backend, error) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.runtime == nil {
		rt, err := agentruntime.New(nil, io.Discard, io.Discard)
		if err != nil {
			return nil, err
		}
		c.runtime = rt
	}
	if !c.pathsSet {
		// Without paths the sessions are unsupported rather than fatal.
		if p, err := agenthost.DefaultPaths(); err == nil {
			c.paths = p
		}
	}
	selfExe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate bonsai executable: %w", ErrSessionUnavailable)
	}
	b := &Backend{runtime: c.runtime, streams: map[*stream]struct{}{}}
	b.sessions = &sessions{
		backend: b,
		paths:   c.paths,
		starter: agenthost.Starter{
			Accounts: c.runtime.Accounts, Sessions: c.runtime.Sessions, Registry: c.runtime.Registry,
			Paths: c.paths, Launch: c.launcher, Origin: "tui",
		},
	}
	b.accounts = newAccounts(c.runtime, c.paths, selfExe)
	return b, nil
}

// API is the backend as the TUI consumes it.
func (b *Backend) API() agentsapi.Backend {
	return agentsapi.Backend{Sessions: b.sessions, Accounts: b.accounts}
}

// Close ends every open stream. It never stops holders: sessions outlive the TUI.
func (b *Backend) Close() error {
	b.mu.Lock()
	b.closed = true
	open := make([]*stream, 0, len(b.streams))
	for s := range b.streams {
		open = append(open, s)
	}
	b.mu.Unlock()
	for _, s := range open {
		_ = s.Close()
	}
	return nil
}

// register starts s under the same lock Close uses to snapshot attachments.
func (b *Backend) register(s *stream) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.streams[s] = struct{}{}
	s.start()
	return true
}

func (b *Backend) deregister(s *stream) {
	b.mu.Lock()
	delete(b.streams, s)
	b.mu.Unlock()
}
