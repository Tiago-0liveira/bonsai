package pty

import (
	"io"
	"testing"
)

type resizeProbeSession struct {
	calls int
	cols  int
	rows  int
}

func (s *resizeProbeSession) Read([]byte) (int, error) { return 0, io.EOF }
func (s *resizeProbeSession) Write(p []byte) (int, error) { return len(p), nil }
func (s *resizeProbeSession) Resize(cols, rows int) error {
	s.calls++
	s.cols, s.rows = cols, rows
	return nil
}
func (s *resizeProbeSession) Size() (int, int, error) { return s.cols, s.rows, nil }
func (s *resizeProbeSession) Close() error { return nil }

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

func TestValidatingSessionRejectsZeroColumns(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(0, 24); err == nil {
		t.Fatal("zero columns should be rejected")
	}
	if probe.calls != 0 {
		t.Fatalf("backend resize calls = %d, want 0", probe.calls)
	}
}

func TestValidatingSessionRejectsZeroRows(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(80, 0); err == nil {
		t.Fatal("zero rows should be rejected")
	}
	if probe.calls != 0 {
		t.Fatalf("backend resize calls = %d, want 0", probe.calls)
	}
}

func TestValidatingSessionRejectsNegativeDimensions(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(-1, 24); err == nil {
		t.Fatal("negative columns should be rejected")
	}
	if probe.calls != 0 {
		t.Fatalf("backend resize calls = %d, want 0", probe.calls)
	}
}

func TestValidatingSessionRejectsOversizedDimensions(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(80, MaxDimension+1); err == nil {
		t.Fatal("oversized rows should be rejected")
	}
	if probe.calls != 0 {
		t.Fatalf("backend resize calls = %d, want 0", probe.calls)
	}
}

func TestValidatingSessionForwardsValidResizeExactlyOnce(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(132, 52); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 1 || probe.cols != 132 || probe.rows != 52 {
		t.Fatalf("backend resize = calls=%d size=%dx%d, want calls=1 size=132x52", probe.calls, probe.cols, probe.rows)
	}
}

func TestValidatingSessionAcceptsMaximumDimensions(t *testing.T) {
	probe := &resizeProbeSession{}
	s := &validatingSession{Session: probe}
	if err := s.Resize(MaxDimension, MaxDimension); err != nil {
		t.Fatalf("maximum valid dimensions rejected: %v", err)
	}
	if probe.calls != 1 || probe.cols != MaxDimension || probe.rows != MaxDimension {
		t.Fatalf("backend resize = calls=%d size=%dx%d", probe.calls, probe.cols, probe.rows)
	}
}
