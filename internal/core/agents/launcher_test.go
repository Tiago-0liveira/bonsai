package agents

import (
	"runtime"
	"strings"
	"testing"
)

func TestBuildEnvironmentSetUnsetAndPreserve(t *testing.T) {
	base := []string{"KEEP=yes", "CHANGE=old", "REMOVE=gone"}
	got := BuildEnvironment(base, map[string]string{"CHANGE": "new", "ADDED": "ok"}, []string{"REMOVE"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"KEEP=yes", "CHANGE=new", "ADDED=ok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment missing %q: %v", want, got)
		}
	}
	key := "REMOVE="
	if runtime.GOOS == "windows" {
		key = "REMOVE="
	}
	if strings.Contains(strings.ToUpper(joined), key) {
		t.Fatalf("environment retained removed key: %v", got)
	}
}
