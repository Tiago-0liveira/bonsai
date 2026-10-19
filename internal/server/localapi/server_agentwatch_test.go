package localapi

import (
	"context"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
)

func TestServerCloseJoinsAgentWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	release := make(chan struct{})
	go func() {
		<-ctx.Done()
		<-release
		close(done)
	}()
	s := &Server{agents: &agentAPI{manager: agenthost.NewManager(nil, nil, nil, agenthost.Paths{}, nil, nil)}, stopAgentWatch: cancel, agentWatchDone: done}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while watcher was still running")
	case <-ctx.Done():
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join watcher")
	}
}
