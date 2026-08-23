package audit

import "github.com/chenluuo/smart-project/backend/internal/shared/persistence"

type Log struct {
	ID           uint64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ActorID      *uint64 `json:"actorId,omitempty" gorm:"column:actor_id;index:idx_audit_logs_actor_created,priority:1"`
	Action       string  `json:"action" gorm:"size:128;not null"`
	ResourceType string  `json:"resourceType" gorm:"column:resource_type;size:64;not null"`
	ResourceID   *string `json:"resourceId,omitempty" gorm:"column:resource_id;size:128"`
	Result       string  `json:"result" gorm:"size:32;not null"`
	RequestID    *string `json:"requestId,omitempty" gorm:"column:request_id;size:64"`
	TraceID      *string `json:"traceId,omitempty" gorm:"column:trace_id;size:64;index:idx_audit_logs_trace"`
	persistence.Auditable
}

func (Log) TableName() string { return "audit_logs" }
