//go:build embedui

package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedBundleIsTheProductionBuild(t *testing.T) {
	ui := Dist()
	if ui == nil {
		t.Fatal("Dist() is nil in an embedui build")
	}
	index, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		t.Fatalf("index.html missing from the embedded bundle: %v", err)
	}
	if !strings.Contains(string(index), `name="bonsai-relay-origin"`) {
		t.Fatal("embedded index.html lacks the runtime-config meta tag")
	}
	// The e2e build exposes the store on window; it must never be embedded.
	err = fs.WalkDir(ui, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(ui, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "__bonsaiTestStore") {
			t.Errorf("%s contains the e2e test hook", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
