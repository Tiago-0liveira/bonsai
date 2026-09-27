package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentAccountListWorksOutsideGitRepository(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	var out, errOut bytes.Buffer
	if err := RunWithIO([]string{"agent", "account", "list"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("agent list outside git repo: %v (stderr=%q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "NAME") || !strings.Contains(out.String(), "PROVIDER") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
