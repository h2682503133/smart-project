package agent

import (
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

type SessionStatus string
type MessageRole string
type SuggestionStatus string

const (
	SessionStatusActive SessionStatus = "ACTIVE"
	SessionStatusClosed SessionStatus = "CLOSED"

	MessageRoleUser      MessageRole = "USER"
	MessageRoleAssistant MessageRole = "ASSISTANT"
	MessageRoleSystem    MessageRole = "SYSTEM"
	MessageRoleTool      MessageRole = "TOOL"

	SuggestionStatusPending  SuggestionStatus = "PENDING"
	SuggestionStatusAccepted SuggestionStatus = "ACCEPTED"
	SuggestionStatusRejected SuggestionStatus = "REJECTED"
	SuggestionStatusExpired  SuggestionStatus = "EXPIRED"
)

type Session struct {
	ID            string        `json:"id" gorm:"primaryKey;size:64"`
	UserID        uint64        `json:"userId" gorm:"column:user_id;not null;index:idx_chat_sessions_user_updated,priority:1"`
	PlotID        *uint64       `json:"plotId,omitempty" gorm:"column:plot_id;index:idx_chat_sessions_plot_updated,priority:1"`
	Status        SessionStatus `json:"status" gorm:"size:16;not null"`
	Summary       *string       `json:"summary,omitempty" gorm:"type:text"`
	LastMessageAt *time.Time    `json:"lastMessageAt,omitempty" gorm:"column:last_message_at"`
	ClosedAt      *time.Time    `json:"closedAt,omitempty" gorm:"column:closed_at"`
	persistence.Auditable
}

func (Session) TableName() string { return "chat_sessions" }

type Message struct {
	ID            uint64         `json:"id" gorm:"primaryKey;autoIncrement"`
	SessionID     string         `json:"sessionId" gorm:"column:session_id;size:64;not null;index:idx_chat_messages_session_created,priority:1"`
	Role          MessageRole    `json:"role" gorm:"size:16;not null"`
	Content       string         `json:"content" gorm:"type:longtext;not null"`
	CitationsJSON datatypes.JSON `json:"citations,omitempty" gorm:"column:citations_json;type:json"`
	PlotID        *uint64        `json:"plotId,omitempty" gorm:"column:plot_id;index:idx_chat_messages_plot_created,priority:1"`
	ModelVersion  *string        `json:"modelVersion,omitempty" gorm:"column:model_version;size:64"`
	TraceID       *string        `json:"traceId,omitempty" gorm:"column:trace_id;size:64"`
	CreatedAt     time.Time      `json:"createdAt" gorm:"column:created_at;autoCreateTime;index:idx_chat_messages_session_created,priority:2;index:idx_chat_messages_plot_created,priority:2"`
}

func (Message) TableName() string { return "chat_messages" }

type Suggestion struct {
	ID              string           `json:"id" gorm:"primaryKey;size:64"`
	SessionID       string           `json:"sessionId" gorm:"column:session_id;size:64;not null;index:idx_ai_suggestions_session_created,priority:1"`
	PlotID          uint64           `json:"plotId" gorm:"column:plot_id;not null;index:idx_ai_suggestions_plot_status,priority:1"`
	Action          string           `json:"action" gorm:"size:32;not null"`
	DurationSeconds *int             `json:"durationSeconds,omitempty" gorm:"column:duration_seconds"`
	Confidence      *decimal.Decimal `json:"confidence,omitempty" gorm:"type:decimal(5,4)"`
	Reason          *string          `json:"reason,omitempty" gorm:"size:500"`
	Status          SuggestionStatus `json:"status" gorm:"size:16;not null;index:idx_ai_suggestions_plot_status,priority:2"`
	AcceptedBy      *uint64          `json:"acceptedBy,omitempty" gorm:"column:accepted_by"`
	AcceptedAt      *time.Time       `json:"acceptedAt,omitempty" gorm:"column:accepted_at"`
	CommandID       *string          `json:"commandId,omitempty" gorm:"column:command_id;size:64"`
	persistence.Auditable
}

func (Suggestion) TableName() string { return "ai_suggestions" }
