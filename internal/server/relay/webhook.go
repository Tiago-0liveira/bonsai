package relay

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

const maxWebhookBody = 2 << 20

func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength > maxWebhookBody {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
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
	if signature == "" || len(signature) > 128 || !webhooks.Verify(s.webhookSecret, body, signature) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventName := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" || len(deliveryID) > 128 || eventName == "" || len(eventName) > 100 {
		http.Error(w, "invalid delivery", http.StatusBadRequest)
		return
	}
	event, err := webhooks.Normalize(deliveryID, eventName, body)
	if err != nil {
		if errors.Is(err, webhooks.ErrUnsupported) {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	relayEvent, duplicate, err := s.store.RecordWebhook(deliveryID, PayloadDigest(body), event, time.Now().UTC())
	switch {
	case errors.Is(err, ErrUnauthorizedScope):
		http.Error(w, "repository or installation not authorized", http.StatusForbidden)
		return
	case errors.Is(err, ErrDeliveryConflict):
		http.Error(w, "delivery id reused with different payload", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "relay store unavailable", http.StatusServiceUnavailable)
		return
	}
	if duplicate {
		w.WriteHeader(http.StatusOK)
		return
	}
	s.hub.publish(relayEvent)
	w.WriteHeader(http.StatusAccepted)
}
