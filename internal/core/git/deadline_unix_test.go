//go:build !windows

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// fakeGit puts an executable "git" running script first on PATH.
func fakeGit(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestRunContextDeadlineBoundsDescendantsHoldingOutput(t *testing.T) {
	setsid, _ := exec.LookPath("setsid")
	tests := []struct {
		name, script string
	}{
		{"same process group", "sleep 5 &\nwait\n"},
		// Escapes the group kill, so only WaitDelay can release the pipe.
		{"detached session", setsid + " sleep 5 &\nwait\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "detached session" && setsid == "" {
				t.Skip("no setsid")
			}
			dir := fakeGit(t, tt.script)
			old := gitWaitDelay
			gitWaitDelay = 200 * time.Millisecond
			t.Cleanup(func() { gitWaitDelay = old })

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := RunContext(ctx, dir, "status")
			if err == nil {
				t.Fatal("expected the deadline error")
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("100ms deadline held for %v by a descendant", elapsed)
			}
		})
	}
}
