package pkgmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

type countingProvider struct {
	commandCalls atomic.Int64
}

func (*countingProvider) ID() string { return "count" }

func (*countingProvider) Detect(ctx Context) (Detection, error) {
	return Detection{Applicable: true, ID: "count", Name: "count", Root: ctx.Location.InputDir}, nil
}

func (p *countingProvider) Commands(_ Context, detection Detection) ([]Command, error) {
	p.commandCalls.Add(1)
	return []Command{{
		ID:       "count:one",
		Name:     "one",
		Provider: "count",
		Kind:     CommandBuiltin,
		Invocation: InvocationSpec{
			Program:    "count",
			Prefix:     []string{"one"},
			WorkingDir: detection.Root,
		},
		Source:     Source{Kind: "test", File: "catalog"},
		Confidence: ConfidenceExact,
	}}, nil
}

func (*countingProvider) FingerprintInputs(_ Context, _ Detection) ([]FingerprintInput, error) {
	return []FingerprintInput{{Name: "count/input", Content: []byte("stable")}}, nil
}

func TestCacheMissHitAndRefresh(t *testing.T) {
	isolateUserCache(t)
	provider := &countingProvider{}
	withTestProviders(t, provider)
	dir := t.TempDir()

	first, err := Discover(dir, Options{UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.commandCalls.Load(); got != 1 {
		t.Fatalf("command discovery calls after miss = %d, want 1", got)
	}
	path, err := cachePath(first.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache entry not written: %v", err)
	}

	second, err := Discover(dir, Options{UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Fingerprint != first.Fingerprint {
		t.Fatal("cache hit changed fingerprint")
	}
	if got := provider.commandCalls.Load(); got != 1 {
		t.Fatalf("commands reran on cache hit: calls=%d", got)
	}

	if _, err := Discover(dir, Options{UseCache: true, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	if got := provider.commandCalls.Load(); got != 2 {
		t.Fatalf("Refresh did not bypass cache: calls=%d", got)
	}
}

func TestCacheCorruptionRecoversAsMiss(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, path string, project Project)
	}{
		{
			name: "malformed json",
			mutate: func(t *testing.T, path string, _ Project) {
				t.Helper()
				if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong schema",
			mutate: func(t *testing.T, path string, project Project) {
				t.Helper()
				writeCacheEntry(t, path, CacheEntry{
					SchemaVersion: discoverySchemaVersion + 1,
					Fingerprint:   project.Fingerprint,
					Project:       project,
				})
			},
		},
		{
			name: "fingerprint mismatch",
			mutate: func(t *testing.T, path string, project Project) {
				t.Helper()
				writeCacheEntry(t, path, CacheEntry{
					SchemaVersion: discoverySchemaVersion,
					Fingerprint:   "wrong",
					Project:       project,
				})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserCache(t)
			provider := &countingProvider{}
			withTestProviders(t, provider)
			dir := t.TempDir()
			project, err := Discover(dir, Options{UseCache: true})
			if err != nil {
				t.Fatal(err)
			}
			path, err := cachePath(project.Fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, path, *project)

			recovered, err := Discover(dir, Options{UseCache: true})
			if err != nil {
				t.Fatalf("corrupt cache should recover: %v", err)
			}
			if recovered.Fingerprint != project.Fingerprint {
				t.Fatalf("recovered fingerprint = %q", recovered.Fingerprint)
			}
			if got := provider.commandCalls.Load(); got != 2 {
				t.Fatalf("corrupt cache was not treated as miss: calls=%d", got)
			}
			if _, ok := loadCache(project.Fingerprint); !ok {
				t.Fatal("recomputed cache entry is not valid")
			}
		})
	}
}

func TestCacheIgnoresPartialTempFile(t *testing.T) {
	isolateUserCache(t)
	provider := &countingProvider{}
	withTestProviders(t, provider)
	dir := t.TempDir()
	project, err := Discover(dir, Options{UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(project.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".pkgmgr-partial.tmp"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(dir, Options{UseCache: true}); err != nil {
		t.Fatal(err)
	}
	if got := provider.commandCalls.Load(); got != 1 {
		t.Fatalf("partial temp file prevented cache hit: calls=%d", got)
	}
}

func TestCacheWriteFailureDoesNotFailDiscovery(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	setCacheBase(t, blocker)
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)

	project, err := Discover(dir, Options{UseCache: true})
	if err != nil {
		t.Fatalf("cache persistence failure should not fail discovery: %v", err)
	}
	commandByID(t, project, "node:script:dev")
}

func TestCacheConcurrentSameFingerprintDiscovery(t *testing.T) {
	isolateUserCache(t)
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","test":"vitest"}}`)
	baseline, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			project, err := Discover(dir, Options{UseCache: true})
			if err == nil && project.Fingerprint != baseline.Fingerprint {
				err = &fingerprintError{got: project.Fingerprint, want: baseline.Fingerprint}
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if cached, ok := loadCache(baseline.Fingerprint); !ok || cached.Fingerprint != baseline.Fingerprint {
		t.Fatalf("final concurrent cache entry invalid: ok=%v project=%+v", ok, cached)
	}
}

func TestCacheSerializationRoundTrip(t *testing.T) {
	def := "safe"
	project := Project{
		Location: Location{InputDir: "/input", ProjectRoot: "/project", WorkspaceRoot: "/workspace", RepositoryRoot: "/repo"},
		Providers: []ProviderInfo{
			{ID: "node:pnpm", Name: "pnpm", Root: "/project"},
			{ID: "cargo", Name: "cargo", Root: "/project"},
		},
		Commands: []Command{{
			ID:          "node:script:test",
			Name:        "test",
			Description: "test",
			Provider:    "node:pnpm",
			ProjectRoot: "/project",
			Kind:        CommandProject,
			Args: []Argument{{
				ID: "mode", Kind: ArgumentFlag, Type: ValueEnum, Flags: []string{"--mode"},
				Default: &def, Choices: []string{"safe", "fast"},
				Source: Source{Kind: "override", File: ".bonsai.yaml"}, Confidence: ConfidenceExact,
			}},
			Invocation: InvocationSpec{Program: "pnpm", Prefix: []string{"run", "test"}, WorkingDir: "/project", PassThrough: PassThroughDoubleDash},
			Source:     Source{Kind: "package.json", File: "/project/package.json", Pointer: "scripts.test"},
			Confidence: ConfidenceExact,
			Raw:        "vitest",
		}},
		Fingerprint: "abc",
	}
	entry := CacheEntry{SchemaVersion: discoverySchemaVersion, Fingerprint: project.Fingerprint, Project: project}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CacheEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Project, project) {
		t.Fatalf("round-trip project mismatch:\ngot=%+v\nwant=%+v", decoded.Project, project)
	}
}

type fingerprintError struct {
	got  string
	want string
}

func (e *fingerprintError) Error() string {
	return "fingerprint mismatch: got " + e.got + " want " + e.want
}

func writeCacheEntry(t *testing.T, path string, entry CacheEntry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func withTestProviders(t *testing.T, providers ...Provider) {
	t.Helper()
	old := defaultProviders
	defaultProviders = providers
	t.Cleanup(func() { defaultProviders = old })
}

func isolateUserCache(t *testing.T) {
	t.Helper()
	setCacheBase(t, t.TempDir())
}

func setCacheBase(t *testing.T, base string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LocalAppData", base)
		t.Setenv("LOCALAPPDATA", base)
	case "darwin":
		t.Setenv("HOME", base)
	default:
		t.Setenv("XDG_CACHE_HOME", base)
	}
}
