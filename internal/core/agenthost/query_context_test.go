//go:build !windows

package agenthost

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stalledHolder(t *testing.T) (Record, net.Listener) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agent-dial-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "holder.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return Record{Protocol: Version, Socket: listener.Addr().String()}, listener
}

func awaitCanceled(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request remained blocked")
	}
}

func TestQueryContextCancelsInfoRead(t *testing.T) {
	record, listener := stalledHolder(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := QueryContext(ctx, record)
		result <- err
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	if _, err := r.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	if err := encode(conn, Frame{Type: "ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	cancel()
	awaitCanceled(t, result)
}
