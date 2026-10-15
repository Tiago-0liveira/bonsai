//go:build windows

package agenthost

import "os/exec"

func setDetach(*exec.Cmd) {}

func releaseStdout() {}
