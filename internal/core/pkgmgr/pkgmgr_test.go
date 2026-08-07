package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectAndGetScripts(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm == nil {
		t.Fatal("expected a PackageManager")
	}

	got := pm.GetScripts()
	want := []string{"build", "dev", "test"} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetScripts = %v, want %v", got, want)
	}
}

func TestDetectMissing(t *testing.T) {
	pm, err := Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm != nil {
		t.Fatalf("expected nil PackageManager, got %v", pm)
	}
}
