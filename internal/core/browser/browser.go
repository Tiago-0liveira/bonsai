// Package browser opens a URL in the user's default web browser. It never
// fails loudly: callers print the URL themselves when Open returns an error.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const openTimeout = 10 * time.Second

// Open launches the platform URL handler for rawURL. Only http and https URLs
// are accepted, so nothing else can reach a shell-like handler.
func Open(rawURL string) error {
	if err := validate(rawURL); err != nil {
		return err
	}
	candidates := Commands(runtime.GOOS, IsWSL(), rawURL)
	var errs []error
	for _, argv := range candidates {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		err := cmd.Run()
		cancel()
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", argv[0], err))
	}
	if len(errs) == 0 {
		return errors.New("no browser launcher found")
	}
	return errors.Join(errs...)
}

// Commands lists the launchers to try, in order, for goos. On WSL the Windows
// browser is preferred: wslview, then cmd.exe, then the Linux xdg-open.
func Commands(goos string, wsl bool, rawURL string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"open", rawURL}}
	case "windows":
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", rawURL}}
	}
	var out [][]string
	if wsl {
		out = append(out, []string{"wslview", rawURL})
		// cmd.exe parses its command line; only pass URLs without its
		// metacharacters. The empty argument is start's window title (WSL interop
		// quotes it as "").
		if !strings.ContainsAny(rawURL, "&|<>^%\"()") {
			out = append(out, []string{"cmd.exe", "/c", "start", "", rawURL})
		}
	}
	return append(out, []string{"xdg-open", rawURL})
}

// IsWSL reports whether this process runs inside Windows Subsystem for Linux.
func IsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	raw, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(raw)), "microsoft")
}

func validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: only http(s) URLs are supported", rawURL)
	}
	return nil
}
