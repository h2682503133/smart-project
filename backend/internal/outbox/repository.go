package outbox

import (
	"context"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	*persistence.Repository[Event]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{Repository: persistence.NewRepository[Event](db), db: db}
}

func (r *Repository) FindAvailable(ctx context.Context, now time.Time, limit int) ([]Event, error) {
	var events []Event
	err := r.db.WithContext(ctx).
		Where("status IN ? AND available_at <= ?", []Status{StatusPending, StatusFailed}, now).
		Order("available_at, id").Limit(limit).Find(&events).Error
	return events, err
}

func (r *Repository) FindAvailableByEventPrefix(ctx context.Context, now time.Time, eventPrefix string, limit int) ([]Event, error) {
	var events []Event
	err := r.db.WithContext(ctx).
		Where("status IN ? AND available_at <= ? AND event_type LIKE ?", []Status{StatusPending, StatusFailed}, now, eventPrefix+"%").
		Order("available_at, id").Limit(limit).Find(&events).Error
	return events, err
}

func (r *Repository) ClaimAvailableByEventPrefix(ctx context.Context, now time.Time, eventPrefix string, limit int, lease time.Duration) ([]Event, error) {
	var events []Event
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND available_at <= ? AND event_type LIKE ?", []Status{StatusPending, StatusFailed, StatusProcessing}, now, eventPrefix+"%").
			Order("available_at, id").Limit(limit).Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		ids := make([]uint64, len(events))
		for index := range events {
			ids[index] = events[index].ID
			events[index].Status = StatusProcessing
			events[index].AvailableAt = now.Add(lease)
		}
		return tx.Model(&Event{}).Where("id IN ?", ids).Updates(map[string]any{
			"status": StatusProcessing, "available_at": now.Add(lease), "updated_at": now,
		}).Error
	})
	return events, err
}

func (r *Repository) MarkPublished(ctx context.Context, eventID uint64, now time.Time) error {
	return r.db.WithContext(ctx).Model(&Event{}).Where("id = ?", eventID).Updates(map[string]any{
		"status": StatusPublished, "published_at": now, "last_error": nil, "updated_at": now,
	}).Error
}

func (r *Repository) MarkFailed(ctx context.Context, eventID uint64, message string, availableAt, now time.Time) error {
	return r.db.WithContext(ctx).Model(&Event{}).Where("id = ?", eventID).Updates(map[string]any{
		"status": StatusFailed, "retry_count": gorm.Expr("retry_count + 1"),
		"last_error": message, "available_at": availableAt, "updated_at": now,
	}).Error
}
