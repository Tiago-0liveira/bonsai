package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGardenAutoReviewValidate(t *testing.T) {
	cases := []struct {
		name string
		p    GardenAutoReviewPrefs
		ok   bool
	}{
		{"off", GardenAutoReviewPrefs{}, true}, {"version one", GardenAutoReviewPrefs{Version: 1}, true},
		{"version two", GardenAutoReviewPrefs{Version: 2}, false}, {"negative version", GardenAutoReviewPrefs{Version: -1}, false},
		{"enabled", GardenAutoReviewPrefs{Enabled: true, Account: "acc"}, true}, {"account required", GardenAutoReviewPrefs{Enabled: true}, false},
		{"account padded", GardenAutoReviewPrefs{Account: " acc"}, false}, {"account long", GardenAutoReviewPrefs{Account: strings.Repeat("a", 129)}, false},
		{"account control", GardenAutoReviewPrefs{Account: "acc\x00"}, false}, {"account utf8", GardenAutoReviewPrefs{Account: "\xff"}, false},
		{"model length", GardenAutoReviewPrefs{Model: strings.Repeat("a", 65)}, false}, {"effort space", GardenAutoReviewPrefs{Effort: "high enough"}, false},
		{"permission space", GardenAutoReviewPrefs{PermissionMode: "a\u2003b"}, false}, {"sandbox control", GardenAutoReviewPrefs{Sandbox: "a\nb"}, false},
		{"approval utf8", GardenAutoReviewPrefs{Approval: "\xff"}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidGardenAutoReview)) {
				t.Fatalf("Validate = %v, ok=%v", err, tt.ok)
			}
		})
	}
}

