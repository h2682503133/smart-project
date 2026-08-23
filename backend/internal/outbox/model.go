package outbox

import (
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/datatypes"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusPublished  Status = "PUBLISHED"
	StatusFailed     Status = "FAILED"
)

type Event struct {
	ID            uint64         `json:"id" gorm:"primaryKey;autoIncrement"`
	AggregateType string         `json:"aggregateType" gorm:"column:aggregate_type;size:64;not null;index:idx_outbox_aggregate,priority:1"`
	AggregateID   string         `json:"aggregateId" gorm:"column:aggregate_id;size:128;not null;index:idx_outbox_aggregate,priority:2"`
	EventType     string         `json:"eventType" gorm:"column:event_type;size:128;not null"`
	Payload       datatypes.JSON `json:"payload" gorm:"type:json;not null"`
	Status        Status         `json:"status" gorm:"size:32;not null;index:idx_outbox_status_available,priority:1"`
	AvailableAt   time.Time      `json:"availableAt" gorm:"column:available_at;not null;index:idx_outbox_status_available,priority:2"`
	PublishedAt   *time.Time     `json:"publishedAt,omitempty" gorm:"column:published_at"`
	RetryCount    int            `json:"retryCount" gorm:"column:retry_count;not null;default:0"`
	LastError     *string        `json:"lastError,omitempty" gorm:"column:last_error;size:1000"`
	persistence.Auditable
}

func (Event) TableName() string { return "outbox_events" }
