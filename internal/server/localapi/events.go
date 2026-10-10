package localapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var websocketAuthTimeout = 3 * time.Second

type websocketAuth struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	// ActiveProject optionally names the project the browser has open, so its
	// refresh is prioritized. Older clients omit it.
	ActiveProject string `json:"active_project,omitempty"`
}

// websocketClientFrameLimit bounds client frames: authenticate, then small
// focus messages.
const websocketClientFrameLimit = 4 << 10

// websocketFocus names the project the browser has in view; "" when its page
// is hidden. Older clients never send it, and older servers ignore it.
type websocketFocus struct {
	Type          string `json:"type"`
	ActiveProject string `json:"active_project"`
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !s.allowedHost(r.Host) {
		writeAPIError(w, http.StatusForbidden, "invalid_host", "Host is not allowed")
		return
	}
	if !s.allowedOrigin(r.Header.Get("Origin")) {
		writeAPIError(w, http.StatusForbidden, "invalid_origin", "Origin is not allowed")
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin: func(req *http.Request) bool {
			return s.allowedHost(req.Host) && s.allowedOrigin(req.Header.Get("Origin"))
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadLimit(websocketClientFrameLimit)
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

	subscriptionID, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscriptionID)
	s.stateSync.SetFocus(subscriptionID, auth.ActiveProject)
	defer s.stateSync.ClearFocus(subscriptionID)
	s.stateSync.SubscriberReady(auth.ActiveProject)

	if err := conn.WriteJSON(localEvent{Type: "ready", Epoch: s.stateSync.epoch}); err != nil {
		return
	}
	if err := conn.WriteJSON(localEvent{Type: "catalog", Epoch: s.stateSync.epoch, Projects: s.registry.List()}); err != nil {
		return
	}
	for _, project := range s.registry.List() {
		snapshot, ok := s.stateSync.CachedSnapshot(project.ID)
		if !ok {
			continue
		}
		copy := snapshot
		if err := conn.WriteJSON(localEvent{
			Type:      "project_snapshot",
			ProjectID: project.ID,
			Epoch:     snapshot.Epoch,
			Sequence:  snapshot.Sequence,
			Snapshot:  &copy,
		}); err != nil {
			return
		}
	}
	if err := conn.WriteJSON(localEvent{Type: "bootstrap_complete", Epoch: s.stateSync.epoch}); err != nil {
		return
	}

	done := make(chan struct{})
	// The reader may still apply a focus frame until it exits, so the focus is
	// cleared only after it has.
	defer func() {
		_ = conn.Close()
		<-done
		s.stateSync.ClearFocus(subscriptionID)
	}()
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var message websocketFocus
			if json.Unmarshal(data, &message) == nil && message.Type == "focus" {
				s.stateSync.SetFocus(subscriptionID, message.ActiveProject)
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
			if err := conn.WriteJSON(localEvent{Type: "heartbeat", Epoch: s.stateSync.epoch}); err != nil {
				return
			}
		}
	}
}