func TestGardenChecksValidate(t *testing.T) {
	good := GardenCheck{Name: "Go tests", Executable: "go", Args: []string{"test", "./..."}, Dir: "internal/core"}
	cases := []struct {
		name string
		edit func(*GardenCheck)
		ok   bool
	}{
		{"valid", func(*GardenCheck) {}, true},
		{"absolute", func(c *GardenCheck) { c.Executable = filepath.Join(t.TempDir(), "go") }, true},
		{"relative executable", func(c *GardenCheck) { c.Executable = "bin/go" }, false},
		{"unclean executable", func(c *GardenCheck) {
			c.Executable = filepath.Join(t.TempDir(), "bin") + string(filepath.Separator) + ".." + string(filepath.Separator) + "go"
		}, false},
		{"long executable", func(c *GardenCheck) { c.Executable = strings.Repeat("a", 65) }, false},
		{"blank executable", func(c *GardenCheck) { c.Executable = "" }, false},
		{"empty name", func(c *GardenCheck) { c.Name = "" }, false},
		{"max name", func(c *GardenCheck) { c.Name = strings.Repeat("界", 60) }, true},
		{"long name", func(c *GardenCheck) { c.Name = strings.Repeat("界", 61) }, false},
		{"name format", func(c *GardenCheck) { c.Name = "a\u200db" }, false},
		{"name control", func(c *GardenCheck) { c.Name = "a\nb" }, false},
		{"name utf8", func(c *GardenCheck) { c.Name = "\xff" }, false},
		{"argument limit", func(c *GardenCheck) { c.Args = make([]string, 32) }, true},
		{"argument overflow", func(c *GardenCheck) { c.Args = make([]string, 33) }, false},
		{"argument byte limit", func(c *GardenCheck) { c.Args = []string{strings.Repeat("x", 256)} }, true},
		{"argument bytes overflow", func(c *GardenCheck) { c.Args = []string{strings.Repeat("界", 86)} }, false},
		{"argument control", func(c *GardenCheck) { c.Args = []string{"\t"} }, false},
		{"argument nul", func(c *GardenCheck) { c.Args = []string{"a\x00b"} }, false},
		{"argument utf8", func(c *GardenCheck) { c.Args = []string{"\xff"} }, false},
		{"argv spaces", func(c *GardenCheck) { c.Args = []string{"one typed argument"} }, true},
		{"root dir", func(c *GardenCheck) { c.Dir = "" }, true},
		{"absolute dir", func(c *GardenCheck) { c.Dir = "/tmp" }, false},
		{"parent dir", func(c *GardenCheck) { c.Dir = "../src" }, false},
		{"interior parent", func(c *GardenCheck) { c.Dir = "src/../test" }, false},
		{"backslash", func(c *GardenCheck) { c.Dir = "src\\test" }, false},
		{"unclean dir", func(c *GardenCheck) { c.Dir = "src//test" }, false},
		{"long dir", func(c *GardenCheck) { c.Dir = strings.Repeat("a", 257) }, false},
	}
	for _, ch := range "|&;<>$`" {
		bad := string(ch)
		cases = append(cases, struct {
			name string
			edit func(*GardenCheck)
			ok   bool
		}{"metachar " + bad, func(c *GardenCheck) { c.Args = []string{"a" + bad + "b"} }, false})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := good
			tt.edit(&c)
			err := (GardenAutoReviewPrefs{Checks: []GardenCheck{c}}).Validate()
			if (err == nil) != tt.ok || err != nil && !errors.Is(err, ErrInvalidGardenAutoReview) {
				t.Fatalf("Validate = %v, ok=%v", err, tt.ok)
			}
		})
	}
	for _, shell := range strings.Fields("sh bash zsh fish dash ksh csh tcsh ash mksh yash rc es elvish nu xonsh ion osh ysh oil scsh tclsh wish cmd powershell pwsh env sudo doas su xargs nohup busybox") {
		for _, exe := range []string{shell, strings.ToUpper(shell) + ".EXE", filepath.Join(t.TempDir(), shell)} {
			c := good
			c.Executable = exe
			if err := (GardenAutoReviewPrefs{Checks: []GardenCheck{c}}).Validate(); !errors.Is(err, ErrInvalidGardenAutoReview) {
				t.Errorf("shell %q accepted: %v", exe, err)
			}
		}
	}
	for _, exe := range []string{"verify.cmd", "checks.BAT", "test.Ps1", filepath.Join(t.TempDir(), "custom.cmd"), filepath.Join(t.TempDir(), "custom.bat"), filepath.Join(t.TempDir(), "custom.ps1")} {
		c := good
		c.Executable = exe
		if err := (GardenAutoReviewPrefs{Checks: []GardenCheck{c}}).Validate(); !errors.Is(err, ErrInvalidGardenAutoReview) {
			t.Errorf("shell script %q accepted: %v", exe, err)
		}
	}
	checks := make([]GardenCheck, 9)
	for i := range checks {
		checks[i] = good
		checks[i].Args = []string{strings.Repeat("a", i)}
	}
	if err := (GardenAutoReviewPrefs{Checks: checks[:8]}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (GardenAutoReviewPrefs{Checks: checks}).Validate(); !errors.Is(err, ErrInvalidGardenAutoReview) {
		t.Fatal("check overflow accepted")
	}
	duplicate := good
	duplicate.Name = "different display"
	if err := (GardenAutoReviewPrefs{Checks: []GardenCheck{good, duplicate}}).Validate(); !errors.Is(err, ErrInvalidGardenAutoReview) {
		t.Fatal("duplicate accepted")
	}
	good.Args = nil
	duplicate = good
	duplicate.Args = []string{}
	if err := (GardenAutoReviewPrefs{Checks: []GardenCheck{good, duplicate}}).Validate(); err == nil {
		t.Fatal("empty/nil argv duplicate accepted")
	}
}

func TestGardenChecksText(t *testing.T) {
	checks, err := ParseGardenChecks(" ; go test ./... ; pnpm test ; ")
	if err != nil || FormatGardenChecks(checks) != "go test ./...; pnpm test" {
		t.Fatalf("parse/format = %+v, %v", checks, err)
	}
	again, err := ParseGardenChecks(FormatGardenChecks(checks))
	if err != nil || !reflect.DeepEqual(checks, again) {
		t.Fatalf("round trip = %+v, %v", again, err)
	}
	for _, text := range []string{"bash -c test", "go test | cat", "go $HOME", "go `pwd`", "go a&b", "go <in", "go >out", "go \xff", strings.Repeat("go test;", 9)} {
		if _, err := ParseGardenChecks(text); !errors.Is(err, ErrInvalidGardenAutoReview) {
			t.Errorf("accepted %q: %v", text, err)
		}
	}
	long, err := ParseGardenChecks("go " + strings.Repeat("界", 61))
	if err != nil || utf8.RuneCountInString(long[0].Name) != 60 || !strings.HasSuffix(long[0].Name, "…") {
		t.Fatalf("truncation = %+v, %v", long, err)
	}
}

