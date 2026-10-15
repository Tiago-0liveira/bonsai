package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// agy reads its quota from Google's Cloud Code API: loadCodeAssist names the
// profile's project and retrieveUserQuotaSummary returns the groups and
// buckets `/usage` prints. Two HTTPS calls are much faster than starting agy,
// so usage asks them first and falls back to `/usage` when they fail. Both
// are internal endpoints and can change or disappear without notice.
//
// Bonsai only ever reads the stored access token. It never refreshes it: a
// token close to expiry is left to agy, which refreshes it on the fallback run
// and saves it back to the vault.

// cloudCodeHosts are tried in order: the daily host agy itself talks to, then
// production. Tests point them at a local server so no test reaches the network.
var cloudCodeHosts = []string{
	"https://daily-cloudcode-pa.googleapis.com",
	"https://cloudcode-pa.googleapis.com",
}

const (
	// cloudCodeTimeout bounds the read from one host, so a host that hangs
	// leaves time for the next before the fallback starts agy.
	cloudCodeTimeout = 3 * time.Second
	// tokenMargin: a token that expires sooner than this is not used.
	tokenMargin = time.Minute
	// maxCloudCodeBody caps what is read from a response.
	maxCloudCodeBody = 1 << 20
	// cloudCodeUserAgent is required: without it loadCodeAssist answers that
	// the client is unsupported and names no project.
	cloudCodeUserAgent = "antigravity"
)

var errNoAccessToken = errors.New("antigravity access token missing or expiring")

var cloudCodeClient = &http.Client{}

// projectCache remembers each profile's Cloud Code project, so a read is one
// call instead of two. A failed quota read forgets it.
type projectCache struct {
	mu       sync.Mutex
	projects map[agents.AccountID]string
}

func (c *projectCache) get(id agents.AccountID) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.projects[id]
}

func (c *projectCache) set(id agents.AccountID, project string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if project == "" {
		delete(c.projects, id)
		return
	}
	if c.projects == nil {
		c.projects = map[agents.AccountID]string{}
	}
	c.projects[id] = project
}

// accessToken is the stored access token, unless it is missing or expires
// within tokenMargin.
func (p *Provider) accessToken(account agents.Account) (string, error) {
	if p.accounts == nil {
		return "", errNoAccessToken
	}
	vault, err := (&fileCredentialManager{accounts: p.accounts}).readVault(account)
	if err != nil {
		return "", err
	}
	var oauth struct {
		Token struct {
			AccessToken string    `json:"access_token"`
			Expiry      time.Time `json:"expiry"`
		} `json:"token"`
	}
	if err := json.Unmarshal(vault.OAuth, &oauth); err != nil {
		return "", errNoAccessToken
	}
	token := strings.TrimSpace(oauth.Token.AccessToken)
	if token == "" || p.clock().Add(tokenMargin).After(oauth.Token.Expiry) {
		return "", errNoAccessToken
	}
	return token, nil
}

// cloudCodeUsage reads the quota buckets from the Cloud Code API, trying each
// host until one answers.
func (p *Provider) cloudCodeUsage(ctx context.Context, account agents.Account) ([]agents.UsageLimit, error) {
	token, err := p.accessToken(account)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, host := range cloudCodeHosts {
		hostCtx, cancel := context.WithTimeout(ctx, cloudCodeTimeout)
		limits, err := p.quotaSummary(hostCtx, host, token, account.ID)
		cancel()
		if err == nil {
			return limits, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	if len(errs) == 0 {
		return nil, errors.New("antigravity: no Cloud Code host")
	}
	return nil, errors.Join(errs...)
}

func (p *Provider) quotaSummary(ctx context.Context, host, token string, id agents.AccountID) ([]agents.UsageLimit, error) {
	project := p.projects.get(id)
	if project == "" {
		var info struct {
			Project string `json:"cloudaicompanionProject"`
		}
		body, err := cloudCodeCall(ctx, host, "loadCodeAssist", token, map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY"}})
		if err != nil {
			return nil, err
		}
		if json.Unmarshal(body, &info) != nil || strings.TrimSpace(info.Project) == "" {
			return nil, fmt.Errorf("antigravity loadCodeAssist: no project")
		}
		project = strings.TrimSpace(info.Project)
	}
	body, err := cloudCodeCall(ctx, host, "retrieveUserQuotaSummary", token, map[string]string{"project": project})
	if err != nil {
		p.projects.set(id, "")
		return nil, err
	}
	limits, err := ParseUsage(body)
	if err != nil {
		p.projects.set(id, "")
		return nil, err
	}
	if len(limits) == 0 {
		p.projects.set(id, "")
		return nil, fmt.Errorf("antigravity retrieveUserQuotaSummary: no quota buckets")
	}
	p.projects.set(id, project)
	return limits, nil
}

// cloudCodeCall POSTs one v1internal method and returns the response body.
// Errors name only the method and status: a response may carry account details.
func cloudCodeCall(ctx context.Context, host, method, token string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/v1internal:"+method, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cloudCodeUserAgent)
	resp, err := cloudCodeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("antigravity %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCloudCodeBody))
	if err != nil {
		return nil, fmt.Errorf("antigravity %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity %s: HTTP %d", method, resp.StatusCode)
	}
	return body, nil
}
