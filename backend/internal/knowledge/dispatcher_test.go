package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/outbox"
	"gorm.io/datatypes"
)

type memoryEventStore struct {
	events       []outbox.Event
	publishedIDs []uint64
	failedIDs    []uint64
	lastError    string
	availableAt  time.Time
}

func (s *memoryEventStore) ClaimAvailableByEventPrefix(_ context.Context, _ time.Time, _ string, limit int, _ time.Duration) ([]outbox.Event, error) {
	if len(s.events) > limit {
		return append([]outbox.Event(nil), s.events[:limit]...), nil
	}
	return append([]outbox.Event(nil), s.events...), nil
}

func (s *memoryEventStore) MarkPublished(_ context.Context, eventID uint64, _ time.Time) error {
	s.publishedIDs = append(s.publishedIDs, eventID)
	return nil
}

func (s *memoryEventStore) MarkFailed(_ context.Context, eventID uint64, message string, availableAt, _ time.Time) error {
	s.failedIDs = append(s.failedIDs, eventID)
	s.lastError = message
	s.availableAt = availableAt
	return nil
}

func TestDispatcherPublishesKnowledgeEvent(t *testing.T) {
	const key = "test-internal-service-key-32-characters"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("X-Internal-Service-Key") != key {
			t.Errorf("unexpected request method=%s key=%s", request.Method, request.Header.Get("X-Internal-Service-Key"))
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	store := &memoryEventStore{events: []outbox.Event{{ID: 1, Payload: datatypes.JSON(`{"docId":1,"event":"UPLOADED","version":1}`)}}}
	dispatcher, err := NewDispatcher(store, server.Client(), server.URL, key)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	published, err := dispatcher.DispatchOnce(context.Background(), 10)
	if err != nil || published != 1 || len(store.publishedIDs) != 1 || store.publishedIDs[0] != 1 {
		t.Fatalf("DispatchOnce() = %d, %v; store=%+v", published, err, store)
	}
}

func TestDispatcherRetriesFailedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	fixedTime := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	store := &memoryEventStore{events: []outbox.Event{{ID: 2, RetryCount: 2, Payload: datatypes.JSON(`{"docId":2}`)}}}
	dispatcher, err := NewDispatcher(store, server.Client(), server.URL, "test-internal-service-key-32-characters")
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	dispatcher.now = func() time.Time { return fixedTime }
	published, err := dispatcher.DispatchOnce(context.Background(), 10)
	if published != 0 || err == nil || len(store.failedIDs) != 1 || store.failedIDs[0] != 2 {
		t.Fatalf("DispatchOnce() = %d, %v; store=%+v", published, err, store)
	}
	if store.availableAt.Sub(fixedTime) != 4*time.Second || store.lastError == "" {
		t.Fatalf("unexpected retry state: availableAt=%s error=%q", store.availableAt, store.lastError)
	}
}

func TestNewDispatcherRejectsInvalidURL(t *testing.T) {
	if _, err := NewDispatcher(&memoryEventStore{}, http.DefaultClient, "agent-service:8000", "test-internal-service-key-32-characters"); err == nil {
		t.Fatal("NewDispatcher() error = nil, want invalid absolute URL error")
	}
}