func TestGardenAutoReviewStatePreservation(t *testing.T) {
	for _, raw := range []string{
		`{"version":2,"enabled":true,"account":"acc","future":{"secret_shape":[1,2]}}`,
		`{"version":1,"enabled":true}`,
		`{"version":1,"checks":[{"name":"shell","executable":"bash"}]}`,
		`{"version":"future"}`, `[1,2]`, `null`, `{}`,
		`{"version":1,"enabled":false,"account":""}`,
		`{"version":1,"enabled":false,"checks":[]}`,
		`{"version":2,"VERSION":1,"enabled":true,"account":"acc"}`,
		`{"version":1,"version":1,"enabled":true,"account":"acc"}`,
		`{"version":1,"enabled":true,"account":"acc","future":true}`,
		`{"VERSION":1,"enabled":true,"account":"acc"}`,
		`{"verſion":1,"enabled":true,"account":"acc"}`,
		`{"version":2,"verſion":1,"enabled":true,"account":"acc"}`,
		`{"version":1,"enabled":true,"account":"acc","ſandbox":"workspace-write"}`,
		`{"version":1,"enabled":true,"account":"acc","checKs":[{"name":"test","executable":"go"}]}`,
		`{"version":1,"enabled":true,"account":"acc","checks":[{"name":"test","executable":"go","unknown":true}]}`,
		`{"version":1,"enabled":true,"account":"acc","checks":[{"name":"test","executable":"go","argſ":["test"]}]}`,
		`{"version":1,"enabled":true,"account":"acc","checks":[{"name":"test","executable":"go","NAME":"other"}]}`,
		`{"version":1,"enabled":true,"account":"acc","checks":[{"name":"test","executable":"go","name":"other"}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"prefs":{"garden_auto_review":`+raw+`}}`)
			s, err := LoadState()
			if err != nil || s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
				t.Fatalf("load = %+v, %v", s, err)
			}
			p := s.PrefsSnapshot()
			p.Theme = "test"
			if err := s.SetPrefs(p); err != nil {
				t.Fatal(err)
			}
			p.Sort = "name"
			if err := s.PersistPrefs(2, p); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			var got struct {
				Prefs map[string]json.RawMessage `json:"prefs"`
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			var wantVal, gotVal any
			_ = json.Unmarshal([]byte(raw), &wantVal)
			_ = json.Unmarshal(got.Prefs["garden_auto_review"], &gotVal)
			if !reflect.DeepEqual(wantVal, gotVal) {
				t.Fatalf("policy changed: %s", data)
			}
			before := string(data)
			p.GardenAutoReview = &GardenAutoReviewPrefs{Enabled: true, Account: "a"}
			if err := s.SetPrefs(p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatalf("set preserved = %v", err)
			}
			if err := s.PersistPrefs(3, p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatalf("persist preserved = %v", err)
			}
			data, _ = os.ReadFile(path)
			if string(data) != before || s.Prefs.GardenAutoReview != nil {
				t.Fatal("refusal changed state")
			}
			s, err = LoadState()
			if err != nil || !s.GardenAutoReviewPreserved() {
				t.Fatalf("reload = %v", err)
			}
		})
	}
}

