package clipboard

import (
	"errors"
	"testing"
)

func TestPickFallbackOrder(t *testing.T) {
	found := func(bins ...string) lookPath {
		return func(name string) (string, error) {
			for _, b := range bins {
				if name == b {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}

	cases := []struct {
		name    string
		present []string
		wantBin string
		wantOK  bool
	}{
		{"pbcopy wins", []string{"pbcopy", "xclip"}, "pbcopy", true},
		{"wl-copy next", []string{"wl-copy", "xclip"}, "wl-copy", true},
		{"xclip before xsel", []string{"xsel", "xclip"}, "xclip", true},
		{"xsel last", []string{"xsel"}, "xsel", true},
		{"none", nil, "", false},
	}
	for _, c := range cases {
		got, ok := pick(found(c.present...))
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if ok && got.bin != c.wantBin {
			t.Errorf("%s: picked %q, want %q", c.name, got.bin, c.wantBin)
		}
	}
}

func TestPickArgsPreserved(t *testing.T) {
	got, ok := pick(func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", errors.New("not found")
	})
	if !ok || len(got.args) != 2 || got.args[0] != "-selection" {
		t.Errorf("xclip args lost: %+v", got)
	}
}
