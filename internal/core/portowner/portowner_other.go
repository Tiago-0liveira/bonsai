//go:build !linux && !windows

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
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return Owner{}, false
	}
	return parseLsof(string(out))
}