func TestGardenAutoReviewDefaultAndClone(t *testing.T) {
	path := stateEnv(t)
	for _, body := range []string{`{}`, `{"prefs":{"garden_review":{"enabled":true,"account_id":"acc"}}}`} {
		writeState(t, path, body)
		s, err := LoadState()
		if err != nil || s.Prefs.GardenAutoReview != nil || s.GardenAutoReviewPreserved() {
			t.Fatalf("default = %+v, %v", s, err)
		}
	}
	on := true
	p := Prefs{GardenAutoReview: &GardenAutoReviewPrefs{Version: 1, Enabled: true, Account: "a", FullAccess: &on, Checks: []GardenCheck{{Name: "test", Executable: "go", Args: []string{"test"}}}}}
	c := p.Clone()
	c.GardenAutoReview.Account = "b"
	*c.GardenAutoReview.FullAccess = false
	c.GardenAutoReview.Checks[0].Name = "changed"
	c.GardenAutoReview.Checks[0].Args[0] = "changed"
	if p.GardenAutoReview.Account != "a" || !*p.GardenAutoReview.FullAccess || p.GardenAutoReview.Checks[0].Name != "test" || p.GardenAutoReview.Checks[0].Args[0] != "test" {
		t.Fatal("clone shares data")
	}
	s, _ := LoadState()
	if err := s.PersistPrefs(1, p); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState()
	if err != nil || !reflect.DeepEqual(s.Prefs.GardenAutoReview, p.GardenAutoReview) {
		t.Fatalf("round trip = %+v, %v", s, err)
	}
	invalid := Prefs{GardenAutoReview: &GardenAutoReviewPrefs{Enabled: true}}
	if err := s.SetPrefs(invalid); !errors.Is(err, ErrInvalidGardenAutoReview) {
		t.Fatal(err)
	}
	if err := s.PersistPrefs(2, invalid); !errors.Is(err, ErrInvalidGardenAutoReview) {
		t.Fatal(err)
	}
	passPath, err := GardenReviewPassPath()
	if err != nil || passPath != filepath.Join(filepath.Dir(path), "garden-review-passes.json") {
		t.Fatalf("path = %q, %v", passPath, err)
	}
}

