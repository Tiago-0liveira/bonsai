//go:build linux

package git

import "syscall"

func setParentDeathSignal(attr *syscall.SysProcAttr) {
	// Backstop for uncatchable Bonsai death (including SIGKILL): terminate git
	// so it can clean up lockfiles. This reaches only git; the executable
	// terminates whole registered groups on catchable shutdown signals.
	// Go caveat: Pdeathsig tracks the forking OS thread, which exits early only
	// when a goroutine exits while still locked with runtime.LockOSThread.
	attr.Pdeathsig = syscall.SIGTERM
}
