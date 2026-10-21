package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

func TestAgentHolderGuardSeesUnsettledReservationInWorktree(t *testing.T) {
	paths := agenthost.Paths{Records: t.TempDir(), Sockets: t.TempDir()}
	worktree := t.TempDir()
	// An unheld reserved operation may belong to a holder that is still forking,
	// so it counts as active.
	rec := agenthost.Record{Version: agenthost.RecordVersion, ID: "sess_ab12", WorkDir: filepath.Join(worktree, "sub"), StartHash: "h", StartPhase: "reserved",
		Summary: agentterminal.Summary{ID: "sess_ab12", State: "starting"}}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Records, "sess_ab12.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if busy, err := agentHolderIn(paths, worktree); err != nil || !busy {
		t.Fatalf("holder inside worktree missed: busy=%v err=%v", busy, err)
	}
	if busy, err := agentHolderIn(paths, t.TempDir()); err != nil || busy {
		t.Fatalf("unrelated directory blocked: busy=%v err=%v", busy, err)
	}
}
