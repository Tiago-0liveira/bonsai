package pty

import "testing"

func TestNormalizeSize(t *testing.T) {
	cols, rows, err := NormalizeSize(0, 0)
	if err != nil || cols != DefaultCols || rows != DefaultRows {
		t.Fatalf("NormalizeSize(0,0) = %dx%d, %v", cols, rows, err)
	}
	if _, _, err := NormalizeSize(-1, 24); err == nil {
		t.Fatal("negative columns should fail")
	}
	if err := ValidateSize(MaxDimension+1, 24); err == nil {
		t.Fatal("oversized terminal should fail")
	}
}
