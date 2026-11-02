package git

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseRemotes(t *testing.T) {
	out := "origin\thttps://github.com/acme/app.git (fetch)\n" +
		"origin\thttps://github.com/acme/app.git (push)\n" +
		"fork\tgit@github.com:me/app.git (fetch)\n" +
		"pushonly\thttps://example.com/x.git (push)\n" +
		"noise without tab\n" +
		"spaced\t/tmp/a path/repo (fetch)\r\n"
	want := []Remote{
		{"origin", "https://github.com/acme/app.git"},
		{"fork", "git@github.com:me/app.git"},
		{"spaced", "/tmp/a path/repo"},
	}
	if got := parseRemotes(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRemotes = %+v, want %+v", got, want)
	}
}

func TestRemoteListerUsesInjectedRunner(t *testing.T) {
	var gotDir string
	var gotArgs []string
	l := RemoteLister{Dir: "/repo", Run: func(_ context.Context, dir string, args ...string) (string, error) {
		gotDir, gotArgs = dir, args
		return "origin\thttps://github.com/acme/app (fetch)\n", nil
	}}
	remotes, err := l.ListRemotes(context.Background())
	if err != nil || len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Fatalf("ListRemotes = %+v, %v", remotes, err)
	}
	if gotDir != "/repo" || !reflect.DeepEqual(gotArgs, []string{"remote", "-v"}) {
		t.Fatalf("runner got dir %q args %v", gotDir, gotArgs)
	}
}

func TestRemoteListerWrapsRunnerError(t *testing.T) {
	boom := errors.New("boom")
	_, err := RemoteLister{Run: func(context.Context, string, ...string) (string, error) { return "", boom }}.ListRemotes(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
}

func TestRemoteListerRealGit(t *testing.T) {
	dir := gitInit(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/app.git")
	remotes, err := RemoteLister{Dir: dir}.ListRemotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0] != (Remote{"origin", "https://github.com/acme/app.git"}) {
		t.Fatalf("remotes = %+v", remotes)
	}
}
