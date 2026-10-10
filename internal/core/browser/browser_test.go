package browser

import (
	"reflect"
	"testing"
)

func TestCommandsPerPlatform(t *testing.T) {
	const u = "https://app.bonsai.dev/app"
	cases := []struct {
		goos string
		wsl  bool
		want [][]string
	}{
		{"linux", false, [][]string{{"xdg-open", u}}},
		{"darwin", false, [][]string{{"open", u}}},
		{"windows", false, [][]string{{"rundll32", "url.dll,FileProtocolHandler", u}}},
		{"linux", true, [][]string{{"wslview", u}, {"cmd.exe", "/c", "start", "", u}, {"xdg-open", u}}},
	}
	for _, c := range cases {
		if got := Commands(c.goos, c.wsl, u); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("Commands(%s, wsl=%v) = %v, want %v", c.goos, c.wsl, got, c.want)
		}
	}
}

func TestCommandsSkipCmdForMetacharacters(t *testing.T) {
	got := Commands("linux", true, "http://127.0.0.1:7001/app?a=1&b=2")
	for _, argv := range got {
		if argv[0] == "cmd.exe" {
			t.Fatalf("cmd.exe offered for a URL with metacharacters: %v", got)
		}
	}
}

func TestOpenRejectsNonHTTP(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "calc.exe", "http://"} {
		if err := Open(u); err == nil {
			t.Fatalf("Open(%q) accepted", u)
		}
	}
}
