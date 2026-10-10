package webhooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// LivePath is the only route of the live-updates receiver.
const LivePath = "/github/webhook"

const (
	liveMaxBody   = 2 << 20
	liveMaxDedupe = 4096
)

// LiveEvent is the normalized delivery the live-updates receiver hands to the
// local API. Like Event it carries no command, path or free text; unlike the
// relay's Event it names the repository and the commit, because repository
// webhooks are matched to local projects by name and checks by SHA.
type LiveEvent struct {
	DeliveryID         string `json:"delivery_id"`
	Event              string `json:"event"`
	Action             string `json:"action,omitempty"`
	RepositoryID       int64  `json:"repository_id,omitempty"`
	RepositoryFullName string `json:"repository_full_name,omitempty"`
	HookID             int64  `json:"hook_id,omitempty"`
	Ref                string `json:"ref,omitempty"`
	PullRequestNumber  int    `json:"pull_request,omitempty"`
	HeadSHA            string `json:"head_sha,omitempty"`
}

// ChecksEvent reports whether the event changes CI state for HeadSHA.
func (e LiveEvent) ChecksEvent() bool {
	switch e.Event {
	case "check_run", "check_suite", "status", "workflow_run":
		return e.HeadSHA != ""
	}
	return false
}

// AllowedLiveEventAction is the live receiver's allowlist: the events a
// Bonsai repository hook subscribes to, plus GitHub's ping. It is separate
// from AllowedEventAction so the frozen hosted relay never changes.
func AllowedLiveEventAction(eventName, action string) bool {
	switch eventName {
	case "push", "status", "ping":
		return action == ""
	case "pull_request", "pull_request_review", "check_run", "check_suite", "workflow_run":
		return AllowedEventAction(eventName, action)
	}
	return false
}

// LiveHookEvents are the events a Bonsai repository hook subscribes to.
var LiveHookEvents = []string{"push", "pull_request", "pull_request_review", "check_run", "check_suite", "status", "workflow_run"}

// NormalizeLive parses only the fields live updates need, after the HMAC
// check. Unsupported events and actions return ErrUnsupported.
func NormalizeLive(deliveryID, eventName string, payload []byte) (LiveEvent, error) {
	var raw struct {
		Action     string
		HookID     int64 `json:"hook_id"`
		Repository struct {
			ID       int64
			FullName string `json:"full_name"`
		}
		Ref         string
		Number      int
		SHA         string
		PullRequest struct {
			Number int
		} `json:"pull_request"`
		CheckRun struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_run"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
		WorkflowRun struct {
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_run"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return LiveEvent{}, err
	}
	if !AllowedLiveEventAction(eventName, raw.Action) {
		return LiveEvent{}, fmt.Errorf("%w: %s/%s", ErrUnsupported, eventName, raw.Action)
	}
	out := LiveEvent{
		DeliveryID:         deliveryID,
		Event:              eventName,
		Action:             raw.Action,
		RepositoryID:       raw.Repository.ID,
		RepositoryFullName: raw.Repository.FullName,
		HookID:             raw.HookID,
		Ref:                raw.Ref,
		PullRequestNumber:  raw.PullRequest.Number,
	}
	if out.PullRequestNumber == 0 {
		out.PullRequestNumber = raw.Number
	}
	switch eventName {
	case "check_run":
		out.HeadSHA = raw.CheckRun.HeadSHA
	case "check_suite":
		out.HeadSHA = raw.CheckSuite.HeadSHA
	case "status":
		out.HeadSHA = raw.SHA
	case "workflow_run":
		out.HeadSHA = raw.WorkflowRun.HeadSHA
	}
	if !validName(out.RepositoryFullName) || (out.HeadSHA != "" && !validSHA(out.HeadSHA)) || len(out.Ref) > 512 {
		return LiveEvent{}, fmt.Errorf("invalid repository, ref or commit in %s delivery", eventName)
	}
	return out, nil
}

func validName(name string) bool {
	if len(name) > 200 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func validSHA(sha string) bool {
	if len(sha) < 7 || len(sha) > 64 {
		return false
	}
	for i := 0; i < len(sha); i++ {
		c := sha[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// LiveReceiver is the internet-facing side of live updates: it verifies
// GitHub's signature, drops duplicates and hands normalized events to deliver.
// It persists nothing. deliver must not block.
type LiveReceiver struct {
	secret  []byte
	deliver func(LiveEvent)

	mu    sync.Mutex
	seen  map[string]string
	order []string
}

// NewLiveReceiver returns a receiver keyed by secret (the HMAC key GitHub
// signs with).
func NewLiveReceiver(secret []byte, deliver func(LiveEvent)) *LiveReceiver {
	return &LiveReceiver{
		secret:  append([]byte(nil), secret...),
		deliver: deliver,
		seen:    map[string]string{},
	}
}

// Handler serves exactly POST /github/webhook; every other path or method is
// 404, so nothing else is reachable through the tunnel. It deliberately has no
// Host check: the tunnel forwards the public host.
func (h *LiveReceiver) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodPost || r.URL.Path != LivePath {
			http.NotFound(w, r)
			return
		}
		h.receive(w, r)
	})
}

func (h *LiveReceiver) receive(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > liveMaxBody {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, liveMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid payload", http.StatusBadRequest)
		}
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if len(signature) > 128 || !Verify(h.secret, body, signature) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventName := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" || len(deliveryID) > 128 || eventName == "" || len(eventName) > 100 {
		http.Error(w, "invalid delivery", http.StatusBadRequest)
		return
	}
	event, err := NormalizeLive(deliveryID, eventName, body)
	if errors.Is(err, ErrUnsupported) {
		// Signed but not something Bonsai acts on: accept so GitHub does not
		// mark the hook as failing, and do no work.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	h.mu.Lock()
	if prior, ok := h.seen[deliveryID]; ok {
		h.mu.Unlock()
		if prior != digest {
			http.Error(w, "delivery id reused with different payload", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	h.seen[deliveryID] = digest
	h.order = append(h.order, deliveryID)
	for len(h.order) > liveMaxDedupe {
		delete(h.seen, h.order[0])
		h.order = h.order[1:]
	}
	h.mu.Unlock()
	if h.deliver != nil {
		h.deliver(event)
	}
	w.WriteHeader(http.StatusAccepted)
}

// NewLiveServer is the HTTP server for the receiver: short timeouts and small
// headers, since anyone on the internet can reach it through the tunnel.
func NewLiveServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}
