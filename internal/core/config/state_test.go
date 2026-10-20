package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPrefsAgentGlyphsJSON(t *testing.T) {
	b, err := json.Marshal(Prefs{AgentGlyphs: "ascii"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"agent_glyphs":"ascii"`) {
		t.Fatalf("marshal = %s, want agent_glyphs key", b)
	}
	var p Prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.AgentGlyphs != "ascii" {
		t.Errorf("round trip AgentGlyphs = %q", p.AgentGlyphs)
	}
	b, _ = json.Marshal(Prefs{})
	if strings.Contains(string(b), "agent_glyphs") {
		t.Errorf("empty AgentGlyphs should be omitted: %s", b)
	}
}

func stateEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	return filepath.Join(dir, "bonsai", "state.json")
}

func writeState(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStateOldFileLoadsWithDefaults(t *testing.T) {
	p := stateEnv(t)
	writeState(t, p, `{"copy_counts":{"a":2},"aliases":[{"Name":"x","Command":"c"}],"prefs":{"theme":"dark"}}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.CopyRank("a") != 2 || len(st.Aliases) != 1 || st.Prefs.Theme != "dark" {
		t.Fatalf("lost data: %+v", st)
	}
	pr := st.PrefsSnapshot()
	if pr.Contrast != "" || pr.Density != "" || pr.StartWorkspace != "" || pr.NoWorkspaceMemory || st.Workspaces != nil {
		t.Fatalf("new defaults not zero: %+v", pr)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, k := range []string{"contrast", "density", "start_workspace", "no_workspace_memory", "workspaces"} {
		if strings.Contains(string(b), k) {
			t.Errorf("omitempty violated for %s: %s", k, b)
		}
	}
}

func TestStateNewFieldsRoundTrip(t *testing.T) {
	stateEnv(t)
	st, _ := LoadState()
	want := Prefs{Contrast: "flat", Density: "compact", StartWorkspace: "garden", NoWorkspaceMemory: true, Keys: map[string]string{"a": "b"}}
	if err := st.PersistPrefs(1, want); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistLastWorkspace(1, t.TempDir(), "settings"); err != nil {
		t.Fatal(err)
	}
	re, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	got := re.PrefsSnapshot()
	if got.Contrast != "flat" || got.Density != "compact" || got.StartWorkspace != "garden" || !got.NoWorkspaceMemory || got.Keys["a"] != "b" {
		t.Fatalf("prefs = %+v", got)
	}
	if len(re.Workspaces) != 1 {
		t.Fatalf("workspaces = %v", re.Workspaces)
	}
}

func TestPersistPrefsOrdering(t *testing.T) {
	p := stateEnv(t)
	st, _ := LoadState()
	if err := st.PersistPrefs(2, Prefs{Theme: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistPrefs(1, Prefs{Theme: "one"}); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("err = %v, want ErrSuperseded", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"two"`) || strings.Contains(string(b), `"one"`) {
		t.Fatalf("disk = %s", b)
	}
	if st.PrefsSnapshot().Theme != "two" {
		t.Fatal("memory overwritten")
	}
}

func TestPersistPrefsFailureKeepsMemory(t *testing.T) {
	p := stateEnv(t)
	st, _ := LoadState()
	if err := st.PersistPrefs(1, Prefs{Theme: "ok"}); err != nil {
		t.Fatal(err)
	}
	// Replace the config dir with a regular file so the write cannot succeed.
	dir := filepath.Dir(p)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistPrefs(2, Prefs{Theme: "bad"}); err == nil {
		t.Fatal("want error")
	}
	if got := st.PrefsSnapshot().Theme; got != "ok" {
		t.Fatalf("theme = %q, want ok", got)
	}
	os.Remove(dir)
	// gen 2 was not consumed by the failure.
	if err := st.PersistPrefs(2, Prefs{Theme: "retry"}); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestUnknownKeysSurvive(t *testing.T) {
	p := stateEnv(t)
	writeState(t, p, `{"copy_counts":{"a":1},"aliases":[{"Name":"x","Command":"c"}],"future":{"n":1},"prefs":{"theme":"t","future_pref":"keep"},"workspaces":{"/r":"garden"}}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		var m map[string]any
		b, _ := os.ReadFile(p)
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["future"]; !ok {
			t.Errorf("top-level extra lost: %s", b)
		}
		if m["prefs"].(map[string]any)["future_pref"] != "keep" {
			t.Errorf("prefs extra lost: %s", b)
		}
		if m["workspaces"].(map[string]any)["/r"] != "garden" || m["copy_counts"].(map[string]any)["a"] != float64(1) || len(m["aliases"].([]any)) != 1 {
			t.Errorf("known data dropped: %s", b)
		}
	}
	if err := st.PersistPrefs(1, Prefs{Theme: "new", Density: "compact"}); err != nil {
		t.Fatal(err)
	}
	check()
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestMalformedStateErrors(t *testing.T) {
	p := stateEnv(t)
	writeState(t, p, `{not json`)
	if _, err := LoadState(); err == nil {
		t.Fatal("want error")
	}
}

func TestPersistLastWorkspace(t *testing.T) {
	stateEnv(t)
	st, _ := LoadState()
	repo := t.TempDir()
	if err := st.PersistLastWorkspace(2, repo, "playground"); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistLastWorkspace(1, repo, "garden"); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("err = %v", err)
	}
	if got := st.LastWorkspace(repo); got != "playground" {
		t.Fatalf("got %q", got)
	}
	if err := st.PersistLastWorkspace(3, repo, ""); err != nil {
		t.Fatal(err)
	}
	if got := st.LastWorkspace(repo); got != "" {
		t.Fatalf("got %q after delete", got)
	}
	re, _ := LoadState()
	if re.LastWorkspace(repo) != "" {
		t.Fatal("delete not persisted")
	}
}

func TestPersistPrefsKeepsOtherData(t *testing.T) {
	stateEnv(t)
	st, _ := LoadState()
	if err := st.AddAlias(Alias{Name: "a", Command: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordCopy("f"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := st.PersistLastWorkspace(1, repo, "garden"); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistPrefs(1, Prefs{Theme: "x"}); err != nil {
		t.Fatal(err)
	}
	re, _ := LoadState()
	if len(re.Aliases) != 1 || re.CopyRank("f") != 1 || re.LastWorkspace(repo) != "garden" || re.PrefsSnapshot().Theme != "x" {
		t.Fatalf("data dropped: %+v", re)
	}
}

func TestRepoIdentity(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	want := RepoIdentity(real)
	forms := map[string]string{
		"trailing slash": real + string(filepath.Separator),
		"dotdot":         filepath.Join(real, "..", "real"),
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err == nil {
		forms["symlink"] = link
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	for name, in := range forms {
		if got := RepoIdentity(in); got != want {
			t.Errorf("%s: %q != %q", name, got, want)
		}
	}
	// Missing dir falls back to the absolute path.
	missing := filepath.Join(root, "nope")
	if got := RepoIdentity(missing); !filepath.IsAbs(got) {
		t.Errorf("missing: %q not absolute", got)
	}
}

func TestPrefsSnapshotIndependent(t *testing.T) {
	stateEnv(t)
	st, _ := LoadState()
	in := Prefs{Keys: map[string]string{"a": "b"}}
	if err := st.PersistPrefs(1, in); err != nil {
		t.Fatal(err)
	}
	in.Keys["a"] = "mutated-input"
	snap := st.PrefsSnapshot()
	snap.Keys["a"] = "mutated-snap"
	if got := st.PrefsSnapshot().Keys["a"]; got != "b" {
		t.Fatalf("keys shared: %q", got)
	}
}

func TestStateConcurrentPersist(t *testing.T) {
	stateEnv(t)
	st, _ := LoadState()
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(3)
		go func(g uint64) {
			defer wg.Done()
			_ = st.PersistPrefs(g, Prefs{Theme: "t", Keys: map[string]string{"k": "v"}})
		}(uint64(i))
		go func() { defer wg.Done(); _ = st.PrefsSnapshot() }()
		go func() { defer wg.Done(); _ = st.Save() }()
	}
	wg.Wait()
}

func TestGardenViewPersistenceScopeOrderingAndSanitization(t *testing.T) {
	p := stateEnv(t)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	a, b := t.TempDir(), t.TempDir()
	if err = st.PersistGardenView(2, a, "board"); err != nil {
		t.Fatal(err)
	}
	if err = st.PersistGardenView(1, a, "setup"); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("stale write: %v", err)
	}
	if err = st.PersistGardenView(3, b, "setup"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"token=secret", "Task title", "/search text", "table", ""} {
		if err = st.PersistGardenView(4, a, value); !errors.Is(err, ErrInvalidGardenView) {
			t.Fatalf("invalid view accepted: %q", value)
		}
	}
	if err = st.PersistPrefs(1, Prefs{Theme: "mono"}); err != nil {
		t.Fatal(err)
	}
	if err = st.PersistLastWorkspace(1, a, "garden"); err != nil {
		t.Fatal(err)
	}
	next, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if next.GardenView(a) != "board" || next.GardenView(b) != "setup" || next.GardenView(t.TempDir()) != "" {
		t.Fatal("view state lost or crossed repos")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"secret", "Task title", "/search text"} {
		if strings.Contains(string(data), value) {
			t.Fatal("content persisted")
		}
	}
}
