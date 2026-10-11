package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/portowner"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

// portUse classifies who holds the loopback port bonsai web wants.
type portUse struct {
	free   bool
	legacy *legacyServeGroup // a per-repo `bonsai serve` / dev stack group
	bonsai bool              // something answering as a bonsai local API
	owner  portowner.Owner
	known  bool // owner was identified
}

// legacyServeGroup is a repository-scoped serve group found on disk in a
// per-repo daemon's runtime directory.
type legacyServeGroup struct {
	root        string
	workspaceID string
	workspace   string
	mode        procstore.ServeMode
	apiPort     int
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// inspectPort reports whether port is free and, if not, whether a per-repo
// serve group owns it. The slower probes that only matter for the error
// message (who else holds it) run later, in describePortOwner.
func inspectPort(port int, webHome string) portUse {
	if portFree(port) {
		return portUse{free: true}
	}
	return portUse{legacy: findLegacyServeGroup(port, webHome)}
}

// describePortOwner fills in who holds the port, for the failure message.
func describePortOwner(port int, use portUse) portUse {
	use.bonsai = probeBonsaiAPI(port)
	use.owner, use.known = portowner.Find(port)
	return use
}

// preflightPort makes sure the API port is usable before asking the daemon to
// start anything. A per-repo production `bonsai serve` group on the port is
// the previous generation of this very stack, so it is stopped and replaced;
// anything else is reported with one concrete fix.
func (w *webCLI) preflightPort(port int) (webFailure, bool) {
	use := inspectPort(port, w.home)
	if use.free {
		return webFailure{}, true
	}
	if g := use.legacy; g != nil && g.mode != procstore.ServeModeDevelopment {
		if err := client.For(g.root).ServeStop(g.workspaceID); err == nil && waitPortFree(port, 5*time.Second) {
			fmt.Fprintf(w.out, "Stopped the per-repo local API of %s; bonsai web replaces it.\n", g.workspace)
			return webFailure{}, true
		}
	}
	return portFailure(port, describePortOwner(port, use)), false
}

func portFailure(port int, use portUse) webFailure {
	alternative := strconv.Itoa(port + 10)
	fix := "bonsai web --port " + alternative + "      or set api_port in your bonsai web settings (bonsai web setup)"
	switch {
	case use.legacy != nil && use.legacy.mode == procstore.ServeModeDevelopment:
		return webFailure{
			process: "api",
			detail:  fmt.Sprintf("port %d is used by the bonsai development stack of %s", port, use.legacy.workspace),
			fix:     "in " + use.legacy.workspace + " run: bonsai __serve-dev-stack stop      or      bonsai web --port " + alternative,
		}
	case use.legacy != nil:
		return webFailure{
			process: "api",
			detail:  fmt.Sprintf("port %d is used by the per-repo local API of %s and it did not stop", port, use.legacy.workspace),
			fix:     fix,
		}
	case use.bonsai:
		who := "another bonsai local API"
		if use.known {
			who += fmt.Sprintf(" (pid %d)", use.owner.PID)
		}
		return webFailure{process: "api", detail: fmt.Sprintf("port %d is used by %s", port, who), fix: fix}
	}
	who := "another program"
	if use.known {
		if use.owner.Name != "" {
			who += fmt.Sprintf(" (pid %d, %s)", use.owner.PID, use.owner.Name)
		} else {
			who += fmt.Sprintf(" (pid %d)", use.owner.PID)
		}
	}
	return webFailure{process: "api", detail: fmt.Sprintf("port %d is used by %s", port, who), fix: fix}
}

func waitPortFree(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if portFree(port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// probeBonsaiAPI reports whether the port answers bonsai's public /version
// probe (the only unauthenticated, Origin-less route the API serves).
func probeBonsaiAPI(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Version    string `json:"version"`
		APIVersion int    `json:"api_version"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&body) == nil && body.APIVersion > 0
}

// findLegacyServeGroup looks through every live per-repo daemon for a serve
// group whose API is running and really owns port. Persisted specs alone are
// not trusted: a stale record must never get an unrelated group stopped.
func findLegacyServeGroup(port int, webHome string) *legacyServeGroup {
	daemons, err := procstore.ListDaemons()
	if err != nil {
		return nil
	}
	owner, ownerKnown := portowner.Owner{}, false
	for _, d := range daemons {
		if filepath.Clean(d.Root) == filepath.Clean(webHome) {
			continue
		}
		for _, g := range persistedServeGroups(d.Root) {
			if g.apiPort != port {
				continue
			}
			group, err := client.For(d.Root).ServeStatus(g.workspaceID)
			if err != nil || group == nil {
				continue
			}
			var api *procstore.ServeProcess
			for i := range group.Processes {
				if group.Processes[i].Name == "api" {
					api = &group.Processes[i]
				}
			}
			if api == nil || api.State != "ready" || api.PID <= 0 {
				continue
			}
			if !ownerKnown {
				owner, ownerKnown = portowner.Find(port)
			}
			if ownerKnown && owner.PID != api.PID {
				continue
			}
			found := g
			return &found
		}
	}
	return nil
}

// persistedServeGroups lists the serve groups a repository daemon has on disk.
func persistedServeGroups(root string) []legacyServeGroup {
	dir := filepath.Join(procstore.New(root).Dir(), "serve")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []legacyServeGroup
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var rt struct {
			Spec procstore.ServeSpec `json:"spec"`
		}
		if json.Unmarshal(raw, &rt) != nil || rt.Spec.WorkspaceID == "" || rt.Spec.Scope != procstore.ServeScopeRepository {
			continue
		}
		mode := rt.Spec.Mode
		if mode == "" {
			mode = procstore.ServeModeProduction
		}
		out = append(out, legacyServeGroup{
			root: root, workspaceID: rt.Spec.WorkspaceID, workspace: rt.Spec.WorkspacePath,
			mode: mode, apiPort: rt.Spec.APIPort,
		})
	}
	return out
}
