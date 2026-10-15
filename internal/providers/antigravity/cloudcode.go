package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// agy reads its quota from Google's Cloud Code API: loadCodeAssist names the
// profile's project, its plan (paidTier.name) and its credit balance
// (paidTier.availableCredits), and retrieveUserQuotaSummary returns the groups and buckets `/usage` prints. Two HTTPS calls are much faster than starting agy,
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
	// cloudCodeHedge is how long a host may take before the next one is asked
	// too. The daily host sometimes answers in 3 s; production does not.
	cloudCodeHedge = 800 * time.Millisecond
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

// projectCache remembers each profile's Cloud Code project, so the quota read
// starts with loadCodeAssist instead of waiting for it. A failed quota read
// forgets it.
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

// cloudCodeRead is what one Cloud Code read returns: the quota buckets and what
// loadCodeAssist named, the plan ("" when it did not answer) and the credit
// balance (nil then).
type cloudCodeRead struct {
	limits  []agents.UsageLimit
	plan    string
	credits []agents.UsageCredit
}

// cloudCodeUsage reads the quota buckets and the plan from the Cloud Code API.
// The first host is asked first; the next is asked when it fails or has not
// answered within cloudCodeHedge, and the first answer wins.
func (p *Provider) cloudCodeUsage(ctx context.Context, account agents.Account) (cloudCodeRead, error) {
	token, err := p.accessToken(account)
	if err != nil {
		return cloudCodeRead{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		read cloudCodeRead
		err  error
	}
	answers := make(chan answer, len(cloudCodeHosts))
	ask := func(host string) {
		hostCtx, cancelHost := context.WithTimeout(ctx, cloudCodeTimeout)
		defer cancelHost()
		read, err := p.quotaSummary(hostCtx, host, token, account.ID)
		answers <- answer{read, err}
	}
	var errs []error
	next, pending := 0, 0
	for {
		if next < len(cloudCodeHosts) {
			go ask(cloudCodeHosts[next])
			next++
			pending++
		}
		hedge := time.NewTimer(cloudCodeHedge)
		select {
		case a := <-answers:
			hedge.Stop()
			pending--
			if a.err == nil {
				return a.read, nil
			}
			errs = append(errs, a.err)
			if ctx.Err() != nil || (next >= len(cloudCodeHosts) && pending == 0) {
				return cloudCodeRead{}, errors.Join(errs...)
			}
			if next >= len(cloudCodeHosts) {
				// Only a hedged host is left: wait for it.
				a := <-answers
				pending--
				if a.err == nil {
					return a.read, nil
				}
				return cloudCodeRead{}, errors.Join(append(errs, a.err)...)
			}
		case <-hedge.C:
			if next >= len(cloudCodeHosts) {
				// Nothing left to hedge with: wait for the pending hosts.
				for pending > 0 {
					a := <-answers
					pending--
					if a.err == nil {
						return a.read, nil
					}
					errs = append(errs, a.err)
				}
				return cloudCodeRead{}, errors.Join(errs...)
			}
		case <-ctx.Done():
			hedge.Stop()
			return cloudCodeRead{}, errors.Join(append(errs, ctx.Err())...)
		}
	}
}

// codeAssist is what loadCodeAssist answers that Bonsai uses.
type codeAssist struct {
	project string
	plan    string
	credits []agents.UsageCredit
	err     error
}

// creditTypeGoogleOneAI is the credit type agy spends: Google One AI credits.
const creditTypeGoogleOneAI = "GOOGLE_ONE_AI"

// loadCodeAssist names the profile's project, plan and credits. The plan is
// paidTier's name, or currentTier's when the account has no paid tier. The
// balance is the sum of paidTier.availableCredits; the API leaves the list out
// when the account has none, and a balance of 0 is not shown.
func loadCodeAssist(ctx context.Context, host, token string) codeAssist {
	body, err := cloudCodeCall(ctx, host, "loadCodeAssist", token, map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY"}})
	if err != nil {
		return codeAssist{err: err}
	}
	var info struct {
		Project  string `json:"cloudaicompanionProject"`
		PaidTier struct {
			Name             string `json:"name"`
			AvailableCredits []struct {
				CreditType   string `json:"creditType"`
				CreditAmount string `json:"creditAmount"` // int64 as a string
			} `json:"availableCredits"`
		} `json:"paidTier"`
		CurrentTier struct{ Name string } `json:"currentTier"`
	}
	if json.Unmarshal(body, &info) != nil || strings.TrimSpace(info.Project) == "" {
		return codeAssist{err: fmt.Errorf("antigravity loadCodeAssist: no project")}
	}
	plan := cleanPlan(info.PaidTier.Name)
	if plan == "" {
		plan = cleanPlan(info.CurrentTier.Name)
	}
	var balance float64
	for _, credit := range info.PaidTier.AvailableCredits {
		if credit.CreditType != creditTypeGoogleOneAI {
			continue
		}
		if amount, err := strconv.ParseInt(strings.TrimSpace(credit.CreditAmount), 10, 64); err == nil && amount > 0 {
			balance += float64(amount)
		}
	}
	assist := codeAssist{project: strings.TrimSpace(info.Project), plan: plan}
	if balance > 0 {
		assist.credits = []agents.UsageCredit{{ID: "antigravity_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: &balance, Enabled: true}}
	}
	return assist
}

// quotaSummary reads the quota and the plan from one host. loadCodeAssist
// runs beside the quota call when the project is remembered; a failed
// loadCodeAssist then only leaves the plan out.
func (p *Provider) quotaSummary(ctx context.Context, host, token string, id agents.AccountID) (cloudCodeRead, error) {
	project := p.projects.get(id)
	loaded := make(chan codeAssist, 1)
	go func() { loaded <- loadCodeAssist(ctx, host, token) }()
	var assist codeAssist
	received := false
	if project == "" {
		assist, received = <-loaded, true
		if assist.err != nil {
			return cloudCodeRead{}, assist.err
		}
		project = assist.project
	}
	body, err := cloudCodeCall(ctx, host, "retrieveUserQuotaSummary", token, map[string]string{"project": project})
	if err != nil {
		p.projects.set(id, "")
		return cloudCodeRead{}, err
	}
	limits, err := ParseUsage(body)
	if err != nil {
		p.projects.set(id, "")
		return cloudCodeRead{}, err
	}
	if len(limits) == 0 {
		p.projects.set(id, "")
		return cloudCodeRead{}, fmt.Errorf("antigravity retrieveUserQuotaSummary: no quota buckets")
	}
	if !received {
		assist = <-loaded
	}
	if assist.err == nil {
		project = assist.project
	}
	p.projects.set(id, project)
	return cloudCodeRead{limits: limits, plan: assist.plan, credits: assist.credits}, nil
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
