package git

import (
	"errors"
	"os/exec"
	"testing"
)

func TestValidBranchName(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{
		{"main", nil},
		{"feature/awesome", nil},
		{"fix-1.2_x", nil},
		{"ünï/cödé", nil},
		{"a.b/c.d", nil},
		{"", ErrBranchEmpty},
		{"@", ErrBranchAt},
		{"HEAD", ErrBranchHEAD},
		{"my test", ErrBranchSpace},
		{"my\ttest", ErrBranchControl},
		{"a\x7fb", ErrBranchControl},
		{"a..b", ErrBranchDoubleDot},
		{"a~b", ErrBranchForbiddenChar},
		{"a^b", ErrBranchForbiddenChar},
		{"a:b", ErrBranchForbiddenChar},
		{"a?b", ErrBranchForbiddenChar},
		{"a*b", ErrBranchForbiddenChar},
		{"a[b", ErrBranchForbiddenChar},
		{`a\b`, ErrBranchForbiddenChar},
		{"-x", ErrBranchLeadingDash},
		{"--force", ErrBranchLeadingDash},
		{"a/", ErrBranchEdgeSlash},
		{"/a", ErrBranchEdgeSlash},
		{"a//b", ErrBranchDoubleSlash},
		{"a.lock", ErrBranchLockSuffix},
		{"a.lock/b", ErrBranchLockSuffix},
		{"a/.hidden", ErrBranchLeadingDot},
		{".hidden", ErrBranchLeadingDot},
		{"a@{b", ErrBranchAtBrace},
		{"a.", ErrBranchTrailingDot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidBranchName(tt.name); !errors.Is(err, tt.want) {
				t.Fatalf("ValidBranchName(%q) = %v, want %v", tt.name, err, tt.want)
			}
			// Agreement with git itself where it is installed.
			if _, err := exec.LookPath("git"); err != nil {
				return
			}
			gitOK := exec.Command("git", "check-ref-format", "--branch", tt.name).Run() == nil
			if gitOK != (tt.want == nil) && tt.name != "HEAD" && tt.name != "@" { // --branch expands these two instead of judging them
				t.Fatalf("git accepts %q = %v, ValidBranchName disagrees", tt.name, gitOK)
			}
		})
	}
}

func TestSuggestBranchName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"my test", "my-test"},
		{"a  b", "a--b"},
		{"feat: add x?", "feat-add-x"},
		{"a..b", "a.b"},
		{"-x", "x"},
		{"a//b/", "a/b"},
		{"x.lock", "x"},
		{"a@{1}", "a@1}"},
		{"???", ""},
		{"ok", "ok"},
	}
	for _, tt := range tests {
		if got := SuggestBranchName(tt.in); got != tt.want {
			t.Errorf("SuggestBranchName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if got := SuggestBranchName(tt.in); got != "" && ValidBranchName(got) != nil {
			t.Errorf("suggestion %q for %q is invalid", got, tt.in)
		}
	}
}

func TestAddWorktreeNewBranchRejectsInvalidName(t *testing.T) {
	err := AddWorktreeNewBranch(t.TempDir(), "unused", "my test", "")
	if !errors.Is(err, ErrBranchSpace) {
		t.Fatalf("err = %v", err)
	}
}
