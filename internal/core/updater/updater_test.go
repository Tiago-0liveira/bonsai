package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.10.0", "v0.9.9", true}, {"v1.0.0", "v0.99.99", true}, {"v1.2.3", "1.2.3", false}, {"v1.2.2", "v1.2.3", false}, {"v1.2.3-rc.1", "v1.2.2", false}, {"v1.2.3", "dev", false}, {"garbage", "v1.0.0", false},
	} {
		if got := Newer(tt.latest, tt.current); got != tt.want {
			t.Errorf("Newer(%q,%q)=%v", tt.latest, tt.current, got)
		}
	}
}
func archive(t *testing.T, goos, name string) []byte {
	t.Helper()
	var b bytes.Buffer
	if goos == "windows" {
		z := zip.NewWriter(&b)
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = w.Write([]byte("new binary"))
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
	} else {
		g := gzip.NewWriter(&b)
		z := tar.NewWriter(g)
		if e := z.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: 10, Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		_, _ = z.Write([]byte("new binary"))
		_ = z.Close()
		_ = g.Close()
	}
	return b.Bytes()
}
func TestArchiveAndChecksum(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			name := "bonsai"
			if goos == "windows" {
				name += ".exe"
			}
			data := archive(t, goos, name)
			b, e := extract(data, goos)
			if e != nil || string(b) != "new binary" {
				t.Fatalf("extract: %q %v", b, e)
			}
			asset, _ := AssetName(goos, "arm64")
			sums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(data), asset))
			if e = verify(data, sums, asset); e != nil {
				t.Fatal(e)
			}
			data[0] ^= 1
			if verify(data, sums, asset) == nil {
				t.Fatal("accepted corrupted download")
			}
			if verify(data, nil, asset) == nil {
				t.Fatal("accepted missing checksum")
			}
			if _, e = extract(archive(t, goos, "../"+name), goos); e == nil {
				t.Fatal("accepted path traversal member")
			}
		})
	}
	if _, e := AssetName("linux", "386"); e == nil {
		t.Fatal("accepted unsupported architecture")
	}
}
func TestReplaceAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bonsai")
	if e := os.WriteFile(path, []byte("old"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := replace(path, []byte("new")); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new" {
		t.Fatal(string(b))
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0755 {
			t.Fatal("lost executable permissions")
		}
	}
	if e := os.WriteFile(path+".update-lock", nil, 0600); e != nil {
		t.Fatal(e)
	}
	if replace(path, []byte("bad")) == nil {
		t.Fatal("ignored concurrent updater")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "new" {
		t.Fatal("modified locked executable")
	}
}
func TestLatestAndCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"tag_name":"v1.2.3"}`) }))
	defer s.Close()
	c := &Client{HTTP: s.Client(), API: s.URL}
	for i := 0; i < 2; i++ {
		r, e := c.CachedLatest(context.Background())
		if e != nil || r.Tag != "v1.2.3" {
			t.Fatalf("%+v %v", r, e)
		}
	}
	if calls != 1 {
		t.Fatalf("cache missed: %d", calls)
	}
	cacheDir, _ := os.UserCacheDir()
	_ = os.WriteFile(filepath.Join(cacheDir, "bonsai", "update.json"), []byte("broken"), 0600)
	if _, e := c.CachedLatest(context.Background()); e != nil || calls != 2 {
		t.Fatal("corrupt cache did not refresh")
	}
}
func TestNetworkErrors(t *testing.T) {
	for _, body := range []string{`bad json`, `{"tag_name":"v1.0.0-rc.1"}`, `{"tag_name":"v1.0.0","draft":true}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := &Client{HTTP: s.Client(), API: s.URL}
		if _, e := c.Latest(context.Background()); e == nil {
			t.Errorf("accepted %s", body)
		}
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "limited", 429) }))
	defer s.Close()
	c := &Client{HTTP: s.Client(), API: s.URL}
	if _, e := c.Latest(context.Background()); e == nil {
		t.Fatal("ignored HTTP error")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, e := c.Latest(ctx); e == nil {
		t.Fatal("ignored canceled request")
	}
	r := Release{Tag: "v1.0.0", Assets: []Asset{{Name: "checksums.txt", URL: "https://example.com/checksums.txt"}}}
	if _, e := r.asset("checksums.txt"); e == nil {
		t.Fatal("accepted untrusted URL")
	}
}

// Rewrite only the transport destination: asset URL validation still sees official URLs.
type rewriteTransport struct {
	base      string
	transport http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u, err := url.Parse(r.base + req.URL.Path)
	if err != nil {
		return nil, err
	}
	clone.URL = u
	return r.transport.RoundTrip(clone)
}
func TestInstallPipeline(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupt=%v", corrupt), func(t *testing.T) {
			name := "bonsai"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			data := archive(t, runtime.GOOS, name)
			asset, _ := AssetName(runtime.GOOS, runtime.GOARCH)
			sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), asset)
			if corrupt {
				data[0] ^= 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "checksums.txt") {
					fmt.Fprint(w, sums)
				} else {
					_, _ = w.Write(data)
				}
			}))
			defer server.Close()
			c := &Client{HTTP: &http.Client{Transport: rewriteTransport{server.URL, server.Client().Transport}}}
			prefix := "https://github.com/" + Repository + "/releases/download/v1.0.0/"
			release := Release{Tag: "v1.0.0", Assets: []Asset{{Name: asset, URL: prefix + asset}, {Name: "checksums.txt", URL: prefix + "checksums.txt"}}}
			target := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			err := c.installAt(context.Background(), release, target)
			got, _ := os.ReadFile(target)
			if corrupt {
				if err == nil || string(got) != "original" {
					t.Fatalf("corrupt update changed executable: %q, %v", got, err)
				}
			} else {
				if err != nil || string(got) != "new binary" {
					t.Fatalf("install failed: %q, %v", got, err)
				}
			}
		})
	}
}
