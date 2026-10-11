package localapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

type cloudflareRoundTrip func(*http.Request) (*http.Response, error)

func (f cloudflareRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCloudflareReadinessChecksConnectionAndShowsAuthenticationFailure(t *testing.T) {
	process := procstore.ServeProcess{ID: 18, StartedAt: time.Now(), State: "running"}
	log := "INF Starting metrics server on 127.0.0.1:20241/metrics\n" +
		"ERR Register tunnel error from server side error=\"Unauthorized: Tunnel not found\" connIndex=0\n"
	for _, tc := range []struct {
		name      string
		code      int
		body      string
		err       error
		wantError bool
	}{
		{"disconnected", 503, `{"readyConnections":0}`, nil, true},
		{"connected", 200, `{"readyConnections":1}`, nil, false},
		{"zero connections", 200, `{"readyConnections":0}`, nil, true},
		{"invalid response", 200, `invalid`, nil, true},
		{"unreachable", 0, "", errors.New("connection refused"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &cloudflareReady{client: &http.Client{Transport: cloudflareRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1:20241/ready" {
					t.Fatalf("readiness URL = %s", req.URL)
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}}
			err := probe.check(context.Background(), process, log)
			if (err != nil) != tc.wantError {
				t.Fatalf("readiness = %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "Unauthorized: Tunnel not found") {
				t.Fatalf("lost Cloudflare's reason: %v", err)
			}
		})
	}
}

func TestCloudflareReadinessCacheBelongsToOneProcessRun(t *testing.T) {
	calls := 0
	probe := &cloudflareReady{client: &http.Client{Transport: cloudflareRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"readyConnections":4}`)), Header: make(http.Header)}, nil
	})}}
	process := procstore.ServeProcess{ID: 18, StartedAt: time.Now()}
	if err := probe.check(context.Background(), process, "Starting metrics server on 127.0.0.1:20241/metrics"); err != nil {
		t.Fatal(err)
	}
	// The log tail can lose the startup line while the same connector runs.
	if err := probe.check(context.Background(), process, "Registered tunnel connection connIndex=0"); err != nil || calls != 2 {
		t.Fatalf("same run: %v, calls %d", err, calls)
	}
	// Daemon restarts retain the process record ID, but change StartedAt.
	process.StartedAt = process.StartedAt.Add(time.Second)
	if err := probe.check(context.Background(), process, "Starting tunnel"); err == nil || calls != 2 {
		t.Fatalf("probed the predecessor's endpoint: %v, calls %d", err, calls)
	}
	if err := probe.check(context.Background(), process, "Starting metrics server on 127.0.0.1:20242/metrics"); err != nil || calls != 3 || probe.url != "http://127.0.0.1:20242/ready" {
		t.Fatalf("new run: %v, calls %d, url %s", err, calls, probe.url)
	}
}

func TestCloudflareReadinessRefusesRedirects(t *testing.T) {
	probe := &cloudflareReady{}
	process := procstore.ServeProcess{ID: 18, StartedAt: time.Now()}
	// Initialize the real client's policy without opening a socket.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = probe.check(ctx, process, "Starting metrics server on 127.0.0.1:20241/metrics")
	if probe.client.CheckRedirect == nil || probe.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("readiness requests must not follow redirects outside loopback")
	}
}
