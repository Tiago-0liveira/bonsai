package agym

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCleanBranchName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"feat/export-csv", "feat/export-csv"},
		{"`fix/memory-leak`", "fix/memory-leak"},
		{"\"feat/something\"", "feat/something"},
		{"git checkout -b fix/issue-123", "fix/issue-123"},
		{"branch: feat/add-ui", "feat/add-ui"},
		{"git branch: fix/login-error", "fix/login-error"},
		{"  fix/spaced-out  \n\nother text", "fix/spaced-out"},
		{"feat/complex^test:name~1", "feat/complex-test-name-1"},
		{"", ""},
		{"   \n\n  ", ""},
		{"feat/ends-with-lock.lock", "feat/ends-with-lock"},
	}

	for _, tc := range tests {
		got := CleanBranchName(tc.input)
		if got != tc.want {
			t.Errorf("CleanBranchName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSlugifyTask(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Fix memory leak in auth middleware", "fix/memory-leak-auth-middleware"},
		{"Add CSV export to tables", "feat/csv-export-tables"},
		{"Update readme documentation", "docs/readme-documentation"},
		{"Refactor database connection pool", "refactor/database-connection-pool"},
		{"Run unit test benchmarks", "test/unit-test-benchmarks"},
		{"", "feat/agent-task"},
	}

	for _, tc := range tests {
		got := SlugifyTask(tc.input)
		if got != tc.want {
			t.Errorf("SlugifyTask(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGenerateBranchName(t *testing.T) {
	t.Run("LLM returns clean branch name", func(t *testing.T) {
		client := NewClient("agym").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
			return []byte("feat/awesome-feature\n"), nil, nil
		})

		branch, err := client.GenerateBranchName(context.Background(), "personal", "Add an awesome feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "feat/awesome-feature" {
			t.Fatalf("got %q, want feat/awesome-feature", branch)
		}
	})

	t.Run("LLM fails falls back to slug", func(t *testing.T) {
		client := NewClient("agym").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
			return nil, []byte("error: network failure"), errors.New("exit 1")
		})

		branch, err := client.GenerateBranchName(context.Background(), "personal", "Fix crash on EOF in parser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(branch, "fix/") {
			t.Fatalf("expected fix/ prefix, got %q", branch)
		}
	})
}
