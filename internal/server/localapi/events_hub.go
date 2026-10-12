package localapi

import "sync"

type localEvent struct {
	Type      string           `json:"type"`
	ProjectID string           `json:"project_id,omitempty"`
	EntityID  string           `json:"entity_id,omitempty"`
	Component string           `json:"component,omitempty"`
	Epoch     string           `json:"epoch,omitempty"`
	Sequence  uint64           `json:"sequence,omitempty"`
	Projects  []ProjectInfo    `json:"projects"`
	Snapshot  *browserSnapshot `json:"snapshot,omitempty"`
	Revision  uint64           `json:"-"` // agents_changed only, see wire
}

// wire returns what the websocket sends for an event: agents_changed is only
// {type, revision}; every other event keeps its full shape.
func (e localEvent) wire() any {
	if e.Type == agentsChangedEvent {
		return struct {
			Type     string `json:"type"`
			Revision uint64 `json:"revision"`
		}{e.Type, e.Revision}
	}
	return e
}

type eventHub struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]chan localEvent
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: map[int]chan localEvent{}}
}

func (h *eventHub) subscribe() (int, <-chan localEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	ch := make(chan localEvent, 64)
	h.subscribers[h.nextID] = ch
	return h.nextID, ch
}

func (h *eventHub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := h.subscribers[id]
	if ch == nil {
		return
	}
	delete(h.subscribers, id)
	close(ch)
}

func (h *eventHub) publish(event localEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			delete(h.subscribers, id)
			close(ch)
		}
	}
}

func (h *eventHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}
