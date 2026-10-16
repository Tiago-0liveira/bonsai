//go:build windows

package exec

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// jobs holds a Job Object per spawned command. taskkill /T walks a snapshot of
// the parent/child tree, so a descendant created while the snapshot is taken
// (cmd.exe forking ping right as Kill runs) escapes it and keeps the output pipe
// and working directory open. TerminateJobObject kills every member atomically.
var (
	jobsMu sync.Mutex
	jobs   = map[*exec.Cmd]windows.Handle{}
)

// attachProcessTree puts a started cmd into its own Job Object. Failure is
// non-fatal: killProcessTree still falls back to taskkill. The job deliberately
// lacks KILL_ON_JOB_CLOSE so releasing the handle never kills detached children.
func attachProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	jobsMu.Lock()
	jobs[cmd] = job
	jobsMu.Unlock()
}

func releaseProcessTree(cmd *exec.Cmd) {
	jobsMu.Lock()
	job, ok := jobs[cmd]
	delete(jobs, cmd)
	jobsMu.Unlock()
	if ok {
		_ = windows.CloseHandle(job)
	}
}

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	jobsMu.Lock()
	job, ok := jobs[cmd]
	jobsMu.Unlock()
	if ok {
		_ = windows.TerminateJobObject(job, 1)
	}
	// Still walk the tree: descendants forked before the job was attached are not
	// members, and cmd may never have had a job.
	KillPID(cmd.Process.Pid)
}

func terminateProcessTree(cmd *exec.Cmd, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminatePIDTree(cmd.Process.Pid, grace)
}

func terminatePIDTree(pid int, grace time.Duration) {
	if pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid)).Run()
	if grace > 0 {
		time.Sleep(grace)
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}

// KillPID terminates the process and all child processes it spawned.
func KillPID(pid int) {
	if pid <= 0 {
		return
	}
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	_ = killCmd.Run()
}
