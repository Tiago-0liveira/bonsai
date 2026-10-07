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
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool {
		return r.Host == s.expectedHost && r.Header.Get("Origin") == s.browserOrigin
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
	project, id := r.PathValue("projectId"), r.PathValue("sessionId")
	summary, ok := s.agents.manager.Get(project, id)
	if !ok {
		return
	}
	if _, err = s.agentWorktree(r, summary.WorktreeID); err != nil {
		return
	}
	if _, err = s.agents.runtime.Accounts.Get(summary.AccountID); err != nil {
		return
	}
	generation := s.stateSync.epoch
	if attach.Generation != "" && attach.Generation != generation {
		attach.Offset = 0
	}
	subscriber, writer, replay, frames, err := s.agents.manager.Attach(project, id, attach.Offset)
	if err != nil {
		return
	}
	defer s.agents.manager.Detach(id, subscriber)
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
				if len(message.Data) > agentterminal.InputLimit || s.agents.manager.Input(id, subscriber, message.Data) != nil {
					return
				}
			case "resize":
				if s.agents.manager.Resize(id, subscriber, message.Cols, message.Rows) != nil {
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
