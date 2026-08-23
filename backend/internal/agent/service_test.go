package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	sessions map[string]*Session
	messages map[string][]Message
	nextID   uint64
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: make(map[string]*Session), messages: make(map[string][]Message)}
}

func (s *memoryStore) CreateSession(_ context.Context, session *Session) error {
	copy := *session
	s.sessions[session.ID] = &copy
	return nil
}

func (s *memoryStore) AppendMessage(_ context.Context, message *Message) error {
	session := s.sessions[message.SessionID]
	if session == nil {
		return ErrNotFound
	}
	if session.Status != SessionStatusActive {
		return ErrConflict
	}
	s.nextID++
	message.ID = s.nextID
	s.messages[message.SessionID] = append(s.messages[message.SessionID], *message)
	session.LastMessageAt = &message.CreatedAt
	return nil
}

func (s *memoryStore) AppendMessageByOwner(ctx context.Context, message *Message, userID uint64) error {
	session := s.sessions[message.SessionID]
	if session == nil || session.UserID != userID {
		return ErrNotFound
	}
	return s.AppendMessage(ctx, message)
}

func (s *memoryStore) ListMessagesByOwner(_ context.Context, sessionID string, userID uint64, page, pageSize int) ([]Message, int64, error) {
	session := s.sessions[sessionID]
	if session == nil || session.UserID != userID {
		return nil, 0, ErrNotFound
	}
	all := s.messages[sessionID]
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []Message{}, int64(len(all)), nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return append([]Message(nil), all[start:end]...), int64(len(all)), nil
}

func (s *memoryStore) CloseSessionByOwner(_ context.Context, sessionID string, userID uint64, now time.Time) (*Session, error) {
	session := s.sessions[sessionID]
	if session == nil || session.UserID != userID {
		return nil, ErrNotFound
	}
	session.Status = SessionStatusClosed
	session.ClosedAt = &now
	return session, nil
}

func TestServiceSessionMessageLifecycle(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)
	fixedTime := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }
	plotID := uint64(11)

	session, err := service.CreateSession(context.Background(), 7, &plotID)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.UserID != 7 || session.Status != SessionStatusActive || session.PlotID == nil || *session.PlotID != 11 {
		t.Fatalf("unexpected session: %+v", session)
	}

	message, err := service.AppendMessage(context.Background(), session.ID, MessageInput{
		Role: "assistant", Content: " 建议观察土壤湿度。 ", PlotID: &plotID,
		Citations: []byte(`[{"title":"灌溉建议"}]`), ModelVersion: "model-v1", TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if message.ID != 1 || message.Content != "建议观察土壤湿度。" || message.Role != MessageRoleAssistant {
		t.Fatalf("unexpected message: %+v", message)
	}

	list, err := service.ListMessages(context.Background(), 7, session.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("unexpected message list: %+v", list)
	}

	if _, err := service.ListMessages(context.Background(), 8, session.ID, 1, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner ListMessages() error = %v, want ErrNotFound", err)
	}
	if _, err := service.AppendMessageByOwner(context.Background(), 8, session.ID, MessageInput{Role: "USER", Content: "cross owner"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner AppendMessageByOwner() error = %v, want ErrNotFound", err)
	}
	if _, err := service.CloseSession(context.Background(), 7, session.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if _, err := service.AppendMessage(context.Background(), session.ID, MessageInput{Role: "USER", Content: "closed"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("append to closed session error = %v, want ErrConflict", err)
	}
}

func TestServiceRejectsInvalidMessage(t *testing.T) {
	service := NewService(newMemoryStore())
	tests := []MessageInput{
		{Role: "UNKNOWN", Content: "content"},
		{Role: "USER", Content: ""},
		{Role: "USER", Content: "content", Citations: []byte(`{"broken"`)},
	}
	for _, input := range tests {
		if _, err := service.AppendMessage(context.Background(), "chat_1", input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("AppendMessage(%+v) error = %v, want ErrInvalidInput", input, err)
		}
	}
}
