package agents

import (
	"context"
	"encoding/json"
	"testing"
)

type structuredStub struct {
	Provider
	support StructuredSupport
}

func (s structuredStub) Structured() StructuredSupport { return s.support }
func (structuredStub) RunStructured(context.Context, StructuredRequest) (json.RawMessage, error) {
	return nil, nil
}

func TestStructuredOf(t *testing.T) {
	if s := StructuredOf(nil); s.Supported || s.Reason == "" {
		t.Fatalf("a provider without the capability must be unsupported with a reason: %+v", s)
	}
	want := StructuredSupport{Supported: true, Mechanism: "m"}
	if got := StructuredOf(structuredStub{support: want}); got != want {
		t.Fatalf("got %+v", got)
	}
}
