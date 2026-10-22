package ui

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

func TestWithGardenStoresBackendWithoutCallingIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fb := gardenfake.New(gardenapi.StateNone)
	m := New(t.TempDir(), &config.Config{}, &config.State{}, WithGarden(fb))
	if m.garden.api == nil {
		t.Fatal("backend not stored")
	}
	if len(fb.Calls()) != 0 {
		t.Fatalf("constructing the model called the backend: %v", fb.Calls())
	}
	if New(t.TempDir(), &config.Config{}, &config.State{}).garden.api != nil {
		t.Fatal("a model without WithGarden must have no backend")
	}
}
