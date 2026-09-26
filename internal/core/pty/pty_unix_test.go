//go:build !windows

package pty

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func collectProcess(t *testing.T, p Process) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&out, p.Session())
		close(readDone)
	}()
	waitErr := p.Wait(context.Background())
	select {
	case <-readDone:
	case <-time.After(time.Second):
		_ = p.Session().Close()
		<-readDone
	}
	return out.Bytes(), waitErr
}

func TestUnixPTYIsTTYAndResizes(t *testing.T) {
	cmd := exec.Command("sh", "-c", "test -t 0 && test -t 1 && test -t 2 || exit 9; printf '__TTY_OK__\\n'; read _; stty size")
	p, err := Start(cmd, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Session().Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	cols, rows, err := p.Session().Size()
	if err != nil {
		t.Fatal(err)
	}
	if cols != 100 || rows != 40 {
		t.Fatalf("size = %dx%d, want 100x40", cols, rows)
	}
	if _, err := p.Session().Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	out, waitErr := collectProcess(t, p)
	_ = p.Session().Close()
	if waitErr != nil {
		t.Fatalf("wait: %v; output=%q", waitErr, out)
	}
	text := strings.ReplaceAll(string(out), "\r", "")
	if !strings.Contains(text, "__TTY_OK__") || !strings.Contains(text, "40 100") {
		t.Fatalf("PTY output = %q", text)
	}
}

func TestUnixPTYReportsNonZeroExit(t *testing.T) {
	p, err := Start(exec.Command("sh", "-c", "exit 7"), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, waitErr := collectProcess(t, p)
	_ = p.Session().Close()
	ee, ok := waitErr.(*exec.ExitError)
	if !ok || ee.ExitCode() != 7 {
		t.Fatalf("wait error = %#v, want exit code 7", waitErr)
	}
}

func TestUnixPTYCloseUnblocksRead(t *testing.T) {
	cmd := exec.Command("sh", "-c", "cat")
	p, err := Start(cmd, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		_, _ = p.Session().Read(buf)
		close(done)
	}()
	if err := p.Session().Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.Wait(ctx)
}
