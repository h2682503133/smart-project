package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/audit"
	"github.com/chenluuo/smart-project/backend/internal/outbox"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListActive(ctx context.Context, category string) ([]Document, error) {
	query := r.db.WithContext(ctx).Where("status = ?", StatusActive)
	if category != "" {
		query = query.Where("category = ?", category)
	}
	var documents []Document
	err := query.Order("category ASC, title ASC, version DESC").Find(&documents).Error
	return documents, err
}

func (r *Repository) Create(ctx context.Context, document *Document, actorID uint64, traceID string) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(document).Error; err != nil {
			return err
		}
		return createSideEffects(tx, document, actorID, traceID, "KNOWLEDGE_DOCUMENT_REGISTER", "UPLOADED", document.CreatedAt)
	})
	return normalizeRepositoryError(err)
}

func (r *Repository) Transition(ctx context.Context, documentID, actorID uint64, target Status, traceID string, now time.Time) (*Document, error) {
	var document Document
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&document, documentID).Error; err != nil {
			return err
		}
		if !validTransition(document.Status, target) {
			return ErrConflict
		}

		updates := map[string]any{"status": target, "updated_at": now}
		event := "UPDATED"
		action := "KNOWLEDGE_DOCUMENT_" + string(target)
		switch target {
		case StatusApproved:
			updates["approved_by"] = actorID
			document.ApprovedBy = &actorID
		case StatusActive:
			updates["published_at"] = now
			document.PublishedAt = &now
			var previousActive []Document
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("title = ? AND status = ? AND id <> ?", document.Title, StatusActive, document.ID).
				Find(&previousActive).Error; err != nil {
				return err
			}
			for index := range previousActive {
				previousActive[index].Status = StatusArchived
				previousActive[index].ArchivedAt = &now
				previousActive[index].UpdatedAt = now
				if err := tx.Model(&Document{}).Where("id = ?", previousActive[index].ID).
					Updates(map[string]any{"status": StatusArchived, "archived_at": now, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := createSideEffects(tx, &previousActive[index], actorID, traceID,
					"KNOWLEDGE_DOCUMENT_ARCHIVED", "ARCHIVED", now); err != nil {
					return err
				}
			}
		case StatusArchived:
			updates["archived_at"] = now
			document.ArchivedAt = &now
			event = "ARCHIVED"
		}
		if err := tx.Model(&Document{}).Where("id = ?", document.ID).Updates(updates).Error; err != nil {
			return err
		}
		document.Status = target
		document.UpdatedAt = now
		return createSideEffects(tx, &document, actorID, traceID, action, event, now)
	})
	return &document, normalizeRepositoryError(err)
}

func createSideEffects(tx *gorm.DB, document *Document, actorID uint64, traceID, action, event string, now time.Time) error {
	resourceID := strconv.FormatUint(document.ID, 10)
	var traceIDPointer *string
	if traceID != "" {
		traceIDPointer = &traceID
	}
	log := audit.Log{
		ActorID: &actorID, Action: action, ResourceType: "knowledge_document", ResourceID: &resourceID,
		Result: "SUCCESS", TraceID: traceIDPointer,
		Auditable: persistence.Auditable{CreatedAt: now, UpdatedAt: now},
	}
	if err := tx.Create(&log).Error; err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"docId": document.ID, "event": event, "version": document.Version,
		"objectKey": document.ObjectKey, "traceId": traceID,
	})
	if err != nil {
		return err
	}
	outboxEvent := outbox.Event{
		AggregateType: "knowledge_document", AggregateID: resourceID,
		EventType: "KNOWLEDGE_DOCUMENT_" + event, Payload: datatypes.JSON(payload),
		Status: outbox.StatusPending, AvailableAt: now,
		Auditable: persistence.Auditable{CreatedAt: now, UpdatedAt: now},
	}
	return tx.Create(&outboxEvent).Error
}

func validTransition(current, target Status) bool {
	switch current {
	case StatusDraft:
		return target == StatusApproved
	case StatusApproved:
		return target == StatusActive
	case StatusActive:
		return target == StatusArchived
	default:
		return false
	}
}

func normalizeRepositoryError(err error) error {
	if err == nil || errors.Is(err, ErrConflict) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	var mysqlErr *drivermysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrConflict
	}
	return err
}
