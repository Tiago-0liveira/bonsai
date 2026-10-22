package gardenapi

import "testing"

func TestGateDropsStaleAndForeignReplies(t *testing.T) {
	stamp := func(local string, epoch, gen uint64) Stamp {
		return Stamp{LocalRepoID: local, GardenID: 7, Epoch: epoch, Generation: gen}
	}
	var g Gate
	steps := []struct {
		name string
		in   Stamp
		want bool
	}{
		{"first reply", stamp("clone-a", 1, 2), true},
		{"older generation", stamp("clone-a", 1, 1), false},
		{"repeated generation", stamp("clone-a", 1, 2), false},
		{"newer generation", stamp("clone-a", 1, 4), true},
		{"late reply from before", stamp("clone-a", 1, 3), false},
		{"other clone, newer generation", stamp("clone-b", 1, 9), false},
		{"older epoch, newer generation", stamp("clone-a", 0, 9), false},
		{"new epoch", stamp("clone-a", 2, 5), true},
		{"unscoped reply", Stamp{Generation: 99}, false},
	}
	for _, s := range steps {
		if got := g.Accept(s.in); got != s.want {
			t.Errorf("%s: Accept = %v, want %v", s.name, got, s.want)
		}
	}
	if g.Last().Generation != 5 {
		t.Fatalf("Last = %+v, want generation 5", g.Last())
	}
	g.Reset()
	if !g.Accept(stamp("clone-b", 1, 1)) {
		t.Fatal("Reset gate must accept the next clone")
	}
}

func TestAccessAndPresenceStrings(t *testing.T) {
	for a, want := range map[Access]string{AccessNone: "none", AccessRead: "read", AccessWrite: "write", AccessAdmin: "admin"} {
		if a.String() != want || a.CanSetup() != (a >= AccessWrite) {
			t.Errorf("Access %d = %q setup=%v", a, a.String(), a.CanSetup())
		}
	}
	for p, want := range map[Presence]string{PresenceUnknown: "unknown", PresencePresent: "present", PresenceAbsent: "absent"} {
		if p.String() != want {
			t.Errorf("Presence %d = %q", p, p.String())
		}
	}
}

func TestProjectCompatible(t *testing.T) {
	if (Project{Stage: FieldPresent, Type: FieldPresent}).Compatible() != true || (Project{Stage: FieldPresent}).Compatible() {
		t.Fatal("Compatible needs both fields")
	}
}
