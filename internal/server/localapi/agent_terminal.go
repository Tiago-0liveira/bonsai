package localapi

import (
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/gorilla/websocket"
)

func isAgentTerminalRoute(path string) bool {
	p := strings.Split(path, "/")
	return len(p) == 7 && p[0] == "" && p[1] == "api" && p[2] == "projects" && p[3] != "" && p[4] == "agents" && p[5] != "" && p[6] == "terminal"
}
func (s *Server) agentTerminal(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	project, id := r.PathValue("projectId"), r.PathValue("sessionId")
	manager := s.agents.manager
	serveTerminal(s, w, r, func() (terminalEndpoint[agentterminal.Summary], bool) {
		summary, ok := manager.Get(project, id)
		if !ok {
			return nil, false
		}
		if _, err := s.agentWorktree(r, summary.WorktreeID); err != nil {
			return nil, false
		}
		if _, err := s.agents.runtime.Accounts.Get(summary.AccountID); err != nil {
			return nil, false
		}
		// The holder owns the stream, so its generation, not the API's epoch,
		// tells a reconnecting browser whether its offset still applies.
		return func(cursor uint64, generation string) (terminalAttachment[agentterminal.Summary], error) {
			a, err := manager.Attach(project, id, cursor, generation, "web")
			if err != nil {
				return terminalAttachment[agentterminal.Summary]{}, err
			}
			return terminalAttachment[agentterminal.Summary]{
				generation: a.Generation,
				writer:     a.Writer(),
				replay:     a.Replay,
				frames:     a.Frames,
				detach:     a.Close,
				input:      a.Input,
				resize:     a.Resize,
				take:       a.Take,
			}, nil
		}, true
	})
}

// terminalEndpoint attaches a websocket to one terminal stream (an agent
// session or a setup job) at cursor; generation is the one the browser last
// saw.
type terminalEndpoint[S any] func(cursor uint64, generation string) (terminalAttachment[S], error)

// terminalAttachment is one websocket's attachment to a terminal stream.
type terminalAttachment[S any] struct {
	generation string
	writer     bool
	replay     []agentterminal.StreamFrame[S]
	frames     <-chan agentterminal.StreamFrame[S]
	detach     func()
	input      func(data []byte) error
	resize     func(cols, rows int) error
	// take asks for control (a writer frame confirms it); nil when the
	// stream's writer cannot change hands.
	take func() error
}

// streamAttachment attaches to an in-process stream, which lives as long as
// the API: its generation is the API's epoch.
func streamAttachment[S any](epoch string, cursor uint64, generation string, attach func(uint64) (uint64, bool, []agentterminal.StreamFrame[S], <-chan agentterminal.StreamFrame[S], error), detach func(uint64), input func(uint64, []byte) error, resize func(uint64, int, int) error) (terminalAttachment[S], error) {
	if generation != "" && generation != epoch {
		cursor = 0
	}
	subscriber, writer, replay, frames, err := attach(cursor)
	if err != nil {
		return terminalAttachment[S]{}, err
	}
	return terminalAttachment[S]{
		generation: epoch,
		writer:     writer,
		replay:     replay,
		frames:     frames,
		detach:     func() { detach(subscriber) },
		input:      func(data []byte) error { return input(subscriber, data) },
		resize:     func(cols, rows int) error { return resize(subscriber, cols, rows) },
	}, nil
}

// serveTerminal runs the terminal websocket protocol (version 1): authenticate,
// attach, then ready, replay and live frames; the writer may send input and
// resize, and any client of a stream with a take may ask for control. A
// writer frame tells the browser its control changed; input or resize sent
// without control (just after losing it) is dropped. resolve runs after
// authentication and finds the stream.
func serveTerminal[S any](s *Server, w http.ResponseWriter, r *http.Request, resolve func() (terminalEndpoint[S], bool)) {
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool {
		return s.allowedHost(r.Host) && s.allowedOrigin(r.Header.Get("Origin"))
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(96 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(websocketAuthTimeout))
	var auth websocketAuth
	if err := conn.ReadJSON(&auth); err != nil || auth.Type != "authenticate" || !s.sessions.valid(auth.Token) {
		return
	}
	var attach struct {
		Type       string `json:"type"`
		Offset     uint64 `json:"offset"`
		Generation string `json:"generation"`
	}
	if err := conn.ReadJSON(&attach); err != nil || attach.Type != "attach" {
		return
	}
	endpoint, ok := resolve()
	if !ok {
		return
	}
	attachment, err := endpoint(attach.Offset, attach.Generation)
	if err != nil {
		return
	}
	defer attachment.detach()
	generation, replay, frames := attachment.generation, attachment.replay, attachment.frames
	var writer atomic.Bool
	writer.Store(attachment.writer)
	send := func(value any) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(value)
	}
	if send(map[string]any{"type": "ready", "version": 1, "generation": generation, "writer": writer.Load()}) != nil {
		return
	}
	for _, frame := range replay {
		if send(frame) != nil {
			return
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var message struct {
				Type string `json:"type"`
				Data []byte `json:"data"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if conn.ReadJSON(&message) != nil {
				return
			}
			switch message.Type {
			case "input":
				if len(message.Data) > agentterminal.InputLimit {
					return
				}
				if writer.Load() && attachment.input(message.Data) != nil {
					return
				}
			case "resize":
				if writer.Load() && attachment.resize(message.Cols, message.Rows) != nil {
					return
				}
			case "take":
				if attachment.take == nil || attachment.take() != nil {
					return
				}
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		case <-heartbeat.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		case frame, ok := <-frames:
			if !ok {
				return
			}
			if frame.Type == "writer" && frame.Writer != nil {
				writer.Store(*frame.Writer)
			}
			if send(frame) != nil {
				return
			}
		}
	}
}
