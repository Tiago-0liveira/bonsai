package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestStateRebasesUnknownKeys(t *testing.T) {
	path := stateEnv(t)
	writeState(t, path, `{"prefs":{"removed_pref":1},"removed_top":1}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	writeState(t, path, `{"prefs":{"future_pref":{"n":2}},"future_top":[1,2]}`)
	if err := st.RecordCopy("file"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	prefs := got["prefs"].(map[string]any)
	if !reflect.DeepEqual(got["future_top"], []any{float64(1), float64(2)}) || !reflect.DeepEqual(prefs["future_pref"], map[string]any{"n": float64(2)}) {
		t.Fatalf("new unknown keys lost: %s", data)
	}
	if _, ok := got["removed_top"]; ok {
		t.Fatalf("deleted top-level key restored: %s", data)
	}
	if _, ok := prefs["removed_pref"]; ok {
		t.Fatalf("deleted prefs key restored: %s", data)
	}
}

func TestPrefsMergeConcurrentFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*State, Prefs) error
	}{
		{"SetPrefs", func(s *State, p Prefs) error { return s.SetPrefs(p) }},
		{"PersistPrefs", func(s *State, p Prefs) error { return s.PersistPrefs(1, p) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"prefs":{"theme":"bonsai","density":"comfortable"}}`)
			a, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			b, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			mine := a.PrefsSnapshot()
			mine.Theme = "nord"
			if err := tc.write(a, mine); err != nil {
				t.Fatal(err)
			}
			mine = b.PrefsSnapshot()
			mine.Density = "compact"
			if err := tc.write(b, mine); err != nil {
				t.Fatal(err)
			}
			reloaded, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range []Prefs{b.PrefsSnapshot(), reloaded.PrefsSnapshot()} {
				if got.Theme != "nord" || got.Density != "compact" {
					t.Fatalf("merged prefs = %+v", got)
				}
			}
			// The caller keeps editing its original working copy after a save.
			mine.Contrast = "flat"
			if err := tc.write(b, mine); err != nil {
				t.Fatal(err)
			}
			reloaded, err = LoadState()
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range []Prefs{b.PrefsSnapshot(), reloaded.PrefsSnapshot()} {
				if got.Theme != "nord" || got.Density != "compact" || got.Contrast != "flat" {
					t.Fatalf("merged prefs after second edit = %+v", got)
				}
			}
		})
	}
}

