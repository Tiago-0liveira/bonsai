package pkgmgr

import (
	"reflect"
	"testing"
)

func TestMergeCommandsDeterministicAndPreservesCollisions(t *testing.T) {
	src := Source{Kind: "test", File: "fixture", Line: 7}
	commands := mergeCommands(
		[]Command{{ID: "make:target:test", Name: "test", Provider: "make", Source: src, Confidence: ConfidenceHigh}},
		[]Command{{ID: "node:script:test", Name: "test", Provider: "node:pnpm", Source: src, Confidence: ConfidenceExact}},
		[]Command{{ID: "cargo:builtin:test", Name: "test", Provider: "cargo", Source: src, Confidence: ConfidenceExact}},
	)

	got := make([]string, 0, len(commands))
	for _, cmd := range commands {
		got = append(got, cmd.ID)
		if cmd.Source != src {
			t.Fatalf("source lost for %s: %+v", cmd.ID, cmd.Source)
		}
		if cmd.Confidence == "" {
			t.Fatalf("confidence lost for %s", cmd.ID)
		}
	}
	want := []string{"cargo:builtin:test", "make:target:test", "node:script:test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func TestMergeCommandsDuplicateIDLastWinsConsistently(t *testing.T) {
	got := mergeCommands(
		[]Command{{ID: "same", Name: "first", Provider: "x"}},
		[]Command{{ID: "same", Name: "second", Provider: "x"}},
	)
	if len(got) != 1 || got[0].Name != "second" {
		t.Fatalf("duplicate merge = %+v, want last command", got)
	}
}

func TestMergeCommandsStableOrdering(t *testing.T) {
	input := []Command{
		{ID: "b:2", Name: "z", Provider: "b"},
		{ID: "a:2", Name: "same", Provider: "a"},
		{ID: "a:1", Name: "same", Provider: "a"},
		{ID: "b:1", Name: "a", Provider: "b"},
	}
	first := mergeCommands(input)
	second := mergeCommands(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("merge order changed: first=%+v second=%+v", first, second)
	}
}
