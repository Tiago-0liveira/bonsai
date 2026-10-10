package websetup

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func root(path string) config.ProjectRoot {
	return config.ProjectRoot{ID: config.PathID("root", path), Path: path}
}

func TestNewDraftFirstRun(t *testing.T) {
	d := NewDraft(config.DefaultWebConfig(), false, nil, []string{"/h/code", "/h/dev"}, "/h/bonsai")
	if d.Config.Interfaces != (config.WebInterfaces{Local: true}) {
		t.Fatalf("a brand-new user gets this computer only, got %+v", d.Config.Interfaces)
	}
	if len(d.Folders) != 3 || !d.Folders[0].ThisRepo || d.Folders[0].Path != "/h/bonsai" {
		t.Fatalf("folders = %+v", d.Folders)
	}
	for _, f := range d.Folders {
		if !f.Checked {
			t.Fatalf("first run preselects every suggestion: %+v", f)
		}
	}
	// A scan that finds nothing unchecks an untouched suggestion...
	d.SetScan(2, nil, "")
	if d.Folders[2].Checked {
		t.Fatal("empty suggestion stayed checked")
	}
	// ...but never this repo, and never a row the user toggled.
	d.SetScan(0, nil, "")
	if !d.Folders[0].Checked {
		t.Fatal("this repo got unchecked")
	}
	d.Toggle(1)
	d.Toggle(1)
	d.SetScan(1, nil, "")
	if !d.Folders[1].Checked {
		t.Fatal("scan overrode the user's choice")
	}
}

func TestNewDraftExistingUser(t *testing.T) {
	cfg := config.DefaultWebConfig() // hosted on
	d := NewDraft(cfg, true, []config.ProjectRoot{root("/h/code")}, []string{"/h/code", "/h/code/inner", "/h/dev"}, "/h/code/repo")
	if !d.Config.Interfaces.Hosted {
		t.Fatal("existing users keep their saved interfaces")
	}
	if len(d.Folders) != 2 || !d.Folders[0].Existing() || d.Folders[1].Path != "/h/dev" {
		t.Fatalf("roots first, covered suggestions and repo skipped: %+v", d.Folders)
	}
	if d.Folders[1].Checked {
		t.Fatal("suggestions are not preselected once roots exist")
	}
}

func TestBuildPlanFirstRun(t *testing.T) {
	before := NewDraft(config.DefaultWebConfig(), false, nil, []string{"/h/code"}, "/h/bonsai")
	after := before.Clone()
	after.SetScan(0, []Repo{{ID: "p1"}}, "")
	after.SetScan(1, []Repo{{ID: "p2"}, {ID: "p3"}, {ID: "p1"}}, "")
	p := BuildPlan(before, after, nil, true)
	if !p.Start || p.RestartAPI || p.Config.SetupVersion != config.WebSetupVersion {
		t.Fatalf("plan = %+v", p)
	}
	if !reflect.DeepEqual(p.AddRoots, []string{"/h/bonsai", "/h/code"}) || !reflect.DeepEqual(p.SelectRepos, []string{"p1", "p2", "p3"}) {
		t.Fatalf("roots %v repos %v", p.AddRoots, p.SelectRepos)
	}
	if before.Folders[0].Scanned {
		t.Fatal("Clone shares folders with the original")
	}
}

