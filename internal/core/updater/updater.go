// Package updater downloads and verifies official Bonsai releases.
package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	testTransportMu sync.RWMutex
	testTransport   http.RoundTripper
)

// SetHTTPTransportForTesting overrides the HTTP transport used by updater clients during tests.
func SetHTTPTransportForTesting(tr http.RoundTripper) {
	testTransportMu.Lock()
	testTransport = tr
	testTransportMu.Unlock()
}

// ClearHTTPTransportForTesting clears the HTTP transport test override.
func ClearHTTPTransportForTesting() {
	testTransportMu.Lock()
	testTransport = nil
	testTransportMu.Unlock()
}

// Asset represents a release download asset.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release represents release metadata from GitHub.
type Release struct {
	Tag        string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
}

// Asset returns the browser download URL for the asset with the specified name.
func (r Release) Asset(name string) (string, error) {
	return r.asset(name)
}

// AssetURL returns the browser download URL for the asset with the specified name.
func (r Release) AssetURL(name string) (string, error) {
	return r.asset(name)
}

func (r Release) asset(name string) (string, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			// Only accept assets hosted by the official repository, even from a cached response.
			prefix := "https://github.com/" + Repository + "/releases/download/" + r.Tag + "/"
			if !strings.HasPrefix(a.URL, prefix) {
				return "", fmt.Errorf("untrusted asset URL")
			}
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("release has no %s", name)
}

// Client handles interaction with the release API and binary distribution.
type Client struct {
	HTTP *http.Client
	API  string
}

// New creates a new Client configured for the default repository releases.
func New() *Client {
	testTransportMu.RLock()
	tr := testTransport
	testTransportMu.RUnlock()

	httpClient := &http.Client{Timeout: 2 * time.Minute}
	if tr != nil {
		httpClient.Transport = tr
	}

	return &Client{
		HTTP: httpClient,
		API:  DefaultAPI(),
	}
}

func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	return b, nil
}

// Latest retrieves the latest stable release from GitHub.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	return FetchLatestRelease(ctx, c.HTTP, c.API)
}

// CachedLatest retrieves the latest release, using cached state when recent.
// Failures are also cached to ensure offline startup does not block repeatedly.
func (c *Client) CachedLatest(ctx context.Context) (Release, error) {
	st, err := LoadState()
	if err == nil && st != nil {
		diff := time.Now().Unix() - st.LastCheckedAt
		if diff >= 0 && diff < CheckIntervalSeconds && st.LatestVersion != "" {
			return Release{
				Tag:     st.LatestVersion,
				HTMLURL: st.ReleaseURL,
			}, nil
		}
	}

	r, err := c.Latest(ctx)
	if err != nil {
		lastChecked := time.Now().Unix()
		if st != nil && st.LastCheckedAt > lastChecked {
			lastChecked = st.LastCheckedAt
		}
		_ = SaveState(&State{
			LastCheckedAt: lastChecked,
		})
		return r, err
	}

	assetName, _ := AssetName(runtime.GOOS, runtime.GOARCH)
	downloadURL, _ := r.asset(assetName)
	_ = SaveState(&State{
		LastCheckedAt: time.Now().Unix(),
		LatestVersion: r.Tag,
		ReleaseURL:    r.HTMLURL,
		DownloadURL:   downloadURL,
	})

	return r, nil
}
