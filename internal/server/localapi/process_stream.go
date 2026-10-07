package localapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
	"github.com/gorilla/websocket"
)

func isProcessTerminalRoute(path string) bool {
	p := strings.Split(path, "/")
	return len(p) == 7 && p[1] == "api" && p[2] == "projects" && p[3] != "" && p[4] == "processes" && p[5] != "" && p[6] == "terminal"
}
func (s *Server) processTerminal(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	streamer, ok := s.registry.Default().daemon.(interface {
		StreamProcess(context.Context, int, string, int64, func(*protocol.Response) error) error
	})
	if !ok {
		writeAPIError(w, 503, "process_stream_unavailable", "Process streaming unavailable")
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
	conn.SetReadLimit(16 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(websocketAuthTimeout))
	var auth websocketAuth
	if conn.ReadJSON(&auth) != nil || auth.Type != "authenticate" || !s.sessions.valid(auth.Token) {
		return
	}
	var attach struct {
		Type       string `json:"type"`
		Generation string `json:"generation"`
		Offset     int64  `json:"offset"`
	}
	if conn.ReadJSON(&attach) != nil || attach.Type != "attach" || attach.Offset < 0 {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					cancel()
					return
				}
			}
		}
	}()
	send := func(value any) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(value)
	}
	first := true
	err = streamer.StreamProcess(ctx, id, attach.Generation, attach.Offset, func(frame *protocol.Response) error {
		if first {
			if err := send(map[string]any{"type": "ready", "version": 1, "generation": frame.Generation}); err != nil {
				return err
			}
			first = false
		}
		if frame.Gap {
			if err := send(map[string]any{"type": "gap", "offset": frame.Offset - int64(len(frame.Data))}); err != nil {
				return err
			}
		}
		if len(frame.Data) > 0 {
			if err := send(map[string]any{"type": "output", "offset": frame.Offset, "data": frame.Data}); err != nil {
				return err
			}
		}
		if frame.Record != nil {
			return send(map[string]any{"type": "status", "status": processSummary(s.registry.Default().info.ID, frame.Record)})
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		_ = send(map[string]any{"type": "error", "message": err.Error()})
	}
}