func TestRecordCopyRejectsInvalidDiskState(t *testing.T) {
	for _, body := range []string{
		`{"copy_counts":"x","prefs":{"theme":"sakura","future_pref":42},"future_top":true}`,
		`{not json`,
	} {
		t.Run(body, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"copy_counts":{"file":2},"prefs":{"theme":"bonsai"}}`)
			st, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			writeState(t, path, body)
			if err := st.RecordCopy("file"); err == nil || !strings.Contains(err.Error(), "parse state:") || errors.Unwrap(err) == nil {
				t.Errorf("RecordCopy error = %v, want wrapped parse state error", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != body {
				t.Errorf("invalid state overwritten: %s", data)
			}
			if got := st.CopyRank("file"); got != 2 {
				t.Errorf("in-memory copy count = %d, want 2", got)
			}
		})
	}
}

func TestRecordCopyRejectsUnreadableDiskState(t *testing.T) {
	path := stateEnv(t)
	writeState(t, path, `{"copy_counts":{"file":2}}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	// A directory produces a read error even when tests run with elevated permissions.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err = st.RecordCopy("file")
	var pathErr *os.PathError
	if err == nil || !strings.Contains(err.Error(), "read state:") || !errors.As(err, &pathErr) {
		t.Fatalf("RecordCopy error = %v, want wrapped read state error", err)
	}
	if got := st.CopyRank("file"); got != 2 {
		t.Fatalf("in-memory copy count = %d, want 2", got)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("unreadable state replaced: %v", err)
	}
}

func TestStateRebasesPreservedGardenPolicy(t *testing.T) {
	for _, body := range []string{
		`{"prefs":{"garden_auto_review":{"version":2,"enabled":true,"future":42}}}`,
		`{"Prefs":{"garden_auto_review":{"version":2,"enabled":true,"future":42}}}`,
		`{"prefs":{"GARDEN_AUTO_REVIEW":[1,2]}}`,
	} {
		for _, tc := range []struct {
			name  string
			write func(*State) error
		}{
			{"RecordCopy", func(s *State) error { return s.RecordCopy("file") }},
			{"SetPrefs", func(s *State) error { return s.SetPrefs(Prefs{Theme: "nord"}) }},
			{"PersistPrefs", func(s *State) error { return s.PersistPrefs(1, Prefs{Density: "compact"}) }},
		} {
			t.Run(tc.name+"/"+body, func(t *testing.T) {
				path := stateEnv(t)
				s, err := LoadState()
				if err != nil {
					t.Fatal(err)
				}
				writeState(t, path, body)
				if err := tc.write(s); err != nil {
					t.Fatal(err)
				}
				got, err := LoadState()
				if err != nil {
					t.Fatal(err)
				}
				if !got.GardenAutoReviewPreserved() || got.PrefsSnapshot().GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
					t.Fatal("newer disk policy was lost or authorized")
				}
				var original, saved map[string]any
				if err := json.Unmarshal([]byte(body), &original); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				for key, value := range original {
					for prefKey, prefValue := range value.(map[string]any) {
						if !reflect.DeepEqual(saved[key].(map[string]any)[prefKey], prefValue) {
							t.Fatalf("changed preserved %s.%s: %s", key, prefKey, data)
						}
					}
				}
			})
		}
	}
}

