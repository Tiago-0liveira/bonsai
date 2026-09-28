//go:build windows

package pty

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func collectWindowsProcess(t *testing.T, p Process) ([]byte, error) {
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

func TestWindowsConPTYOutputResizeAndExitCode(t *testing.T) {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	p, err := Start(exec.Command(shell, "/c", "echo pty-ok"), 80, 24)
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
	out, waitErr := collectWindowsProcess(t, p)
	_ = p.Session().Close()
	if waitErr != nil || !strings.Contains(string(out), "pty-ok") {
		t.Fatalf("output=%q wait=%v", out, waitErr)
	}

	p, err = Start(exec.Command(shell, "/c", "exit /b 7"), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, waitErr = collectWindowsProcess(t, p)
	_ = p.Session().Close()
	ee, ok := waitErr.(*exec.ExitError)
	if !ok || ee.ExitCode() != 7 {
		t.Fatalf("wait error = %#v, want exit code 7", waitErr)
	}
}