func TestBuildPlanTargetedRestart(t *testing.T) {
	cfg := config.DefaultWebConfig()
	before := NewDraft(cfg, true, []config.ProjectRoot{root("/h/code"), root("/h/old")}, []string{"/h/work"}, "")
	running := &Running{APIPort: cfg.APIPort, Hosted: true}

	cases := []struct {
		name    string
		edit    func(*Draft)
		restart bool
		reason  string
	}{
		{"nothing", func(*Draft) {}, false, ""},
		{"roots only", func(d *Draft) { d.Toggle(1); d.Toggle(2) }, false, ""},
		{"auto-open only", func(d *Draft) { d.Config.OpenBrowser = false }, false, ""},
		{"port", func(d *Draft) { d.Config.APIPort = 7011 }, true, "port changed"},
		{"hosted", func(d *Draft) { d.Config.Interfaces.Hosted = false }, true, "hosted app access changed"},
		{"both", func(d *Draft) { d.Config.APIPort = 7011; d.Config.Interfaces.Hosted = false }, true, "port and hosted app access changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := before.Clone()
			tc.edit(&after)
			p := BuildPlan(before, after, running, false)
			if p.RestartAPI != tc.restart || p.RestartReason != tc.reason || p.Start {
				t.Fatalf("plan = %+v", p)
			}
			if BuildPlan(before, after, nil, false).RestartAPI {
				t.Fatal("nothing restarts when bonsai web is not running")
			}
		})
	}

	// Running on a --port override: leaving the port alone restarts nothing.
	override := &Running{APIPort: 17931, Hosted: true}
	if p := BuildPlan(before, before.Clone(), override, false); p.RestartAPI {
		t.Fatalf("untouched port restarted a --port stack: %+v", p)
	}
	hostedOnly := before.Clone()
	hostedOnly.Config.Interfaces.Hosted = false
	if p := BuildPlan(before, hostedOnly, override, false); !p.RestartAPI || p.RestartPort != 17931 {
		t.Fatalf("a hosted-only change must restart on the running port: %+v", p)
	}
	newPort := before.Clone()
	newPort.Config.APIPort = 7011
	if p := BuildPlan(before, newPort, override, false); p.RestartPort != 7011 {
		t.Fatalf("a port change restarts on the new port: %+v", p)
	}

	after := before.Clone()
	after.Toggle(1) // remove /h/old
	after.Toggle(2) // add /h/work
	p := BuildPlan(before, after, running, false)
	if !reflect.DeepEqual(p.AddRoots, []string{"/h/work"}) || len(p.RemoveRoots) != 1 || p.RemoveRoots[0].ID != config.PathID("root", "/h/old") || !p.ProjectsChanged() {
		t.Fatalf("plan = %+v", p)
	}
}

func TestDiff(t *testing.T) {
	home := filepath.FromSlash("/h")
	code, old, work := filepath.Join(home, "code"), filepath.Join(home, "old"), filepath.Join(home, "work")
	before := NewDraft(config.DefaultWebConfig(), true, []config.ProjectRoot{root(code), root(old)}, []string{work}, "")
	after := before.Clone()
	after.Toggle(1)
	after.Toggle(2)
	after.Config.APIPort = 7011
	after.Config.Interfaces.Hosted = false
	after.Config.OpenBrowser = false
	got := Diff(before, after, home)
	sep := string(filepath.Separator)
	want := []Change{
		{Label: "Folders", To: "+ ~" + sep + "work  − ~" + sep + "old"},
		{Label: "Open in", From: "this computer + hosted app", To: "this computer"},
		{Label: "Auto-open", From: "on", To: "off"},
		{Label: "Port", From: "7001", To: "7011"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff =\n%+v\nwant\n%+v", got, want)
	}
	if len(Diff(before, before.Clone(), home)) != 0 {
		t.Fatal("identical drafts differ")
	}
}

func TestSummaries(t *testing.T) {
	cfg := config.DefaultWebConfig()
	if got := OpenInSummary(cfg); got != "this computer + hosted app · http://127.0.0.1:7001 · auto-open on" {
		t.Fatal(got)
	}
	cfg.Interfaces = config.WebInterfaces{Hosted: true}
	if got := OpenInSummary(cfg); got != "hosted app only · auto-open on" {
		t.Fatal(got)
	}
	if got := UpdatesSummary(cfg, "2 min"); got != "Standard · checks GitHub every ~2 min" {
		t.Fatal(got)
	}
	home := filepath.FromSlash("/h")
	d := Draft{Folders: []Folder{
		{Path: filepath.Join(home, "code"), Checked: true, Scanned: true, Repos: []Repo{{ID: "a"}, {ID: "b"}}},
		{Path: filepath.Join(home, "bonsai"), Checked: true, Scanned: true, Repos: []Repo{{ID: "a"}}},
		{Path: filepath.Join(home, "dev"), Checked: false},
	}}
	sep := string(filepath.Separator)
	if got := ProjectsSummary(d, home); got != "~"+sep+"bonsai, ~"+sep+"code · 2 repos" {
		t.Fatal(got)
	}
	if got := TildePath(home, filepath.FromSlash("/elsewhere")); got != filepath.FromSlash("/elsewhere") {
		t.Fatal(got)
	}
}
