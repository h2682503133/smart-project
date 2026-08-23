package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

var (
	ErrInvalidInput = errors.New("invalid agent input")
	ErrNotFound     = errors.New("agent resource not found")
	ErrConflict     = errors.New("agent resource state conflict")
)

type Store interface {
	CreateSession(context.Context, *Session) error
	AppendMessage(context.Context, *Message) error
	AppendMessageByOwner(context.Context, *Message, uint64) error
	ListMessagesByOwner(context.Context, string, uint64, int, int) ([]Message, int64, error)
	CloseSessionByOwner(context.Context, string, uint64, time.Time) (*Session, error)
}

type MessageInput struct {
	Role         string
	Content      string
	Citations    json.RawMessage
	PlotID       *uint64
	ModelVersion string
	TraceID      string
}

type MessageList struct {
	Items    []Message `json:"items"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Total    int64     `json:"total"`
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) CreateSession(ctx context.Context, userID uint64, plotID *uint64) (*Session, error) {
	if userID == 0 || (plotID != nil && *plotID == 0) {
		return nil, ErrInvalidInput
	}
	id, err := newPublicID("chat")
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	session := &Session{
		ID: id, UserID: userID, PlotID: plotID, Status: SessionStatusActive,
	}
	session.CreatedAt, session.UpdatedAt = now, now
	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create chat session: %w", err)
	}
	return session, nil
}

func (s *Service) AppendMessage(ctx context.Context, sessionID string, input MessageInput) (*Message, error) {
	message, err := s.newMessage(sessionID, input)
	if err != nil {
		return nil, err
	}
	if err := s.store.AppendMessage(ctx, message); err != nil {
		return nil, fmt.Errorf("append chat message: %w", err)
	}
	return message, nil
}

func (s *Service) AppendMessageByOwner(ctx context.Context, userID uint64, sessionID string, input MessageInput) (*Message, error) {
	if userID == 0 {
		return nil, ErrInvalidInput
	}
	message, err := s.newMessage(sessionID, input)
	if err != nil {
		return nil, err
	}
	if err := s.store.AppendMessageByOwner(ctx, message, userID); err != nil {
		return nil, fmt.Errorf("append chat message by owner: %w", err)
	}
	return message, nil
}

func (s *Service) newMessage(sessionID string, input MessageInput) (*Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	input.Content = strings.TrimSpace(input.Content)
	input.ModelVersion = strings.TrimSpace(input.ModelVersion)
	input.TraceID = strings.TrimSpace(input.TraceID)
	role := MessageRole(strings.ToUpper(strings.TrimSpace(input.Role)))
	if sessionID == "" || len(sessionID) > 64 || input.Content == "" || len(input.Content) > 100_000 || !validMessageRole(role) {
		return nil, ErrInvalidInput
	}
	if input.PlotID != nil && *input.PlotID == 0 {
		return nil, ErrInvalidInput
	}
	if len(input.ModelVersion) > 64 || len(input.TraceID) > 64 {
		return nil, ErrInvalidInput
	}
	var citations datatypes.JSON
	if len(input.Citations) > 0 && string(input.Citations) != "null" {
		if !json.Valid(input.Citations) || len(input.Citations) > 64*1024 {
			return nil, ErrInvalidInput
		}
		citations = append(datatypes.JSON(nil), input.Citations...)
	}
	now := s.now().UTC()
	message := &Message{
		SessionID: sessionID, Role: role, Content: input.Content, CitationsJSON: citations,
		PlotID: input.PlotID, ModelVersion: optionalString(input.ModelVersion), TraceID: optionalString(input.TraceID), CreatedAt: now,
	}
	return message, nil
}

func (s *Service) ListMessages(ctx context.Context, userID uint64, sessionID string, page, pageSize int) (MessageList, error) {
	if userID == 0 || strings.TrimSpace(sessionID) == "" {
		return MessageList{}, ErrInvalidInput
	}
	page, pageSize = normalizePage(page, pageSize)
	items, total, err := s.store.ListMessagesByOwner(ctx, sessionID, userID, page, pageSize)
	if err != nil {
		return MessageList{}, fmt.Errorf("list chat messages: %w", err)
	}
	return MessageList{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) CloseSession(ctx context.Context, userID uint64, sessionID string) (*Session, error) {
	if userID == 0 || strings.TrimSpace(sessionID) == "" {
		return nil, ErrInvalidInput
	}
	session, err := s.store.CloseSessionByOwner(ctx, sessionID, userID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("close chat session: %w", err)
	}
	return session, nil
}

func validMessageRole(role MessageRole) bool {
	switch role {
	case MessageRoleUser, MessageRoleAssistant, MessageRoleSystem, MessageRoleTool:
		return true
	default:
		return false
	}
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func newPublicID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}
