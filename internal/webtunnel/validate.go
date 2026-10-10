package webtunnel

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// portReference matches a ":<digits>" port reference ("127.0.0.1:7002",
// "http://host:7002/", "--metrics :7001"), capturing the whole digit run.
var portReference = regexp.MustCompile(`:(\d+)`)

// ValidateArgv is the rail that keeps a tunnel pointed at the webhook
// receiver: the argv must reference the webhook port, must never mention the
// API port, and every host:port reference in it must be the webhook port (so
// it cannot forward a project service either). Both the CLI and the daemon
// run it; the daemon's check is the one that counts.
func ValidateArgv(argv []string, webhookPort, apiPort int) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("tunnel command is empty")
	}
	if webhookPort < 1 || webhookPort > 65535 {
		return fmt.Errorf("webhook port %d is not a valid port", webhookPort)
	}
	if apiPort < 1 || apiPort > 65535 {
		return fmt.Errorf("api port %d is not a valid port", apiPort)
	}
	if webhookPort == apiPort {
		return fmt.Errorf("webhook port must differ from the api port %d", apiPort)
	}
	webhook, api := strconv.Itoa(webhookPort), strconv.Itoa(apiPort)
	referencesWebhook := false
	for _, arg := range argv {
		if containsNumber(arg, api) {
			return fmt.Errorf("tunnel command must never reference the api port %s (argument %q)", api, arg)
		}
		if containsNumber(arg, webhook) {
			referencesWebhook = true
		}
		// "[::1]" is an address, not a port reference.
		for _, m := range portReference.FindAllStringSubmatch(strings.ReplaceAll(arg, "[::1]", "[ipv6-loopback]"), -1) {
			if m[1] != webhook {
				return fmt.Errorf("tunnel command may only target the webhook port %s, not :%s (argument %q)", webhook, m[1], arg)
			}
		}
	}
	if !referencesWebhook {
		return fmt.Errorf("tunnel command must target the webhook port %s", webhook)
	}
	return nil
}

// containsNumber reports whether number occurs in s with no digit right
// before or after it.
func containsNumber(s, number string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], number)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(number)
		if (start == 0 || !isDigit(s[start-1])) && (end == len(s) || !isDigit(s[end])) {
			return true
		}
		i = start + 1
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
