package localapi

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var websocketAuthTimeout = 3 * time.Second

type websocketAuth struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Host != s.expectedHost {
		writeAPIError(w, http.StatusForbidden, "invalid_host", "Host is not allowed")
		return
	}
	if r.Header.Get("Origin") != s.browserOrigin {
		writeAPIError(w, http.StatusForbidden, "invalid_origin", "Origin is not allowed")
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin: func(req *http.Request) bool {
			return req.Host == s.expectedHost && req.Header.Get("Origin") == s.browserOrigin
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(websocketAuthTimeout))
	var auth websocketAuth
	if err := conn.ReadJSON(&auth); err != nil || auth.Type != "authenticate" || !s.sessions.valid(auth.Token) {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "authentication required"),
			time.Now().Add(time.Second),
		)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	subscriptionID, events := s.events.subscribe()
	defer s.events.unsubscribe(subscriptionID)
	if err := conn.WriteJSON(map[string]any{"type": "ready", "sequence": s.sequence.Load()}); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case event, ok := <-events:
			if !ok {
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "client is too slow"),
					time.Now().Add(time.Second),
				)
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(map[string]any{"type": "heartbeat", "sequence": s.sequence.Load()}); err != nil {
				return
			}
		}
	}
}
