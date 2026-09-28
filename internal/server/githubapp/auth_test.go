package githubapp

import "testing"

func TestSecureCookiesFollowOriginScheme(t *testing.T) {
	if !(&Manager{Origin: "https://bonsai.example"}).secureCookies() {
		t.Fatal("HTTPS origin must use Secure cookies")
	}
	if (&Manager{Origin: "http://127.0.0.1:7003"}).secureCookies() {
		t.Fatal("loopback HTTP origin must allow local development cookies")
	}
}
