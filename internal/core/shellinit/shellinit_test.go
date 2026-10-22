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
		{"B", 12, "B12 "},
	}
	for _, tc := range tests {
		if got := Prefix(tc.glyph, tc.depth); got != tc.want {
			t.Errorf("Prefix(%q,%d) = %q, want %q", tc.glyph, tc.depth, got, tc.want)
		}
	}
}
