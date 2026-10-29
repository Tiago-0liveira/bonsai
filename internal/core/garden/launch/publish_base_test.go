package launch

import "testing"

func TestPRBasePreservesSlashedBranches(t *testing.T) {
	tests := []struct {
		ref, remote, want string
		ok                bool
	}{
		{"origin/release/v2", "origin", "release/v2", true},
		{"release/v2", "origin", "release/v2", true},
		{"main", "origin", "main", true},
		{"", "origin", "", false},
	}
	for _, tt := range tests {
		got, ok := prBase(tt.ref, tt.remote)
		if got != tt.want || ok != tt.ok {
			t.Errorf("prBase(%q, %q) = %q, %v; want %q, %v", tt.ref, tt.remote, got, ok, tt.want, tt.ok)
		}
	}
}
