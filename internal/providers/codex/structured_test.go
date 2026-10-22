package codex

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestStructuredUnsupported(t *testing.T) {
	if s := agents.StructuredOf(New(nil, nil)); s.Supported || s.Reason == "" {
		t.Fatalf("verdict = %+v", s)
	}
}
