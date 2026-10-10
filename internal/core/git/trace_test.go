package git

import (
	"context"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
)

func TestRunContextCountsGitSpawns(t *testing.T) {
	dir := gitInit(t)
	before, _ := trace.Counts()
	for i := 0; i < 3; i++ {
		if _, err := RunContext(context.Background(), dir, "rev-parse", "HEAD"); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := trace.Counts()
	if got := after - before; got != 3 {
		t.Fatalf("git spawn counter advanced by %d, want 3", got)
	}
}
