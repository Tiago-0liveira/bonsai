package runtime

import "testing"

func TestProductionBrowserOrigin(t *testing.T) {
	if ProductionBrowserOrigin != "https://app.bonsai.dev" {
		t.Fatalf("production origin = %q", ProductionBrowserOrigin)
	}
}
