package terminal

import (
	"testing"
)

func TestEnsureVisibleClampsBounds(t *testing.T) {
	m := New()
	m.SetSize(40, 10)
	m.SetContent("line 0\nline 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\nline 12\nline 13\nline 14\nline 15\nline 16\nline 17\nline 18\nline 19")

	// Negative line should not cause negative YOffset
	m.EnsureVisible(-5)
	if m.vp.YOffset < 0 {
		t.Errorf("expected YOffset >= 0, got %d", m.vp.YOffset)
	}

	// Line within visible window [0..9]
	m.EnsureVisible(5)
	if m.vp.YOffset != 0 {
		t.Errorf("expected YOffset 0 for visible line 5, got %d", m.vp.YOffset)
	}

	// Line beyond bottom window (e.g. line 15 with height 10 -> offset 6)
	m.EnsureVisible(15)
	if m.vp.YOffset != 6 {
		t.Errorf("expected YOffset 6, got %d", m.vp.YOffset)
	}

	// Scrolling back up
	m.EnsureVisible(2)
	if m.vp.YOffset != 2 {
		t.Errorf("expected YOffset 2, got %d", m.vp.YOffset)
	}

	// Scrolling to 0
	m.EnsureVisible(0)
	if m.vp.YOffset != 0 {
		t.Errorf("expected YOffset 0, got %d", m.vp.YOffset)
	}
}
