package localapi

import (
	"net/http"
	"strings"
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
			return terminalEndpoint[agentterminal.Summary]{}, false
		}
		if _, err := s.agentWorktree(r, summary.WorktreeID); err != nil {
			return terminalEndpoint[agentterminal.Summary]{}, false
		}
		if _, err := s.agents.runtime.Accounts.Get(summary.AccountID); err != nil {
			return terminalEndpoint[agentterminal.Summary]{}, false
		}
		return terminalEndpoint[agentterminal.Summary]{
			attach: func(cursor uint64) (uint64, bool, []agentterminal.Frame, <-chan agentterminal.Frame, error) {
				return manager.Attach(project, id, cursor)
			},
			detach: func(subscriber uint64) { manager.Detach(id, subscriber) },
			input:  func(subscriber uint64, data []byte) error { return manager.Input(id, subscriber, data) },
			resize: func(subscriber uint64, cols, rows int) error { return manager.Resize(id, subscriber, cols, rows) },
		}, true
	})
}

// terminalEndpoint is one terminal stream a websocket attaches to: an agent
// session or a setup job.
type terminalEndpoint[S any] struct {
	attach func(cursor uint64) (uint64, bool, []agentterminal.StreamFrame[S], <-chan agentterminal.StreamFrame[S], error)
	detach func(subscriber uint64)
	input  func(subscriber uint64, data []byte) error
	resize func(subscriber uint64, cols, rows int) error
}

// serveTerminal runs the terminal websocket protocol (version 1): authenticate,
// attach, then ready, replay and live frames; the writer may send input and
// resize. resolve runs after authentication and finds the stream.
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
	generation := s.stateSync.epoch
	if attach.Generation != "" && attach.Generation != generation {
		attach.Offset = 0
	}
	subscriber, writer, replay, frames, err := endpoint.attach(attach.Offset)
	if err != nil {
		return
	}
	defer endpoint.detach(subscriber)
	send := func(value any) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(value)
	}
	if send(map[string]any{"type": "ready", "version": 1, "generation": generation, "writer": writer}) != nil {
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
			if !writer {
				return
			}
			switch message.Type {
			case "input":
				if len(message.Data) > agentterminal.InputLimit || endpoint.input(subscriber, message.Data) != nil {
					return
				}
			case "resize":
				if endpoint.resize(subscriber, message.Cols, message.Rows) != nil {
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
			if send(frame) != nil {
				return
			}
		}
	}
}
