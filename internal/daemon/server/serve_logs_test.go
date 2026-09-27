package server

import "testing"

func TestFilterServeLog(t *testing.T) {
	data := "[10:00:00] api OUT ready\n[10:00:01] webhook ERR denied\n[10:00:02] api OUT accepted\n"
	if got := filterServeLog(data, "api", "accept", false); got != "[10:00:02] api OUT accepted\n" {
		t.Fatalf("filtered = %q", got)
	}
	if got := filterServeLog(data, "webhook", "DENIED", true); got != "[10:00:01] webhook ERR denied\n" {
		t.Fatalf("case-insensitive = %q", got)
	}
}