func TestGardenAutoReviewCaseVariantsQuarantined(t *testing.T) {
	for _, prefs := range []string{
		`{"GARDEN_AUTO_REVIEW":{"version":1,"enabled":true,"account":"acc"}}`,
		`{"Garden_Auto_Review":{"version":1,"enabled":true,"account":"acc"},"garden_auto_review":{"version":1,"enabled":true,"account":"other"}}`,
		`{"GARDEN_AUTO_REVIEW":{"version":2,"enabled":true,"account":"future"}}`,
	} {
		t.Run(prefs, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"prefs":`+prefs+`}`)
			var want map[string]json.RawMessage
			if err := json.Unmarshal([]byte(prefs), &want); err != nil {
				t.Fatal(err)
			}
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() || !reflect.DeepEqual(s.extraPrefs, want) {
				t.Fatalf("not quarantined: %+v, raw=%s", s.Prefs.GardenAutoReview, s.extraPrefs)
			}
			p := s.PrefsSnapshot()
			p.Editor = "test-editor"
			if err := s.SetPrefs(p); err != nil {
				t.Fatal(err)
			}
			if err := s.PersistPrefs(1, p); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var disk struct {
				Prefs map[string]json.RawMessage `json:"prefs"`
			}
			if err := json.Unmarshal(data, &disk); err != nil {
				t.Fatal(err)
			}
			for key, raw := range want {
				var expected, actual any
				_ = json.Unmarshal(raw, &expected)
				_ = json.Unmarshal(disk.Prefs[key], &actual)
				if !reflect.DeepEqual(expected, actual) {
					t.Fatalf("lost %s: %s", key, data)
				}
			}
			p.GardenAutoReview = &GardenAutoReviewPrefs{Account: "replacement"}
			if err := s.SetPrefs(p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatal(err)
			}
			if err := s.PersistPrefs(2, p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(data) {
				t.Fatal("refusal rewrote quarantine")
			}
			s, err = LoadState()
			if err != nil || s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
				t.Fatal("reload lost quarantine", err)
			}
		})
	}
}

func TestGardenCheckPortablePaths(t *testing.T) {
	for _, dir := range []string{"C:/outside", "C:outside", `\\server\share`, `\outside`, "/outside", "a/../b", "./a", ".", "./.", "a/.", "a:stream", `a\b`} {
		t.Run(dir, func(t *testing.T) {
			p := GardenAutoReviewPrefs{Checks: []GardenCheck{{Name: "test", Executable: "go", Dir: dir}}}
			if err := p.Validate(); !errors.Is(err, ErrInvalidGardenAutoReview) {
				t.Fatalf("accepted dir %q: %v", dir, err)
			}
		})
	}
	for _, exe := range []string{"C:/outside", "C:outside", `\\server\share`, "a/../b", "./a", filepath.Join(t.TempDir(), "go")} {
		t.Run("executable/"+exe, func(t *testing.T) {
			p := GardenAutoReviewPrefs{Checks: []GardenCheck{{Name: "test", Executable: exe}}}
			want := filepath.IsAbs(exe) && filepath.Clean(exe) == exe
			if err := p.Validate(); (err == nil) != want {
				t.Fatalf("executable %q: %v, want accepted=%v", exe, err, want)
			}
		})
	}
}

func TestGardenAutoReviewTopLevelPrefsQuarantined(t *testing.T) {
	for _, body := range []string{
		`{"Prefs":{"garden_auto_review":{"version":2,"enabled":true,"account":"future"},"theme":"old","future":{"a":[1,2]}}}`,
		`{"Prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"}}}`,
		`{"prefſ":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"}}}`,
		`{"prefſ":{"theme":"old"},"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"}}}`,
		`{"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"}},"prefſ":{"theme":"old"}}`,
		`{"PREFS":{"garden_auto_review":{"version":2,"enabled":true,"account":"future"}},"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"},"unknown":42}}`,
		`{"PREFS":{"Garden_Auto_Review":[1,2]},"prefs":{"GARDEN_AUTO_REVIEW":{"version":1,"enabled":true,"account":"acc"}}}`,
		`{"PREFS":{"theme":"old"},"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"acc"}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, body)
			var original map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &original); err != nil {
				t.Fatal(err)
			}
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
				t.Fatalf("policy escaped quarantine: %+v", s.Prefs)
			}
			p := s.PrefsSnapshot()
			p.Editor = "test-editor"
			if err := s.SetPrefs(p); err != nil {
				t.Fatal(err)
			}
			p.Sort = "name"
			if err := s.PersistPrefs(1, p); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved map[string]json.RawMessage
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			for key, raw := range original {
				var want, got any
				_ = json.Unmarshal(raw, &want)
				_ = json.Unmarshal(saved[key], &got)
				if key == "prefs" {
					for prefKey, prefRaw := range want.(map[string]any) {
						if !reflect.DeepEqual(prefRaw, got.(map[string]any)[prefKey]) {
							t.Fatalf("lost prefs.%s: %s", prefKey, data)
						}
					}
				} else if !reflect.DeepEqual(want, got) {
					t.Fatalf("changed raw %s object: %s", key, data)
				}
			}
			var canonical Prefs
			if err := json.Unmarshal(saved["prefs"], &canonical); err != nil || canonical.Editor != p.Editor || canonical.Sort != p.Sort {
				t.Fatalf("unrelated prefs not saved: %s, %v", data, err)
			}
			p.GardenAutoReview = &GardenAutoReviewPrefs{Account: "replacement"}
			if err := s.SetPrefs(p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatalf("set quarantine = %v", err)
			}
			if err := s.PersistPrefs(2, p); !errors.Is(err, ErrGardenAutoReviewNewer) {
				t.Fatalf("persist quarantine = %v", err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(data) {
				t.Fatal("refusal rewrote policy")
			}
			s, err = LoadState()
			if err != nil || s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
				t.Fatalf("reload lost quarantine: %v", err)
			}
		})
	}
}

func TestGardenCheckExecutableVariants(t *testing.T) {
	cases := []struct {
		exe string
		ok  bool
	}{
		{"rbash", false}, {"rksh", false}, {"ksh93", false}, {"bash5.2", false}, {"zsh-5.9", false},
		{"rsh", false}, {"rBASH5.2.EXE", false}, {"rKSH93.exe", false}, {"pwsh7.4", false}, {"tclsh8.6", false},
		{"python", true}, {"python3.12", true}, {"go", true}, {"make", true}, {"npm", true}, {"pnpm", true}, {"rg", true},
		{"bashful", true}, {"shellcheck", true},
	}
	for _, tt := range cases {
		for _, exe := range []string{tt.exe, filepath.Join(t.TempDir(), tt.exe)} {
			t.Run(exe, func(t *testing.T) {
				err := (GardenAutoReviewPrefs{Checks: []GardenCheck{{Name: "check", Executable: exe}}}).Validate()
				if (err == nil) != tt.ok || err != nil && !errors.Is(err, ErrInvalidGardenAutoReview) {
					t.Fatalf("Validate = %v, want allowed=%v", err, tt.ok)
				}
			})
		}
	}
}

func TestGardenAutoReviewUnrelatedDecodeMatchesBase(t *testing.T) {
	for _, body := range []string{
		`{"prefs":{"garden_review":{"enabled":true,"account_id":"acc"}},"Prefs":{"garden_review":null}}`,
		`{"Prefs":{"garden_review":null},"prefs":{"garden_review":{"enabled":true,"account_id":"acc"}}}`,
		`{"prefs":{"garden_review":{"enabled":true,"account_id":"acc"}},"prefs":{"garden_review":null}}`,
		`{"prefs":{"theme":"first","garden_review":{"enabled":true,"account_id":"acc"}},"prefſ":{"theme":"last","garden_review":null}}`,
		`{"prefs":{"theme":"first","garden_review":{"enabled":true,"account_id":"acc"}},"Prefs":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			// This is the base decoder's shape, before GardenAutoReview existed.
			var base struct {
				Prefs struct {
					Theme        string             `json:"theme,omitempty"`
					GardenReview *GardenReviewPrefs `json:"garden_review,omitempty"`
				} `json:"prefs"`
			}
			if err := json.Unmarshal([]byte(body), &base); err != nil {
				t.Fatal(err)
			}
			path := stateEnv(t)
			writeState(t, path, body)
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if s.Prefs.Theme != base.Prefs.Theme || !reflect.DeepEqual(s.Prefs.GardenReview, base.Prefs.GardenReview) {
				t.Fatalf("unrelated decode changed: got %+v, base %+v", s.Prefs, base.Prefs)
			}
		})
	}
}

