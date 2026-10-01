package ghcli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryWithForcedColorEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh uses a shell script")
	}
	dir := t.TempDir()
	// Simulate gh styling included headers when terminal color is forced.
	script := `#!/bin/sh
if [ -n "$CLICOLOR_FORCE$FORCE_COLOR$GH_FORCE_TTY" ] || [ "$NO_COLOR" != "1" ]; then
  printf 'HTTP/1.1 200 OK\r\n\033[1;34mAccess-Control-Allow-Origin\033[m: *\r\n\r\n'
else
  printf 'HTTP/1.1 200 OK\r\nAccess-Control-Allow-Origin: *\r\n\r\n'
fi
printf '{"id":123,"full_name":"owner/repo","default_branch":"main"}'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("GH_FORCE_TTY", "100%")
	t.Setenv("NO_COLOR", "")
	repo, err := New(dir).Repository(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if repo.FullName != "owner/repo" || repo.DefaultBranch != "main" {
		t.Fatalf("unexpected repository: %+v", repo)
	}
}
