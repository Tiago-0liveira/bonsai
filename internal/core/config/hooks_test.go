package config

import "testing"

func TestBaseBranchKeepsSlashedNames(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "main"},
		{"main", "main"},
		{"origin/main", "main"},
		{"origin/release/v2", "release/v2"},
		{"refs/remotes/origin/release/v2", "release/v2"},
		{"remotes/origin/release/v2", "release/v2"},
		{"refs/heads/release/v2", "release/v2"},
		{"fork/feat/x/y", "feat/x/y"},
	}
	for _, tt := range tests {
		if got := BaseBranch(tt.in); got != tt.want {
			t.Errorf("BaseBranch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBranchOnRemote(t *testing.T) {
	tests := []struct{ ref, remote, want string }{
		{"main", "origin", "main"},
		{"release/v2", "origin", "release/v2"},
		{"origin/release/v2", "origin", "release/v2"},
		{"refs/remotes/origin/release/v2", "origin", "release/v2"},
		{"refs/heads/release/v2", "origin", "release/v2"},
		{"origin/main", "fork", "origin/main"},
	}
	for _, tt := range tests {
		if got := BranchOnRemote(tt.ref, tt.remote); got != tt.want {
			t.Errorf("BranchOnRemote(%q, %q) = %q, want %q", tt.ref, tt.remote, got, tt.want)
		}
	}
}
