package agents

import (
	"errors"
	"testing"
)

type plainProvider struct{ Provider }

func (plainProvider) ID() ProviderID { return "plain" }

type readOnlyProvider struct {
	plainProvider
	support ReadOnlySupport
}

func (p readOnlyProvider) ReadOnly() ReadOnlySupport { return p.support }

func TestReadOnlyOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Provider
		ok   bool
	}{
		{"no capability", plainProvider{}, false},
		{"refusal", readOnlyProvider{support: ReadOnlySupport{Reason: "no sandbox"}}, false},
		{"enforced", readOnlyProvider{support: ReadOnlySupport{Supported: true, Mechanism: "sandbox"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadOnlyOf(tc.p); got.Supported != tc.ok {
				t.Fatalf("ReadOnlyOf = %+v", got)
			}
			err := RequireReadOnly(tc.p)
			if tc.ok != (err == nil) || (!tc.ok && !errors.Is(err, ErrReadOnlyUnsupported)) {
				t.Fatalf("RequireReadOnly = %v", err)
			}
		})
	}
}

func TestLaunchOptionsValuesReadOnly(t *testing.T) {
	if v := (LaunchOptions{ReadOnly: true}).Values(); v["read_only"] != "true" {
		t.Fatalf("Values = %v", v)
	}
	if v := (LaunchOptions{}).Values(); v != nil {
		t.Fatalf("Values = %v", v)
	}
}
