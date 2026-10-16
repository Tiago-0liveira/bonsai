package agentsbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var _ agentsapi.Sessions = (*sessions)(nil)

type sessions struct {
	backend *Backend
	paths   agenthost.Paths
	starter agenthost.Starter
}

func (s *sessions) Supported() bool { return agentterminal.Supported && s.paths.Records != "" }

func session(r agenthost.Record) agentsapi.Session {
	return agentsapi.Session{Summary: r.Summary, WorkDir: filepath.Clean(r.WorkDir)}
}

func (s *sessions) List(ctx context.Context) ([]agentsapi.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.Supported() {
		return nil, nil
	}
	records, err := agenthost.List(s.paths)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", ErrSessionUnavailable)
	}
	out := make([]agentsapi.Session, 0, len(records))
	for _, r := range records {
		out = append(out, session(r))
	}
	return out, nil
}

func (s *sessions) Changes(ctx context.Context) (<-chan struct{}, error) {
	if !s.Supported() {
		ch := make(chan struct{})
		close(ch)
		return ch, nil
	}
	ch, err := agenthost.Watch(ctx, s.paths)
	if err != nil {
		return nil, fmt.Errorf("watch agent sessions: %w", ErrSessionUnavailable)
	}
	return ch, nil
}

func (s *sessions) Start(ctx context.Context, req agentsapi.StartRequest) (agentsapi.Session, error) {
	record, err := s.starter.Start(ctx, agenthost.StartRequest{
		Project: req.ProjectID, Tree: req.WorktreeID, Dir: req.WorkDir, Name: req.Name,
		AccountID: req.AccountID, Cols: req.Cols, Rows: req.Rows, Launch: req.Launch,
	})
	if err != nil {
		if ctx.Err() != nil {
			return agentsapi.Session{}, ctx.Err()
		}
		return agentsapi.Session{}, fmt.Errorf("start agent session: %w", ErrSessionUnavailable)
	}
	out := session(record)
	return out, nil
}

func (s *sessions) Stop(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := agenthost.Read(s.paths, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || id == "" {
			return ErrSessionNotFound
		}
		return fmt.Errorf("read agent session: %w", ErrSessionUnavailable)
	}
	if err := agenthost.StopHolder(s.paths, record, ""); err != nil {
		return fmt.Errorf("stop agent session: %w", ErrSessionUnavailable)
	}
	return nil
}

func (s *sessions) Attach(ctx context.Context, id string, opts agentsapi.AttachOptions) (agentsapi.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := agenthost.Read(s.paths, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || id == "" {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("read agent session: %w", ErrSessionUnavailable)
	}
	if !record.Alive || !record.Summary.Active() {
		// Ended records have no holder to dial; their final status remains viewable.
		return newEndedStream(record), nil
	}
	conn, ready, err := agenthost.DialContext(ctx, record, agenthost.Request{
		Mode: agenthost.ModeTerminal, Client: "tui", ReadOnly: opts.ReadOnly, Take: opts.Take,
		Offset: opts.Offset, Generation: opts.Generation,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, agenthost.ErrVersion) {
			return nil, fmt.Errorf("attach agent session: %w", agentsapi.ErrVersion)
		}
		return nil, fmt.Errorf("attach agent session: %w", ErrSessionUnavailable)
	}
	info, err := agenthost.QueryContext(ctx, record)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("query agent session: %w", ErrSessionUnavailable)
	}
	st := newStream(s.backend, record, conn, ready, opts.ReadOnly, info)
	if !s.backend.register(st) {
		_ = conn.Close()
		return nil, ErrBackendClosed
	}
	return st, nil
}

// sizePollInterval is how often an attached stream re-reads the holder's PTY
// size, since the protocol has no size frame.
var sizePollInterval = 2 * time.Second
