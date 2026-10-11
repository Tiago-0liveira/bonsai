package webtunnel

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	cloudflareMetrics = regexp.MustCompile(`Starting metrics server on (\S+)/metrics`)
	cloudflareError   = regexp.MustCompile(`error="([^"]+)"`)
)

// CloudflaredReadyURL learns the connector's own readiness endpoint. Only
// loopback listeners are probed, and redirects are refused by the caller.
func CloudflaredReadyURL(log string) (string, bool) {
	all := cloudflareMetrics.FindAllStringSubmatch(log, -1)
	if len(all) == 0 {
		return "", false
	}
	host, port, err := net.SplitHostPort(all[len(all)-1][1])
	if err != nil {
		return "", false
	}
	if host == "localhost" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(host)
	p, err := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || err != nil || p < 1 || p > 65535 {
		return "", false
	}
	return "http://" + net.JoinHostPort(host, port) + "/ready", true
}

// CloudflaredPermanentError identifies failures that waiting for GitHub's
// ping cannot resolve. The exact diagnostic stays visible in setup/status.
func CloudflaredPermanentError(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "unauthorized") || strings.Contains(lower, "tunnel not found") ||
		strings.Contains(lower, "error locating origin cert") ||
		(strings.Contains(lower, "credentials file") && strings.Contains(lower, "doesn't exist"))
}

// CloudflaredFailure returns the latest permanent failure since registration.
// A successful /ready probe takes precedence over errors of other connections.
func CloudflaredFailure(log string) string {
	failure := ""
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "Registered tunnel connection") {
			failure = ""
		}
		if !CloudflaredPermanentError(line) {
			continue
		}
		failure = strings.TrimSpace(line)
		if match := cloudflareError.FindStringSubmatch(line); match != nil {
			failure = match[1]
		}
	}
	return failure
}
