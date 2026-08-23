package knowledge

import (
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
)

type Status string

const (
	StatusDraft    Status = "DRAFT"
	StatusApproved Status = "APPROVED"
	StatusActive   Status = "ACTIVE"
	StatusArchived Status = "ARCHIVED"
)

type Document struct {
	ID          uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Title       string     `json:"title" gorm:"size:255;not null;uniqueIndex:uk_knowledge_doc_title_version,priority:1"`
	Category    string     `json:"category" gorm:"size:64;not null;index:idx_knowledge_doc_status_category,priority:2"`
	ObjectKey   string     `json:"objectKey" gorm:"column:object_key;size:512;not null"`
	FileHash    string     `json:"fileHash" gorm:"column:file_hash;size:64;not null;uniqueIndex:uk_knowledge_doc_file_hash"`
	Source      *string    `json:"source,omitempty" gorm:"size:255"`
	Status      Status     `json:"status" gorm:"size:16;not null;index:idx_knowledge_doc_status_category,priority:1"`
	Version     int        `json:"version" gorm:"not null;uniqueIndex:uk_knowledge_doc_title_version,priority:2;index:idx_knowledge_doc_status_category,priority:3"`
	UploadedBy  uint64     `json:"uploadedBy" gorm:"column:uploaded_by;not null"`
	ApprovedBy  *uint64    `json:"approvedBy,omitempty" gorm:"column:approved_by"`
	PublishedAt *time.Time `json:"publishedAt,omitempty" gorm:"column:published_at"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty" gorm:"column:archived_at"`
	persistence.Auditable
}

func (Document) TableName() string { return "knowledge_documents" }
