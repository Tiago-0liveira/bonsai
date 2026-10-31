//go:build linux

package git

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func TestKillTreeOnCancelSetsParentDeathSignal(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "git")
	killTreeOnCancel(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("process attributes = %+v, want Pdeathsig SIGTERM", cmd.SysProcAttr)
	}
}
