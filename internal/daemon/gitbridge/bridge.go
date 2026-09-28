package gitbridge

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Bridge struct {
	URL, Credential string
	RepositoryIDs   []string
	Executor        *Executor
	Snapshots       <-chan domain.RepositoryState
}

func (b *Bridge) Run(ctx context.Context) error {
	u, e := url.Parse(b.URL)
	if e != nil || u.Scheme != "wss" || u.User != nil || u.Host == "" {
		return fmt.Errorf("bridge requires a wss URL")
	}
	if b.Credential == "" {
		return domain.ErrAuth
	}
	delay := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		_ = b.connect(ctx)
		if time.Since(start) > time.Minute {
			delay = time.Second
		}
		jitter := time.Duration(randBytes()%1000) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay + jitter):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
	}
	return ctx.Err()
}
func randBytes() uint16 { var b [2]byte; _, _ = rand.Read(b[:]); return uint16(b[0])<<8 | uint16(b[1]) }
func (b *Bridge) connect(ctx context.Context) error {
	header := http.Header{"Authorization": []string{"Bearer " + b.Credential}}
	conn, _, e := websocket.DefaultDialer.DialContext(ctx, b.URL, header)
	if e != nil {
		return e
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	send := func(v Frame) error {
		mu.Lock()
		defer mu.Unlock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}
	go func() { <-ctx.Done(); conn.Close() }()
	conn.SetReadLimit(2 << 20)
	conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	if e = send(Frame{Type: "hello", Repositories: b.RepositoryIDs}); e != nil {
		return e
	}

	// Serialize snapshot calculation and transmission. Queued watcher snapshots
	// are refresh hints, never replayed as stale state after reconnect.
	var snapshotMu sync.Mutex
	latest := map[string]domain.RepositoryState{}
	sendSnapshot := func(id string, affected []string) error {
		snapshotMu.Lock()
		defer snapshotMu.Unlock()

		var snapshot domain.RepositoryState
		var err error
		if prior, ok := latest[id]; ok && len(affected) > 0 {
			raw, _ := json.Marshal(prior)
			_ = json.Unmarshal(raw, &snapshot)
			for i := range snapshot.Worktrees {
				wt := &snapshot.Worktrees[i]
				for _, changed := range affected {
					if wt.ID != changed {
						continue
					}
					st, e := b.Executor.Local.Status(ctx, wt.ID)
					if e != nil {
						return e
					}
					wt.Status = &st
					wt.HeadSHA = st.HeadSHA
					wt.Branch = st.Branch
				}
			}
		} else {
			snapshot, err = b.Executor.Local.Repository(ctx, id)
			if err != nil {
				return err
			}
		}
		latest[id] = snapshot

		var cursor uint64
		if b.Executor.Journal != nil {
			err = b.Executor.Journal.Update(func(d store.Data) error {
				cursor, _ = store.Get[uint64](d, "event_cursors", "sent:"+id)
				cursor++
				return store.Put(d, "event_cursors", "sent:"+id, cursor)
			})
			if err != nil {
				return err
			}
		}
		return send(Frame{Type: "snapshot", RepositoryID: id, Snapshot: &snapshot, Cursor: cursor})
	}
	for _, id := range b.RepositoryIDs {
		if e = sendSnapshot(id, nil); e != nil {
			return e
		}
	}
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); e != nil {
					cancel()
					return
				}
			case snapshot, ok := <-b.Snapshots:
				if !ok {
					return
				}
				if sendSnapshot(snapshot.ID, snapshot.AffectedWorktrees) != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Keep the socket reader active during long Git operations so ping/pong and
	// disconnect cancellation continue to work while commands execute serially.
	frames := make(chan Frame, 32)
	readErrors := make(chan error, 1)
	go func() {
		for {
			var frame Frame
			if err := conn.ReadJSON(&frame); err != nil {
				readErrors <- err
				cancel()
				return
			}
			conn.SetReadDeadline(time.Now().Add(70 * time.Second))
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		var frame Frame
		select {
		case frame = <-frames:
		case err := <-readErrors:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}

		if frame.Type == "ack" && b.Executor.Journal != nil {
			if err := b.Executor.Journal.Update(func(d store.Data) error {
				last, _ := store.Get[uint64](d, "event_cursors", "ack:"+frame.RepositoryID)
				if frame.Cursor > last {
					return store.Put(d, "event_cursors", "ack:"+frame.RepositoryID, frame.Cursor)
				}
				return nil
			}); err != nil {
				return err
			}
			continue
		}
		if frame.Type != "command" || frame.Command == nil {
			continue
		}
		c := *frame.Command
		allowed := false
		for _, id := range b.RepositoryIDs {
			allowed = allowed || id == c.RepositoryID
		}
		if !allowed {
			return domain.ErrForbidden
		}
		opCtx, done := context.WithTimeout(ctx, 2*time.Minute)
		if IsOperation(c.Type) {
			op := domain.Operation{ID: c.ID, WorktreeID: c.WorktreeID, Kind: strings.TrimPrefix(c.Type, "git."), State: "running", UpdatedAt: time.Now().UTC()}
			if e = send(Frame{Type: "operation", RepositoryID: c.RepositoryID, Operation: &op}); e != nil {
				done()
				return e
			}
		}
		result := b.Executor.Execute(opCtx, c)
		done()
		if e = send(Frame{Type: "result", Result: &result}); e != nil {
			return e
		}
		if !IsRead(c.Type) {
			if err := sendSnapshot(c.RepositoryID, nil); err != nil {
				return err
			}
		}
	}
}

// MarshalArguments keeps command payloads structured, never shell strings.
func MarshalArguments(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
