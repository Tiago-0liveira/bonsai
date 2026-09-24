package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogWriterRotationFailurePreservesLiveLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc.log")
	w, err := newLogWriter(path, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}

	// A non-empty directory at the backup path makes replacement fail on every
	// supported platform. Rotation must fall back to appending the live log,
	// never truncate it.
	backup := path + ".1"
	if err := os.Mkdir(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatalf("write should continue after recoverable rotation failure: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "first\n") || !strings.Contains(got, "second\n") {
		t.Fatalf("live log was lost during failed rotation: %q", got)
	}
}

func TestLogWriterRepeatedRotationReplacesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc.log")
	w, err := newLogWriter(path, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("aaaa\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("bbbb\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("cccc\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(backup); got != "bbbb\n" {
		t.Fatalf("backup = %q, want latest previous generation %q", got, "bbbb\n")
	}
	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(live); got != "cccc\n" {
		t.Fatalf("live = %q, want %q", got, "cccc\n")
	}
	if gen := w.Generation(); gen != 2 {
		t.Fatalf("generation = %d, want 2", gen)
	}
}
