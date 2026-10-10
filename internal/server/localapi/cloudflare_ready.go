package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// cloudflareReady checks the connector's active Cloudflare connections,
// rather than inferring connectivity from a running OS process. Cache its
// address per process run because its startup line can leave the log tail.
type cloudflareReady struct {
	client    *http.Client
	processID int
	startedAt time.Time
	url       string
}

func (r *cloudflareReady) check(ctx context.Context, process procstore.ServeProcess, log string) error {
	if r.processID != process.ID || !r.startedAt.Equal(process.StartedAt) {
		r.url = ""
		r.processID, r.startedAt = process.ID, process.StartedAt
	}
	if public, ok := webtunnel.CloudflaredReadyURL(log); ok {
		r.url = public
	}
	disconnected := func(reason string) error {
		if failure := webtunnel.CloudflaredFailure(log); failure != "" {
			reason = failure
		}
		return fmt.Errorf("Cloudflare tunnel is not connected: %s; run bonsai web restart tunnel", reason)
	}
	if r.url == "" {
		return disconnected("waiting for cloudflared's readiness endpoint")
	}
	if r.client == nil {
		r.client = &http.Client{
			Timeout:       2 * time.Second,
			Transport:     &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return disconnected(err.Error())
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return disconnected("readiness check failed: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return disconnected(fmt.Sprintf("readiness returned HTTP %d", resp.StatusCode))
	}
	var ready struct {
		Connections uint `json:"readyConnections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&ready); err != nil || ready.Connections == 0 {
		return disconnected("the connector has no active Cloudflare connections")
	}
	return nil
}
