//go:build linux

package git

import "syscall"

func setParentDeathSignal(attr *syscall.SysProcAttr) {
	// Terminate git when Bonsai dies, allowing git to clean up its lockfiles.
	// Go caveat: Pdeathsig tracks the forking OS thread, which exits early only
	// when a goroutine exits while still locked with runtime.LockOSThread.
	attr.Pdeathsig = syscall.SIGTERM
}
