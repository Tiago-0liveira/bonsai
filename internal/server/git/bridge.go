package git

import (
	"context"
	"crypto/rand"
	"encoding/json"
	bridge "github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/gorilla/websocket"
	"net/http"
	"strings"
	"sync"
	"time"
)

type connection struct {
	ws      *websocket.Conn
	device  Device
	mu      sync.Mutex
	pending map[string]chan bridge.Result
	done    chan struct{}
}

func (c *connection) send(frame bridge.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(frame)
}

type commandRecord struct {
	Command bridge.Command `json:"command"`
	Result  *bridge.Result `json:"result,omitempty"`
}

func (s *Service) Bridge(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	device, e := s.device(token)
	if e != nil {
		writeError(w, e)
		return
	}
	// Device credentials are not browser sessions. Reject browser origins.
	if r.Header.Get("Origin") != "" {
		writeError(w, domain.ErrForbidden)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, HandshakeTimeout: 10 * time.Second}
	ws, e := upgrader.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	s.connections.Add(1)
	defer s.connections.Done()
	defer ws.Close()
	ws.SetReadLimit(16 << 20)
	ws.SetReadDeadline(time.Now().Add(20 * time.Second))
	var hello bridge.Frame
	if ws.ReadJSON(&hello) != nil || hello.Type != "hello" {
		return
	}
	ids := map[string]bool{}
	for _, id := range hello.Repositories {
		if !device.has(id) || !s.authorized(device.UserID, id, true) || ids[id] {
			return
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return
	}
	c := &connection{ws: ws, device: device, pending: map[string]chan bridge.Result{}, done: make(chan struct{})}
	s.mu.Lock()
	for id := range ids {
		if old := s.devices[id]; old != nil {
			old.ws.Close()
		}
		s.devices[id] = c
	}
	s.mu.Unlock()
	defer func() {
		close(c.done)
		s.mu.Lock()
		for id := range ids {
			if s.devices[id] == c {
				delete(s.devices, id)
			}
		}
		s.mu.Unlock()
		for id := range ids {
			_ = s.publish(id, "bonsai", "daemon.disconnected", device.ID, "", nil)
		}
	}()
	for id := range ids {
		_ = s.publish(id, "bonsai", "daemon.connected", device.ID, "", nil)
	}
	ws.SetReadDeadline(time.Now().Add(70 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-t.C:
				if _, e := s.device(token); e != nil {
					ws.Close()
					return
				}
				if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
					ws.Close()
					return
				}
			}
		}
	}()
	// Resume only journaled commands. The daemon dedupes them durably.
	var retry []bridge.Command
	s.Store.View(func(d store.Data) error {
		for key := range d["daemon_commands"] {
			v, ok := store.Get[commandRecord](d, "daemon_commands", key)
			if ok && v.Result == nil && ids[v.Command.RepositoryID] && s.authorized(v.Command.UserID, v.Command.RepositoryID, true) {
				retry = append(retry, v.Command)
			}
		}
		return nil
	})
	for i := range retry {
		if c.send(bridge.Frame{Type: "command", Command: &retry[i]}) != nil {
			return
		}
	}
	for {
		var frame bridge.Frame
		if ws.ReadJSON(&frame) != nil {
			return
		}
		ws.SetReadDeadline(time.Now().Add(70 * time.Second))
		switch frame.Type {
		case "snapshot":
			if !ids[frame.RepositoryID] || frame.Snapshot == nil {
				return
			}
			if e = s.setLocal(frame.RepositoryID, *frame.Snapshot); e != nil {
				return
			}
			if frame.Cursor > 0 {
				if e = s.Store.Update(func(d store.Data) error {
					return store.Put(d, "event_cursors", device.ID+":"+frame.RepositoryID, frame.Cursor)
				}); e != nil {
					return
				}
				if c.send(bridge.Frame{Type: "ack", RepositoryID: frame.RepositoryID, Cursor: frame.Cursor}) != nil {
					return
				}
			}

		case "operation":
			if !ids[frame.RepositoryID] || frame.Operation == nil {
				return
			}
			if e = s.publish(frame.RepositoryID, "daemon", "git.operation.updated", frame.Operation.ID, "", frame.Operation); e != nil {
				return
			}
		case "result":
			if frame.Result == nil {
				return
			}
			result := *frame.Result
			var completed bridge.Command
			e = s.Store.Update(func(d store.Data) error {
				v, ok := store.Get[commandRecord](d, "daemon_commands", result.ID)
				if !ok {
					return nil
				}
				if !ids[v.Command.RepositoryID] {
					return domain.ErrForbidden
				}

				completed = v.Command
				v.Result = &result
				return store.Put(d, "daemon_commands", result.ID, v)
			})
			if e != nil {
				return
			}

			if bridge.IsOperation(completed.Type) {
				var op domain.Operation
				if result.Error != nil {
					op = domain.Operation{ID: completed.ID, WorktreeID: completed.WorktreeID, State: "failed", Error: result.Error.Message, UpdatedAt: time.Now().UTC()}
				} else {
					_ = json.Unmarshal(result.Payload, &op)
				}
				_ = s.publish(completed.RepositoryID, "daemon", "git.operation.updated", op.ID, result.ID+":final", op)
			}
			c.mu.Lock()
			ch := c.pending[result.ID]
			if ch != nil {
				select {
				case ch <- result:
				default:
				}
			}
			c.mu.Unlock()
		default:
			return
		}
	}
}
func (s *Service) execute(ctx context.Context, c bridge.Command) (bridge.Result, error) {
	if !s.authorized(c.UserID, c.RepositoryID, !bridge.IsRead(c.Type)) {
		return bridge.Result{}, domain.ErrForbidden
	}
	if !bridge.Allowed(c.Type) {
		return bridge.Result{}, domain.ErrInvalid
	}
	if c.ID == "" {
		c.ID = rand.Text()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	conn := s.devices[c.RepositoryID]
	s.mu.Unlock()
	if conn == nil {
		return bridge.Result{}, domain.ErrOffline
	}
	if !bridge.IsRead(c.Type) {
		var old commandRecord
		var exists bool
		e := s.Store.Update(func(d store.Data) error {
			old, exists = store.Get[commandRecord](d, "daemon_commands", c.ID)
			if exists {
				a := old.Command
				b := c
				a.CreatedAt = time.Time{}
				b.CreatedAt = time.Time{}
				ba, _ := json.Marshal(a)
				bb, _ := json.Marshal(b)
				if string(ba) != string(bb) {
					return domain.ErrInvalid
				}
				return nil
			}
			return store.Put(d, "daemon_commands", c.ID, commandRecord{Command: c})
		})
		if e != nil {
			return bridge.Result{}, e
		}
		if exists {
			c = old.Command
			if old.Result != nil {
				return *old.Result, nil
			}
		}
	}
	ch := make(chan bridge.Result, 1)
	conn.mu.Lock()
	if conn.pending[c.ID] != nil {
		conn.mu.Unlock()
		return bridge.Result{}, domain.ErrBusy
	}
	conn.pending[c.ID] = ch
	conn.mu.Unlock()
	defer func() { conn.mu.Lock(); delete(conn.pending, c.ID); conn.mu.Unlock() }()
	if bridge.IsOperation(c.Type) {
		_ = s.publish(c.RepositoryID, "bonsai", "git.operation.updated", c.ID, c.ID+":queued", domain.Operation{ID: c.ID, WorktreeID: c.WorktreeID, Kind: c.Type, State: "queued", UpdatedAt: time.Now().UTC()})
	}
	if e := conn.send(bridge.Frame{Type: "command", Command: &c}); e != nil {
		return bridge.Result{}, domain.ErrOffline
	}
	select {
	case result := <-ch:
		return result, nil
	case <-conn.done:
		return bridge.Result{}, domain.ErrUncertain
	case <-ctx.Done():
		return bridge.Result{}, domain.ErrUncertain
	}
}

// Close ends device streams and waits for their final persisted events.
func (s *Service) Close() {
	s.mu.Lock()
	for _, conn := range s.devices {
		conn.ws.Close()
	}
	s.mu.Unlock()
	s.connections.Wait()
}
