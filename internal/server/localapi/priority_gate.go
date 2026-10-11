package localapi

import "sync"

// priorityGate is a counting semaphore with two lanes. A freed slot goes to the
// oldest high-lane waiter, then to the oldest low-lane waiter. Admission order
// is decided when enter is called, not when the waiting goroutine happens to be
// scheduled, so callers that enter in a known order are served in that order.
type priorityGate struct {
	mu   sync.Mutex
	free int
	high []chan struct{}
	low  []chan struct{}
}

func newPriorityGate(slots int) *priorityGate {
	return &priorityGate{free: slots}
}

// enter reserves a place in line. The returned channel is closed once the
// caller holds a slot; the holder must call release exactly once.
func (g *priorityGate) enter(high bool) <-chan struct{} {
	ticket := make(chan struct{})
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.free > 0 {
		g.free--
		close(ticket)
		return ticket
	}
	if high {
		g.high = append(g.high, ticket)
	} else {
		g.low = append(g.low, ticket)
	}
	return ticket
}

func (g *priorityGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case len(g.high) > 0:
		close(g.high[0])
		g.high = g.high[1:]
	case len(g.low) > 0:
		close(g.low[0])
		g.low = g.low[1:]
	default:
		g.free++
	}
}
