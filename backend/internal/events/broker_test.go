package events

import (
	"errors"
	"testing"
	"time"
)

func TestBrokerFiltersByOwnerAndReplaysAfterLastEventID(t *testing.T) {
	broker := NewBroker(10)
	first, err := broker.Publish(Event{Type: "telemetry.updated", OwnerID: 7, ResourceID: "plot-1"})
	if err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if _, err := broker.Publish(Event{Type: "alert.created", OwnerID: 8, ResourceID: "alert-foreign"}); err != nil {
		t.Fatalf("publish foreign event: %v", err)
	}
	second, err := broker.Publish(Event{Type: "command.result", OwnerID: 7, ResourceID: "command-1"})
	if err != nil {
		t.Fatalf("publish second event: %v", err)
	}

	subscription := broker.Subscribe(7, first.ID)
	defer subscription.Close()
	select {
	case event := <-subscription.Events:
		if event.ID != second.ID {
			t.Fatalf("replayed event ID = %q, want %q", event.ID, second.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}
	select {
	case event := <-subscription.Events:
		t.Fatalf("unexpected foreign event replay: %+v", event)
	default:
	}
}

func TestBrokerRemovesClosedAndSlowSubscribers(t *testing.T) {
	broker := NewBroker(100)
	subscription := broker.Subscribe(7, "")
	if count := broker.SubscriberCount(7); count != 1 {
		t.Fatalf("subscriber count = %d, want 1", count)
	}
	subscription.Close()
	if count := broker.SubscriberCount(7); count != 0 {
		t.Fatalf("subscriber count after close = %d, want 0", count)
	}

	slow := broker.Subscribe(7, "")
	defer slow.Close()
	for index := 0; index <= defaultSubscriberBuffer; index++ {
		if _, err := broker.Publish(Event{Type: "telemetry.updated", OwnerID: 7, ResourceID: "plot-1"}); err != nil {
			t.Fatalf("publish event %d: %v", index, err)
		}
	}
	if count := broker.SubscriberCount(7); count != 0 {
		t.Fatalf("slow subscriber count = %d, want 0", count)
	}
}

func TestBrokerRejectsEventsWithoutRoutingMetadata(t *testing.T) {
	broker := NewBroker(10)
	_, err := broker.Publish(Event{Type: "telemetry.updated", ResourceID: "plot-1"})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
}
