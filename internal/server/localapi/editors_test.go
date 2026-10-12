package localapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/editors"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

type editorTestRegistry struct{ *syncTestRegistry }

func (r *editorTestRegistry) Worktree(_ context.Context, id string) (projectServices, bool) {
	project := r.Default()
	for _, tree := range project.daemon.(*syncTestDaemon).worktrees(nil) {
		if tree.ID == id {
			return project, true
		}
	}
	return projectServices{}, false
}

func TestEnvironmentEditorAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	s := newUserScopedTestServer(t)
	dir := t.TempDir()
	command := filepath.Join(dir, "zed")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"$BONSAI_EDITOR_TEST_OUTPUT\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	s.editors = editors.Discovery{Environment: editors.Environment{ID: "wsl:test:Ubuntu", Label: "WSL (Ubuntu)"}, OS: runtime.GOOS, WSL: true, Path: dir, WindowsMounts: []string{t.TempDir()}}
	settingsPath := s.rootsPath + ".editors.json"
	if _, err := config.UpdateEditorSettings(settingsPath, "windows:test", "zed", 0); err != nil {
		t.Fatal(err)
	}
	read := authorizedRequest(t, s, "GET", "/api/settings/editors", "", nil)
	var settings editorSettingsResponse
	if err := json.Unmarshal(read.Body.Bytes(), &settings); err != nil || read.Code != 200 || settings.Preferred != "" || len(settings.Editors) != 1 {
		t.Fatalf("Windows preference leaked: %d %s (%v)", read.Code, read.Body, err)
	}
	save := func(environment, editor string, revision uint64) *httptest.ResponseRecorder {
		return authorizedRequest(t, s, "POST", "/api/settings/editors", "editor-save", map[string]any{"environment_id": environment, "editor": editor, "revision": revision})
	}
	if got := save("windows:test", "zed", 1); got.Code != 409 {
		t.Fatalf("foreign environment accepted: %d %s", got.Code, got.Body)
	}
	if got := save(s.editors.Environment.ID, "/bin/sh", 1); got.Code != 400 {
		t.Fatalf("arbitrary executable accepted: %d %s", got.Code, got.Body)
	}
	if got := save(s.editors.Environment.ID, "zed", 1); got.Code != 200 {
		t.Fatalf("save: %d %s", got.Code, got.Body)
	}
	if got := save(s.editors.Environment.ID, "zed", 1); got.Code != 409 {
		t.Fatalf("stale settings accepted: %d %s", got.Code, got.Body)
	}
	root := t.TempDir()
	linked := filepath.Join(root, "wt-00")
	if err := os.Mkdir(linked, 0755); err != nil {
		t.Fatal(err)
	}
	filename := "file with spaces $(touch sentinel).ts"
	file := filepath.Join(linked, filename)
	if err := os.WriteFile(file, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	// Editors see resolved paths; macOS temp folders are behind a /var symlink.
	realLinked, evalErr := filepath.EvalSymlinks(linked)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	realFile := filepath.Join(realLinked, filename)
	project := projectServices{info: ProjectInfo{ID: "project", Available: true, Path: root}, daemon: &syncTestDaemon{root: root, extraWorktrees: 1}}
	s.registry = &editorTestRegistry{&syncTestRegistry{entries: map[string]projectServices{"project": project}}}
	id := gitlocal.ID(localRepositoryID, linked)
	output := filepath.Join(t.TempDir(), "launch")
	t.Setenv("BONSAI_EDITOR_TEST_OUTPUT", output)
	open := func(path string) *httptest.ResponseRecorder {
		return authorizedRequest(t, s, "POST", "/api/worktrees/"+id+"/open-editor", "editor-open", map[string]string{"environment_id": s.editors.Environment.ID, "path": path})
	}
	if got := open(filename); got.Code != 202 {
		t.Fatalf("open: %d %s", got.Code, got.Body)
	}
	args, err := os.ReadFile(output)
	if err != nil || string(args) != realLinked+"\n"+realFile+"\n" {
		t.Fatalf("wrong worktree/arguments: %q (%v)", args, err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	// Opening the worktree folder passes only the directory (no --goto/file).
	openFolder := func(body map[string]any) *httptest.ResponseRecorder {
		body["environment_id"] = s.editors.Environment.ID
		return authorizedRequest(t, s, "POST", "/api/worktrees/"+id+"/open-editor", "editor-open", body)
	}
	if got := openFolder(map[string]any{"folder": true}); got.Code != 202 {
		t.Fatalf("open folder: %d %s", got.Code, got.Body)
	}
	args, err = os.ReadFile(output)
	if err != nil || string(args) != realLinked+"\n"+realLinked+"\n" {
		t.Fatalf("folder arguments: %q (%v)", args, err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	kate := filepath.Join(dir, "kate")
	if err := os.WriteFile(kate, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := openFolder(map[string]any{"folder": true, "editor": "kate"}); got.Code != 409 || !strings.Contains(got.Body.String(), "editor_folder_unsupported") {
		t.Fatalf("folder-less editor opened a folder: %d %s", got.Code, got.Body)
	}
	if got := openFolder(map[string]any{"folder": true, "editor": "missing"}); got.Code != 409 {
		t.Fatalf("unknown editor override accepted: %d %s", got.Code, got.Body)
	}
	if got := openFolder(map[string]any{"folder": true, "path": filename}); got.Code != 400 {
		t.Fatalf("folder with path accepted: %d %s", got.Code, got.Body)
	}
	if err := os.Remove(kate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("rejected folder request launched an editor")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(linked, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside", file, ".", "escape", "missing.ts"} {
		if got := open(path); got.Code != 400 && got.Code != 404 {
			t.Fatalf("unsafe/non-file %q accepted: %d %s", path, got.Code, got.Body)
		}
	}
	if got := authorizedRequest(t, s, "POST", "/api/worktrees/unknown/open-editor", "editor-open", map[string]string{"environment_id": s.editors.Environment.ID, "path": filename}); got.Code != 404 {
		t.Fatalf("unknown worktree: %d %s", got.Code, got.Body)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("invalid requests launched an editor")
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'no display connection' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := open(filename); got.Code != 502 || !strings.Contains(got.Body.String(), "no display connection") {
		t.Fatalf("failure not surfaced: %d %s", got.Code, got.Body)
	}
	if err := os.Remove(command); err != nil {
		t.Fatal(err)
	}
	if got := open(filename); got.Code != 409 {
		t.Fatalf("uninstalled editor accepted: %d %s", got.Code, got.Body)
	}
	read = authorizedRequest(t, s, "GET", "/api/settings/editors", "", nil)
	if err := json.Unmarshal(read.Body.Bytes(), &settings); err != nil || settings.Preferred != "" || settings.SavedPreferred != "zed" || len(settings.Editors) != 0 {
		t.Fatalf("uninstalled editor listed: %s (%v)", read.Body, err)
	}
}

func TestOpenFileManagerAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	s := newUserScopedTestServer(t)
	bin := t.TempDir()
	output := filepath.Join(t.TempDir(), "reveal")
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+output+"\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	s.editors = editors.Discovery{Environment: editors.Environment{ID: "linux:test", Label: "Linux"}, OS: "linux", Path: bin}
	root := t.TempDir()
	linked := filepath.Join(root, "wt-00")
	if err := os.Mkdir(linked, 0755); err != nil {
		t.Fatal(err)
	}
	realLinked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	project := projectServices{info: ProjectInfo{ID: "project", Available: true, Path: root}, daemon: &syncTestDaemon{root: root, extraWorktrees: 1}}
	s.registry = &editorTestRegistry{&syncTestRegistry{entries: map[string]projectServices{"project": project}}}
	id := gitlocal.ID(localRepositoryID, linked)
	reveal := func(id, environment string) *httptest.ResponseRecorder {
		return authorizedRequest(t, s, "POST", "/api/worktrees/"+id+"/open-file-manager", "reveal", map[string]string{"environment_id": environment})
	}

	read := authorizedRequest(t, s, "GET", "/api/settings/editors", "", nil)
	var settings editorSettingsResponse
	if err := json.Unmarshal(read.Body.Bytes(), &settings); err != nil || settings.FileManager.Label != "file manager" {
		t.Fatalf("file manager label: %s (%v)", read.Body, err)
	}
	if got := reveal(id, "windows:other"); got.Code != 409 {
		t.Fatalf("foreign environment accepted: %d %s", got.Code, got.Body)
	}
	if got := reveal("unknown", s.editors.Environment.ID); got.Code != 404 {
		t.Fatalf("unknown worktree: %d %s", got.Code, got.Body)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("rejected request opened a file manager")
	}
	if got := reveal(id, s.editors.Environment.ID); got.Code != 202 || !strings.Contains(got.Body.String(), `"file_manager":"file manager"`) {
		t.Fatalf("reveal: %d %s", got.Code, got.Body)
	}
	if args, err := os.ReadFile(output); err != nil || string(args) != realLinked+"\n" {
		t.Fatalf("revealed folder: %q (%v)", args, err)
	}
	if err := os.Remove(filepath.Join(bin, "xdg-open")); err != nil {
		t.Fatal(err)
	}
	if got := reveal(id, s.editors.Environment.ID); got.Code != 502 || !strings.Contains(got.Body.String(), "file_manager_failed") {
		t.Fatalf("missing file manager: %d %s", got.Code, got.Body)
	}
}

func TestEditorRoutesRequireSession(t *testing.T) {
	s := newTestServer(t)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/settings/editors"}, {"POST", "/api/settings/editors"}, {"POST", "/api/worktrees/unknown/open-editor"},
		{"POST", "/api/worktrees/unknown/open-file-manager"},
	} {
		req := httptest.NewRequest(route.method, "http://127.0.0.1:7001"+route.path, strings.NewReader(`{}`))
		req.Header.Set("Origin", testBrowserOrigin)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", route.path, rec.Code)
		}
	}
}
