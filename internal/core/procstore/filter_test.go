package procstore_test

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestFilterLog(t *testing.T) {
	input := "line 1\nline 2: ERROR occurred\nline 3\nline 4: error found\nline 5\n"

	t.Run("tail", func(t *testing.T) {
		got := procstore.FilterLog(input, 2, "", false)
		want := "line 4: error found\nline 5\n"
		if got != want {
			t.Errorf("FilterLog tail = %q, want %q", got, want)
		}
	})

	t.Run("grep case sensitive", func(t *testing.T) {
		got := procstore.FilterLog(input, 0, "ERROR", false)
		want := "line 2: ERROR occurred\n"
		if got != want {
			t.Errorf("FilterLog grep = %q, want %q", got, want)
		}
	})

	t.Run("grep case insensitive", func(t *testing.T) {
		got := procstore.FilterLog(input, 0, "error", true)
		want := "line 2: ERROR occurred\nline 4: error found\n"
		if got != want {
			t.Errorf("FilterLog grep -i = %q, want %q", got, want)
		}
	})

	t.Run("tail and grep combined", func(t *testing.T) {
		got := procstore.FilterLog(input, 3, "error", true)
		want := "line 4: error found\n"
		if got != want {
			t.Errorf("FilterLog tail+grep = %q, want %q", got, want)
		}
	})
}
