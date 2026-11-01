package theme

import "testing"

func TestCheckGlyphDistinctWithoutColor(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		seen := map[string]bool{}
		for _, state := range []string{"pass", "fail", "pending", "unknown"} {
			g := CheckGlyph(state, ascii)
			if seen[g] {
				t.Fatalf("duplicate mark %q", g)
			}
			seen[g] = true
			if ascii && (len(g) != 1 || g[0] > 127) {
				t.Fatalf("non-ASCII mark %q", g)
			}
		}
		if CheckGlyph("cancel", ascii) != CheckGlyph("fail", ascii) {
			t.Fatal("cancel should indicate failure")
		}
	}
}
