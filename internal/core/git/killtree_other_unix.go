//go:build !windows && !linux

package git

import "syscall"

func setParentDeathSignal(_ *syscall.SysProcAttr) {}
