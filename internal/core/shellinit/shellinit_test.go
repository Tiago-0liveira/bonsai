package shellinit

import "testing"

func TestDepthFrom(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0}, {"abc", 0}, {"-3", 0}, {"0", 0}, {"2", 2},
		{" 4 ", 4}, {"99", 99}, {"100", 99}, {"99999999999999999999", 0},
	}
	for _, tc := range tests {
		if got := DepthFrom(tc.in); got != tc.want {
			t.Errorf("DepthFrom(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDepthReadsEnv(t *testing.T) {
	t.Setenv(EnvDepth, "3")
	if got := Depth(); got != 3 {
		t.Fatalf("Depth() = %d, want 3", got)
	}
}

func TestPrefix(t *testing.T) {
	tests := []struct {
		glyph string
		depth int
		want  string
	}{
		{"", 1, DefaultGlyph + "1 "},
		{"B", 12, "B:12 "},
		{"bonsai", 2, "bonsai:2 "},
		{"x9", 3, "x9:3 "},
		{"🪴", 2, "🪴2 "},
		{"🌳", 2, "🌳2 "},
	}
	for _, tc := range tests {
		if got := Prefix(tc.glyph, tc.depth); got != tc.want {
			t.Errorf("Prefix(%q,%d) = %q, want %q", tc.glyph, tc.depth, got, tc.want)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{
			"removes every listed key",
			[]string{"BONSAI_DEPTH=2", "BONSAI_PROMPT_PREFIX=x ", "BONSAI_ORIG_ZDOTDIR=/z", "BONSAI_ORIG_ENV=/e", "__BONSAI_PROMPT_INSTALLED=1"},
			[]string{},
		},
		{
			// Lookalikes must survive: only exact keys are ours.
			"keeps lookalikes and order",
			[]string{"A=1", "BONSAI_DEPTHX=1", "BONSAI_DEPTH=3", "BONSAI_REPO=x", "PATH=/bin"},
			[]string{"A=1", "BONSAI_DEPTHX=1", "BONSAI_REPO=x", "PATH=/bin"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ScrubEnv(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("ScrubEnv(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ScrubEnv(%q) = %q, want %q", tc.in, got, tc.want)
				}
			}
		})
	}
}

func TestPrefixUnsafeGlyphFallsBack(t *testing.T) {
	unsafe := []string{"\\u", "$(id)", "%F{red}", "`id`", `"`, "'", "!", "a\x1b[0m", "a\n", "a b", "​", "abcdefghijklmnopqrstuvwxyz"}
	for _, g := range unsafe {
		if got, want := Prefix(g, 2), "🪴2 "; got != want {
			t.Errorf("Prefix(%q) = %q, want %q", g, got, want)
		}
	}
	for _, g := range []string{"🪴", "🌳", "🌱", ">"} {
		if got, want := Prefix(g, 1), g+"1 "; got != want {
			t.Errorf("Prefix(%q) = %q, want %q", g, got, want)
		}
	}
	if got := Prefix("", 3); got != "🪴3 " {
		t.Errorf("empty = %q", got)
	}
}
