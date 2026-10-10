package webtunnel

import (
	"strings"
	"testing"
)

func TestCloudflaredReadyURLUsesOnlyLoopback(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"127.0.0.1:20241", "http://127.0.0.1:20241/ready"},
		{"localhost:20242", "http://127.0.0.1:20242/ready"},
		{"0.0.0.0:20241", "http://127.0.0.1:20241/ready"},
		{"[::1]:20241", "http://[::1]:20241/ready"},
		{"[::]:20241", "http://[::1]:20241/ready"},
		{"example.com:20241", ""},
		{"192.168.1.1:20241", ""},
		{"127.0.0.1:99999", ""},
		{"127.0.0.1:0", ""},
	} {
		got, ok := CloudflaredReadyURL("INF Starting metrics server on " + tc.address + "/metrics")
		if got != tc.want || ok != (tc.want != "") {
			t.Fatalf("%s: %s, %v", tc.address, got, ok)
		}
	}
}

func TestCloudflaredFailureClearsAfterReconnection(t *testing.T) {
	log := "INF Registered tunnel connection connIndex=0\n" +
		"ERR Register tunnel error error=\"Unauthorized: Tunnel not found\" connIndex=0\n"
	if got := CloudflaredFailure(log); got != "Unauthorized: Tunnel not found" {
		t.Fatalf("failure = %q", got)
	}
	log += "INF Registered tunnel connection connIndex=0\n"
	if got := CloudflaredFailure(log); got != "" {
		t.Fatalf("old authentication failure survived recovery: %s", got)
	}
	if CloudflaredPermanentError("waiting for the readiness endpoint") || CloudflaredPermanentError("connection refused") || !CloudflaredPermanentError(strings.ToUpper("Unauthorized: Tunnel not found")) {
		t.Fatal("transient connection failures must remain retryable")
	}
}
