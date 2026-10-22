package claude

import "os/exec"

// isolate keeps exec's default cancellation (kill the process) on Windows.
func isolate(*exec.Cmd) {}
