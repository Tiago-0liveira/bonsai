//go:build !windows

package git

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKillTreeOnCancelStartsNewSession(t *testing.T) {
	for _, name := range []string{"sh", "ps"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s unavailable: %v", name, err)
		}
	}
	if out, err := exec.Command("ps", "-o", "sid=", "-p", strconv.Itoa(os.Getpid())).CombinedOutput(); err != nil {
		t.Skipf("ps cannot report session IDs: %v: %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' "$$"; ps -o sid= -p "$$"`)
	killTreeOnCancel(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("want PID and session ID, got %q", out)
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid != cmd.Process.Pid {
		t.Fatalf("child PID = %q, want %d", fields[0], cmd.Process.Pid)
	}
	sid, err := strconv.Atoi(fields[1])
	if err != nil || sid != pid {
		t.Fatalf("child session ID = %q, want child PID %d", fields[1], pid)
	}
}
