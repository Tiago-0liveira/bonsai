//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package agentterminal

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestChildEnvironment(t *testing.T) {
	cases := []struct {
		name     string
		base     []string
		prepared agents.PreparedSession
		want     []string
		absent   []string
	}{
		{
			name: "unset TERM gets default and COLORTERM added",
			base: []string{"PATH=/bin"},
			want: []string{"COLORTERM=truecolor", "TERM=xterm-256color", "PATH=/bin"},
		},
		{
			name: "dumb TERM replaced",
			base: []string{"TERM=dumb"},
			want: []string{"COLORTERM=truecolor", "TERM=xterm-256color"},
		},
		{
			name: "empty TERM replaced",
			base: []string{"TERM="},
			want: []string{"TERM=xterm-256color"},
		},
		{
			name: "other TERM kept, inherited COLORTERM overridden",
			base: []string{"TERM=screen-256color", "COLORTERM=24bit"},
			want: []string{"TERM=screen-256color", "COLORTERM=truecolor"},
		},
		{
			name:   "force vars removed",
			base:   []string{"TERM=xterm", "FORCE_COLOR=1", "CLICOLOR_FORCE=1", "CLICOLOR=1"},
			want:   []string{"CLICOLOR=1", "COLORTERM=truecolor"},
			absent: []string{"FORCE_COLOR=1", "CLICOLOR_FORCE=1"},
		},
		{
			name: "NO_COLOR kept untouched",
			base: []string{"NO_COLOR=1", "TERM=xterm"},
			want: []string{"NO_COLOR=1"},
		},
		{
			name:     "EnvSet wins over defaults",
			base:     []string{"FORCE_COLOR=1", "TERM=dumb"},
			prepared: agents.PreparedSession{EnvSet: map[string]string{"TERM": "vt100", "COLORTERM": "yes", "FORCE_COLOR": "3"}},
			want:     []string{"TERM=vt100", "COLORTERM=yes", "FORCE_COLOR=3"},
		},
		{
			name:     "EnvUnset still applies",
			base:     []string{"TERM=xterm", "SECRET=1"},
			prepared: agents.PreparedSession{EnvUnset: []string{"SECRET", "COLORTERM"}},
			absent:   []string{"SECRET=1", "COLORTERM=truecolor"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := childEnvironment(tc.prepared, tc.base)
			for _, want := range tc.want {
				if !slices.Contains(got, want) {
					t.Errorf("missing %q in %v", want, got)
				}
			}
			for _, absent := range tc.absent {
				if slices.Contains(got, absent) {
					t.Errorf("unexpected %q in %v", absent, got)
				}
			}
		})
	}
}

func TestPTYChildSeesTerminalColorEnvironment(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "1")
	m, _, account, _ := testManager(t, `printf 'ENV:%s:%s:%s:%s:%s\n' "$TERM" "$COLORTERM" "${FORCE_COLOR-unset}" "${CLICOLOR_FORCE-unset}" "$NO_COLOR"`)
	s, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, _, replay, frames, err := m.Attach("project", s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	done := false
	for _, f := range replay {
		output.Write(f.Data)
		done = done || (f.Type == "status" && f.Status != nil && !f.Status.Active())
	}
	timeout := time.After(5 * time.Second)
	for !done {
		select {
		case f := <-frames:
			output.Write(f.Data)
			done = f.Type == "status" && f.Status != nil && !f.Status.Active()
		case <-timeout:
			t.Fatalf("no final status; output %q", output.String())
		}
	}
	if !strings.Contains(output.String(), "ENV:xterm-256color:truecolor:unset:unset:1") {
		t.Fatalf("unexpected child environment: %q", output.String())
	}
}
