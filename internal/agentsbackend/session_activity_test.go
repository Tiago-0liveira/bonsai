package agentsbackend

import (
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// The TUI reads activity through the same session it lists, so the
// conversion from a holder record must carry it.
func TestSessionCarriesActivity(t *testing.T) {
	a := &agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonPermission, Detail: "Bash", Since: time.Unix(1, 0)}
	got := session(agenthost.Record{WorkDir: "/w/", Summary: agentterminal.Summary{ID: "s", State: "running", Activity: a}})
	if got.Activity == nil || *got.Activity != *a || got.WorkDir != "/w" {
		t.Fatalf("session = %+v", got)
	}
	if session(agenthost.Record{}).Activity != nil {
		t.Fatal("a record without activity must stay nil")
	}
}
