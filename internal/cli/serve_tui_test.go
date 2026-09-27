package cli

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestServeTUIFiltersLogs(t *testing.T) {
	m := serveTUIModel{
		group: &procstore.ServeGroup{Processes: []procstore.ServeProcess{{Name: "api"}, {Name: "webhook"}}},
		selected: 1,
		search:   "denied",
		height:   40,
		lines: []string{
			"[10:00:00] api OUT ready",
			"[10:00:01] webhook ERR denied request",
			"[10:00:02] webhook OUT accepted",
		},
	}
	got := m.visibleLines()
	if len(got) != 1 || got[0] != "[10:00:01] webhook ERR denied request" {
		t.Fatalf("visible = %#v", got)
	}
}
