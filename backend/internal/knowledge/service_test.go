package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

type memoryObjects struct {
	objects map[string][]byte
	removed []string
}

func (s *memoryObjects) Put(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = contents
	return nil
}

func (s *memoryObjects) Remove(_ context.Context, key string) error {
	delete(s.objects, key)
	s.removed = append(s.removed, key)
	return nil
}

func (s *memoryObjects) PresignedGet(_ context.Context, key string, _ time.Duration) (string, error) {
	if _, ok := s.objects[key]; !ok {
		return "", fmt.Errorf("missing object %s", key)
	}
	return "https://objects.test/" + key, nil
}

type memoryStore struct {
	documents map[uint64]*Document
	nextID    uint64
}

func newMemoryStore() *memoryStore { return &memoryStore{documents: make(map[uint64]*Document)} }

func (s *memoryStore) ListActive(_ context.Context, category string) ([]Document, error) {
	var result []Document
	for _, document := range s.documents {
		if document.Status == StatusActive && (category == "" || document.Category == category) {
			result = append(result, *document)
		}
	}
	return result, nil
}

func (s *memoryStore) Create(_ context.Context, document *Document, _ uint64, _ string) error {
	for _, existing := range s.documents {
		if existing.FileHash == document.FileHash || existing.Title == document.Title && existing.Version == document.Version {
			return ErrConflict
		}
	}
	s.nextID++
	document.ID = s.nextID
	copy := *document
	s.documents[document.ID] = &copy
	return nil
}

func (s *memoryStore) Transition(_ context.Context, documentID, actorID uint64, target Status, _ string, now time.Time) (*Document, error) {
	document := s.documents[documentID]
	if document == nil {
		return nil, ErrNotFound
	}
	if !validTransition(document.Status, target) {
		return nil, ErrConflict
	}
	document.Status = target
	document.UpdatedAt = now
	if target == StatusApproved {
		document.ApprovedBy = &actorID
	}
	if target == StatusActive {
		document.PublishedAt = &now
	}
	if target == StatusArchived {
		document.ArchivedAt = &now
	}
	return document, nil
}

func TestServiceDocumentLifecycle(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)
	fixedTime := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }

	document, err := service.Register(context.Background(), 1, RegisterInput{
		Title: " 番茄灌溉指南 ", Category: " tomato ", ObjectKey: "knowledge/tomato.pdf",
		FileHash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Source: "农业站",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if document.ID != 1 || document.Status != StatusDraft || document.FileHash[0] != 'a' || document.Version != 1 {
		t.Fatalf("unexpected registered document: %+v", document)
	}
	if _, err := service.Publish(context.Background(), 2, document.ID, "trace-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("publish draft error = %v, want ErrConflict", err)
	}
	approved, err := service.Approve(context.Background(), 2, document.ID, "trace-1")
	if err != nil || approved.Status != StatusApproved || approved.ApprovedBy == nil || *approved.ApprovedBy != 2 {
		t.Fatalf("Approve() = %+v, %v", approved, err)
	}
	active, err := service.Publish(context.Background(), 2, document.ID, "trace-1")
	if err != nil || active.Status != StatusActive || active.PublishedAt == nil {
		t.Fatalf("Publish() = %+v, %v", active, err)
	}
	documents, err := service.ListActive(context.Background(), "tomato")
	if err != nil || len(documents) != 1 || documents[0].ID != document.ID {
		t.Fatalf("ListActive() = %+v, %v", documents, err)
	}
	archived, err := service.Archive(context.Background(), 2, document.ID, "trace-1")
	if err != nil || archived.Status != StatusArchived || archived.ArchivedAt == nil {
		t.Fatalf("Archive() = %+v, %v", archived, err)
	}
}

func TestServiceRejectsInvalidAndDuplicateDocuments(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)
	valid := RegisterInput{
		Title: "指南", Category: "general", ObjectKey: "knowledge/guide.pdf",
		FileHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if _, err := service.Register(context.Background(), 1, valid); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if _, err := service.Register(context.Background(), 1, valid); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Register() error = %v, want ErrConflict", err)
	}
	valid.FileHash = "not-a-sha256"
	if _, err := service.Register(context.Background(), 1, valid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid hash Register() error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceUploadsAndSignsDocument(t *testing.T) {
	store := newMemoryStore()
	objects := &memoryObjects{}
	service := NewService(store, objects)
	service.ConfigureObjectAccess(1024, 5*time.Minute)

	document, err := service.Upload(context.Background(), 1, UploadInput{
		Title: "指南", Category: "general", Filename: "guide.pdf", ContentType: "application/pdf",
		Size: 8, Reader: bytes.NewReader([]byte("document")),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if len(objects.objects) != 1 || document.ObjectKey == "" || document.FileHash != "43cc23fa52b87b4cc1d02b5b114154151d6adddb17c9fddc06b027fa99e24008" {
		t.Fatalf("unexpected upload: document=%+v objects=%+v", document, objects.objects)
	}
	if _, err := service.Approve(context.Background(), 2, document.ID, ""); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if _, err := service.Publish(context.Background(), 2, document.ID, ""); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	views, err := service.ListActive(context.Background(), "")
	if err != nil || len(views) != 1 || views[0].DownloadURL == "" {
		t.Fatalf("ListActive() = %+v, %v", views, err)
	}

	if _, err := service.Upload(context.Background(), 1, UploadInput{
		Title: "duplicate", Category: "general", Filename: "duplicate.pdf",
		Size: 8, Reader: bytes.NewReader([]byte("document")),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Upload() error = %v, want ErrConflict", err)
	}
	if len(objects.removed) != 1 {
		t.Fatalf("rollback removed=%v, want one object", objects.removed)
	}

	if _, err := service.Upload(context.Background(), 1, UploadInput{
		Title: "large", Category: "general", Filename: "large.pdf",
		Size: 1025, Reader: bytes.NewReader(make([]byte, 1025)),
	}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("large Upload() error = %v, want ErrFileTooLarge", err)
	}
}
