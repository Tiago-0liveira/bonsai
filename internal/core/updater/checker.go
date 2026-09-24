package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SemVer represents parsed semantic version components (Major.Minor.Patch[-Prerelease]).
type SemVer struct {
	Major      uint64
	Minor      uint64
	Patch      uint64
	Prerelease string
}

// ParseSemver parses a semver string (e.g., "1.2.3", "v1.2.3", "v1.2.3-rc.1").
func ParseSemver(v string) (SemVer, error) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")

	if v == "" {
		return SemVer{}, fmt.Errorf("empty version string")
	}

	// Strip build metadata (anything after '+')
	if idx := strings.IndexByte(v, '+'); idx != -1 {
		v = v[:idx]
	}

	var prerelease string
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		prerelease = v[idx+1:]
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("invalid semver format %q (expected Major.Minor.Patch)", v)
	}

	major, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid major version in %q: %w", v, err)
	}
	minor, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid minor version in %q: %w", v, err)
	}
	patch, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid patch version in %q: %w", v, err)
	}

	return SemVer{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: prerelease,
	}, nil
}

// Compare compares two semantic version strings.
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func Compare(v1, v2 string) (int, error) {
	s1, err := ParseSemver(v1)
	if err != nil {
		return 0, err
	}
	s2, err := ParseSemver(v2)
	if err != nil {
		return 0, err
	}

	if s1.Major != s2.Major {
		if s1.Major < s2.Major {
			return -1, nil
		}
		return 1, nil
	}
	if s1.Minor != s2.Minor {
		if s1.Minor < s2.Minor {
			return -1, nil
		}
		return 1, nil
	}
	if s1.Patch != s2.Patch {
		if s1.Patch < s2.Patch {
			return -1, nil
		}
		return 1, nil
	}

	// Normal release has higher precedence than pre-release of the same Major.Minor.Patch
	if s1.Prerelease == "" && s2.Prerelease != "" {
		return 1, nil
	}
	if s1.Prerelease != "" && s2.Prerelease == "" {
		return -1, nil
	}
	if s1.Prerelease != s2.Prerelease {
		if s1.Prerelease < s2.Prerelease {
			return -1, nil
		}
		return 1, nil
	}

	return 0, nil
}

// IsNewerVersion returns true if remote is strictly newer than current according to SemVer rules.
// Development builds ("dev") and unparseable versions return false.
// Pre-releases on remote are ignored unless current is already running a pre-release.
func IsNewerVersion(current, remote string) bool {
	if current == "dev" || remote == "dev" {
		return false
	}

	sCurrent, err := ParseSemver(current)
	if err != nil {
		return false
	}
	sRemote, err := ParseSemver(remote)
	if err != nil {
		return false
	}

	// Ignore pre-release unless currently running a pre-release
	if sRemote.Prerelease != "" && sCurrent.Prerelease == "" {
		return false
	}

	cmp, err := Compare(current, remote)
	if err != nil {
		return false
	}
	return cmp < 0
}

// Newer compares latest and current versions numerically.
// Provided for backward compatibility; calls IsNewerVersion(current, latest).
func Newer(latest, current string) bool {
	return IsNewerVersion(current, latest)
}

// FetchLatestRelease queries the GitHub Release API endpoint for the latest release.
func FetchLatestRelease(ctx context.Context, httpClient *http.Client, apiURL string) (Release, error) {
	var r Release
	if httpClient == nil {
		testTransportMu.RLock()
		tr := testTransport
		testTransportMu.RUnlock()
		httpClient = &http.Client{Timeout: BackgroundTimeout, Transport: tr}
	}
	if apiURL == "" {
		apiURL = DefaultAPI()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()

	// Rate limiting detection: HTTP 403 or X-RateLimit-Remaining: 0
	if resp.StatusCode == http.StatusForbidden || resp.Header.Get("X-RateLimit-Remaining") == "0" {
		// Advance cooldown forward by 1 hour
		_ = SaveState(&State{
			LastCheckedAt: time.Now().Unix() + RateLimitCooldownSeconds,
		})
		return r, fmt.Errorf("GitHub API rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return r, err
	}

	if err := json.Unmarshal(body, &r); err != nil {
		return r, err
	}

	semver, err := ParseSemver(r.Tag)
	if err != nil {
		return r, fmt.Errorf("invalid release tag: %w", err)
	}
	if semver.Prerelease != "" || r.Draft || r.Prerelease {
		return r, fmt.Errorf("release is not stable")
	}

	if r.HTMLURL == "" {
		r.HTMLURL = "https://github.com/" + Repository + "/releases/tag/" + r.Tag
	}

	return r, nil
}

// PeriodicCheckHook runs on CLI startup for standard commands.
// It checks update_state.json, fetches the latest release if due with a short timeout,
// and prompts the user to update if a newer version is available.
func PeriodicCheckHook(ctx context.Context, currentVersion string, in io.Reader, out, errOut io.Writer) error {
	if currentVersion == "dev" {
		return nil
	}

	if !ShouldCheckForUpdate() {
		return nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, BackgroundTimeout)
	defer cancel()

	client := New()
	r, err := FetchLatestRelease(checkCtx, client.HTTP, client.API)
	if err != nil {
		// If network fails, timeouts, or hits rate limits, fail silently and update
		// last_checked_at so it does not retry on every immediate subsequent command.
		st, _ := LoadState()
		lastChecked := time.Now().Unix()
		if st != nil && st.LastCheckedAt > lastChecked {
			lastChecked = st.LastCheckedAt // preserve rate limit cooldown if set
		}
		_ = SaveState(&State{
			LastCheckedAt: lastChecked,
		})
		return nil
	}

	// Update cached state
	assetName, _ := AssetName(runtime.GOOS, runtime.GOARCH)
	downloadURL, _ := r.asset(assetName)
	_ = SaveState(&State{
		LastCheckedAt: time.Now().Unix(),
		LatestVersion: r.Tag,
		ReleaseURL:    r.HTMLURL,
		DownloadURL:   downloadURL,
	})

	if !IsNewerVersion(currentVersion, r.Tag) {
		return nil
	}

	// Prompt the user to update
	accepted, pErr := PromptUserToUpdate(in, out, currentVersion, r.Tag, r.HTMLURL)
	if pErr != nil || !accepted {
		return nil
	}

	fmt.Fprintf(out, "Updating bonsai to %s...\n", r.Tag)
	installCtx, installCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer installCancel()

	if err := client.Install(installCtx, r); err != nil {
		if IsPermissionError(err) {
			fmt.Fprintln(errOut, "Permission denied. Please run 'sudo bonsai --update' or update via your package manager.")
		} else {
			fmt.Fprintf(errOut, "Update failed: %v\n", err)
		}
		return nil
	}

	fmt.Fprintf(out, "Successfully updated bonsai to version %s!\n", r.Tag)
	_ = SaveState(&State{
		LastCheckedAt: time.Now().Unix(),
		LatestVersion: r.Tag,
		ReleaseURL:    r.HTMLURL,
		DownloadURL:   downloadURL,
	})

	return nil
}
