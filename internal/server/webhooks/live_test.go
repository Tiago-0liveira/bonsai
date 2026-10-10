package webhooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const liveTestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type liveRecorder struct {
	mu     sync.Mutex
	events []LiveEvent
}

func (r *liveRecorder) deliver(e LiveEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *liveRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func newLiveForTest() (*LiveReceiver, *liveRecorder) {
	rec := &liveRecorder{}
	return NewLiveReceiver([]byte(liveTestSecret), rec.deliver), rec
}

func liveRequest(h http.Handler, method, path, delivery, event string, body []byte, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1:7002"+path, bytes.NewReader(body))
	req.Host = "first-quiet-example-words.trycloudflare.com"
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func signed(body []byte) string { return Sign([]byte(liveTestSecret), body) }

const livePush = `{"ref":"refs/heads/main","repository":{"id":123,"full_name":"Octo/Repo"}}`

func TestLiveReceiverServesOnlyTheWebhookRoute(t *testing.T) {
	receiver, rec := newLiveForTest()
	h := receiver.Handler()
	body := []byte(livePush)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, LivePath},
		{http.MethodPut, LivePath},
		{http.MethodPost, "/"},
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/version"},
		{http.MethodGet, "/events"},
		{http.MethodPost, "/api/session"},
		{http.MethodGet, "/api/projects"},
		{http.MethodGet, "/app"},
		{http.MethodGet, "/ws"},
		{http.MethodPost, LivePath + "/"},
		{http.MethodPost, "/github/webhook/../api/session"},
	} {
		got := liveRequest(h, tc.method, tc.path, "d", "push", body, signed(body))
		if got.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, got.Code)
		}
	}
	if rec.count() != 0 {
		t.Fatalf("non-webhook routes delivered %d events", rec.count())
	}
	// The marker query GitHub posts with is part of the one route.
	if got := liveRequest(h, http.MethodPost, LivePath+"?bonsai=install-1", "d1", "push", body, signed(body)); got.Code != http.StatusAccepted {
		t.Fatalf("webhook with marker query = %d", got.Code)
	}
	if got := liveRequest(h, http.MethodPost, LivePath, "d2", "push", body, signed(body)); got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
}

func TestLiveReceiverRejectsBeforeParsing(t *testing.T) {
	receiver, rec := newLiveForTest()
	h := receiver.Handler()
	body := []byte(livePush)
	for name, signature := range map[string]string{
		"missing":   "",
		"bad":       "sha256=bad",
		"wrong key": Sign([]byte("another-secret"), body),
		"sha1":      "sha1=" + strings.Repeat("0", 40),
		"oversized": "sha256=" + strings.Repeat("0", 200),
	} {
		if got := liveRequest(h, http.MethodPost, LivePath, "d", "push", body, signature); got.Code != http.StatusUnauthorized {
			t.Errorf("%s signature = %d, want 401", name, got.Code)
		}
	}
	// Not JSON, but unsigned: the signature check comes first.
	if got := liveRequest(h, http.MethodPost, LivePath, "d", "push", []byte("{not json"), "sha256=00"); got.Code != http.StatusUnauthorized {
		t.Errorf("unsigned garbage = %d, want 401", got.Code)
	}
	garbage := []byte("{not json")
	if got := liveRequest(h, http.MethodPost, LivePath, "d", "push", garbage, signed(garbage)); got.Code != http.StatusBadRequest {
		t.Errorf("signed garbage = %d, want 400", got.Code)
	}
	oversized := bytes.Repeat([]byte(" "), liveMaxBody+1)
	if got := liveRequest(h, http.MethodPost, LivePath, "d", "push", oversized, signed(oversized)); got.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized = %d, want 413", got.Code)
	}
	if got := liveRequest(h, http.MethodPost, LivePath, "", "push", body, signed(body)); got.Code != http.StatusBadRequest {
		t.Errorf("missing delivery id = %d, want 400", got.Code)
	}
	if got := liveRequest(h, http.MethodPost, LivePath, strings.Repeat("d", 129), "push", body, signed(body)); got.Code != http.StatusBadRequest {
		t.Errorf("long delivery id = %d, want 400", got.Code)
	}
	if got := liveRequest(h, http.MethodPost, LivePath, "d", "", body, signed(body)); got.Code != http.StatusBadRequest {
		t.Errorf("missing event = %d, want 400", got.Code)
	}
	if rec.count() != 0 {
		t.Fatalf("rejected requests delivered %d events", rec.count())
	}
}

