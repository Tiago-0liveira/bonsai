package drafts

import (
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

func TestAssess(t *testing.T) {
	full := Draft{Title: "T", Spec: spec.Spec{CurrentBehavior: "p", DesiredBehavior: "o", AcceptanceCriteria: "c"}}
	with := func(f func(*Draft)) Draft { d := full; f(&d); return d }
	for _, tc := range []struct {
		name       string
		d          Draft
		missing    []string
		unresolved int
	}{
		{"ready", full, nil, 0},
		{"empty", Draft{}, []string{"Title", "Problem", "Outcome", "Criteria"}, 0},
		{"blank title", with(func(d *Draft) { d.Title = "  " }), []string{"Title"}, 0},
		{"no outcome", with(func(d *Draft) { d.Spec.DesiredBehavior = "" }), []string{"Outcome"}, 0},
		{"none", with(func(d *Draft) { d.Spec.OpenQuestions = "- None" }), nil, 0},
		{"two open", with(func(d *Draft) {
			d.Spec.OpenQuestions = "- Which API?\n\n1. Who owns it?\n* [x] Done one\n- Resolved: yes"
		}), nil, 2},
		{"bullet only", with(func(d *Draft) { d.Spec.OpenQuestions = "•\n-" }), nil, 0},
		{"checked upper", with(func(d *Draft) { d.Spec.OpenQuestions = "[X] answered" }), nil, 0},
	} {
		r := Assess(tc.d)
		if !reflect.DeepEqual(r.Missing, tc.missing) || r.Unresolved != tc.unresolved {
			t.Errorf("%s: %+v", tc.name, r)
		}
		if r.Ready() != (len(tc.missing) == 0 && tc.unresolved == 0) {
			t.Errorf("%s: Ready = %v", tc.name, r.Ready())
		}
	}
}

func TestDraftLabel(t *testing.T) {
	for in, want := range map[ID]string{"draft-1a2b3c4d5e": "D-1A2B3C", "draft-ab": "D-AB", "xyz1234567": "D-XYZ123"} {
		if got := DraftLabel(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}
