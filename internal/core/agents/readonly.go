package agents

import "fmt"

// ReadOnlySupport is a provider's verdict on running a session that cannot
// change anything. Supported is true only when the provider's own CLI enforces
// the restriction below the model (a tool allowlist or an OS sandbox). A prompt
// that asks the model not to edit, or a mode that merely proposes a plan, is
// not enforcement and never makes a provider Supported.
type ReadOnlySupport struct {
	Supported bool
	// Mechanism names what enforces the restriction, such as "OS sandbox
	// (codex -s read-only)". Empty when unsupported.
	Mechanism string
	// Allows lists what the session may still do, in the words a person accepts
	// before a read-only run starts. Empty when unsupported.
	Allows []string
	// PlanFile is true when the provider can additionally let a read-only session write one named file
	// (LaunchOptions.PlanFile) and nothing else. A provider that cannot confine the write must leave it false.
	PlanFile bool
	// Reason says why the provider cannot be read-only; empty when supported.
	Reason string
}

// ReadOnlyEnforcer is implemented by providers that know whether they can run
// read-only. A provider that does not implement it is unsupported.
type ReadOnlyEnforcer interface {
	ReadOnly() ReadOnlySupport
}

// ReadOnlyOf reports p's read-only verdict. Providers without the capability
// are unsupported: there is no unrestricted fallback.
func ReadOnlyOf(p Provider) ReadOnlySupport {
	if e, ok := p.(ReadOnlyEnforcer); ok {
		return e.ReadOnly()
	}
	return ReadOnlySupport{Reason: "the provider has no enforced read-only mode"}
}

// RequireReadOnly returns ErrReadOnlyUnsupported when p cannot enforce read-only
// access. Launch paths call it before any session exists.
func RequireReadOnly(p Provider) error {
	if s := ReadOnlyOf(p); !s.Supported {
		reason := s.Reason
		if reason == "" {
			reason = "no enforced read-only mode"
		}
		return fmt.Errorf("%w: %s: %s", ErrReadOnlyUnsupported, p.ID(), reason)
	}
	return nil
}
