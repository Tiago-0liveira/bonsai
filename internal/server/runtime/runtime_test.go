package runtime

import "testing"

func TestRequireBrowserOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:7003",
		"http://localhost:7003",
		"https://bonsai.example",
	} {
		if err := requireBrowserOrigin(origin); err != nil {
			t.Fatalf("%s: %v", origin, err)
		}
	}
	for _, origin := range []string{
		"http://example.com",
		"ftp://127.0.0.1:7003",
		"http://127.0.0.1:7003/path",
		"",
	} {
		if err := requireBrowserOrigin(origin); err == nil {
			t.Fatalf("%s: expected rejection", origin)
		}
	}
}
