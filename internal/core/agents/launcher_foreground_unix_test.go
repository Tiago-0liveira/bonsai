//go:build !windows

package agents

import (
	"context"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestForegroundLauncherRestoresTerminalMode(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	// The child leaves the terminal raw, as a UI killed mid-run does.
	launcher := NewForegroundLauncher(slave, nil, nil)
	err = launcher.RunForeground(context.Background(), PreparedSession{
		Executable: "/bin/sh", Args: []string{"-c", "stty raw -echo"}, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal mode not restored: before %+v, after %+v", before, after)
	}
}

// Ctrl+C is delivered to Bonsai and the child alike; Bonsai must keep running
// until the child ends so cleanup still happens.
func TestForegroundLauncherSurvivesInterrupt(t *testing.T) {
	launcher := NewForegroundLauncher(nil, nil, nil)
	done := make(chan error, 1)
	go func() {
		done <- launcher.RunForeground(context.Background(), PreparedSession{
			Executable: "/bin/sh", Args: []string{"-c", "sleep 1"}, Dir: t.TempDir(),
		})
	}()
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunForeground: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("launcher did not return")
	}
}
