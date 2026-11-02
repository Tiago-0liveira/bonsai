package agents

import (
	"context"
	"encoding/json"
)

// StructuredSupport is a provider's verdict on one-shot, tool-free calls that
// answer with a JSON object matching a schema. Supported is true only when the
// provider's own CLI removes tools below the model; a prompt asking the model
// not to use them is not enforcement.
type StructuredSupport struct {
	Supported bool
	// Mechanism names what removes the tools, such as "claude -p --tools ''".
	// Empty when unsupported.
	Mechanism string
	// Reason says why the provider cannot do it; empty when supported.
	Reason string
}

// StructuredRequest is one tool-free call.
type StructuredRequest struct {
	Account Account
	// Model and Effort override the profile's settings when non-empty.
	Model, Effort string
	// System is fixed text and may appear in argv.
	System string
	// Input is user-derived and travels on stdin only: never argv or env.
	Input []byte
	// Schema is the JSON Schema the answer must satisfy.
	Schema json.RawMessage
	// MaxOutput caps the bytes read from the provider; exceeding it fails with
	// ErrStructuredOutput. Zero selects the provider default.
	MaxOutput int64
}

// StructuredRunner is implemented by providers that can run a StructuredRequest.
// RunStructured returns the answer object as JSON. Errors never carry the
// provider's raw output.
type StructuredRunner interface {
	Structured() StructuredSupport
	RunStructured(context.Context, StructuredRequest) (json.RawMessage, error)
}

// StructuredOf reports p's structured-call verdict. Providers without the
// capability are unsupported: there is no tool-enabled fallback.
func StructuredOf(p Provider) StructuredSupport {
	if r, ok := p.(StructuredRunner); ok {
		return r.Structured()
	}
	return StructuredSupport{Reason: "the provider has no tool-free structured mode"}
}
