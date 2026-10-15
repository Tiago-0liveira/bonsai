package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// cloudCodeServer stands in for the Cloud Code API. status, when set, is the
// answer to every call.
type cloudCodeServer struct {
	t      *testing.T
	mu     sync.Mutex
	status int
	calls  map[string]int
	// loadBody, when set, is the loadCodeAssist answer; loadStatus, when set,
	// its status.
	loadBody   string
	loadStatus int
}

func newCloudCodeServer(t *testing.T) (*cloudCodeServer, string) {
	t.Helper()
	s := &cloudCodeServer{t: t, calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls[r.URL.Path]++
		status, loadBody, loadStatus := s.status, s.loadBody, s.loadStatus
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer live-token" || r.Header.Get("User-Agent") != cloudCodeUserAgent {
			t.Errorf("headers = %v", r.Header)
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			if loadStatus != 0 {
				w.WriteHeader(loadStatus)
				return
			}
			if loadBody == "" {
				loadBody = `{"cloudaicompanionProject":"proj-1","paidTier":{"name":"Google AI Pro","availableCredits":[{"creditType":"GOOGLE_ONE_AI","creditAmount":"250"}]}}`
			}
			_, _ = w.Write([]byte(loadBody))
		case "/v1internal:retrieveUserQuotaSummary":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["project"] != "proj-1" {
				t.Errorf("summary body = %v", body)
			}
			_, _ = w.Write([]byte(`{"groups":[{"displayName":"Gemini Models","buckets":[` +
				`{"bucketId":"gemini-weekly","displayName":"Weekly Limit Remaining","window":"weekly","resetTime":"2026-10-22T15:24:57Z","remainingFraction":0.5},` +
				`{"bucketId":"gemini-5h","displayName":"Five Hour Limit Remaining","window":"5h","resetTime":"2026-10-15T20:24:57Z","remainingFraction":1}]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *cloudCodeServer) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls["/v1internal:"+method]
}

func useCloudCodeHosts(t *testing.T, hosts ...string) {
	t.Helper()
	old := cloudCodeHosts
	cloudCodeHosts = hosts
	t.Cleanup(func() { cloudCodeHosts = old })
}

// signInToken stores a login whose access token expires at expiry.
func (e *accountEnv) signInToken(t *testing.T, email string, expiry time.Time) {
	t.Helper()
	oauth, _ := json.Marshal(map[string]any{"token": map[string]any{"access_token": "live-token", "refresh_token": "r", "expiry": expiry.Format(time.RFC3339Nano)}})
	vault := credentialVault{Version: 1, Identity: email, Generation: 1, OAuth: oauth}
	if err := (&fileCredentialManager{accounts: e.accounts}).writeVault(e.account, vault); err != nil {
		t.Fatal(err)
	}
}

func TestUsageReadsCloudCodeWithoutStartingAgy(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 2 || snapshot.Limits[0].ID != "gemini-weekly" || *snapshot.Limits[0].RemainingFraction != 0.5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Limits[0].Window != "weekly" || snapshot.Limits[0].Group != "Gemini Models" || snapshot.Limits[0].ResetsAt == nil {
		t.Fatalf("limit = %+v", snapshot.Limits[0])
	}
	if len(snapshot.Credits) != 1 || *snapshot.Credits[0].Balance != 250 || snapshot.Credits[0].Unit != agents.CreditUnitCredits || snapshot.Plan != "Google AI Pro" {
		t.Fatalf("credits=%+v plan=%q", snapshot.Credits, snapshot.Plan)
	}
	// Every read asks loadCodeAssist again: it answers the plan and the credits.
	if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	if server.count("loadCodeAssist") != 2 || server.count("retrieveUserQuotaSummary") != 2 {
		t.Fatalf("calls = %v", server.calls)
	}
	if e.runs.Load() != 0 {
		t.Fatalf("agy ran %d times", e.runs.Load())
	}
}

func TestCreditsComeFromAvailableCredits(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want float64
	}{
		"none":         {`{"cloudaicompanionProject":"proj-1","paidTier":{"name":"Google AI Pro"}}`, 0},
		"one":          {`{"cloudaicompanionProject":"proj-1","paidTier":{"availableCredits":[{"creditType":"GOOGLE_ONE_AI","creditAmount":"1500"}]}}`, 1500},
		"summed":       {`{"cloudaicompanionProject":"proj-1","paidTier":{"availableCredits":[{"creditType":"GOOGLE_ONE_AI","creditAmount":"100"},{"creditType":"GOOGLE_ONE_AI","creditAmount":"20"}]}}`, 120},
		"other type":   {`{"cloudaicompanionProject":"proj-1","paidTier":{"availableCredits":[{"creditType":"CREDIT_TYPE_UNSPECIFIED","creditAmount":"9"}]}}`, 0},
		"bad amounts":  {`{"cloudaicompanionProject":"proj-1","paidTier":{"availableCredits":[{"creditType":"GOOGLE_ONE_AI","creditAmount":"x"},{"creditType":"GOOGLE_ONE_AI","creditAmount":"-5"}]}}`, 0},
		"no paid tier": {`{"cloudaicompanionProject":"proj-1","currentTier":{"name":"Antigravity"}}`, 0},
	} {
		t.Run(name, func(t *testing.T) {
			e := newAccountEnv(t)
			server, url := newCloudCodeServer(t)
			server.loadBody = tc.body
			useCloudCodeHosts(t, url)
			e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))
			snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == 0 && len(snapshot.Credits) != 0:
				t.Fatalf("a zero balance is shown: %+v", snapshot.Credits)
			case tc.want != 0 && (len(snapshot.Credits) != 1 || *snapshot.Credits[0].Balance != tc.want):
				t.Fatalf("credits = %+v, want %v", snapshot.Credits, tc.want)
			}
		})
	}
}

func TestUsageAsksTheNextHostWhenTheFirstIsSlow(t *testing.T) {
	e := newAccountEnv(t)
	_, fastURL := newCloudCodeServer(t)
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close does not wait
	useCloudCodeHosts(t, slow.URL, fastURL)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	start := time.Now()
	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if took := time.Since(start); took > cloudCodeTimeout/2 {
		t.Fatalf("took %v: waited for the slow host", took)
	}
	if e.runs.Load() != 0 {
		t.Fatalf("agy ran %d times", e.runs.Load())
	}
}

func TestUsageTriesTheNextCloudCodeHost(t *testing.T) {
	e := newAccountEnv(t)
	down, downURL := newCloudCodeServer(t)
	down.status = http.StatusServiceUnavailable
	up, upURL := newCloudCodeServer(t)
	useCloudCodeHosts(t, downURL, upURL)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if down.count("loadCodeAssist") != 1 || up.count("retrieveUserQuotaSummary") != 1 {
		t.Fatalf("down=%v up=%v", down.calls, up.calls)
	}
	if e.usageCmds.Load() != 0 {
		t.Fatalf("/usage runs = %d", e.usageCmds.Load())
	}
}

func TestUsageFallsBackToAgyWhenCloudCodeFails(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	// The first read stores the plan.
	if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	// The quota call then fails after the project was found: the project is
	// forgotten and agy answers its quota. It has no credits; the stored plan
	// stands and no agy run reads one.
	server.mu.Lock()
	server.status = http.StatusForbidden
	server.mu.Unlock()
	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 4 || len(snapshot.Credits) != 0 || snapshot.Plan != "Google AI Pro" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if e.usageCmds.Load() != 1 || e.runs.Load() != 1 {
		t.Fatalf("agy runs = %d (/usage %d)", e.runs.Load(), e.usageCmds.Load())
	}
	if e.provider.projects.get(e.account.ID) != "" {
		t.Fatal("project kept after a failed quota read")
	}
}

func TestUsageSkipsCloudCodeWithAnExpiringToken(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(30*time.Second))

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 4 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if server.count("loadCodeAssist")+server.count("retrieveUserQuotaSummary") != 0 {
		t.Fatalf("expiring token was sent: %v", server.calls)
	}
	if e.usageCmds.Load() != 1 {
		t.Fatalf("/usage runs = %d", e.usageCmds.Load())
	}
}

func TestUsageShowsThePlanFromLoadCodeAssist(t *testing.T) {
	e := newAccountEnv(t)
	_, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || snapshot.Plan != "Google AI Pro" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if state := e.provider.readPlanState(e.account); state.Plan != "Google AI Pro" || state.Email != "me@example.com" || state.CheckedAt.IsZero() {
		t.Fatalf("stored plan = %+v", state)
	}
	// Listing profiles shows the stored plan without any call.
	if info := e.provider.DescribeAccount(t.Context(), e.account); info.Plan != "Google AI Pro" {
		t.Fatalf("describe = %+v", info)
	}
}

func TestPlanFallsBackToTheCurrentTier(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	server.loadBody = `{"cloudaicompanionProject":"proj-1","currentTier":{"name":"Antigravity"}}`
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || snapshot.Plan != "Antigravity" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestFailedLoadCodeAssistKeepsTheQuotaAndStoredPlan(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))
	if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	// The project is remembered, so the quota is read without waiting for
	// loadCodeAssist; its failure leaves credits unknown and the plan stored.
	server.mu.Lock()
	server.loadStatus = http.StatusInternalServerError
	server.mu.Unlock()
	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 2 || len(snapshot.Credits) != 0 || snapshot.Plan != "Google AI Pro" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if e.runs.Load() != 0 {
		t.Fatalf("agy ran %d times", e.runs.Load())
	}
}
