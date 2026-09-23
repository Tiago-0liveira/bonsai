// Package updater downloads and verifies official Bonsai releases.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const Repository = "Tiago-0liveira/bonsai"
const maxDownload = 128 << 20

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Assets     []Asset `json:"assets"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
}
type Client struct {
	HTTP *http.Client
	API  string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Minute}, API: "https://api.github.com/repos/" + Repository + "/releases/latest"}
}

var stable = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func parts(v string) ([]uint64, error) {
	m := stable.FindStringSubmatch(v)
	if m == nil {
		return nil, fmt.Errorf("unsupported version %q", v)
	}
	p := make([]uint64, 3)
	for i := range p {
		n, e := strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return nil, e
		}
		p[i] = n
	}
	return p, nil
}

// Newer compares stable versions numerically. Development builds are not upgraded implicitly.
func Newer(latest, current string) bool {
	a, e := parts(latest)
	if e != nil {
		return false
	}
	b, e := parts(current)
	if e != nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "bonsai-updater")
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
func (c *Client) Latest(ctx context.Context) (Release, error) {
	var r Release
	b, e := c.get(ctx, c.API, 2<<20)
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if _, e = parts(r.Tag); e != nil {
		return r, e
	}
	if r.Draft || r.Prerelease {
		return r, fmt.Errorf("release is not stable")
	}
	return r, nil
}
func AssetName(goos, arch string) (string, error) {
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("unsupported platform %s/%s", goos, arch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "bonsai_" + goos + "_" + arch + ext, nil
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
func verify(data, sums []byte, name string) error {
	var expected string
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if expected != "" {
				return fmt.Errorf("duplicate checksum")
			}
			expected = f[0]
		}
	}
	decoded, e := hex.DecodeString(expected)
	if e != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("missing or invalid checksum for %s", name)
	}
	actual := sha256.Sum256(data)
	if !bytes.Equal(decoded, actual[:]) {
		return fmt.Errorf("SHA256 mismatch for %s", name)
	}
	return nil
}
func extract(data []byte, goos string) ([]byte, error) {
	name := "bonsai"
	if goos == "windows" {
		name += ".exe"
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range z.File {
			if f.Name == name && f.Mode().IsRegular() {
				r, e := f.Open()
				if e != nil {
					return nil, e
				}
				defer r.Close()
				return readBinary(r)
			}
		}
	} else {
		z, e := gzip.NewReader(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		defer z.Close()
		t := tar.NewReader(z)
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.Name == name && h.Typeflag == tar.TypeReg {
				return readBinary(t)
			}
		}
	}
	return nil, fmt.Errorf("archive contains no %s", name)
}
func readBinary(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, maxDownload+1))
	if e != nil {
		return nil, e
	}
	if len(b) == 0 || len(b) > maxDownload {
		return nil, fmt.Errorf("invalid binary size")
	}
	return b, nil
}
func (c *Client) Install(ctx context.Context, r Release) error {
	target, e := os.Executable()
	if e != nil {
		return e
	}
	target, e = filepath.EvalSymlinks(target)
	if e != nil {
		return e
	}
	return c.installAt(ctx, r, target)
}

func (c *Client) installAt(ctx context.Context, r Release, target string) error {
	name, e := AssetName(runtime.GOOS, runtime.GOARCH)
	if e != nil {
		return e
	}
	url, e := r.asset(name)
	if e != nil {
		return e
	}
	sumsURL, e := r.asset("checksums.txt")
	if e != nil {
		return e
	}
	sums, e := c.get(ctx, sumsURL, 2<<20)
	if e != nil {
		return e
	}
	data, e := c.get(ctx, url, maxDownload)
	if e != nil {
		return e
	}
	if e = verify(data, sums, name); e != nil {
		return e
	}
	binary, e := extract(data, runtime.GOOS)
	if e != nil {
		return e
	}
	return replace(target, binary)
}
func replace(target string, binary []byte) error {
	info, e := os.Stat(target)
	if e != nil {
		return e
	}
	// An exclusive lock prevents concurrent invocations from replacing each other.
	lock, e := os.OpenFile(target+".update-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("lock installation (check directory permissions or another updater): %w", e)
	}
	lock.Close()
	defer os.Remove(target + ".update-lock")
	f, e := os.CreateTemp(filepath.Dir(target), ".bonsai-update-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(binary); e != nil {
		f.Close()
		return e
	}
	if e = f.Chmod(info.Mode().Perm()); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return swap(tmp, target)
}

// CachedLatest also caches failures, so offline startup never repeatedly blocks on the network.
func (c *Client) CachedLatest(ctx context.Context) (Release, error) {
	dir, e := os.UserCacheDir()
	if e != nil {
		return c.Latest(ctx)
	}
	path := filepath.Join(dir, "bonsai", "update.json")
	type cache struct {
		Checked time.Time
		Release Release
	}
	var entry cache
	if b, e := os.ReadFile(path); e == nil && json.Unmarshal(b, &entry) == nil {
		age := time.Since(entry.Checked)
		if age >= 0 && age < 24*time.Hour {
			return entry.Release, nil
		}
	}
	r, err := c.Latest(ctx)
	entry = cache{time.Now(), r}
	if err != nil {
		entry.Release = Release{}
	}
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		if f, e := os.CreateTemp(filepath.Dir(path), "update-*"); e == nil {
			name := f.Name()
			_ = json.NewEncoder(f).Encode(entry)
			_ = f.Close()
			_ = os.Rename(name, path)
			_ = os.Remove(name)
		}
	}
	return r, err
}
