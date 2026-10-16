package agentterminal

import (
	"context"
	"errors"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// RunSession runs one agent session to its end: the provider prepares it, its
// process runs in stream until it exits or ctx is canceled, then the provider
// finalizes it and the session store removes its runtime, under a fresh
// context so cancellation never skips cleanup.
//
// update applies one change to the session's summary (provider session ID,
// running, the final state); the caller serializes changes and publishes each
// result. An error from the change that marks the session running stops it.
func RunSession(ctx context.Context, stream *Stream[Summary], provider agents.Provider, account agents.Account, runtime agents.Session, sessions agents.SessionStore, cols, rows int, launch agents.LaunchOptions, update func(func(*Summary) error) error) {
	prepared, err := provider.PrepareSession(ctx, agents.PrepareSessionRequest{Account: account, Session: runtime, Launch: launch})
	preparedOK := err == nil
	code := -1
	if err == nil {
		_ = update(func(s *Summary) error {
			s.ProviderSessionID = prepared.ProviderSessionID
			return nil
		})
		code, err = runInStream(ctx, stream, prepared, cols, rows, func() error {
			return update(func(s *Summary) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				s.State = "running"
				now := time.Now().UTC()
				s.StartedAt = &now
				return nil
			})
		})
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var finalErr error
	if preparedOK {
		finalErr = provider.FinalizeSession(cleanupCtx, agents.FinalizeSessionRequest{Account: account, Session: runtime})
	}
	cleanupErr := sessions.Cleanup(runtime)
	_ = update(func(s *Summary) error {
		now := time.Now().UTC()
		s.EndedAt = &now
		s.ExitCode = &code
		s.State = "exited"
		if s.Error != "" {
			s.State = "failed"
		}
		if err != nil && ctx.Err() == nil {
			s.State = "failed"
			s.Error = "Agent failed to start or exited unsuccessfully. Check the " + agents.Label(provider) + " installation and profile setup."
		}
		if errors.Join(finalErr, cleanupErr) != nil {
			s.Error = "Profile finalization or session cleanup failed."
		}
		return nil
	})
}
