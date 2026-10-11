package ghcli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
)

func TestTransportCountsGHSpawns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell-script gh stub")
	}
	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf 'HTTP/1.1 200 OK\\r\\nContent-Type: application/json\\r\\n\\r\\n{}'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, before := trace.Counts()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/repos/o/r", nil)
	resp, err := transport{Dir: t.TempDir()}.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	_, after := trace.Counts()
	if after-before != 1 {
		t.Fatalf("gh spawn counter advanced by %d, want 1", after-before)
	}
}
