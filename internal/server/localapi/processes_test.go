//go:build linux || darwin

package localapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	daemonserver "github.com/Tiago-0liveira/bonsai/internal/daemon/server"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Keep deterministic Git discovery while launch, persistence and streaming use
// the real process daemon. Shared by HTTP tests and the full-stack browser fixture.
type processFixtureDaemon struct {
	*client.Client
	git            *syncTestDaemon
	extraWorktrees []domain.Worktree
}

func (d *processFixtureDaemon) Git(command gitbridge.Command) (*gitbridge.Result, error) {
	return d.GitContext(context.Background(), command)
}
func (d *processFixtureDaemon) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	result, err := d.git.GitContext(ctx, command)
	if err != nil || len(d.extraWorktrees) == 0 {
		return result, err
	}
	var payload any
	switch command.Type {
	case "git.worktrees":
		var trees []domain.Worktree
		if err := json.Unmarshal(result.Payload, &trees); err != nil {
			return nil, err
		}
		payload = append(trees, d.extraWorktrees...)
	case "git.repository.refresh":
		var repository domain.RepositoryState
		if err := json.Unmarshal(result.Payload, &repository); err != nil {
			return nil, err
		}
		repository.Worktrees = append(repository.Worktrees, d.extraWorktrees...)
		for _, tree := range d.extraWorktrees {
			repository.Branches = append(repository.Branches, domain.Branch{Name: tree.Branch, LocalHeadSHA: tree.HeadSHA})
		}
		payload = repository
	case "git.branches":
		var branches []domain.Branch
		if err := json.Unmarshal(result.Payload, &branches); err != nil {
			return nil, err
		}
		for _, tree := range d.extraWorktrees {
			branches = append(branches, domain.Branch{Name: tree.Branch, LocalHeadSHA: tree.HeadSHA})
		}
		payload = branches
	default:
		return result, nil
	}
	raw, err := json.Marshal(payload)
	return &gitbridge.Result{ID: result.ID, Payload: raw}, err
}

// All paths and processes belong to temporary test directories. The extra
// project and branch let the browser exercise restoration across real scopes.
func attachRestorationFixture(t *testing.T, s *Server) {
	t.Helper()
	registry := s.registry.(*syncTestRegistry)
	project := registry.entries["repo"]
	project.info.WorkspaceID = "local"
	project.info.Launch = true
	feature := t.TempDir()
	for _, file := range []string{"package.json", "process-fixture.cjs"} {
		data, err := os.ReadFile(filepath.Join(project.info.Path, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(feature, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project.daemon.(*processFixtureDaemon).extraWorktrees = []domain.Worktree{{
		ID: publicWorktreeID(feature), RepositoryID: localRepositoryID, Path: feature, Branch: "feat/restoration", HeadSHA: "feature-head",
		Status: &domain.WorkingTreeStatus{Branch: "feat/restoration", HeadState: "branch", HeadSHA: "feature-head", Files: []domain.FileStatus{}},
	}}
	registry.entries["repo"] = project
	otherRoot := t.TempDir()
	metadata, err := gitstore.Open(filepath.Join(t.TempDir(), "restoration-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry.entries["repo-other"] = projectServices{
		info:  ProjectInfo{ID: "repo-other", Name: "Restoration fixture", Path: otherRoot, Available: true, WorkspaceID: "local", DefaultBranch: "main", FullName: "fixture/other"},
		state: metadata, daemon: &syncTestDaemon{root: otherRoot}, github: &syncTestGitHub{},
	}
	s.stateSync.ReconcileCatalog()
}
func attachProcessFixture(t *testing.T, s *Server) {
	t.Helper()
	root := s.registry.Default().info.Path
	runtimeDir, err := os.MkdirTemp("", "bonsai-process-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	manifest := `{"packageManager":"pnpm@12.9.1","scripts":{"fail":"node process-fixture.cjs","stay":"node process-fixture.cjs stay","scroll":"node process-fixture.cjs scroll"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	script := `process.stdout.write('\x1b[32mINITIAL_OUTPUT\x1b[0m\n');
process.stdout.write('ARGV:'+JSON.stringify(process.argv.slice(2))+'\n');
if(process.argv[2]==='scroll')for(let i=0;i<100;i++)process.stdout.write('SCROLL_LINE_'+i+'\n');
process.stdout.write(Buffer.from([0xc3]));
setTimeout(()=>{process.stdout.write(Buffer.from([0xa9]));process.stdout.write('FINAL_WITHOUT_NEWLINE');if(!['stay','scroll'].includes(process.argv[2]))process.exit(2);else setInterval(()=>process.stdout.write(process.argv[2]==='scroll'?'\nSCROLL_TICK\n':'.'),1000)},150);`
	if err := os.WriteFile(filepath.Join(root, "process-fixture.cjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	srv, err := daemonserver.NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Run() }()
	c := client.For(root)
	t.Cleanup(func() {
		_ = c.Shutdown(true)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("process fixture did not shut down")
		}
	})
	registry := s.registry.(*syncTestRegistry)
	p := registry.entries["repo"]
	p.daemon = &processFixtureDaemon{Client: c, git: &syncTestDaemon{root: root}}
	registry.entries["repo"] = p
}
func TestProcessAPIValidationPreviewAndFailedSummary(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	attachProcessFixture(t, s)
	root := s.registry.Default().info.Path
	token, _ := s.sessions.create()
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+s.expectedHost+path, strings.NewReader(body))
		r.Host = s.expectedHost
		r.Header.Set("Origin", s.browserOrigin)
		r.Header.Set("X-Bonsai-Session", token.Token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	worktree := publicWorktreeID(root)
	body := `{"worktree_id":"` + worktree + `","command_id":"node:script:fail","values":{"args":["dev","argument with spaces"]}}`
	preview := request("/api/projects/repo/process-preview", body)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), `"args":["run","fail","dev","argument with spaces"]`) {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	invalid := request("/api/projects/repo/processes", strings.TrimSuffix(body, "}")+`,"policy":{"mode":"always","max_restarts":-1}}`)
	if invalid.Code != 400 {
		t.Fatalf("invalid policy: %d %s", invalid.Code, invalid.Body.String())
	}
	response := request("/api/projects/repo/processes", strings.TrimSuffix(body, "}")+`,"policy":{"mode":"no","max_restarts":0}}`)
	if response.Code != 201 {
		t.Fatalf("launch: %d %s", response.Code, response.Body.String())
	}
	var summary browserProcessSummary
	if err := json.Unmarshal(response.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ID != "repo:1" || summary.ProjectID != "repo" || summary.WorktreeID != worktree || summary.Policy.Mode != "no" {
		t.Fatalf("summary: %+v", summary)
	}
	configuration := "pkgmgr:\n  commands:\n    - id: custom:missing\n      name: missing\n      command:\n        program: /no-such-bonsai-executable\n"
	if err := os.WriteFile(filepath.Join(root, ".bonsai.yaml"), []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	missing := request("/api/projects/repo/processes", `{"worktree_id":"`+worktree+`","command_id":"custom:missing","policy":{"mode":"no","max_restarts":0}}`)
	if missing.Code != 201 {
		t.Fatalf("failed launch response: %d %s", missing.Code, missing.Body.String())
	}
	var failure browserProcessSummary
	if err := json.Unmarshal(missing.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.ID != "repo:2" || failure.Status != "failed" || failure.ExitCode == nil || *failure.ExitCode != -1 || failure.ExitError == "" {
		t.Fatalf("failed summary: %+v", failure)
	}

}
