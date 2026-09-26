package server_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func TestPTYIntegrationHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(os.Args) {
		return
	}
	mode := os.Args[sep+1]
	switch mode {
	case "hold":
		fmt.Fprintln(os.Stdout, "PTY-READY")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSuffix(scanner.Text(), "\r")
			if line == "exit" {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stdout, "reply:%s\n", line)
		}
	case "ticker":
		fmt.Fprintln(os.Stdout, "tick-one")
		time.Sleep(300 * time.Millisecond)
		fmt.Fprintln(os.Stdout, "tick-two")
		time.Sleep(200 * time.Millisecond)
	case "ansi":
		_, _ = os.Stdout.Write([]byte("\x1b[31mRED\x1b[0m\r\n"))
	case "burst":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 16<<10))
		fmt.Fprintln(os.Stdout, "BURST-END")
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func ptyHelperCommand(t *testing.T, mode string) (string, []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe, []string{"-test.run=^TestPTYIntegrationHelper$", "--", mode}
}

func readPTYUntil(t *testing.T, att client.PTYAttachment, timeout time.Duration, want string) (string, uint64) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var out strings.Builder
	var seq uint64
	for {
		select {
		case ev, ok := <-att.Events():
			if !ok {
				t.Fatalf("PTY attachment closed before %q; output=%q", want, out.String())
			}
			if ev.Kind == protocol.KindPTYOutput {
				out.Write(ev.Data)
				if ev.Seq > seq {
					seq = ev.Seq
				}
				if strings.Contains(out.String(), want) {
					return out.String(), seq
				}
			}
			if ev.Kind == protocol.KindPTYError {
				t.Fatalf("PTY error: %s", ev.Error)
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q; output=%q", want, out.String())
		}
	}
}

func waitPTYExit(t *testing.T, att client.PTYAttachment, timeout time.Duration) client.PTYEvent {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-att.Events():
			if !ok {
				t.Fatal("PTY attachment closed without exit event")
			}
			if ev.Kind == protocol.KindPTYExit {
				return ev
			}
		case <-timer.C:
			t.Fatal("timed out waiting for PTY exit")
		}
	}
}

func TestPTYStructuredSpawnInputAndKill(t *testing.T) {
	c, root := newDaemon(t)
	working := filepath.Join(root, "apps", "terminal")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	program, args := ptyHelperCommand(t, "hold")
	rec, err := c.SpawnPTYExec(root, "feat/pty", working, "terminal", program, args, 100, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.IOMode != procstore.IOModePTY || rec.PTYCols != 100 || rec.PTYRows != 30 {
		t.Fatalf("PTY metadata = %+v", rec)
	}
	if rec.Policy.Mode != procstore.PolicyNo {
		t.Fatalf("PTY default policy = %+v, want no", rec.Policy)
	}
	if rec.Program != program || rec.WorkingDir != working || rec.Worktree != root {
		t.Fatalf("structured metadata = %+v", rec)
	}

	att, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer att.Close()
	readPTYUntil(t, att, 3*time.Second, "PTY-READY")
	if err := att.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, att, 3*time.Second, "reply:hello")

	killed, err := c.Kill(rec.ID, false, "")
	if err != nil || len(killed) != 1 {
		t.Fatalf("kill = %v, %v", killed, err)
	}
	waitPTYExit(t, att, 3*time.Second)
	if !waitFor(t, 3*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusStopped
	}) {
		t.Fatalf("PTY process did not stop: %+v", recByID(t, c, rec.ID))
	}
}

func TestPTYANSIReplayAndReconnect(t *testing.T) {
	c, root := newDaemon(t)
	program, args := ptyHelperCommand(t, "ansi")
	rec, err := c.SpawnPTYExec(root, "", root, "ansi", program, args, 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	att, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	out, seq := readPTYUntil(t, att, 3*time.Second, "\x1b[31mRED\x1b[0m")
	if !strings.Contains(out, "\x1b[31mRED\x1b[0m") {
		t.Fatalf("ANSI bytes changed: %q", out)
	}
	_ = att.Close()

	replay, err := c.AttachPTY(context.Background(), rec.ID, seq-1)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	out, _ = readPTYUntil(t, replay, 3*time.Second, "\x1b[31mRED\x1b[0m")
	if !strings.Contains(out, "\x1b[31mRED\x1b[0m") {
		t.Fatalf("replayed ANSI bytes changed: %q", out)
	}
}

func TestPTYReconnectReplaysMissedOutput(t *testing.T) {
	c, root := newDaemon(t)
	program, args := ptyHelperCommand(t, "ticker")
	rec, err := c.SpawnPTYExec(root, "", root, "ticker", program, args, 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	att, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, seq := readPTYUntil(t, att, 3*time.Second, "tick-one")
	_ = att.Close()

	time.Sleep(400 * time.Millisecond)
	replay, err := c.AttachPTY(context.Background(), rec.ID, seq)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	readPTYUntil(t, replay, 3*time.Second, "tick-two")
}

func TestPTYMultipleObserversAndResizeRestart(t *testing.T) {
	c, root := newDaemon(t)
	program, args := ptyHelperCommand(t, "hold")
	rec, err := c.SpawnPTYExec(root, "", root, "multi", program, args, 90, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	readPTYUntil(t, a, 3*time.Second, "PTY-READY")
	readPTYUntil(t, b, 3*time.Second, "PTY-READY")
	if err := a.Write([]byte("shared\n")); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, a, 3*time.Second, "reply:shared")
	readPTYUntil(t, b, 3*time.Second, "reply:shared")

	if err := a.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.PTYCols == 120 && r.PTYRows == 40
	}) {
		t.Fatalf("resize not persisted: %+v", recByID(t, c, rec.ID))
	}
	oldPID := rec.PID
	restarted, err := c.Restart(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.PID == oldPID || restarted.PTYCols != 120 || restarted.PTYRows != 40 {
		t.Fatalf("restart metadata = %+v, old pid=%d", restarted, oldPID)
	}
	fresh, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	readPTYUntil(t, fresh, 3*time.Second, "PTY-READY")
}

func TestPTYLiveOutputSurvivesLogRotation(t *testing.T) {
	c, root := newDaemonWithLogCap(t, 1024)
	program, args := ptyHelperCommand(t, "burst")
	rec, err := c.SpawnPTYExec(root, "", root, "burst", program, args, 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	att, err := c.AttachPTY(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer att.Close()
	readPTYUntil(t, att, 3*time.Second, "BURST-END")
	waitPTYExit(t, att, 3*time.Second)

	data, err := c.Store().ReadCombinedLog(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("BURST-END")) {
		t.Fatal("durable rotated log lost terminal output")
	}
}
