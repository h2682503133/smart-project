package events

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultSubscriberBuffer = 64

var ErrInvalidEvent = errors.New("invalid event")

// Event is delivered only to connections owned by OwnerID. Payload fields are
// emitted at the top level of the SSE data object together with eventTime and
// resourceId.
type Event struct {
	ID         string
	Type       string
	OwnerID    uint64
	EventTime  time.Time
	ResourceID string
	Payload    map[string]any
}

type Subscription struct {
	Events <-chan Event
	close  func()
	once   sync.Once
}

func (s *Subscription) Close() {
	if s == nil || s.close == nil {
		return
	}
	s.once.Do(s.close)
}

type Broker struct {
	mu           sync.Mutex
	historyLimit int
	history      []Event
	subscribers  map[uint64]map[uint64]chan Event
	nextEventID  atomic.Uint64
	nextClientID uint64
}

func NewBroker(historyLimit int) *Broker {
	if historyLimit < 1 {
		historyLimit = 1
	}
	return &Broker{
		historyLimit: historyLimit,
		history:      make([]Event, 0, historyLimit),
		subscribers:  make(map[uint64]map[uint64]chan Event),
	}
}

// Publish stores an event for reconnect replay and sends it only to the
// matching owner. A slow connection is closed so it can reconnect and replay
// from its last received event ID instead of blocking publishers.
func (b *Broker) Publish(event Event) (Event, error) {
	event.Type = strings.TrimSpace(event.Type)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	if event.OwnerID == 0 || !ValidType(event.Type) || event.ResourceID == "" || containsLineBreak(event.Type) || containsLineBreak(event.ID) {
		return Event{}, ErrInvalidEvent
	}
	if event.ID == "" {
		event.ID = formatEventID(b.nextEventID.Add(1))
	}
	if event.EventTime.IsZero() {
		event.EventTime = time.Now().UTC()
	}
	event.Payload = clonePayload(event.Payload)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.history = append(b.history, event)
	if overflow := len(b.history) - b.historyLimit; overflow > 0 {
		copy(b.history, b.history[overflow:])
		b.history = b.history[:b.historyLimit]
	}
	for clientID, subscriber := range b.subscribers[event.OwnerID] {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(b.subscribers[event.OwnerID], clientID)
		}
	}
	if len(b.subscribers[event.OwnerID]) == 0 {
		delete(b.subscribers, event.OwnerID)
	}
	return event, nil
}

// Subscribe registers one user connection. If lastEventID is present in the
// retained history, matching events published after it are replayed first.
func (b *Broker) Subscribe(ownerID uint64, lastEventID string) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	replay := b.replayLocked(ownerID, strings.TrimSpace(lastEventID))
	bufferSize := defaultSubscriberBuffer
	if len(replay)+1 > bufferSize {
		bufferSize = len(replay) + 1
	}
	ch := make(chan Event, bufferSize)
	for _, event := range replay {
		ch <- event
	}
	b.nextClientID++
	clientID := b.nextClientID
	if b.subscribers[ownerID] == nil {
		b.subscribers[ownerID] = make(map[uint64]chan Event)
	}
	b.subscribers[ownerID][clientID] = ch

	return &Subscription{Events: ch, close: func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		ownerSubscribers := b.subscribers[ownerID]
		if current, exists := ownerSubscribers[clientID]; exists {
			close(current)
			delete(ownerSubscribers, clientID)
		}
		if len(ownerSubscribers) == 0 {
			delete(b.subscribers, ownerID)
		}
	}}
}

func (b *Broker) SubscriberCount(ownerID uint64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers[ownerID])
}

func (b *Broker) replayLocked(ownerID uint64, lastEventID string) []Event {
	if lastEventID == "" {
		return nil
	}
	position := -1
	for index := len(b.history) - 1; index >= 0; index-- {
		if b.history[index].ID == lastEventID {
			position = index
			break
		}
	}
	if position < 0 {
		return nil
	}
	replay := make([]Event, 0, len(b.history)-position-1)
	for _, event := range b.history[position+1:] {
		if event.OwnerID == ownerID {
			replay = append(replay, event)
		}
	}
	return replay
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func formatEventID(value uint64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = digits[value%10]
		value /= 10
	}
	return string(buffer[position:])
}
