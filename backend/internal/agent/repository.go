package agent

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateSession(ctx context.Context, session *Session) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if session.PlotID != nil {
			var count int64
			if err := tx.Table("plots").Where("id = ? AND owner_id = ?", *session.PlotID, session.UserID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return ErrNotFound
			}
		}
		return tx.Create(session).Error
	})
}

func (r *Repository) AppendMessage(ctx context.Context, message *Message) error {
	return r.appendMessage(ctx, message, 0)
}

func (r *Repository) AppendMessageByOwner(ctx context.Context, message *Message, userID uint64) error {
	if userID == 0 {
		return ErrInvalidInput
	}
	return r.appendMessage(ctx, message, userID)
}

func (r *Repository) appendMessage(ctx context.Context, message *Message, userID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session Session
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", message.SessionID)
		if userID != 0 {
			query = query.Where("user_id = ?", userID)
		}
		if err := query.Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if session.Status != SessionStatusActive {
			return ErrConflict
		}
		if message.PlotID != nil && (session.PlotID == nil || *message.PlotID != *session.PlotID) {
			return ErrInvalidInput
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		return tx.Model(&Session{}).Where("id = ?", session.ID).Updates(map[string]any{
			"last_message_at": message.CreatedAt,
			"updated_at":      message.CreatedAt,
		}).Error
	})
}

func (r *Repository) ListMessagesByOwner(ctx context.Context, sessionID string, userID uint64, page, pageSize int) ([]Message, int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&Session{}).Where("id = ? AND user_id = ?", sessionID, userID).Count(&count).Error; err != nil {
		return nil, 0, err
	}
	if count == 0 {
		return nil, 0, ErrNotFound
	}
	query := r.db.WithContext(ctx).Model(&Message{}).Where("session_id = ?", sessionID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var messages []Message
	err := query.Order("created_at ASC, id ASC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&messages).Error
	return messages, total, err
}

func (r *Repository) CloseSessionByOwner(ctx context.Context, sessionID string, userID uint64, now time.Time) (*Session, error) {
	var session Session
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", sessionID, userID).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if session.Status == SessionStatusClosed {
			return nil
		}
		session.Status = SessionStatusClosed
		session.ClosedAt = &now
		session.UpdatedAt = now
		return tx.Model(&Session{}).Where("id = ?", session.ID).Updates(map[string]any{
			"status": SessionStatusClosed, "closed_at": now, "updated_at": now,
		}).Error
	})
	return &session, err
}
