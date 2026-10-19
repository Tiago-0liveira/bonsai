package agenthost

import (
	"context"
	"errors"
	"testing"
)

func TestDialContextCanceledBeforeConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := DialContext(ctx, Record{Protocol: Version, Socket: "unreachable"}, Request{Mode: ModeStatus})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DialContext error = %v", err)
	}
}
