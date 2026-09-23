package updater

import (
	"fmt"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/version"
)

const (
	// Repository is the GitHub repository (owner/repo) hosting bonsai releases.
	Repository = "Tiago-0liveira/bonsai"

	// CheckIntervalSeconds is the cooldown period between background update checks (24 hours).
	CheckIntervalSeconds = 86400

	// BackgroundTimeout is the timeout for periodic non-blocking background checks.
	BackgroundTimeout = 3 * time.Second

	// ExplicitTimeout is the timeout for explicit update checks (--update / -u).
	ExplicitTimeout = 15 * time.Second

	// RateLimitCooldownSeconds is the cooldown applied when hitting GitHub rate limits (1 hour).
	RateLimitCooldownSeconds = 3600

	// maxDownload is the maximum allowed download size (128 MB).
	maxDownload = 128 << 20
)

var (
	apiEndpointMu       sync.RWMutex
	apiEndpointOverride string
)

// SetAPIEndpointForTesting overrides the releases API endpoint during tests.
func SetAPIEndpointForTesting(endpoint string) {
	apiEndpointMu.Lock()
	apiEndpointOverride = endpoint
	apiEndpointMu.Unlock()
}

// ClearAPIEndpointForTesting clears the API endpoint test override.
func ClearAPIEndpointForTesting() {
	apiEndpointMu.Lock()
	apiEndpointOverride = ""
	apiEndpointMu.Unlock()
}

// DefaultAPI returns the GitHub Releases API URL for the latest release.
func DefaultAPI() string {
	apiEndpointMu.RLock()
	override := apiEndpointOverride
	apiEndpointMu.RUnlock()
	if override != "" {
		return override
	}
	return "https://api.github.com/repos/" + Repository + "/releases/latest"
}

// UserAgent returns the HTTP User-Agent header for updater requests.
func UserAgent() string {
	return fmt.Sprintf("bonsai-updater/%s", version.String())
}
