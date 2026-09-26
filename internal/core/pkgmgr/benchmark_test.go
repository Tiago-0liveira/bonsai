package pkgmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func BenchmarkDiscoverNode(b *testing.B) {
	dir := b.TempDir()
	benchmarkWrite(b, dir, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`)
	benchmarkWrite(b, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Discover(dir, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiscoverCargoStatic(b *testing.B) {
	dir := b.TempDir()
	benchmarkWrite(b, dir, "Cargo.toml", "[package]\nname=\"bench\"\nversion=\"0.1.0\"\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Discover(dir, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiscoverMake(b *testing.B) {
	dir := b.TempDir()
	benchmarkWrite(b, dir, "Makefile", "build:\n\t@true\ntest: build\n\t@true\ndeploy:\n\t@true\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Discover(dir, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFingerprint(b *testing.B) {
	inputs := []FingerprintInput{
		{Name: "node/package.json", Content: []byte(`{"scripts":{"dev":"vite"}}`)},
		{Name: "node/pnpm-lock.yaml", Content: []byte("lockfileVersion: '9.0'\n")},
		{Name: "override/.bonsai.yaml#pkgmgr", Content: []byte(`{"commands":[]}`)},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = computeFingerprint([]string{"node:pnpm"}, inputs)
	}
}

func BenchmarkCacheHit(b *testing.B) {
	base := b.TempDir()
	switch runtime.GOOS {
	case "windows":
		b.Setenv("LocalAppData", base)
		b.Setenv("LOCALAPPDATA", base)
	case "darwin":
		b.Setenv("HOME", base)
	default:
		b.Setenv("XDG_CACHE_HOME", base)
	}
	dir := b.TempDir()
	benchmarkWrite(b, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
	if _, err := Discover(dir, Options{UseCache: true}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Discover(dir, Options{UseCache: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkWrite(b *testing.B, dir, name, content string) {
	b.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
}
