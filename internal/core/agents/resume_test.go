package agents

import (
	"errors"
	"reflect"
	"testing"
)

func TestLaunchOptionsRoundTrip(t *testing.T) {
	yes, no := true, false
	tests := []LaunchOptions{
		{},
		{Model: "m", Effort: "high", PermissionMode: "plan"},
		{Model: "m", Sandbox: "read-only", Approval: "never", ReadOnly: true},
		{Model: "m", FullAccess: &yes},
		{Model: "m", FullAccess: &no},
	}
	for _, o := range tests {
		got := LaunchOptionsFromValues(o.Model, "n", o.Values())
		want := o
		want.DisplayName = "n"
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip = %+v, want %+v", got, want)
		}
	}
}

func TestLaunchOptionsFromValuesIgnoresJunk(t *testing.T) {
	got := LaunchOptionsFromValues("m", "", map[string]string{"bogus": "x", "full_access": "maybe"})
	if got.FullAccess != nil || got.Model != "m" {
		t.Fatalf("got %+v", got)
	}
}

func TestNotResumableUnwraps(t *testing.T) {
	err := NotResumable(ReasonNotFound)
	if !errors.Is(err, ErrNotResumable) {
		t.Fatal("does not wrap ErrNotResumable")
	}
	var nr *NotResumableError
	if !errors.As(err, &nr) || nr.Reason != ReasonNotFound {
		t.Fatalf("As = %v", nr)
	}
	if err.Error() != "cannot resume: "+ReasonNotFound {
		t.Fatalf("Error() = %q", err.Error())
	}
}
