package knowledge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid knowledge document input")
	ErrNotFound     = errors.New("knowledge document not found")
	ErrConflict     = errors.New("knowledge document state conflict")
	ErrUnavailable  = errors.New("knowledge object storage unavailable")
	ErrFileTooLarge = errors.New("knowledge document exceeds upload limit")
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Store interface {
	ListActive(context.Context, string) ([]Document, error)
	Create(context.Context, *Document, uint64, string) error
	Transition(context.Context, uint64, uint64, Status, string, time.Time) (*Document, error)
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Remove(context.Context, string) error
	PresignedGet(context.Context, string, time.Duration) (string, error)
}

type RegisterInput struct {
	Title     string
	Category  string
	ObjectKey string
	FileHash  string
	Source    string
	Version   int
	TraceID   string
}

type UploadInput struct {
	Title       string
	Category    string
	Filename    string
	ContentType string
	Source      string
	Version     int
	Size        int64
	Reader      io.Reader
	TraceID     string
}

type DocumentView struct {
	ID          uint64     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	Version     int        `json:"version"`
	Source      *string    `json:"source,omitempty"`
	DownloadURL string     `json:"downloadUrl,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type Service struct {
	store            Store
	objects          ObjectStore
	maxUploadBytes   int64
	signedURLTimeout time.Duration
	now              func() time.Time
}

func NewService(store Store, options ...ObjectStore) *Service {
	service := &Service{store: store, maxUploadBytes: 20 * 1024 * 1024, signedURLTimeout: 15 * time.Minute, now: time.Now}
	if len(options) > 0 {
		service.objects = options[0]
	}
	return service
}

func (s *Service) ConfigureObjectAccess(maxUploadBytes int64, signedURLTimeout time.Duration) {
	if maxUploadBytes > 0 {
		s.maxUploadBytes = maxUploadBytes
	}
	if signedURLTimeout > 0 {
		s.signedURLTimeout = signedURLTimeout
	}
}

func (s *Service) MaxUploadBytes() int64 { return s.maxUploadBytes }

func (s *Service) ListActive(ctx context.Context, category string) ([]DocumentView, error) {
	category = strings.TrimSpace(category)
	if len(category) > 64 {
		return nil, ErrInvalidInput
	}
	documents, err := s.store.ListActive(ctx, category)
	if err != nil {
		return nil, fmt.Errorf("list active knowledge documents: %w", err)
	}
	views := make([]DocumentView, 0, len(documents))
	for index := range documents {
		view := DocumentView{
			ID: documents[index].ID, Title: documents[index].Title, Category: documents[index].Category,
			Version: documents[index].Version, Source: documents[index].Source,
			PublishedAt: documents[index].PublishedAt, UpdatedAt: documents[index].UpdatedAt,
		}
		if s.objects != nil {
			downloadURL, err := s.objects.PresignedGet(ctx, documents[index].ObjectKey, s.signedURLTimeout)
			if err != nil {
				return nil, fmt.Errorf("sign knowledge document download: %w", err)
			}
			view.DownloadURL = downloadURL
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Upload(ctx context.Context, actorID uint64, input UploadInput) (*Document, error) {
	if s.objects == nil {
		return nil, ErrUnavailable
	}
	if input.Reader == nil || input.Size < 1 || input.Size > s.maxUploadBytes {
		if input.Size > s.maxUploadBytes {
			return nil, ErrFileTooLarge
		}
		return nil, ErrInvalidInput
	}
	contents, err := io.ReadAll(io.LimitReader(input.Reader, s.maxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read knowledge document: %w", err)
	}
	if int64(len(contents)) > s.maxUploadBytes {
		return nil, ErrFileTooLarge
	}
	digest := sha256.Sum256(contents)
	objectKey, err := newObjectKey(input.Filename)
	if err != nil {
		return nil, err
	}
	if err := s.objects.Put(ctx, objectKey, bytes.NewReader(contents), int64(len(contents)), input.ContentType); err != nil {
		return nil, fmt.Errorf("upload knowledge document: %w", err)
	}
	document, err := s.Register(ctx, actorID, RegisterInput{
		Title: input.Title, Category: input.Category, ObjectKey: objectKey,
		FileHash: hex.EncodeToString(digest[:]), Source: input.Source, Version: input.Version, TraceID: input.TraceID,
	})
	if err != nil {
		_ = s.objects.Remove(ctx, objectKey)
		return nil, err
	}
	return document, nil
}

func (s *Service) Register(ctx context.Context, actorID uint64, input RegisterInput) (*Document, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Category = strings.TrimSpace(input.Category)
	input.ObjectKey = strings.TrimSpace(input.ObjectKey)
	input.FileHash = strings.ToLower(strings.TrimSpace(input.FileHash))
	input.Source = strings.TrimSpace(input.Source)
	input.TraceID = strings.TrimSpace(input.TraceID)
	if actorID == 0 || input.Title == "" || len(input.Title) > 255 || input.Category == "" || len(input.Category) > 64 ||
		input.ObjectKey == "" || len(input.ObjectKey) > 512 || !sha256Pattern.MatchString(input.FileHash) || len(input.Source) > 255 || len(input.TraceID) > 64 {
		return nil, ErrInvalidInput
	}
	if input.Version == 0 {
		input.Version = 1
	}
	if input.Version < 1 {
		return nil, ErrInvalidInput
	}
	now := s.now().UTC()
	document := &Document{
		Title: input.Title, Category: input.Category, ObjectKey: input.ObjectKey,
		FileHash: input.FileHash, Source: optionalValue(input.Source), Status: StatusDraft,
		Version: input.Version, UploadedBy: actorID,
	}
	document.CreatedAt, document.UpdatedAt = now, now
	if err := s.store.Create(ctx, document, actorID, input.TraceID); err != nil {
		return nil, fmt.Errorf("register knowledge document: %w", err)
	}
	return document, nil
}

func (s *Service) Approve(ctx context.Context, actorID, documentID uint64, traceID string) (*Document, error) {
	return s.transition(ctx, actorID, documentID, StatusApproved, traceID)
}

func (s *Service) Publish(ctx context.Context, actorID, documentID uint64, traceID string) (*Document, error) {
	return s.transition(ctx, actorID, documentID, StatusActive, traceID)
}

func (s *Service) Archive(ctx context.Context, actorID, documentID uint64, traceID string) (*Document, error) {
	return s.transition(ctx, actorID, documentID, StatusArchived, traceID)
}

func (s *Service) transition(ctx context.Context, actorID, documentID uint64, target Status, traceID string) (*Document, error) {
	traceID = strings.TrimSpace(traceID)
	if actorID == 0 || documentID == 0 || len(traceID) > 64 {
		return nil, ErrInvalidInput
	}
	document, err := s.store.Transition(ctx, documentID, actorID, target, traceID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("transition knowledge document to %s: %w", target, err)
	}
	return document, nil
}

func optionalValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func newObjectKey(filename string) (string, error) {
	filename = path.Base(strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/"))
	if filename == "." || filename == "/" || filename == "" || len(filename) > 255 {
		return "", ErrInvalidInput
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate knowledge object key: %w", err)
	}
	return path.Join("knowledge", strconv.FormatInt(time.Now().UTC().Unix()/86400, 10), hex.EncodeToString(random[:])+"-"+filename), nil
}
