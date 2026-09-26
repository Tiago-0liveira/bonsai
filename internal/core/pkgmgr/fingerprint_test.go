package pkgmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintChangesOnlyForRelevantInputs(t *testing.T) {
	t.Run("node manifest", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
		before := discoverFingerprint(t, dir)
		write(t, dir, "src/app.js", "console.log('unrelated')\n")
		if got := discoverFingerprint(t, dir); got != before {
			t.Fatalf("unrelated source edit changed fingerprint: %s -> %s", before, got)
		}
		write(t, dir, "package.json", `{"scripts":{"dev":"vite","test":"vitest"}}`)
		if got := discoverFingerprint(t, dir); got == before {
			t.Fatal("package.json change did not change fingerprint")
		}
	})

	t.Run("cargo manifest", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "Cargo.toml", "[package]\nname=\"x\"\nversion=\"0.1.0\"\n")
		before := discoverFingerprint(t, dir)
		write(t, dir, "Cargo.toml", "[package]\nname=\"x\"\nversion=\"0.2.0\"\n")
		if got := discoverFingerprint(t, dir); got == before {
			t.Fatal("Cargo.toml change did not change fingerprint")
		}
	})

	t.Run("makefile", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "Makefile", "build:\n\t@true\n")
		before := discoverFingerprint(t, dir)
		write(t, dir, "Makefile", "build:\n\t@true\ntest:\n\t@true\n")
		if got := discoverFingerprint(t, dir); got == before {
			t.Fatal("Makefile change did not change fingerprint")
		}
	})

	t.Run("pkgmgr override", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
		write(t, dir, ".bonsai.yaml", "pkgmgr:\n  commands: []\n")
		before := discoverFingerprint(t, dir)
		write(t, dir, ".bonsai.yaml", `pkgmgr:
  commands:
    - id: node:script:dev
      description: changed
`)
		if got := discoverFingerprint(t, dir); got == before {
			t.Fatal("pkgmgr override change did not change fingerprint")
		}
	})
}

func TestFingerprintIgnoresGitAndUnrelatedState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	write(t, dir, ".git/refs/heads/main", "aaaaaaaa\n")
	write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
	before := discoverFingerprint(t, dir)

	write(t, dir, ".git/HEAD", "ref: refs/heads/feature\n")
	write(t, dir, ".git/refs/heads/feature", "bbbbbbbb\n")
	write(t, dir, "README.md", "new commit would touch this only\n")
	after := discoverFingerprint(t, dir)
	if after != before {
		t.Fatalf("Git/branch/unrelated changes affected fingerprint: %s -> %s", before, after)
	}
}

func TestFingerprintInputEnumerationOrderDoesNotMatter(t *testing.T) {
	idsA := []string{"make", "node:npm", "cargo"}
	idsB := []string{"cargo", "make", "node:npm"}
	inputsA := []FingerprintInput{
		{Name: "z", Content: []byte("3")},
		{Name: "a", Content: []byte("1")},
		{Name: "m", Content: []byte("2")},
	}
	inputsB := []FingerprintInput{inputsA[1], inputsA[2], inputsA[0]}
	a := computeFingerprint(idsA, inputsA)
	b := computeFingerprint(idsB, inputsB)
	if a != b {
		t.Fatalf("enumeration order changed fingerprint: %s != %s", a, b)
	}
}

func TestFingerprintEquivalentContentAcrossWorktrees(t *testing.T) {
	makeProject := func() string {
		dir := t.TempDir()
		write(t, dir, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"dev":"vite"}}`)
		write(t, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
		return dir
	}
	a, b := makeProject(), makeProject()
	fa, fb := discoverFingerprint(t, a), discoverFingerprint(t, b)
	if fa != fb {
		t.Fatalf("equivalent worktrees produced different fingerprints: %s != %s", fa, fb)
	}
}

func discoverFingerprint(t *testing.T, dir string) string {
	t.Helper()
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
	return project.Fingerprint
}
