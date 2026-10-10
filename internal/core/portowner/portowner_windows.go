package portowner

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func find(port int) (Owner, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return Owner{}, false
	}
	pid, ok := parseNetstat(string(out), port)
	if !ok {
		return Owner{}, false
	}
	owner := Owner{PID: pid}
	if list, err := exec.CommandContext(ctx, "tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output(); err == nil {
		owner.Name = parseTasklist(string(list))
	}
	return owner, true
}
