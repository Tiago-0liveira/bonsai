package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
}

func newCloudCodeServer(t *testing.T) (*cloudCodeServer, string) {
	t.Helper()
	s := &cloudCodeServer{t: t, calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls[r.URL.Path]++
		status := s.status
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
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"proj-1","paidTier":{"name":"Google AI Pro"}}`))
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

// noPlanProbe keeps tests about quota from waiting on a plan probe.
func (e *accountEnv) noPlanProbe(t *testing.T, email string) {
	t.Helper()
	if err := writePrivateJSON(planPath(e.accounts, e.account), planState{Plan: "Google AI Pro", Email: email, CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func (e *accountEnv) usageRuns(t *testing.T) (usage, credits int) {
	t.Helper()
	return int(e.usageCmds.Load()), int(e.creditCmds.Load())
}

func TestUsageReadsCloudCodeWithoutStartingAgy(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))
	e.noPlanProbe(t, "me@example.com")

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if err != nil || len(snapshot.Limits) != 2 || snapshot.Limits[0].ID != "gemini-weekly" || *snapshot.Limits[0].RemainingFraction != 0.5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Limits[0].Window != "weekly" || snapshot.Limits[0].Group != "Gemini Models" || snapshot.Limits[0].ResetsAt == nil {
		t.Fatalf("limit = %+v", snapshot.Limits[0])
	}
	// No balance was stored yet: the first read shows none and reads one.
	if len(snapshot.Credits) != 0 {
		t.Fatalf("credits = %+v", snapshot.Credits)
	}
	if usage, credits := e.usageRuns(t); usage != 0 || credits != 1 {
		t.Fatalf("agy runs: /usage=%d /credits=%d", usage, credits)
	}
	snapshot, err = e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if err != nil || len(snapshot.Credits) != 1 || *snapshot.Credits[0].Balance != 250 {
		t.Fatalf("stored credits = %+v err=%v", snapshot.Credits, err)
	}
	// The project is remembered and fresh credits are not read again.
	if server.count("loadCodeAssist") != 1 || server.count("retrieveUserQuotaSummary") != 2 {
		t.Fatalf("calls = %v", server.calls)
	}
	if usage, credits := e.usageRuns(t); usage != 0 || credits != 1 {
		t.Fatalf("agy runs: /usage=%d /credits=%d", usage, credits)
	}
	if info, err := os.Stat(filepath.Join(e.accounts.AccountDir(e.account.ID), "credits.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credits.json = %v, %v", info, err)
	}
	// An explicit refresh reads the credits again.
	if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	e.provider.waitPlanProbes()
	if _, credits := e.usageRuns(t); credits != 2 {
		t.Fatalf("refresh /credits runs = %d", credits)
	}
}

func TestUsageTriesTheNextCloudCodeHost(t *testing.T) {
	e := newAccountEnv(t)
	down, downURL := newCloudCodeServer(t)
	down.status = http.StatusServiceUnavailable
	up, upURL := newCloudCodeServer(t)
	useCloudCodeHosts(t, downURL, upURL)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))
	e.noPlanProbe(t, "me@example.com")

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if err != nil || len(snapshot.Limits) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if down.count("loadCodeAssist") != 1 || up.count("retrieveUserQuotaSummary") != 1 {
		t.Fatalf("down=%v up=%v", down.calls, up.calls)
	}
	if usage, _ := e.usageRuns(t); usage != 0 {
		t.Fatalf("/usage runs = %d", usage)
	}
}

func TestUsageFallsBackToAgyWhenCloudCodeFails(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(time.Hour))
	e.noPlanProbe(t, "me@example.com")

	// The quota call fails after the project was found: the project is
	// forgotten and agy answers.
	server.mu.Lock()
	server.status = http.StatusForbidden
	server.mu.Unlock()
	e.provider.projects.set(e.account.ID, "proj-1")
	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if err != nil || len(snapshot.Limits) != 4 || len(snapshot.Credits) != 1 || *snapshot.Credits[0].Balance != 250 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if usage, credits := e.usageRuns(t); usage != 1 || credits != 1 {
		t.Fatalf("agy runs: /usage=%d /credits=%d", usage, credits)
	}
	if e.provider.projects.get(e.account.ID) != "" {
		t.Fatal("project kept after a failed quota read")
	}
	// The fallback stored the balance it read.
	if state := e.provider.readCreditsState(e.account); state.Balance == nil || *state.Balance != 250 || state.CheckedAt.IsZero() {
		t.Fatalf("credits state = %+v", state)
	}
}

func TestUsageSkipsCloudCodeWithAnExpiringToken(t *testing.T) {
	e := newAccountEnv(t)
	server, url := newCloudCodeServer(t)
	useCloudCodeHosts(t, url)
	e.signInToken(t, "me@example.com", time.Now().Add(30*time.Second))
	e.noPlanProbe(t, "me@example.com")

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if err != nil || len(snapshot.Limits) != 4 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if server.count("loadCodeAssist")+server.count("retrieveUserQuotaSummary") != 0 {
		t.Fatalf("expiring token was sent: %v", server.calls)
	}
	if usage, _ := e.usageRuns(t); usage != 1 {
		t.Fatalf("/usage runs = %d", usage)
	}
}