func TestLiveReceiverDedupesAndIgnoresUnsupported(t *testing.T) {
	receiver, rec := newLiveForTest()
	h := receiver.Handler()
	body := []byte(livePush)
	if got := liveRequest(h, http.MethodPost, LivePath, "d1", "push", body, signed(body)); got.Code != http.StatusAccepted {
		t.Fatalf("first = %d", got.Code)
	}
	if got := liveRequest(h, http.MethodPost, LivePath, "d1", "push", body, signed(body)); got.Code != http.StatusOK {
		t.Fatalf("duplicate = %d, want 200", got.Code)
	}
	other := []byte(`{"ref":"refs/heads/dev","repository":{"id":123,"full_name":"Octo/Repo"}}`)
	if got := liveRequest(h, http.MethodPost, LivePath, "d1", "push", other, signed(other)); got.Code != http.StatusConflict {
		t.Fatalf("reused id = %d, want 409", got.Code)
	}
	if rec.count() != 1 {
		t.Fatalf("delivered %d events, want 1", rec.count())
	}
	for event, payload := range map[string]string{
		"issues":                      `{"action":"opened","repository":{"id":1}}`,
		"pull_request":                `{"action":"not-real","repository":{"id":1}}`,
		"pull_request_review_comment": `{"action":"created","repository":{"id":1}}`,
		"release":                     `{"action":"published","repository":{"id":1}}`,
		"installation":                `{"action":"created"}`,
	} {
		raw := []byte(payload)
		if got := liveRequest(h, http.MethodPost, LivePath, "u-"+event, event, raw, signed(raw)); got.Code != http.StatusAccepted {
			t.Errorf("unsupported %s = %d, want 202", event, got.Code)
		}
	}
	if rec.count() != 1 {
		t.Fatalf("unsupported events were delivered: %d", rec.count())
	}

	// The dedupe window is bounded: the oldest id is forgotten.
	for i := 0; i < liveMaxDedupe; i++ {
		receiver.mu.Lock()
		receiver.seen["filler-"+strconv.Itoa(i)] = ""
		receiver.order = append(receiver.order, "filler-"+strconv.Itoa(i))
		receiver.mu.Unlock()
	}
	_ = liveRequest(h, http.MethodPost, LivePath, "d2", "push", body, signed(body))
	receiver.mu.Lock()
	_, kept := receiver.seen["d1"]
	size := len(receiver.seen)
	receiver.mu.Unlock()
	if kept || size > liveMaxDedupe {
		t.Fatalf("dedupe not bounded: kept d1=%v size=%d", kept, size)
	}
}

func TestNormalizeLive(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		event, body string
		want        LiveEvent
	}{
		{"push", livePush, LiveEvent{Event: "push", RepositoryID: 123, RepositoryFullName: "Octo/Repo", Ref: "refs/heads/main"}},
		{"pull_request", `{"action":"opened","number":42,"pull_request":{"number":42},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "pull_request", Action: "opened", RepositoryID: 1, RepositoryFullName: "o/r", PullRequestNumber: 42}},
		{"pull_request_review", `{"action":"submitted","pull_request":{"number":7},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "pull_request_review", Action: "submitted", RepositoryID: 1, RepositoryFullName: "o/r", PullRequestNumber: 7}},
		{"check_run", `{"action":"completed","check_run":{"head_sha":"` + sha + `"},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "check_run", Action: "completed", RepositoryID: 1, RepositoryFullName: "o/r", HeadSHA: sha}},
		{"check_suite", `{"action":"completed","check_suite":{"head_sha":"` + sha + `"},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "check_suite", Action: "completed", RepositoryID: 1, RepositoryFullName: "o/r", HeadSHA: sha}},
		{"status", `{"sha":"` + sha + `","state":"success","repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "status", RepositoryID: 1, RepositoryFullName: "o/r", HeadSHA: sha}},
		{"workflow_run", `{"action":"completed","workflow_run":{"head_sha":"` + sha + `","head_branch":"main"},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "workflow_run", Action: "completed", RepositoryID: 1, RepositoryFullName: "o/r", HeadSHA: sha}},
		{"ping", `{"zen":"Keep it logically awesome.","hook_id":99,"hook":{"config":{"url":"https://x/github/webhook"}},"repository":{"id":1,"full_name":"o/r"}}`,
			LiveEvent{Event: "ping", RepositoryID: 1, RepositoryFullName: "o/r", HookID: 99}},
	} {
		got, err := NormalizeLive("id", tc.event, []byte(tc.body))
		if err != nil {
			t.Fatalf("%s: %v", tc.event, err)
		}
		tc.want.DeliveryID = "id"
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.event, got, tc.want)
		}
		if got.ChecksEvent() != (tc.want.HeadSHA != "") {
			t.Errorf("%s: ChecksEvent = %v", tc.event, got.ChecksEvent())
		}
	}
	for name, body := range map[string]string{
		"bad sha":   `{"sha":"not a sha; rm -rf","repository":{"id":1,"full_name":"o/r"}}`,
		"bad name":  `{"ref":"refs/heads/main","repository":{"id":1,"full_name":"o/r\nx"}}`,
		"short sha": `{"sha":"abc","repository":{"id":1,"full_name":"o/r"}}`,
	} {
		event := "status"
		if strings.HasPrefix(name, "bad name") {
			event = "push"
		}
		if _, err := NormalizeLive("id", event, []byte(body)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Normalized events never carry free text from the payload.
	got, _ := NormalizeLive("id", "ping", []byte(`{"zen":"secret-ish text","hook_id":1,"repository":{"id":1,"full_name":"o/r"}}`))
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "secret-ish") {
		t.Fatalf("normalized event kept payload text: %s", raw)
	}
}

func TestLiveAllowlistLeavesRelayUnchanged(t *testing.T) {
	// The shared relay allowlist is frozen: no status, no ping.
	for _, event := range []string{"status", "ping"} {
		if AllowedEventAction(event, "") {
			t.Errorf("relay allowlist gained %s", event)
		}
		if !AllowedLiveEventAction(event, "") {
			t.Errorf("live allowlist lacks %s", event)
		}
	}
	for _, event := range LiveHookEvents {
		if event == "push" || event == "status" {
			if !AllowedLiveEventAction(event, "") {
				t.Errorf("live allowlist rejects %s", event)
			}
			continue
		}
		ok := false
		for _, action := range []string{"opened", "submitted", "completed"} {
			ok = ok || AllowedLiveEventAction(event, action)
		}
		if !ok {
			t.Errorf("live allowlist has no action for hook event %s", event)
		}
	}
	if AllowedLiveEventAction("issue_comment", "created") || AllowedLiveEventAction("workflow_job", "completed") {
		t.Error("live allowlist accepts events the hook never subscribes to")
	}
}
