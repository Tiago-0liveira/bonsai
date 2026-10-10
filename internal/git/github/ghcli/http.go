package ghcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
)

// Shared is the process-wide in-process GitHub client state: one token
// source, one keep-alive HTTP/2 transport, one conditional-request cache and
// one rate-limit view, shared by every project.
type Shared struct {
	tokens *TokenSource
	base   http.RoundTripper
	cache  *etagCache
	rates  *rateTracker

	mu    sync.Mutex
	bases map[string]string
}

func NewShared() *Shared {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConnsPerHost = 16
	return &Shared{
		tokens: NewTokenSource(),
		base:   transport,
		cache:  newETagCache(etagCacheBytes),
		rates:  newRateTracker(),
		bases:  map[string]string{},
	}
}

// SetTokenRunner replaces `gh auth token` (tests).
func (s *Shared) SetTokenRunner(run TokenRunner) {
	s.tokens.mu.Lock()
	s.tokens.run = run
	s.tokens.entries = map[string]*tokenEntry{}
	s.tokens.mu.Unlock()
}

// SetAPIBase points host's REST API at baseURL (tests and GHES overrides).
func (s *Shared) SetAPIBase(host, baseURL string) {
	s.mu.Lock()
	s.bases[host] = strings.TrimRight(baseURL, "/")
	s.mu.Unlock()
}

func (s *Shared) apiBase(host string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if base, ok := s.bases[host]; ok {
		return base
	}
	if host == DefaultHost {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// tokenHost maps a request URL back to the gh host whose token it may carry.
// Only the exact scheme and authority of a known API base qualify, so the
// token never goes anywhere else.
func (s *Shared) tokenHost(u *url.URL) (string, bool) {
	s.mu.Lock()
	bases := make(map[string]string, len(s.bases)+1)
	for host, base := range s.bases {
		bases[host] = base
	}
	s.mu.Unlock()
	if _, ok := bases[DefaultHost]; !ok {
		bases[DefaultHost] = "https://api.github.com"
	}
	for host, base := range bases {
		b, err := url.Parse(base)
		if err == nil && strings.EqualFold(u.Scheme, b.Scheme) && strings.EqualFold(u.Host, b.Host) {
			return host, true
		}
	}
	if strings.EqualFold(u.Scheme, "https") && (strings.HasPrefix(u.Path, "/api/v3/") || u.Path == "/api/graphql") {
		return strings.ToLower(u.Hostname()), true
	}
	return "", false
}

// RateLimit returns the last core rate-limit window seen for host.
func (s *Shared) RateLimit(host string) (Rate, bool) { return s.rates.get(host) }

// AuthStatus reports whether the gh login of host can be used.
func (s *Shared) AuthStatus(ctx context.Context, host string) AuthStatus {
	return s.tokens.Status(ctx, host)
}

// Prewarm reads host's token and opens a connection to its API with
// GET /rate_limit, which does not count against the rate limit and seeds the
// rate-limit view. The API server runs it at startup so that the first browser
// connection does not wait on the gh process or a TLS handshake. It does
// nothing beyond the token read when gh has no login for host.
func (s *Shared) Prewarm(ctx context.Context, host string) {
	if _, err := s.tokens.Token(ctx, host); err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase(host)+"/rate_limit", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{
		Transport:     &httpTransport{shared: s},
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	drain(resp)
}

// Service returns a GitHub service for the repository checked out at dir. It
// talks to GitHub in-process and falls back to `gh api` only when gh cannot
// hand out a token.
func (s *Shared) Service(dir string) *Service {
	c := app.New(tokens{})
	c.BaseURL = s.apiBase(DefaultHost)
	c.HTTP = &http.Client{
		Transport:     &httpTransport{shared: s, cli: transport{Dir: dir}},
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Service{Client: c, shared: s, host: DefaultHost}
}

// DefaultHost is the only host repository identities resolve to today; see
// sanitizeRemoteIdentity in internal/git/local.
const DefaultHost = "github.com"

type httpTransport struct {
	shared *Shared
	cli    transport
}

func (t *httpTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host, ok := t.shared.tokenHost(r.URL)
	if !ok {
		closeBody(r)
		return nil, fmt.Errorf("refusing to send GitHub credentials to %s", r.URL.Host)
	}
	token, err := t.shared.tokens.Token(r.Context(), host)
	if errors.Is(err, errUseCLI) {
		if t.cli.Dir == "" {
			closeBody(r)
			return nil, err
		}
		return t.cli.RoundTrip(r)
	}
	if err != nil {
		closeBody(r)
		return nil, err
	}
	retry := r.GetBody != nil || r.Body == nil || r.Body == http.NoBody
	resp, err := t.shared.send(r, host, token)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || !retry {
		return resp, err
	}
	// The token may have been revoked or rotated: read it again, once.
	t.shared.tokens.Invalidate(host, token)
	next, err := t.shared.tokens.Token(r.Context(), host)
	if err != nil || next == token {
		return resp, nil
	}
	again := r.Clone(r.Context())
	if r.GetBody != nil {
		body, err := r.GetBody()
		if err != nil {
			return resp, nil
		}
		again.Body = body
	}
	drain(resp)
	return t.shared.send(again, host, next)
}

func (s *Shared) send(r *http.Request, host, token string) (*http.Response, error) {
	req := r.Clone(r.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	var cached *etagEntry
	key, tokenID := "", ""
	if req.Method == http.MethodGet {
		key = host + " " + req.URL.String() + " " + req.Header.Get("Accept")
		tokenID = fingerprint(token)
		if cached = s.cache.get(key, tokenID); cached != nil {
			if cached.etag != "" {
				req.Header.Set("If-None-Match", cached.etag)
			} else {
				req.Header.Set("If-Modified-Since", cached.lastModified)
			}
		}
	}
	trace.AddHTTP()
	resp, err := s.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	s.rates.observe(host, resp.Header)
	if resp.StatusCode == http.StatusNotModified && cached != nil {
		trace.AddNotModified()
		drain(resp)
		header := cached.header.Clone()
		header.Set(app.NotModifiedHeader, "1")
		for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-RateLimit-Resource"} {
			if value := resp.Header.Get(name); value != "" {
				header.Set(name, value)
			}
		}
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Proto:         resp.Proto,
			ProtoMajor:    resp.ProtoMajor,
			ProtoMinor:    resp.ProtoMinor,
			Header:        header,
			Body:          io.NopCloser(bytes.NewReader(cached.body)),
			ContentLength: int64(len(cached.body)),
			Request:       r,
		}, nil
	}
	etag, lastModified := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	if key == "" || resp.StatusCode != http.StatusOK || (etag == "" && lastModified == "") {
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, etagMaxEntryBytes+1))
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	if len(body) > etagMaxEntryBytes {
		// Too large to keep: hand the rest of the stream through unchanged.
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}
		return resp, nil
	}
	resp.Body.Close()
	header := http.Header{}
	for _, name := range []string{"Content-Type", "Link", "ETag", "Last-Modified"} {
		if value := resp.Header.Get(name); value != "" {
			header.Set(name, value)
		}
	}
	s.cache.put(&etagEntry{key: key, tokenID: tokenID, etag: etag, lastModified: lastModified, header: header, body: body})
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

func fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}

func closeBody(r *http.Request) {
	if r.Body != nil {
		r.Body.Close()
	}
}
