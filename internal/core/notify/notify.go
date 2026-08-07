// Package notify sends best-effort desktop notifications. Failures are silent —
// notifications must never disrupt the TUI.
package notify

import (
	"os/exec"
	"runtime"
)

// Send posts a desktop notification with the given title and body. It is
// fire-and-forget: any error (missing tool, unsupported OS) is swallowed.
func Send(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + quote(body) + " with title " + quote(title)
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		cmd = exec.Command("notify-send", title, body)
	case "windows":
		ps := "New-BurntToastNotification -Text " + quote(title) + "," + quote(body)
		cmd = exec.Command("powershell", "-NoProfile", "-Command", ps)
	default:
		return
	}
	_ = cmd.Start()
	if cmd.Process != nil {
		go func() { _ = cmd.Wait() }() // reap without blocking
	}
}

// quote wraps s in double quotes, escaping embedded quotes, for safe embedding in
// the osascript / powershell one-liners.
func quote(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	out = append(out, '"')
	return string(out)
}