func TestGardenAutoReviewDuplicatesPreserveLastPolicy(t *testing.T) {
	for _, body := range []string{
		`{"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"first"}},"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"last"}}}`,
		`{"prefs":{"garden_auto_review":{"version":1,"enabled":true,"account":"first"},"garden_auto_review":{"version":1,"enabled":true,"account":"last"}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, body)
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if s.Prefs.GardenAutoReview != nil || !s.GardenAutoReviewPreserved() {
				t.Fatalf("duplicate accepted: %+v", s.Prefs)
			}
			p := s.PrefsSnapshot()
			p.Theme = "test"
			if err := s.SetPrefs(p); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Prefs struct {
					Theme            string                `json:"theme"`
					GardenAutoReview GardenAutoReviewPrefs `json:"garden_auto_review"`
				} `json:"prefs"`
			}
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Prefs.Theme != "test" || saved.Prefs.GardenAutoReview.Account != "last" {
				t.Fatalf("last duplicate lost: %s", data)
			}
		})
	}
}

func TestGardenAutoReviewCanonicalPolicies(t *testing.T) {
	for _, raw := range []string{
		`{"version":0,"enabled":false}`,
		`{"enabled":true, "account":"acc", "version":1}`,
		`{"version":1,"enabled":true,"account":"acc","full_access":false,"checks":[{"name":"test","executable":"go","args":["test"]}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			path := stateEnv(t)
			writeState(t, path, `{"prefs":{"garden_auto_review":`+raw+`}}`)
			s, err := LoadState()
			if err != nil {
				t.Fatal(err)
			}
			var want GardenAutoReviewPrefs
			if err := json.Unmarshal([]byte(raw), &want); err != nil {
				t.Fatal(err)
			}
			if s.GardenAutoReviewPreserved() || !reflect.DeepEqual(s.Prefs.GardenAutoReview, &want) {
				t.Fatalf("canonical policy rejected: %+v", s.Prefs)
			}
		})
	}
}

func TestParseGardenChecksRoundTripsThroughJSON(t *testing.T) {
	parsed, err := ParseGardenChecks("make; go test ./...")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded []GardenCheck
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, reloaded) {
		t.Fatalf("reloaded checks differ: %#v != %#v", reloaded, parsed)
	}
}
