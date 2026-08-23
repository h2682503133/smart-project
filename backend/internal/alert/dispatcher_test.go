package alert

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/outbox"
	"gorm.io/datatypes"
)

type alertEventStoreStub struct {
	events       []outbox.Event
	publishedIDs []uint64
	failedIDs    []uint64
	lastPrefix   string
	lastError    string
	availableAt  time.Time
}

func (s *alertEventStoreStub) ClaimAvailableByEventPrefix(_ context.Context, _ time.Time, prefix string, limit int, _ time.Duration) ([]outbox.Event, error) {
	s.lastPrefix = prefix
	if len(s.events) > limit {
		return append([]outbox.Event(nil), s.events[:limit]...), nil
	}
	return append([]outbox.Event(nil), s.events...), nil
}

func (s *alertEventStoreStub) MarkPublished(_ context.Context, id uint64, _ time.Time) error {
	s.publishedIDs = append(s.publishedIDs, id)
	return nil
}

func (s *alertEventStoreStub) MarkFailed(_ context.Context, id uint64, message string, availableAt, _ time.Time) error {
	s.failedIDs = append(s.failedIDs, id)
	s.lastError, s.availableAt = message, availableAt
	return nil
}

func TestAlertDispatcherPublishesToAgent(t *testing.T) {
	const key = "test-internal-service-key-32-characters"
	const original = `{"ruleId":2,"triggerValue":28.6,"extra":{"source":"telemetry"}}`
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("X-Internal-Service-Key") != key {
			t.Errorf("unexpected request method=%s key=%s", request.Method, request.Header.Get("X-Internal-Service-Key"))
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != original {
			t.Errorf("forwarded body = %s, want %s", body, original)
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	store := &alertEventStoreStub{events: []outbox.Event{{ID: 1, Payload: datatypes.JSON(original)}}}
	dispatcher, err := NewDispatcher(store, server.Client(), server.URL, key)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	published, err := dispatcher.DispatchOnce(context.Background(), 10)
	if err != nil || published != 1 || store.lastPrefix != "ALERT_" || len(store.publishedIDs) != 1 {
		t.Fatalf("DispatchOnce() = %d, %v; store=%+v", published, err, store)
	}
}

func TestAlertDispatcherRetriesAgentFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	fixed := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	store := &alertEventStoreStub{events: []outbox.Event{{ID: 2, RetryCount: 2, Payload: datatypes.JSON(`{"alert_id":9}`)}}}
	dispatcher, err := NewDispatcher(store, server.Client(), server.URL, "test-internal-service-key-32-characters")
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	dispatcher.now = func() time.Time { return fixed }
	published, err := dispatcher.DispatchOnce(context.Background(), 10)
	if published != 0 || err == nil || len(store.failedIDs) != 1 || store.availableAt.Sub(fixed) != 4*time.Second {
		t.Fatalf("DispatchOnce() = %d, %v; store=%+v", published, err, store)
	}
}