func TestPrefsRejectPolicyUpgradedOnDisk(t *testing.T) {
	for _, persist := range []bool{false, true} {
		t.Run(fmt.Sprint(persist), func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"a"}}}`)
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			mine := s.PrefsSnapshot()
			mine.Theme = "nord"
			const newer = `{"prefs":{"garden_auto_review":{"version":2,"future":42}}}`
			writeState(t, path, newer)
			write := func(p Prefs) error {
				if persist {
					return s.PersistPrefs(1, p)
				}
				return s.SetPrefs(p)
			}
			if err := write(mine); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatalf("upgraded policy write = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != newer {
				t.Fatalf("refusal changed disk: %s, %v", data, err)
			}
			mine.GardenAutoReview = nil
			if err := write(mine); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSaveReplacesKnownFields(t *testing.T) {
	path := stateEnv(t)
	writeState(t, path, `{"copy_counts":{"old":1},"prefs":{"theme":"bonsai"}}`)
	s, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	writeState(t, path, `{"copy_counts":{"new":2},"prefs":{"theme":"nord","density":"compact"}}`)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.CopyRank("old") != 1 || got.CopyRank("new") != 0 || got.PrefsSnapshot().Theme != "bonsai" || got.PrefsSnapshot().Density != "" {
		t.Fatalf("Save merged instead of replacing: %+v", got.diskLocked())
	}
}

func TestSaveResetsPrefsMergeBase(t *testing.T) {
	path := stateEnv(t)
	writeState(t, path, `{"prefs":{"theme":"bonsai"}}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs.Theme = "nord"
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	mine := st.PrefsSnapshot()
	writeState(t, path, `{"prefs":{"theme":"sakura"}}`)
	mine.Density = "compact"
	if err := st.PersistPrefs(1, mine); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.PrefsSnapshot(); got.Theme != "sakura" || got.Density != "compact" {
		t.Fatalf("merged prefs after Save = %+v", got)
	}
}

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
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(configDir, "bonsai", "state.json")
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
	if pr.Contrast != "" || pr.Density != "" || pr.StartWorkspace != "" || pr.NoWorkspaceMemory || pr.GardenAssist != nil || st.Workspaces != nil {
		t.Fatalf("new defaults not zero: %+v", pr)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, k := range []string{"contrast", "density", "start_workspace", "no_workspace_memory", "garden_assist", "workspaces"} {
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
	if err := st.PersistPrefs(1, Prefs{Theme: "new", Density: "compact", GardenAssist: &GardenAssistPrefs{Account: "c1", Questions: 7}}); err != nil {
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
	if err = st.PersistGardenView(3, b, "table"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"token=secret", "Task title", "/search text", "tables", ""} {
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
	if next.GardenView(a) != "board" || next.GardenView(b) != "table" || next.GardenView(t.TempDir()) != "" {
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

func TestGardenAssistPrefsRoundTripAndClone(t *testing.T) {
	stateEnv(t)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	want := GardenAssistPrefs{Account: "claude-work", Model: "opus", Effort: "high", Questions: 7, Guidance: "focus on UX"}
	p := Prefs{GardenAssist: &want}
	clone := p.Clone()
	clone.GardenAssist.Model = "sonnet"
	if p.GardenAssist.Model != "opus" {
		t.Fatal("Clone shared AI defaults")
	}
	if err := st.PersistPrefs(1, p); err != nil {
		t.Fatal(err)
	}
	p.GardenAssist.Account = "mutated"
	snap := st.PrefsSnapshot()
	snap.GardenAssist.Questions = 2
	re, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	expected := GardenAssistPrefs{Account: "claude-work", Model: "opus", Effort: "high", Questions: 7, Guidance: "focus on UX"}
	if got := *re.PrefsSnapshot().GardenAssist; got != expected {
		t.Fatalf("disk defaults = %+v", got)
	}
	if got := *st.PrefsSnapshot().GardenAssist; got != expected {
		t.Fatalf("snapshot shared defaults: %+v", got)
	}
	data, err := json.Marshal(Prefs{GardenAssist: &GardenAssistPrefs{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"garden_assist":{}}` {
		t.Fatalf("omitempty = %s", data)
	}
}

func TestGardenDraftSelectionIsOrderedOpaqueAndPersisted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := st.PersistGardenDraft(2, repo, "draft-1a2b3c", 3); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistGardenDraft(1, repo, "draft-old", 0); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("stale write: %v", err)
	}
	if err := st.PersistGardenDraft(3, repo, "Some authored text!", 0); !errors.Is(err, ErrInvalidGardenDraft) {
		t.Fatalf("text accepted as an ID: %v", err)
	}
	again, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if id, sec := again.GardenDraft(repo); id != "draft-1a2b3c" || sec != 3 {
		t.Fatalf("restored %q %d", id, sec)
	}
	if err := again.PersistGardenDraft(4, repo, "", 0); err != nil {
		t.Fatal(err)
	}
	if id, _ := again.GardenDraft(repo); id != "" {
		t.Fatalf("not forgotten: %q", id)
	}
}

func TestPrefsShellInitJSON(t *testing.T) {
	b, err := json.Marshal(Prefs{DepthGlyph: "B", ShellPrompt: "off"})
	if err != nil {
		t.Fatal(err)
	}
	var p Prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.DepthGlyph != "B" || p.ShellPrompt != "off" {
		t.Errorf("round trip = %+v", p)
	}
	b, _ = json.Marshal(Prefs{})
	if strings.Contains(string(b), "depth_glyph") || strings.Contains(string(b), "shell_prompt") {
		t.Errorf("empty fields should be omitted: %s", b)
	}
}

func TestGardenAttentionViewPersistence(t *testing.T) {
	stateEnv(t)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := st.PersistGardenView(1, dir, "attention"); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistGardenView(2, dir, "attention-other"); !errors.Is(err, ErrInvalidGardenView) {
		t.Fatalf("unknown view: %v", err)
	}
	next, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if next.GardenView(dir) != "attention" {
		t.Fatal("attention view lost")
	}
}
