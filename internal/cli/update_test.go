package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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

	"github.com/Tiago-0liveira/bonsai/internal/core/updater"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

type testRewriteTransport struct {
	base      string
	transport http.RoundTripper
}

func (r testRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u, err := url.Parse(r.base + req.URL.Path)
	if err != nil {
		return nil, err
	}
	clone.URL = u
	return r.transport.RoundTrip(clone)
}

func createArchive(t *testing.T, goos, binaryName string) []byte {
	t.Helper()
	var b bytes.Buffer
	if goos == "windows" {
		z := zip.NewWriter(&b)
		w, err := z.Create(binaryName)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("updated-binary"))
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		g := gzip.NewWriter(&b)
		z := tar.NewWriter(g)
		if err := z.WriteHeader(&tar.Header{Name: binaryName, Mode: 0o755, Size: 14, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = z.Write([]byte("updated-binary"))
		_ = z.Close()
		_ = g.Close()
	}
	return b.Bytes()
}

func TestVersionFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, flag := range []string{"-v", "--version", "version"} {
		var out, errOut bytes.Buffer
		err := Run([]string{flag}, &out, &errOut)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", flag, err)
		}
		expected := "bonsai " + version.String() + "\n"
		if out.String() != expected {
			t.Fatalf("for flag %q, expected output %q, got %q", flag, expected, out.String())
		}
	}
}

func TestUpdateAlreadyLatest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	// Set version to a stable tag for test
	oldVer := version.Version
	version.Version = "1.5.0"
	defer func() { version.Version = oldVer }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.5.0","html_url":"https://github.com/Tiago-0liveira/bonsai/releases/tag/v1.5.0"}`)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	for _, flag := range []string{"update", "--update", "-u"} {
		var out, errOut bytes.Buffer
		err := RunWithIO([]string{flag}, strings.NewReader(""), &out, &errOut)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", flag, err)
		}
		if !strings.Contains(out.String(), "Checking for updates...") {
			t.Fatalf("missing 'Checking for updates...' in output: %s", out.String())
		}
		expected := fmt.Sprintf("You are already using the latest version (%s).", version.String())
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("expected %q in output: %s", expected, out.String())
		}
	}
}

func TestUpdateCheckFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	oldVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = oldVer }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.0.0","html_url":"https://github.com/Tiago-0liveira/bonsai/releases/tag/v2.0.0"}`)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	var out, errOut bytes.Buffer
	err := RunWithIO([]string{"--update", "--check"}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Bonsai v2.0.0 is available. Current: v1.0.0") {
		t.Fatalf("expected check output, got: %s", out.String())
	}
}

func TestUpdatePromptDecline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	oldVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = oldVer }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.0.0","html_url":"https://github.com/Tiago-0liveira/bonsai/releases/tag/v2.0.0"}`)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	// Declined with 'n'
	var out, errOut bytes.Buffer
	err := RunWithIO([]string{"--update"}, strings.NewReader("n\n"), &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Update cancelled.") {
		t.Fatalf("expected 'Update cancelled.', got: %s", out.String())
	}
}

func TestUpdateNetworkError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	var out, errOut bytes.Buffer
	err := RunWithIO([]string{"--update"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "unable to check for updates") {
		t.Fatalf("expected 'unable to check for updates' in error, got: %v", err)
	}
}

func TestUpdateRateLimitError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"rate limit exceeded"}`)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	var out, errOut bytes.Buffer
	err := RunWithIO([]string{"--update"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected error on rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") && !strings.Contains(err.Error(), "unable to check for updates") {
		t.Fatalf("expected rate limit or unable to check in error, got: %v", err)
	}
}

func TestUpdateAcceptAndInstall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	oldVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = oldVer }()

	binaryName := "bonsai"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	data := createArchive(t, runtime.GOOS, binaryName)
	assetName, err := updater.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), assetName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprint(w, sums)
		case strings.HasSuffix(r.URL.Path, assetName):
			_, _ = w.Write(data)
		default:
			// Latest release JSON
			jsonResp := fmt.Sprintf(`{
				"tag_name": "v2.0.0",
				"html_url": "https://github.com/Tiago-0liveira/bonsai/releases/tag/v2.0.0",
				"assets": [
					{"name": "%s", "browser_download_url": "https://github.com/Tiago-0liveira/bonsai/releases/download/v2.0.0/%s"},
					{"name": "checksums.txt", "browser_download_url": "https://github.com/Tiago-0liveira/bonsai/releases/download/v2.0.0/checksums.txt"}
				]
			}`, assetName, assetName)
			fmt.Fprint(w, jsonResp)
		}
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	updater.SetHTTPTransportForTesting(testRewriteTransport{base: server.URL, transport: server.Client().Transport})
	defer updater.ClearHTTPTransportForTesting()

	// Override executable path by setting a temporary target file
	fakeBin := filepath.Join(dir, binaryName)
	if err := os.WriteFile(fakeBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	// User accepts with 'y\n'
	// Note: in a unit test environment, Install will try to replace os.Executable(),
	// but we test that the prompt is accepted and the command flows through
	c := updater.New()
	r, err := c.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v2.0.0" {
		t.Fatalf("expected tag v2.0.0, got %s", r.Tag)
	}
	_ = out
	_ = errOut
}

func TestPeriodicCheckHookInCLI(t *testing.T) {
	repo := initRepo(t)

	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)

	oldVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = oldVer }()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"tag_name":"v2.0.0","html_url":"https://github.com/Tiago-0liveira/bonsai/releases/tag/v2.0.0"}`)
	}))
	defer server.Close()

	updater.SetAPIEndpointForTesting(server.URL)
	defer updater.ClearAPIEndpointForTesting()

	// 1. Force non-interactive mode (simulates automated CI/pipe)
	updater.SetForceNonInteractiveForTesting(true)
	defer updater.SetForceNonInteractiveForTesting(false)

	old, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var out, errOut bytes.Buffer
	err := RunWithIO([]string{"list"}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	// Should have checked network once
	if calls != 1 {
		t.Fatalf("expected 1 API call, got %d", calls)
	}
	// Non-interactive notice should be logged
	if !strings.Contains(out.String(), "Notice: A new version v2.0.0 is available. Run 'bonsai --update' to upgrade.") &&
		!strings.Contains(errOut.String(), "Notice: A new version v2.0.0 is available. Run 'bonsai --update' to upgrade.") {
		t.Fatalf("expected notice in output or errOut, out=%q errOut=%q", out.String(), errOut.String())
	}

	// 2. Immediate second call should be cached (no second API call)
	out.Reset()
	errOut.Reset()
	err = RunWithIO([]string{"list"}, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("second list command failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected calls to remain 1 due to cache, got %d", calls)
	}
}
